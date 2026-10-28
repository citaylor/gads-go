package conformance

import (
	"encoding/base64"
	"regexp"
	"strconv"

	"github.com/ctrlo/gads-go/internal/gads"
)

// stepViews observes the views available to the user of the sheet's layout
// (GADS::Views->user_views_all).
func (r *Runner) stepViews(args map[string]any) any {
	layout := r.sheet(args["sheet"]).Layout()
	vs := r.e.NewViews(layout)
	if o, ok := args["other_user"]; ok && o != nil {
		id := r.user(o).ID
		vs.OtherUserID = &id
	}
	names, ids := []any{}, []any{}
	for _, v := range vs.UserViewsAll() {
		names = append(names, v.Name)
		ids = append(ids, v.ID)
	}
	return map[string]any{"names": names, "ids": ids}
}

var heldViewRe = regexp.MustCompile(`^@held:(\w+)$`)

// stepViewHeld operates on a view object kept between steps, using the
// sheet's layout.
func (r *Runner) stepViewHeld(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "get"
	}
	name := str(args["as"])
	if name == "" {
		gads.Fail("Held view alias (as) required")
	}
	layout := r.sheet(args["sheet"]).Layout()
	if r.heldViews == nil {
		r.heldViews = map[string]*gads.View{}
		r.heldViewNew = map[string]map[string]any{}
	}
	if op == "new" {
		if idv, ok := args["id"]; ok && idv != nil {
			// Perl loads a view lazily, so its access check is made when
			// the view is first used: defer loading until then
			r.heldViewNew[name] = args
			delete(r.heldViews, name)
			return map[string]any{}
		}
		view := r.e.NewView(layout)
		r.viewAttrs(view, layout, args)
		r.heldViews[name] = view
		delete(r.heldViewNew, name)
		return map[string]any{}
	}
	if na, ok := r.heldViewNew[name]; ok {
		delete(r.heldViewNew, name)
		ids := str(na["id"])
		var id int64
		if m := heldViewRe.FindStringSubmatch(ids); m != nil {
			id = r.heldViews[m[1]].ID
		} else if n, err := strconv.ParseInt(ids, 10, 64); err == nil {
			id = n
		} else if n, ok := r.views[ids]; ok {
			id = n
		} else {
			gads.Fail("Unknown view %s", ids)
		}
		view := r.e.LoadView(id, layout)
		r.viewAttrs(view, layout, na)
		r.heldViews[name] = view
	}
	view, ok := r.heldViews[name]
	if !ok {
		gads.Fail("Unknown held view %s", name)
	}
	switch op {
	case "set":
		r.viewAttrs(view, layout, args)
		if g, ok := args["groups"]; ok {
			var fields []string
			for _, x := range strList(g) {
				fields = append(fields, r.filterID(layout, x))
			}
			view.SetGroups(fields)
		}
		if f, ok := args["filter"]; ok && f == nil {
			view.SetFilter(nil)
		}
	case "write":
		view.Write(false, false)
		if a, ok := args["view_as"]; ok {
			r.views[str(a)] = view.ID
		}
		row := r.e.DB.QueryRow(`SELECT "user_id" FROM "view" WHERE "id" = ?`, view.ID)
		return map[string]any{"id": view.ID, "user_id": row["user_id"]}
	case "filter":
		view.Filter()
	case "delete":
		view.Delete()
	case "get":
	default:
		gads.Fail("Unknown held view op %s", op)
	}
	obs := map[string]any{}
	if truthy(args["observe"]) {
		obs["name"] = nil
		if view.Exists() || view.Name != "" {
			obs["name"] = view.Name
		}
		cols := []any{}
		for _, c := range view.Columns() {
			cols = append(cols, c)
		}
		obs["columns"] = cols
	}
	if truthy(args["filter_base64"]) {
		b, _ := base64.StdEncoding.DecodeString(view.Filter().Base64())
		obs["filter_base64"], _ = gads.DecodeJSON(string(b))
	}
	return obs
}

