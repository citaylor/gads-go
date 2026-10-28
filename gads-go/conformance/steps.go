package conformance

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

func (r *Runner) stepSheet(args map[string]any) any {
	alias := str(args["as"])
	if alias == "" {
		gads.Fail("Sheet alias (as) required")
	}
	s := NewDataSheet(r)
	if v, ok := args["instance_id"]; ok {
		s.InstanceID, _ = strconv.ParseInt(str(v), 10, 64)
	}
	if v, ok := args["multivalue"]; ok {
		s.Multivalue = truthy(v)
	}
	if v, ok := args["column_count"].(map[string]any); ok {
		for k, n := range v {
			c, _ := strconv.Atoi(str(n))
			s.ColumnCount[k] = c
		}
	}
	if v, ok := args["curval_offset"]; ok {
		o, _ := strconv.ParseInt(str(v), 10, 64)
		s.CurvalOffset = &o
	}
	if v, ok := args["enumvals_count"]; ok {
		s.EnumvalsCount, _ = strconv.Atoi(str(v))
	}
	if v, ok := args["calc_code"]; ok {
		s.CalcCode = str(v)
		s.calcCodeSet = true
	}
	if v, ok := args["calc_return_type"]; ok {
		s.CalcReturnType = str(v)
	}
	if v, ok := args["rag_code"]; ok {
		s.RagCode = str(v)
		s.ragCodeSet = true
	}
	if v, ok := args["has_rag"]; ok {
		s.HasRag = truthy(v)
	}
	if v, ok := args["optional"]; ok {
		s.Optional = truthy(v)
	}
	if v, ok := args["no_groups"]; ok {
		s.NoGroups = truthy(v)
	}
	if v, ok := args["users_to_create"]; ok {
		s.UsersToCreate = strList(v)
	}
	// As DataSheet's site_id, which only applies when it builds the schema
	// (the first sheet)
	if v, ok := args["site_id"]; ok && len(r.sheets) == 0 {
		id, _ := strconv.ParseInt(str(v), 10, 64)
		r.e.DB.InsertMap("site", map[string]any{"id": id})
		r.e.SiteID = id
	}
	if v, ok := args["multivalue_columns"]; ok {
		s.MultivalueColumns = map[string]bool{}
		if m, ok := v.(map[string]any); ok {
			for c, on := range m {
				s.MultivalueColumns[c] = truthy(on)
			}
		} else {
			for _, c := range strList(v) {
				s.MultivalueColumns[c] = true
			}
		}
	}
	if cv, ok := args["curval"]; ok && cv != nil {
		if other, ok := r.sheets[str(cv)]; ok {
			s.Curval = other.Layout().InstanceID
			if cf, ok := args["curval_fields"]; ok {
				for _, n := range strList(cf) {
					s.CurvalFieldIDs = append(s.CurvalFieldIDs, r.col(other.Layout(), n).ID)
				}
			}
		} else {
			s.Curval, _ = strconv.ParseInt(str(cv), 10, 64)
		}
	}
	if first := r.firstSheet(); first != nil && !truthy(args["own_group"]) {
		s.SetGroup(first.Group())
		// Use the same users as the first sheet
		if truthy(args["share_users"]) {
			s.ShareUsers(first)
		}
	}
	if d, ok := args["data"].([]any); ok {
		s.dataSet = true
		for _, row := range d {
			m, _ := row.(map[string]any)
			conv := map[string]any{}
			for k, v := range m {
				conv[k] = yamlToPerl(v)
			}
			s.Data = append(s.Data, conv)
		}
	}
	r.sheets[alias] = s
	if r.first == "" {
		r.first = alias
	}
	// build_columns: false leaves the sheet's columns unbuilt (as a
	// DataSheet whose columns are never used)
	cols := map[string]*gads.Column{}
	if v, ok := args["build_columns"]; !ok || truthy(v) {
		cols = s.Columns()
	}
	if v, ok := args["create_records"]; !ok || truthy(v) {
		s.CreateRecords()
	}
	colIDs := map[string]any{}
	for n, c := range cols {
		colIDs[n] = c.ID
	}
	internal := map[string]any{}
	for _, c := range s.Layout().All(gads.AllOptions{OnlyInternal: true, IncludeHidden: true}) {
		internal[c.NameShort] = c.ID
	}
	var recs []any
	for _, id := range r.e.DB.QueryInt64s(`SELECT "id" FROM "current" WHERE "instance_id" = ? ORDER BY "id"`, s.Layout().InstanceID) {
		recs = append(recs, id)
	}
	if recs == nil {
		recs = []any{}
	}
	return map[string]any{
		"instance_id": s.Layout().InstanceID,
		"columns":     colIDs,
		"internal":    internal,
		"records":     recs,
	}
}

// yamlToPerl converts YAML scalars as YAML::XS would present them to
// GADS (numbers become strings once stringified).
func yamlToPerl(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = yamlToPerl(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = yamlToPerl(e)
		}
		return out
	case int:
		return int64(x)
	}
	s, _ := yamlScalar(v)
	return s
}

func (r *Runner) stepSetTime(args map[string]any) any {
	r.setTime(str(args["time"]))
	return map[string]any{"time": str(args["time"])}
}

