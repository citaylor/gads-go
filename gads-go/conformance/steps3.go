package conformance

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

var reHeldRef = regexp.MustCompile(`^@held:(\w+)$`)

// heldRef returns a current ID, or "@held:<as>" for that of a held record.
func (r *Runner) heldRef(v any) int64 {
	if m := reHeldRef.FindStringSubmatch(str(v)); m != nil {
		return r.held[m[1]].CurrentID()
	}
	id, _ := strconv.ParseInt(str(v), 10, 64)
	return id
}

// stepHeld performs operations on a record object that is kept between
// steps, mirroring tests that reuse the same GADS::Record object.
func (r *Runner) stepHeld(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		gads.Fail("op required")
	}
	name := str(args["as"])
	if name == "" {
		gads.Fail("as required")
	}
	if r.held == nil {
		r.held = map[string]*gads.Record{}
	}
	var layout *gads.Layout
	if op == "new" || op == "single" || op == "find" {
		layout = r.layoutForStep(args)
	}
	switch op {
	case "new":
		rec := r.newRecord(args, layout)
		if v, ok := args["parent_id"]; ok && v != nil {
			rec.SetParentID(r.heldRef(v))
		}
		if v := args["linked_id"]; truthy(v) {
			var l int64
			if m := reHeldRef.FindStringSubmatch(str(v)); m != nil {
				l = r.held[m[1]].CurrentID()
			} else {
				l, _ = strconv.ParseInt(str(v), 10, 64)
			}
			rec.SetLinkedID(l)
		}
		if !truthy(args["no_initialise"]) {
			rec.Initialise(layout.InstanceID)
		}
		r.held[name] = rec
		return map[string]any{}
	case "single":
		// First record of a sheet, retrieved using Records->single. With
		// panic_on_code, error if any code values need to be evaluated
		// during retrieval (they should have been stored already)
		var u *gads.User
		if v, ok := args["user"]; ok {
			u = r.user(v)
		} else {
			u = r.firstSheet().User()
		}
		r.e.PanicOnCode = truthy(args["panic_on_code"])
		recs := r.e.NewRecords(layout, u)
		if v, ok := args["view_ref"]; ok && v != nil {
			recs.View = r.buildView(args, layout)
		}
		recs.IsDeleted = truthy(args["is_deleted"])
		// nth: the record from calling Single that many times
		if n, _ := strconv.Atoi(str(args["nth"])); n > 1 {
			for i := 1; i < n; i++ {
				recs.Single()
			}
		}
		rec := recs.Single()
		r.e.PanicOnCode = false
		if rec == nil {
			gads.Fail("No records")
		}
		r.held[name] = rec
		op = "get"
	}
	if op == "find" && r.held[name] == nil {
		r.held[name] = r.newRecord(args, layout)
	}
	rec := r.held[name]
	if rec == nil {
		gads.Fail("Unknown held record %s", name)
	}
	if truthy(args["clear_layout_first"]) {
		rec.Layout().Clear()
	}
	layout = rec.Layout()
	obs := map[string]any{}
	switch op {
	case "find":
		// Resolve any held reference before clearing (which removes the ID)
		find := map[string]any{}
		for k, v := range args {
			find[k] = v
		}
		if v, ok := find["current_id"]; ok && v != nil {
			if m := reHeldRef.FindStringSubmatch(str(v)); m != nil {
				find["current_id"] = r.held[m[1]].CurrentID()
			}
		}
		if truthy(args["clear"]) {
			rec.Clear()
		}
		if truthy(args["chronology"]) {
			// Record.FindChronologyID: observe each version's editor, time
			// and the changed fields (name_short and value)
			id, _ := strconv.ParseInt(str(find["current_id"]), 10, 64)
			rec.FindChronologyID(id)
			chron := []any{}
			for _, c := range rec.Chronology() {
				changed := []any{}
				for _, d := range c.Changed {
					changed = append(changed, map[string]any{"name_short": d.Column().NameShort, "value": d.AsString()})
				}
				chron = append(chron, map[string]any{"editor": c.Editor.AsString(), "datetime": c.Datetime.AsString(), "changed": changed})
			}
			obs["chronology"] = chron
		} else {
			r.findRecord(rec, find)
		}
		// version: n then loads the nth version (latest first)
		if v, ok := args["version"]; ok && v != nil {
			n, _ := strconv.Atoi(str(v))
			versions := rec.Versions()
			if n >= len(versions) {
				gads.Fail("No version %d", n)
			}
			id, _ := db.AsInt64(versions[n]["id"])
			rec.Clear()
			rec.FindRecordID(id, gads.FindOptions{})
		}
	case "clone_oldvalue":
		// Make the datums look as if they have been written to: set each
		// one's oldvalue to a clone of itself
		for _, cn := range strList(args["columns"]) {
			d := rec.GetFieldValue(r.col(layout, cn))
			gads.SetDatumOldvalue(d, d.Clone(rec, false))
		}
	case "clear":
		rec.Clear()
	case "initialise":
		rec.Initialise(0)
	case "clone":
		// Clone the record into another held record (to)
		r.held[str(args["to"])] = rec.Clone()
	case "set_from_form":
		// Set values from the html_form of the same columns of another held
		// record (from), as a form submission. as_query uses the as_query
		// of each (curval) value, and replace: [from, to] substitutes text
		src, ok := r.held[str(args["from"])]
		if !ok {
			gads.Fail("Unknown held record %s", str(args["from"]))
		}
		for _, cn := range strList(args["columns"]) {
			col := r.col(rec.Layout(), cn)
			form := src.Fields()[col.ID].HTMLForm()
			vals := []any{}
			for _, v := range form {
				if truthy(args["as_query"]) {
					m, _ := v.(map[string]any)
					vals = append(vals, m["as_query"])
				} else {
					vals = append(vals, v)
				}
			}
			if rp := strList(args["replace"]); len(rp) == 2 {
				for i, v := range vals {
					if sv, ok := v.(string); ok {
						vals[i] = strings.Replace(sv, rp[0], rp[1], 1)
					}
				}
			}
			rec.Fields()[col.ID].SetValue(vals, gads.SetValueOptions{})
		}
	case "load_remembered_values":
		rec.LoadRememberedValues(0)
	case "set":
		r.setValues(rec, layout, args["values"], args)
	case "user":
		rec.SetUser(r.user(args["user"]))
	case "write":
		o := writeOptions(args)
		if wo, ok := args["options"].(map[string]any); ok {
			if truthy(wo["submission_token"]) {
				o.SubmissionToken = rec.SubmissionToken()
			}
			// submitted_fields_of: the fields required by that column's filter
			if c, ok := wo["submitted_fields_of"]; ok && c != nil {
				o.SubmittedFields = rec.Layout().Column(r.col(rec.Layout(), c).ID).SubvalsInputRequired()
			}
		}
		rec.Write(o)
		obs["record_id"] = rec.RecordID()
	case "re_evaluate":
		r.codeDatum(rec, layout, args["column"]).ReEvaluate(gads.ReEvalOptions{})
	case "write_value":
		r.codeDatum(rec, layout, args["column"]).WriteValue()
	case "delete":
		rec.DeleteCurrent(false, 0)
	case "purge":
		rec.PurgeCurrent()
	case "remove_id":
		rec.RemoveID()
	case "write_linked_id":
		var l int64
		if v := args["linked"]; v != nil {
			if m := reHeldRef.FindStringSubmatch(str(v)); m != nil {
				l = r.held[m[1]].CurrentID()
			} else {
				l, _ = strconv.ParseInt(str(v), 10, 64)
			}
		}
		rec.WriteLinkedID(l)
	case "purge_version":
		rec.Purge()
	case "restore":
		rec.Restore()
	case "get":
	default:
		gads.Fail("Unknown held op %s", op)
	}
	if truthy(args["clear_layout"]) {
		rec.Layout().Clear()
	}
	if cid := rec.CurrentID(); cid != 0 {
		obs["current_id"] = cid
	} else {
		obs["current_id"] = nil
	}
	if cols, ok := args["observe"]; ok && cols != nil {
		values := map[string]any{}
		for _, c := range strList(cols) {
			d := rec.GetFieldValue(r.col(layout, c))
			if d == nil {
				values[c] = nil
			} else {
				values[c] = d.AsString()
			}
		}
		obs["values"] = values
	}
	if cols, ok := args["changed"]; ok && cols != nil {
		changed := map[string]any{}
		for _, c := range strList(cols) {
			changed[c] = boolInt(gads.DatumChanged(rec.GetFieldValue(r.col(layout, c))))
		}
		obs["changed"] = changed
	}
	if cols, ok := args["value_refs"]; ok && cols != nil {
		refs := map[string]any{}
		for _, c := range strList(cols) {
			out := []any{}
			if d, ok := rec.GetFieldValue(r.col(layout, c)).(interface{ Values() []any }); ok {
				for _, v := range d.Values() {
					if _, isDate := v.(time.Time); isDate {
						out = append(out, "DateTime")
					} else {
						out = append(out, "")
					}
				}
			}
			refs[c] = out
		}
		obs["value_refs"] = refs
	}
	if truthy(args["versions"]) {
		vs := []any{}
		for _, v := range rec.Versions() {
			t, _ := db.AsTime(v["created"])
			vs = append(vs, t.Format("2006-01-02"))
		}
		obs["versions"] = vs
	}
	if truthy(args["record_meta"]) {
		meta := map[string]any{"edited_user": nil, "deletedby": nil, "deleted": nil}
		if u := rec.EditedUser(); u != nil {
			meta["edited_user"] = u.AsString()
		}
		if u := rec.Deletedby(); u != nil {
			meta["deletedby"] = u.AsString()
		}
		if t := rec.Deleted(); t != nil {
			meta["deleted"] = t.Format("2006-01-02T15:04:05")
		}
		obs["meta"] = meta
	}
	if truthy(args["columns_render"]) {
		names := []any{}
		for _, c := range rec.ColumnsRender() {
			names = append(names, c.Name)
		}
		obs["columns_render"] = names
	}
	if truthy(args["observe_parent"]) {
		obs["parent_id"] = parentObs(rec)
	}
	// Whether values are the same as those of another held record (for
	// values that differ between runs, such as the time)
	if same, ok := args["same_as"].(map[string]any); ok {
		other := r.held[str(same["as"])]
		if other == nil {
			gads.Fail("Unknown held record %s", str(same["as"]))
		}
		m := map[string]any{}
		for _, n := range strList(same["columns"]) {
			c := r.col(layout, n)
			m[n] = boolInt(rec.Fields()[c.ID].AsString() == other.Fields()[c.ID].AsString())
		}
		obs["same_as"] = m
	}
	// Values of a curval field, from the record's layout (which may have
	// been rebuilt)
	if fv, ok := args["filtered_values"]; ok && fv != nil {
		l := rec.Layout()
		token := ""
		if truthy(args["submission_token"]) {
			token = rec.SubmissionToken()
		}
		out := []any{}
		for _, x := range l.Column(r.col(l, fv).ID).FilteredValues(token) {
			m := map[string]any{"id": x.ID, "value": x.Value}
			if x.ID == 0 {
				// A new record (Perl's undef ID)
				m["id"] = nil
			}
			// With filtered_full, also value_id, html and selector_id (with
			// the random token of a new record's replaced by *)
			if truthy(args["filtered_full"]) {
				m["value_id"] = x.ValueID
				m["html"] = x.HTML
				m["selector_id"] = x.SelectorID
				if strings.HasPrefix(x.SelectorID, "new_") {
					m["selector_id"] = "new_*"
				}
			}
			out = append(out, m)
		}
		obs["filtered_values"] = out
	}
	if truthy(args["child_record_ids"]) {
		obs["child_record_ids"] = childrenObs(rec)
	}
	if truthy(args["serial"]) {
		obs["serial"] = rec.Serial()
	}
	if cols, ok := args["datum"]; ok && cols != nil {
		m := map[string]any{}
		for _, c := range strList(cols) {
			m[c] = datumProps(rec.GetFieldValue(r.col(layout, c)), args["datum_props"])
		}
		obs["datum"] = m
	}
	if c, ok := args["for_code_curval"]; ok && c != nil {
		// Values of a curval as passed to code, with all fields of the
		// referred sheet requested
		d := rec.GetFieldValue(r.col(layout, c))
		fields := map[string]bool{}
		for _, fc := range d.Column().LayoutParent().All(gads.AllOptions{}) {
			if fc.NameShort != "" {
				fields[fc.NameShort] = true
			}
		}
		// for_code_fields: request just those fields, and observe the
		// first value's field_values
		ff, hasFF := args["for_code_fields"]
		if hasFF {
			fields = map[string]bool{}
			for _, f := range strList(ff) {
				fields[f] = true
			}
		}
		var list []any
		switch v := d.ForCode(gads.ForCodeContext{Fields: fields}).(type) {
		case []any:
			list = v
		case nil:
		default:
			list = []any{v}
		}
		keys := []any{}
		if len(list) > 0 {
			if m, ok := list[0].(map[string]any); ok {
				if fv, ok := m["field_values"].(map[string]any); ok {
					var ks []string
					for k := range fv {
						ks = append(ks, k)
					}
					sort.Strings(ks)
					for _, k := range ks {
						keys = append(keys, k)
					}
				}
			}
		}
		fc := map[string]any{"count": len(list), "field_values_keys": keys}
		if hasFF {
			var fv any
			if len(list) > 0 {
				if m, ok := list[0].(map[string]any); ok {
					fv = m["field_values"]
				}
			}
			fc["field_values"] = fv
		}
		obs["for_code_curval"] = fc
	}
	return obs
}