// viewAttrs sets view attributes from a step: name, global, is_admin,
// columns, group (name, default or null), other_user, filter.
func (r *Runner) viewAttrs(view *gads.View, layout *gads.Layout, args map[string]any) {
	if v, ok := args["name"]; ok {
		view.Name = str(v)
	}
	if v, ok := args["global"]; ok {
		view.Global = truthy(v)
	}
	if v, ok := args["is_admin"]; ok {
		view.IsAdmin = truthy(v)
	}
	if c, ok := args["columns"]; ok {
		var ids []int64
		for _, n := range strList(c) {
			ids = append(ids, r.col(layout, n).ID)
		}
		view.SetColumns(ids)
	}
	if v, ok := args["group"]; ok {
		if v == nil {
			view.GroupID = nil
		} else {
			g := r.groupID(v)
			view.GroupID = &g
		}
	}
	if o, ok := args["other_user"]; ok && o != nil {
		id := r.user(o).ID
		view.OtherUserID = &id
	}
	if f, ok := args["filter"]; ok && f != nil {
		view.SetFilter(gads.NewFilterHash(r.convertFilter(layout, f), nil))
	}
}

// stepSite sets the site of the schema (as $schema->site_id).
func (r *Runner) stepSite(args map[string]any) any {
	id, _ := strconv.ParseInt(str(args["id"]), 10, 64)
	r.e.SiteID = id
	return map[string]any{}
}

// stepInstances observes the instances the user can access
// (GADS::Instances->all).
func (r *Runner) stepInstances(args map[string]any) any {
	names := []any{}
	is := r.e.NewInstances(r.user(args["user"]))
	for _, l := range is.All() {
		names = append(names, l.Name())
	}
	obs := map[string]any{"names": names}
	if v, ok := args["is_valid"]; ok && v != nil {
		valid := []any{}
		for _, id := range asList(v) {
			n, _ := strconv.ParseInt(str(id), 10, 64)
			valid = append(valid, boolInt(is.IsValid(n) != 0))
		}
		obs["is_valid"] = valid
	}
	return obs
}

// stepTablePermission is one table-level permission test of
// t/018_instance.t: test (delete, purge, download, layout, view_create,
// link) as user, with the layout of sheet either built anew for the user
// (layout_from: new, which also makes it the sheet's layout) or from
// Instances (layout_from: instances). allowed: whether the permission is
// expected (to undo what the test did). reset_groups: afterwards remove the
// table permissions using that layout. Observes the errors of each action
// (null if none).
func (r *Runner) stepTablePermission(args map[string]any) any {
	s := r.sheet(args["sheet"])
	user := r.user(args["user"])
	var layout *gads.Layout
	if from := str(args["layout_from"]); from == "" || from == "new" {
		s.SetUserLayout(user)
		s.ClearLayout()
		layout = s.Layout()
	} else {
		layout = r.e.NewInstances(user).Layout(s.Layout().InstanceID)
	}
	records := r.e.NewRecords(layout, user)
	errors := []any{}
	attempt := func(fn func()) {
		if err := gads.Try(fn); err != nil {
			errors = append(errors, normaliseError(err.Error()))
		} else {
			errors = append(errors, nil)
		}
	}
	setDeleted := func(id int64, deleted bool) {
		var v any
		if deleted {
			v = r.e.Now()
		}
		r.e.DB.Exec(`UPDATE "current" SET "deleted" = ? WHERE "id" = ?`, v, id)
	}
	allowed := truthy(args["allowed"])
	switch test := str(args["test"]); test {
	case "delete":
		rec := records.Single()
		attempt(func() { rec.DeleteCurrent(false, 0) })
		if allowed {
			setDeleted(rec.CurrentID(), false)
		}
	case "purge":
		rec := records.Single()
		currentID, recordID := rec.CurrentID(), rec.RecordID()
		setDeleted(currentID, true)
		attempt(func() { rec.FindDeletedCurrentID(currentID) })
		attempt(func() { rec.FindRecordID(recordID, gads.FindOptions{Deleted: true}) })
		setDeleted(currentID, false)
		attempt(func() { rec.PurgeCurrent() })
		if allowed {
			setDeleted(currentID, true)
			attempt(func() { rec.PurgeCurrent() })
			s.CreateRecords()
		}
		// Purging a single version (which no longer exists if purged)
		attempt(func() { rec.Purge() })
		if allowed {
			errors = errors[:len(errors)-1]
		}
	case "download":
		attempt(func() { records.CSVHeader() })
		attempt(func() { records.CSVLine() })
	case "layout":
		t := true
		col := layout.All(gads.AllOptions{Userinput: &t})[0]
		attempt(func() { col.Write(gads.ColumnWriteOptions{}) })
	case "view_create":
		v := r.e.NewView(layout)
		v.Name = "Test"
		attempt(func() { v.Write(false, false) })
	case "link":
		rec := records.Single()
		attempt(func() { rec.WriteLinkedID(rec.CurrentID()) })
	default:
		gads.Fail("Invalid test: %s", test)
	}
	if truthy(args["reset_groups"]) {
		layout.SetGroups([]string{})
		layout.Write(nil)
	}
	return map[string]any{"errors": errors}
}