func (r *Runner) stepCreateRecords(args map[string]any) any {
	s := r.sheet(args["sheet"])
	if d, ok := args["data"].([]any); ok {
		s.Data = nil
		s.dataSet = true
		for _, row := range d {
			m, _ := row.(map[string]any)
			conv := map[string]any{}
			for k, v := range m {
				conv[k] = yamlToPerl(v)
			}
			s.Data = append(s.Data, conv)
		}
	}
	s.CreateRecords()
	return map[string]any{}
}

func (r *Runner) clearLayouts() {
	for _, s := range r.sheets {
		if s.layout != nil {
			s.layout.Clear()
		}
	}
}

func (r *Runner) stepAddAutocur(args map[string]any) any {
	s := r.sheet(args["sheet"])
	refers := r.sheet(args["refers_to"])
	related := r.col(refers.Layout(), args["related_field"])
	var fields []int64
	if cf, ok := args["curval_fields"]; ok {
		for _, n := range strList(cf) {
			fields = append(fields, r.col(refers.Layout(), n).ID)
		}
	}
	c := s.AddAutocur(refers.Layout().InstanceID, related.ID, fields)
	r.clearLayouts()
	return map[string]any{"id": c.ID, "name": c.Name}
}

func (r *Runner) groupID(ref any) int64 {
	s := str(ref)
	if isDigits(s) {
		id, _ := strconv.ParseInt(s, 10, 64)
		return id
	}
	if s == "default" {
		return r.firstSheet().Group()
	}
	if id, ok := r.groups[s]; ok {
		return id
	}
	id, ok := r.e.DB.QueryInt64(`SELECT "id" FROM "group" WHERE "name" = ? ORDER BY "id" LIMIT 1`, s)
	if !ok {
		gads.Fail("Unknown group %s", s)
	}
	return id
}

func (r *Runner) applyColumnAttrs(c *gads.Column, layout *gads.Layout, args map[string]any) {
	if v, ok := args["name"]; ok {
		c.Name = str(v)
	}
	if v, ok := args["name_short"]; ok {
		c.NameShort = str(v)
	}
	boolAttr := func(name string, set func(bool)) {
		if v, ok := args[name]; ok {
			set(truthy(v))
		}
	}
	boolAttr("optional", func(b bool) { c.Optional = b })
	boolAttr("isunique", func(b bool) { c.IsUnique = b })
	boolAttr("remember", func(b bool) { c.Remember = b })
	boolAttr("multivalue", func(b bool) { c.Multivalue = b })
	boolAttr("textbox", func(b bool) { c.Textbox = b })
	boolAttr("end_node_only", func(b bool) { c.EndNodeOnly = b })
	if v, ok := args["can_child"]; ok {
		c.SetCanChild(truthy(v))
	}
	strAttr := func(name string, set func(string)) {
		if v, ok := args[name]; ok {
			set(str(v))
		}
	}
	strAttr("description", func(s string) { c.Description = s })
	strAttr("helptext", func(s string) { c.Helptext = s })
	strAttr("aggregate", func(s string) { c.Aggregate = s })
	strAttr("force_regex", func(s string) { c.ForceRegex = s })
	strAttr("ordering", func(s string) { c.Ordering = s })
	strAttr("code", func(s string) { c.SetCode(s) })
	strAttr("return_type", func(s string) { c.SetReturnType(s) })
	strAttr("notes", func(s string) { c.Notes = s })
	strAttr("lookup_endpoint", func(s string) { c.LookupEndpoint = s })
	strAttr("lookup_group", func(s string) { c.LookupGroup = s })
	if v, ok := args["group_display"]; ok {
		c.SetGroupDisplay(str(v))
	}
	if v, ok := args["width"]; ok {
		c.Width, _ = strconv.ParseInt(str(v), 10, 64)
	}
	if v, ok := args["decimal_places"]; ok {
		if v == nil {
			c.SetDecimalPlaces(nil)
		} else {
			d, _ := strconv.ParseInt(str(v), 10, 64)
			c.SetDecimalPlaces(&d)
		}
	}
	for _, opt := range []string{"value_selector", "show_add", "delete_not_used", "override_permissions", "limit_rows",
		"show_in_edit", "default_today", "show_datepicker", "show_calculator", "default_to_login", "notify_on_selection",
		"notify_on_selection_subject", "notify_on_selection_message", "max_length"} {
		if v, ok := args[opt]; ok {
			c.SetOption(opt, yamlToPerl(v))
		}
	}
	if v, ok := args["filesize"]; ok {
		f, _ := strconv.ParseInt(str(v), 10, 64)
		c.Filesize = &f
	}
	if ev, ok := args["enumvals"].([]any); ok {
		var vals []map[string]any
		for _, e := range ev {
			if m, ok := e.(map[string]any); ok {
				mm := map[string]any{}
				for k, v := range m {
					mm[k] = yamlToPerl(v)
				}
				vals = append(vals, mm)
			} else {
				vals = append(vals, map[string]any{"value": str(e)})
			}
		}
		c.SetEnumvals(vals)
	}
	if ref, ok := args["refers_to"]; ok && ref != nil {
		s := r.sheet(ref)
		c.SetRefersToInstanceID(s.Layout().InstanceID)
		var ids []int64
		if cf, ok := args["curval_fields"]; ok {
			for _, n := range strList(cf) {
				ids = append(ids, r.col(s.Layout(), n).ID)
			}
		} else {
			for _, cc := range s.Layout().All(gads.AllOptions{ExcludeInternal: true}) {
				if cc.Type != "autocur" {
					ids = append(ids, cc.ID)
				}
			}
		}
		c.SetCurvalFieldIDs(ids)
	} else if cf, ok := args["curval_fields"]; ok {
		parent := c.LayoutParent()
		var ids []int64
		for _, n := range strList(cf) {
			ids = append(ids, r.col(parent, n).ID)
		}
		c.SetCurvalFieldIDs(ids)
	}
	if rf, ok := args["related_field"]; ok && rf != nil {
		id := r.col(layout, rf).ID
		c.RelatedFieldID = &id
	}
	if f, ok := args["filter"]; ok {
		pl := layout
		if c.Type == "curval" {
			pl = c.LayoutParent()
		}
		h := r.convertFilter(pl, f)
		c.SetFilter(gads.NewFilterHash(h, nil))
	}
	if df, ok := args["display_fields"]; ok {
		h := r.convertFilter(layout, df)
		c.SetDisplayFields(gads.NewFilterHash(h, layout))
	}
	if lp, ok := args["link_parent"]; ok {
		if lp == nil {
			c.LinkParentID = nil
		} else {
			id := r.col(layout, lp).ID
			c.LinkParentID = &id
		}
	}
	if t, ok := args["topic"]; ok {
		if !truthy(t) {
			c.TopicID = nil
		} else {
			id, ok := r.topics[str(t)]
			if !ok {
				gads.Fail("Unknown topic %s", str(t))
			}
			c.TopicID = &id
		}
	}
	if p, ok := args["permissions"]; ok {
		sheet := r.firstSheet()
		if ps, ok := args["permissions_sheet"]; ok && ps != nil {
			sheet = r.sheet(ps)
		}
		switch pv := p.(type) {
		case string:
			if pv == "default" {
				c.SetPermissions(map[int64][]string{sheet.Group(): defaultPermissions})
			} else if pv == "none" {
				c.SetPermissions(map[int64][]string{})
			}
		case map[string]any:
			perms := map[int64][]string{}
			for g, list := range pv {
				gid := sheet.Group()
				if g != "default" {
					gid = r.groupID(g)
				}
				perms[gid] = strList(list)
			}
			c.SetPermissions(perms)
		}
	}
}