func (r *Runner) codeDatum(rec *gads.Record, layout *gads.Layout, ref any) gads.CodeDatum {
	d, ok := rec.GetFieldValue(r.col(layout, ref)).(gads.CodeDatum)
	if !ok {
		gads.Fail("Column %v is not a code column", ref)
	}
	return d
}

var defaultDatumProps = []string{"as_string", "blank", "changed"}

// datumProps returns properties of a datum, as the Perl runner's
// _datum_props.
func datumProps(d gads.Datum, props any) any {
	if d == nil {
		return nil
	}
	list := defaultDatumProps
	if props != nil {
		list = strList(props)
	}
	col := d.Column()
	obs := map[string]any{}
	for _, p := range list {
		switch p {
		case "as_string":
			obs[p] = d.AsString()
		case "html":
			obs[p] = d.HTML()
		case "filter_value":
			obs[p] = d.FilterValue()
		case "blank":
			obs[p] = boolInt(d.Blank())
		case "changed":
			obs[p] = boolInt(gads.DatumChanged(d))
		case "text_all":
			out := []any{}
			for _, t := range d.TextAll() {
				out = append(out, t)
			}
			obs[p] = out
		case "deleted_values":
			out := []any{}
			if x, ok := d.(interface{ DeletedValues() []map[string]any }); ok {
				for _, v := range x.DeletedValues() {
					out = append(out, v["value"])
				}
			}
			obs[p] = out
		case "ids":
			out := []any{}
			if x, ok := d.(interface{ IDs() []int64 }); ok {
				for _, id := range x.IDs() {
					out = append(out, id)
				}
			}
			obs[p] = out
		case "html_form_query":
			// The as_query of each curval value of the edit form
			out := []any{}
			for _, v := range d.HTMLForm() {
				if m, ok := v.(map[string]any); ok {
					out = append(out, m["as_query"])
				} else {
					out = append(out, nil)
				}
			}
			obs[p] = out
		case "dependent_not_shown":
			obs[p] = boolInt(gads.DependentNotShown(d, false, ""))
		case "html_form":
			out := []any{}
			for _, v := range d.HTMLForm() {
				if m, ok := v.(map[string]any); ok {
					if id, ok := m["id"]; ok && id != nil {
						out = append(out, id)
					} else {
						out = append(out, m["value_id"])
					}
				} else {
					out = append(out, v)
				}
			}
			obs[p] = out
		case "oldvalue":
			if old := gads.DatumOldvalue(d); old != nil {
				obs[p] = map[string]any{"as_string": old.AsString(), "blank": boolInt(old.Blank())}
			} else {
				obs[p] = nil
			}
		case "empty_value":
			// As checked for new records in t/005_setvalues.t. Only classes
			// with value/ids/id methods in GADS are checked
			empty := true
			switch col.Type {
			case "string", "intgr", "date", "daterange":
			default:
				empty = len(d.RawValues()) == 0
			}
			obs[p] = boolInt(empty)
		case "id":
			switch col.Type {
			case "enum", "tree", "curval", "autocur", "filval", "person":
				var ids []int64
				if x, ok := d.(interface{ IDs() []int64 }); ok {
					ids = x.IDs()
				}
				if len(ids) > 0 {
					obs[p] = ids[0]
				} else {
					obs[p] = nil
				}
			default:
				obs[p] = nil
			}
		case "id_hash":
			switch col.Type {
			case "enum", "curval", "autocur", "filval":
				obs[p] = 1
			default:
				obs[p] = nil
			}
		case "hours":
			hours := []any{}
			if x, ok := d.(interface{ Values() []time.Time }); ok {
				for _, t := range x.Values() {
					hours = append(hours, int64(t.Hour()))
				}
			}
			obs[p] = hours
		default:
			gads.Fail("Unknown datum property %s", p)
		}
	}
	return obs
}