// stepTopic creates (op create: name, sheet or table, as) or updates (op
// update: as, prevent_edit as a topic alias or null) a topic.
func (r *Runner) stepTopic(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "create"
	}
	switch op {
	case "create":
		var iid int64
		if t, ok := args["table"]; ok && t != nil {
			tl := r.tables[str(t)]
			if tl == nil {
				// As the Perl runner's method call on the missing table
				gads.Fail(`Can't call method "instance_id" on an undefined value`)
			}
			iid = tl.InstanceID
		} else {
			iid = r.sheet(args["sheet"]).Layout().InstanceID
		}
		id := r.e.DB.InsertMap("topic", map[string]any{"name": args["name"], "instance_id": iid})
		r.topics[str(args["as"])] = id
		return map[string]any{"id": id}
	case "update":
		id, ok := r.topics[str(args["as"])]
		if !ok {
			gads.Fail("Unknown topic %s", str(args["as"]))
		}
		var prevent any
		if p := args["prevent_edit"]; p != nil {
			prevent = r.topics[str(p)]
		}
		r.e.DB.UpdateMap("topic", map[string]any{"prevent_edit_topic_id": prevent}, map[string]any{"id": id})
		return map[string]any{}
	}
	gads.Fail("Unknown topic op %s", op)
	return nil
}

// stepHistoricPurge purges the values of columns in all versions of a
// record (Result::Current->historic_purge).
func (r *Runner) stepHistoricPurge(args map[string]any) any {
	layout := r.sheet(args["sheet"]).Layout()
	cid, _ := strconv.ParseInt(str(args["current_id"]), 10, 64)
	if row := r.e.DB.QueryRow(`SELECT "id" FROM "current" WHERE "id" = ?`, cid); row == nil {
		gads.Fail("Record %d not found", cid)
	}
	u := args["user"]
	if u == nil {
		u = "superadmin"
	}
	var ids []int64
	for _, c := range strList(args["columns"]) {
		ids = append(ids, r.col(layout, c).ID)
	}
	r.e.HistoricPurge(cid, r.user(u), ids)
	return map[string]any{}
}

// stepDownload produces a CSV download of records (Records->csv_header and
// csv_line), optionally writing a new record part way through.
func (r *Runner) stepDownload(args map[string]any) any {
	layout := r.layoutForStep(args)
	var u *gads.User
	if v, ok := args["user"]; ok {
		u = r.user(v)
	} else {
		u = r.firstSheet().User()
	}
	recs := r.e.NewRecords(layout, u)
	if args["filter"] != nil || args["view"] != nil || args["view_ref"] != nil || args["columns"] != nil {
		recs.View = r.buildView(args, layout)
	}
	lines := []any{}
	obs := map[string]any{"header": recs.CSVHeader()}
	ins, _ := args["insert_after"].(map[string]any)
	insLine, _ := strconv.Atoi(str(ins["line"]))
	for i := 1; ; i++ {
		line, ok := recs.CSVLine()
		if !ok {
			break
		}
		lines = append(lines, line)
		if ins != nil && insLine == i {
			rec := r.e.NewRecord(layout, u)
			rec.Initialise(0)
			r.setValues(rec, layout, ins["values"], ins)
			rec.Write(gads.WriteOptions{NoAlerts: true})
		}
	}
	obs["lines"] = lines
	return obs
}