func (r *Runner) treeInput(tree any) []map[string]any {
	n := 0
	var conv func(node any) map[string]any
	conv = func(node any) map[string]any {
		switch x := node.(type) {
		case map[string]any:
			if t, ok := x["text"]; ok {
				out := map[string]any{"text": str(t), "data": map[string]any{}}
				if id, ok := x["id"]; ok {
					out["id"] = str(id)
				}
				var kids []any
				for _, k := range asList(x["children"]) {
					kids = append(kids, conv(k))
				}
				if kids == nil {
					kids = []any{}
				}
				out["children"] = kids
				return out
			}
			for text, kids := range x {
				n++
				var kl []any
				for _, k := range asList(kids) {
					kl = append(kl, conv(k))
				}
				if kl == nil {
					kl = []any{}
				}
				return map[string]any{"text": text, "data": map[string]any{}, "id": fmt.Sprintf("j1_%d", n), "children": kl}
			}
		}
		n++
		return map[string]any{"text": str(node), "children": []any{}, "data": map[string]any{}, "id": fmt.Sprintf("j1_%d", n)}
	}
	var out []map[string]any
	for _, t := range asList(tree) {
		out = append(out, conv(t))
	}
	return out
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func (r *Runner) stepColumn(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "create"
	}
	s := r.sheet(args["sheet"])
	layout := s.Layout()
	// A column of a table created with layout create_table
	var table *gads.Layout
	if t, ok := args["table"]; ok && t != nil {
		if table, ok = r.tables[str(t)]; !ok {
			gads.Fail("Unknown table %s", str(t))
		}
		layout = table
	}
	if op == "create" {
		typ := str(args["type"])
		if typ == "" {
			gads.Fail("Column type required")
		}
		c := layout.NewColumn(typ)
		if _, ok := args["permissions"]; !ok {
			args["permissions"] = "default"
		}
		r.applyColumnAttrs(c, layout, args)
		c.Write(columnWriteOptions(args))
		if typ == "tree" {
			if t, ok := args["tree"]; ok {
				c.TreeUpdate(r.treeInput(t))
			}
		}
		r.clearLayouts()
		if table != nil {
			table.Clear()
		} else if lc := layout.Column(c.ID); lc != nil {
			s.Columns()[c.Name] = lc
		}
		if a, ok := args["as"]; ok && a != nil {
			if r.cols == nil {
				r.cols = map[string]int64{}
			}
			r.cols[str(a)] = c.ID
		}
		return map[string]any{"id": c.ID, "name": c.Name}
	}
	ref := args["name_ref"]
	if ref == nil {
		ref = args["column"]
	}
	if ref == nil {
		ref = args["name"]
	}
	// held_col: a column object kept with op hold (which, as in tests
	// that reuse a column object, can be written again after deletion)
	var c *gads.Column
	if h, ok := args["held_col"]; ok && h != nil {
		if c = r.heldCols[str(h)]; c == nil {
			gads.Fail("Unknown held column %s", str(h))
		}
	} else {
		c = r.col(layout, ref)
	}
	switch op {
	case "hold":
		if r.heldCols == nil {
			r.heldCols = map[string]*gads.Column{}
		}
		r.heldCols[str(args["as"])] = c
		return map[string]any{"id": c.ID}
	case "clear":
		c.ClearCurval()
		return map[string]any{"id": c.ID}
	case "update":
		set, _ := args["set"].(map[string]any)
		r.applyColumnAttrs(c, layout, set)
		c.Write(columnWriteOptions(args))
		if c.Type == "tree" {
			if t, ok := set["tree"]; ok {
				c.TreeUpdate(r.treeInput(t))
			}
		}
		r.clearLayouts()
		return map[string]any{"id": c.ID}
	case "delete":
		c.Delete()
		r.clearLayouts()
		if table != nil {
			table.Clear()
		}
		return map[string]any{"deleted": c.ID}
	case "update_cached":
		// Optionally from a layout for another user (e.g. none, as for
		// overnight updates)
		if truthy(args["layout_user"]) {
			c = r.col(r.layoutFor(args["sheet"], args["user"]), args["column"])
		}
		c.UpdateCached(false)
		r.clearLayouts()
		return map[string]any{"id": c.ID}
	case "get":
		obs := map[string]any{
			"id":          c.ID,
			"name":        c.Name,
			"name_short":  nullIfEmpty(c.NameShort),
			"type":        c.Type,
			"return_type": c.ReturnTypeFn(),
			"multivalue":  boolStr(c.MultivalueFn()),
			"optional":    boolStr(c.Optional),
			"isunique":    boolStr(c.IsUnique),
			"internal":    boolStr(c.Internal),
			"userinput":   boolStr(c.Userinput()),
			"numeric":     boolInt(c.Numeric()),
			"value_field": c.ValueField(),
			"can_child":   boolInt(c.CanChild()),
		}
		if c.Type == "enum" || c.Type == "tree" {
			var ev []any
			for _, e := range c.Enumvals() {
				ev = append(ev, map[string]any{"id": e["id"], "value": e["value"], "deleted": boolInt(truthy(e["deleted"]))})
			}
			if ev == nil {
				ev = []any{}
			}
			obs["enumvals"] = ev
		}
		if c.Type == "tree" {
			obs["tree"] = c.TreeJSON()
		}
		if !c.Userinput() {
			d := append([]int64(nil), c.DependsOn()...)
			sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
			if d == nil {
				d = []int64{}
			}
			obs["depends_on"] = d
		}
		if v, ok := args["validate"]; ok {
			m := map[string]any{}
			for _, x := range strList(v) {
				m[x] = boolInt(c.Validate(x, false))
			}
			obs["validate"] = m
		}
		if v, ok := args["validate_search"]; ok {
			m := map[string]any{}
			for _, x := range strList(v) {
				var res bool
				if err := gads.Try(func() { res = c.ValidateSearch(x, false, nil) }); err != nil {
					res = false
				}
				m[x] = boolInt(res)
			}
			obs["validate_search"] = m
		}
		if v, ok := args["values_beginning_with"]; ok {
			var out []any
			for _, x := range c.ValuesBeginningWith(str(v), false, false) {
				if _, ok := x["id"]; ok {
					out = append(out, map[string]any{"id": x["id"], "label": x["label"]})
				} else {
					out = append(out, x["label"])
				}
			}
			if out == nil {
				out = []any{}
			}
			obs["values_beginning_with"] = out
		}
		if truthy(args["subvals_input_required"]) {
			names := []string{}
			for _, x := range c.SubvalsInputRequired() {
				names = append(names, x.Name)
			}
			sort.Strings(names)
			obs["subvals_input_required"] = names
		}
		if truthy(args["filtered_values"]) && c.IsCurcommon() {
			var out []any
			for _, x := range c.FilteredValues("") {
				out = append(out, map[string]any{"id": x.ID, "value": x.Value})
			}
			if out == nil {
				out = []any{}
			}
			obs["filtered_values"] = out
		}
		if truthy(args["all_values"]) && c.IsCurcommon() {
			var out []any
			for _, x := range c.AllValues() {
				out = append(out, map[string]any{"id": x.ID, "value": x.Value})
			}
			if out == nil {
				out = []any{}
			}
			obs["all_values"] = out
		}
		return obs
	}
	gads.Fail("Unknown column op %s", op)
	return nil
}