// stepDatum creates a datum directly from a column, as for grouped results
// or values that start as undefined.
func (r *Runner) stepDatum(args map[string]any) any {
	layout := r.sheet(args["sheet"]).Layout()
	col := r.col(layout, args["column"])
	var init []any
	iv, hasInit := args["init_value"]
	if hasInit {
		for _, v := range asList(iv) {
			init = append(init, r.convertValue(col, yamlToPerl(v)))
		}
	}
	d := r.e.NewStandaloneDatum(col, init, hasInit, truthy(args["init_no_value"]))
	if v, ok := args["set_value"]; ok {
		so := gads.SetValueOptions{}
		if o, ok := args["set_options"].(map[string]any); ok {
			so.Bulk = truthy(o["bulk"])
		}
		d.SetValue(r.convertValue(col, yamlToPerl(v)), so)
	}
	return datumProps(d, args["datum_props"])
}

// parentObs is a record's parent ID, or nil if it has none (as Perl undef).
func parentObs(rec *gads.Record) any {
	if p := rec.ParentID(); p != 0 {
		return p
	}
	return nil
}

// childrenObs is a record's child IDs, empty (not nil) when there are none.
func childrenObs(rec *gads.Record) []int64 {
	if c := rec.ChildRecordIDs(); c != nil {
		return c
	}
	return []int64{}
}