func boolStr(b bool) any {
	if b {
		return "1"
	}
	return "0"
}

func boolInt(b bool) any {
	if b {
		return "1"
	}
	return "0"
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *Runner) stepLayout(args map[string]any) any {
	op := str(args["op"])
	s := r.sheet(args["sheet"])
	layout := s.Layout()
	switch op {
	case "create_table":
		// A new table (GADS::Layout->new(name => ...)->write), as the
		// sheet's user
		l := r.e.NewLayout(0, s.User())
		l.Write(map[string]any{"name": args["name"]})
		r.tables[str(args["as"])] = l
		return map[string]any{"instance_id": l.InstanceID}
	case "purge_table":
		l, ok := r.tables[str(args["table"])]
		if !ok {
			gads.Fail("Unknown table %s", str(args["table"]))
		}
		l.Purge()
		return map[string]any{}
	case "delete_table":
		l, ok := r.tables[str(args["table"])]
		if !ok {
			gads.Fail("Unknown table %s", str(args["table"]))
		}
		l.Delete()
		return map[string]any{}
	case "has_column":
		// Whether a column of another sheet (column: alias:name) can be
		// retrieved from this layout by its id or name_short (by)
		parts := strings.SplitN(str(args["column"]), ":", 2)
		src := r.col(r.sheet(parts[0]).Layout(), parts[1])
		var col *gads.Column
		if str(args["by"]) == "name_short" {
			col = layout.ColumnByNameShort(src.NameShort)
		} else {
			col = layout.Column(src.ID)
		}
		return map[string]any{"exists": boolInt(col != nil)}
	case "set_user":
		// Change the user of the sheet's layout (used by everything sharing
		// it, such as held views)
		layout.SetUser(r.user(args["user"]))
		layout.Clear()
		return map[string]any{}
	case "set_alert_columns":
		var ids []int64
		for _, n := range strList(args["columns"]) {
			ids = append(ids, r.col(layout, n).ID)
		}
		layout.SetAlertColumns(ids)
		layout.Write(nil)
		layout.Clear()
		return map[string]any{}
	case "has_children":
		return map[string]any{"has_children": boolInt(layout.HasChildren())}
	case "write_internal":
		// Try writing each internal column, observing the errors (by name,
		// as the internal columns share a position, which PostgreSQL
		// orders arbitrarily)
		out := []any{}
		for _, c := range layout.All(gads.AllOptions{OnlyInternal: true}) {
			var msg any
			if err := gads.Try(func() { c.Write(gads.ColumnWriteOptions{Force: true}) }); err != nil {
				msg = normaliseError(err.Error())
			}
			out = append(out, map[string]any{"name": c.Name, "error": msg})
		}
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].(map[string]any)["name"].(string) < out[j].(map[string]any)["name"].(string)
		})
		return out
	case "delete_all_columns":
		// Delete every column in reverse dependency order, observing for
		// each whether it existed, any error and whether it has gone
		cols := layout.All(gads.AllOptions{OrderDependencies: true})
		out := []any{}
		for i := len(cols) - 1; i >= 0; i-- {
			c := cols[i]
			exists := func() int {
				n, _ := r.e.DB.QueryInt64(`SELECT COUNT(*) FROM "layout" WHERE "id" = ?`, c.ID)
				return int(n)
			}
			before := exists()
			var msg any
			if err := gads.Try(func() { c.Delete() }); err != nil {
				msg = normaliseError(err.Error())
			}
			out = append(out, map[string]any{"name": c.Name, "existed": before, "error": msg, "exists": exists()})
		}
		r.clearLayouts()
		return out
	case "position":
		var ids []int64
		for _, n := range strList(args["columns"]) {
			ids = append(ids, r.col(layout, n).ID)
		}
		layout.Position(ids)
		r.clearLayouts()
		return map[string]any{}
	case "set_groups":
		var set []string
		for _, g := range asList(args["permissions"]) {
			pair := strList(g)
			gid := r.firstSheet().Group()
			if pair[0] != "default" {
				gid = r.groupID(pair[0])
			}
			set = append(set, fmt.Sprintf("%d_%s", gid, pair[1]))
		}
		layout.SetGroups(set)
		layout.Write(nil)
		r.clearLayouts()
		return map[string]any{}
	case "update":
		set := map[string]any{}
		for _, a := range []string{"name", "name_short", "sort_type", "hide_in_selector", "homepage_text"} {
			if v, ok := args[a]; ok {
				set[a] = yamlToPerl(v)
			}
		}
		if v, ok := args["sort_layout"]; ok {
			if v == nil {
				set["sort_layout_id"] = nil
			} else {
				set["sort_layout_id"] = r.col(layout, v).ID
			}
		}
		if v, ok := args["forget_history"]; ok {
			set["forget_history"] = boolIntVal(truthy(v))
		}
		if v, ok := args["default_view_limit_extra"]; ok {
			if v == nil {
				set["default_view_limit_extra_id"] = nil
			} else {
				set["default_view_limit_extra_id"] = r.views[str(v)]
			}
		}
		if v, ok := args["view_limit"]; ok {
			if v == nil {
				set["view_limit_id"] = nil
			} else {
				set["view_limit_id"] = r.views[str(v)]
			}
		}
		layout.Write(set)
		r.clearLayouts()
		return map[string]any{}
	case "columns":
		l := r.layoutFor(args["sheet"], args["user"])
		opts := gads.AllOptions{}
		if o, ok := args["options"].(map[string]any); ok {
			for k, v := range o {
				switch k {
				case "user_can_read":
					opts.UserCanRead = truthy(v)
				case "user_can_write":
					opts.UserCanWrite = truthy(v)
				case "user_can_write_new":
					opts.UserCanWriteNew = truthy(v)
				case "user_can_write_existing":
					opts.UserCanWriteExisting = truthy(v)
				case "exclude_internal":
					opts.ExcludeInternal = truthy(v)
				case "order_dependencies":
					opts.OrderDependencies = truthy(v)
				case "include_hidden":
					opts.IncludeHidden = truthy(v)
				}
			}
		}
		var out []any
		for _, c := range l.All(opts) {
			out = append(out, c.Name)
		}
		if out == nil {
			out = []any{}
		}
		return out
	case "user_can":
		l := r.layoutFor(args["sheet"], args["user"])
		// Permissions are checked differently once all columns are built
		if truthy(args["with_columns"]) {
			l.Columns()
		}
		out := map[string]any{}
		for _, p := range strList(args["permissions"]) {
			out[p] = boolInt(l.UserCan(p))
		}
		return out
	case "delete":
		layout.Delete()
		return map[string]any{}
	}
	gads.Fail("Unknown layout op %s", op)
	return nil
}

func boolIntVal(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (r *Runner) newRecord(args map[string]any, layout *gads.Layout) *gads.Record {
	var u *gads.User
	if v, ok := args["user"]; ok {
		u = r.user(v)
	} else {
		u = r.firstSheet().User()
	}
	rec := r.e.NewRecord(layout, u)
	if v, ok := args["rewind"]; ok && v != nil {
		t := parseDT(str(v))
		rec.Rewind = &t
	}
	return rec
}

func parseDT(s string) time.Time {
	m := regexp.MustCompile(`^(\d{4})-(\d\d)-(\d\d)(?:[ T](\d\d):(\d\d):(\d\d))?`).FindStringSubmatch(s)
	if m == nil {
		gads.Fail("Invalid datetime %s", s)
	}
	at := func(i int) int {
		if m[i] == "" {
			return 0
		}
		n, _ := strconv.Atoi(m[i])
		return n
	}
	return time.Date(at(1), time.Month(at(2)), at(3), at(4), at(5), at(6), 0, time.UTC)
}

func (r *Runner) findRecord(rec *gads.Record, args map[string]any) {
	opts := gads.FindOptions{}
	if fo, ok := args["find_options"].(map[string]any); ok {
		opts.Deleted = truthy(fo["deleted"])
		opts.IncludeDraft = truthy(fo["include_draft"])
	}
	if v, ok := args["current_id"]; ok && v != nil {
		s := str(v)
		if m := regexp.MustCompile(`^@record:(\w+)$`).FindStringSubmatch(s); m != nil {
			s = strconv.FormatInt(r.records[m[1]], 10)
		}
		if m := regexp.MustCompile(`^@held:(\w+)$`).FindStringSubmatch(s); m != nil {
			s = strconv.FormatInt(r.held[m[1]].CurrentID(), 10)
		}
		rec.FindCurrentIDString(s, opts)
		return
	}
	if v, ok := args["record_id"]; ok && v != nil {
		var id int64
		if m := regexp.MustCompile(`^@held_record:(\w+)$`).FindStringSubmatch(str(v)); m != nil {
			id = r.held[m[1]].RecordID()
		} else {
			id, _ = strconv.ParseInt(str(v), 10, 64)
		}
		rec.FindRecordID(id, opts)
		return
	}
	if v, ok := args["serial"]; ok && v != nil {
		id, _ := strconv.ParseInt(str(v), 10, 64)
		rec.FindSerialID(id)
		return
	}
	if v, ok := args["deleted_current_id"]; ok && v != nil {
		id, _ := strconv.ParseInt(str(v), 10, 64)
		rec.FindDeletedCurrentID(id)
		return
	}
	gads.Fail("No record identifier specified")
}

func (r *Runner) setValues(rec *gads.Record, layout *gads.Layout, values any, args map[string]any) {
	type pair struct {
		name string
		val  any
	}
	var pairs []pair
	switch v := values.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			pairs = append(pairs, pair{k, v[k]})
		}
	case []any:
		for _, e := range v {
			if m, ok := e.(map[string]any); ok {
				for k, val := range m {
					pairs = append(pairs, pair{k, val})
				}
			} else if l, ok := e.([]any); ok && len(l) == 2 {
				pairs = append(pairs, pair{str(l[0]), l[1]})
			}
		}
	}
	so := gads.SetValueOptions{}
	if o, ok := args["set_options"].(map[string]any); ok {
		so.Bulk = truthy(o["bulk"])
		so.Draft = truthy(o["draft"])
	}
	for _, p := range pairs {
		c := r.col(layout, p.name)
		d := rec.GetFieldValue(c)
		if d == nil {
			gads.Fail("No datum for column %s", p.name)
		}
		// @own_ids: the datum's own current IDs (a no-op edit)
		if p.val == "@own_ids" {
			ids := []any{}
			if x, ok := d.(interface{ IDs() []int64 }); ok {
				for _, id := range x.IDs() {
					ids = append(ids, id)
				}
			}
			p.val = ids
		}
		// {own_ids_matching: regex}: the IDs of the datum's own curval values
		// whose text matches
		if m, ok := p.val.(map[string]any); ok && m["own_ids_matching"] != nil {
			re := regexp.MustCompile(str(m["own_ids_matching"]))
			ids := []any{}
			if cd, ok := d.(*gads.CurcommonDatum); ok {
				for _, v := range cd.Values() {
					if re.MatchString(v.Value) {
						ids = append(ids, v.ID)
					}
				}
			}
			p.val = ids
		}
		// @own_queries: the query of each of the datum's own curval values
		// with its current ID (as an unchanged curval edit form)
		if p.val == "@own_queries" {
			qs := []any{}
			if cd, ok := d.(*gads.CurcommonDatum); ok {
				for _, v := range cd.Values() {
					qs = append(qs, v.Record.AsQuery(false)+"&current_id="+strconv.FormatInt(v.Record.CurrentID(), 10))
				}
			}
			p.val = qs
		}
		// In a list, {own_id: regex} is the ID of the datum's own curval
		// value whose text matches, and with query that query plus
		// &current_id= that ID
		if l, ok := p.val.([]any); ok {
			out := make([]any, len(l))
			for i, e := range l {
				out[i] = e
				m, ok := e.(map[string]any)
				if !ok || m["own_id"] == nil {
					continue
				}
				re := regexp.MustCompile(str(m["own_id"]))
				var id int64
				if cd, ok := d.(*gads.CurcommonDatum); ok {
					for _, v := range cd.Values() {
						if re.MatchString(v.Value) {
							id = v.ID
							break
						}
					}
				}
				if id == 0 {
					gads.Fail("No own value matching %s", str(m["own_id"]))
				}
				if q, ok := m["query"]; ok && q != nil {
					out[i] = str(r.convertValue(c, str(q))) + "&current_id=" + strconv.FormatInt(id, 10)
				} else {
					out[i] = id
				}
			}
			p.val = out
		}
		d.SetValue(r.convertValue(c, yamlToPerl(p.val)), so)
	}
}

func writeOptions(args map[string]any) gads.WriteOptions {
	o := gads.WriteOptions{NoAlerts: true}
	if wo, ok := args["options"].(map[string]any); ok {
		for k, v := range wo {
			switch k {
			case "draft":
				o.Draft = truthy(v)
			case "no_alerts":
				o.NoAlerts = truthy(v)
			case "force_mandatory":
				o.ForceMandatory = truthy(v)
			case "update_only":
				o.UpdateOnly = truthy(v)
			case "dry_run":
				o.DryRun = truthy(v)
			case "missing_not_fatal":
				o.MissingNotFatal = truthy(v)
			case "no_change_unless_blank":
				o.NoChangeUnlessBlank = truthy(v)
			case "version_datetime":
				if truthy(v) {
					t := parseDT(str(v))
					o.VersionDatetime = &t
				}
			}
		}
	}
	return o
}

func (r *Runner) stepRecord(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "create"
	}
	layout := r.layoutForStep(args)
	rec := r.newRecord(args, layout)
	if cols, ok := args["columns"]; ok && cols != nil {
		var ids []int64
		for _, n := range strList(cols) {
			ids = append(ids, r.col(layout, n).ID)
		}
		if ids == nil {
			ids = []int64{}
		}
		rec.SetColumns(ids)
	}
	wo := writeOptions(args)
	if op == "create" {
		rec.Initialise(layout.InstanceID)
		if v, ok := args["parent_id"]; ok && v != nil {
			rec.SetParentID(r.heldRef(v))
		}
		if v, ok := args["linked_id"]; ok && v != nil {
			id, _ := strconv.ParseInt(str(v), 10, 64)
			rec.SetLinkedID(id)
		}
		r.setValues(rec, layout, args["values"], args)
		rec.Write(wo)
		if a, ok := args["as"]; ok {
			r.records[str(a)] = rec.CurrentID()
		}
		obs := map[string]any{
			"current_id": rec.CurrentID(),
			"record_id":  rec.RecordID(),
			"serial":     rec.Serial(),
		}
		if obsArg, ok := args["observe"]; ok && obsArg != nil {
			obs["record"] = r.observeAfter(args, rec.CurrentID())["values"]
		}
		return obs
	}
	r.findRecord(rec, args)
	switch op {
	case "update":
		r.setValues(rec, layout, args["values"], args)
		var changed map[string]any
		if cc, ok := args["check_changed"]; ok {
			changed = map[string]any{}
			for _, n := range strList(cc) {
				changed[n] = boolInt(gads.DatumChanged(rec.GetFieldValue(r.col(layout, n))))
			}
		}
		rec.Write(wo)
		obs := map[string]any{"current_id": rec.CurrentID(), "record_id": rec.RecordID()}
		if changed != nil {
			obs["changed"] = changed
		}
		if obsArg, ok := args["observe"]; ok && obsArg != nil {
			obs["record"] = r.observeAfter(args, rec.CurrentID())["values"]
		}
		return obs
	case "get":
		obs := r.observeRecord(rec, layout, args["observe"], true)
		if fc, ok := args["for_code"]; ok {
			_ = fc
		}
		if hf, ok := args["html_form"]; ok {
			m := map[string]any{}
			for _, n := range strList(hf) {
				var out []any
				for _, v := range rec.GetFieldValue(r.col(layout, n)).HTMLForm() {
					if mm, ok := v.(map[string]any); ok {
						if id, ok := mm["id"]; ok {
							out = append(out, id)
						} else {
							out = append(out, mm["value_id"])
						}
					} else {
						out = append(out, v)
					}
				}
				if out == nil {
					out = []any{}
				}
				m[n] = out
			}
			obs["html_form"] = m
		}
		if b, ok := args["blank"]; ok {
			m := map[string]any{}
			for _, n := range strList(b) {
				m[n] = boolInt(rec.GetFieldValue(r.col(layout, n)).Blank())
			}
			obs["blank"] = m
		}
		if dv, ok := args["datum_values"]; ok {
			m := map[string]any{}
			for _, n := range strList(dv) {
				m[n] = rec.GetFieldValue(r.col(layout, n)).RawValues()
			}
			obs["datum_values"] = m
		}
		if cr, ok := args["curval_records"]; ok {
			m := map[string]any{}
			for _, n := range strList(cr) {
				var ids []any
				if cd, ok := rec.GetFieldValue(r.col(layout, n)).(*gads.CurcommonDatum); ok {
					for _, v := range cd.Values() {
						ids = append(ids, v.ID)
					}
				}
				if ids == nil {
					ids = []any{}
				}
				m[n] = ids
			}
			obs["curval_records"] = m
		}
		if dn, ok := args["dependent_not_shown"]; ok {
			m := map[string]any{}
			for _, n := range strList(dn) {
				m[n] = boolInt(gads.DependentNotShown(rec.GetFieldValue(r.col(layout, n)), false, ""))
			}
			obs["dependent_not_shown"] = m
		}
		if truthy(args["record_props"]) {
			var linked any
			if l := rec.LinkedID(); l != 0 {
				linked = l
			}
			children := rec.ChildRecordIDs()
			if children == nil {
				children = []int64{}
			}
			obs["props"] = map[string]any{
				"is_draft":         boolInt(rec.IsDraft() != 0),
				"deleted":          boolInt(rec.Deleted() != nil),
				"serial":           rec.Serial(),
				"linked_id":        linked,
				"child_record_ids": children,
			}
		}
		return obs
	case "delete":
		rec.DeleteCurrent(false, 0)
		return map[string]any{"deleted": rec.CurrentID()}
	case "purge":
		rec.PurgeCurrent()
		return map[string]any{"purged": "1"}
	case "restore":
		rec.Restore()
		return map[string]any{"restored": "1"}
	case "versions":
		var out []any
		for _, v := range rec.Versions() {
			t, _ := db.AsTime(v["created"])
			out = append(out, map[string]any{"id": v["id"], "created": t})
		}
		if out == nil {
			out = []any{}
		}
		return out
	case "write_linked_id":
		id, _ := strconv.ParseInt(str(args["linked_id"]), 10, 64)
		rec.WriteLinkedID(id)
		return map[string]any{}
	}
	gads.Fail("Unknown record op %s", op)
	return nil
}

func (r *Runner) observeRecord(rec *gads.Record, layout *gads.Layout, observe any, extra bool) map[string]any {
	values := map[string]any{}
	if obsList, ok := observe.([]any); ok {
		for _, n := range obsList {
			c := r.col(layout, n)
			d := rec.GetFieldValue(c)
			if d == nil {
				values[str(n)] = nil
			} else {
				values[str(n)] = d.AsString()
			}
		}
	} else {
		for _, c := range layout.All(gads.AllOptions{}) {
			d := rec.GetFieldValue(c)
			if d == nil {
				values[c.Name] = nil
			} else {
				values[c.Name] = d.AsString()
			}
		}
	}
	obs := map[string]any{"current_id": rec.CurrentID(), "values": values}
	if extra {
		obs["record_id"] = rec.RecordID()
		if p := rec.ParentID(); p != 0 {
			obs["parent_id"] = p
		} else {
			obs["parent_id"] = nil
		}
	}
	return obs
}

func (r *Runner) observeAfter(args map[string]any, currentID int64) map[string]any {
	layout := r.sheet(args["sheet"]).Layout()
	rec := r.e.NewRecord(layout, r.firstSheet().User())
	rec.FindCurrentID(currentID, gads.FindOptions{})
	var obs any
	if l, ok := args["observe"].([]any); ok {
		obs = l
	}
	return r.observeRecord(rec, layout, obs, false)
}

// columnWriteOptions are the write options of a column step (always forced,
// as the Perl runner does).
func columnWriteOptions(args map[string]any) gads.ColumnWriteOptions {
	o := gads.ColumnWriteOptions{Force: true}
	wo, _ := args["write_options"].(map[string]any)
	for k, v := range wo {
		b := truthy(v)
		switch k {
		case "include_approval_perms":
			o.IncludeApprovalPerms = b
		case "no_cache_update":
			o.NoCacheUpdate = &b
		case "update_dependents":
			o.UpdateDependents = &b
		case "override":
			o.Override = b
		case "report_only":
			o.ReportOnly = b
		case "no_db_add":
			o.NoDBAdd = b
		case "no_alerts":
			o.NoAlerts = b
		case "force":
		default:
			gads.Fail("Unknown column write option %s", k)
		}
	}
	return o
}
