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

func (r *Runner) buildView(args map[string]any, layout *gads.Layout) *gads.View {
	if vr, ok := args["view_ref"]; ok && vr != nil {
		id, ok := r.views[str(vr)]
		if !ok {
			gads.Fail("Unknown view %s", str(vr))
		}
		return r.e.LoadView(id, layout)
	}
	v, _ := args["view"].(map[string]any)
	if v == nil {
		v = map[string]any{}
	}
	f := args["filter"]
	if f == nil {
		f = v["filter"]
	}
	view := r.e.NewView(layout)
	view.Name = "Test view"
	if n, ok := v["name"]; ok {
		view.Name = str(n)
	}
	if h := r.convertFilter(layout, f); h != nil {
		view.SetFilter(gads.NewFilterHash(h, layout))
	}
	cols := args["columns"]
	if cols == nil {
		cols = v["columns"]
	}
	if cols != nil {
		var ids []int64
		for _, n := range strList(cols) {
			ids = append(ids, r.col(layout, n).ID)
		}
		view.SetColumns(ids)
	}
	if sorts, ok := v["sorts"].([]any); ok {
		var fields, types []string
		for _, s := range sorts {
			m, _ := s.(map[string]any)
			fields = append(fields, r.filterID(layout, m["name"]))
			t := str(m["type"])
			if t == "" {
				t = "asc"
			}
			types = append(types, t)
		}
		view.SetSorts(fields, types)
	}
	if groups, ok := v["groups"].([]any); ok {
		var fields []string
		for _, g := range groups {
			fields = append(fields, r.filterID(layout, g))
		}
		view.SetGroups(fields)
	}
	return view
}

// stepRepeatSingle retrieves the first record (Records.Single) repeat times
// with a view, as for random sorts: observes for each of columns whether the
// values differed between retrievals (many) or not (one, with the value).
func (r *Runner) stepRepeatSingle(args map[string]any) any {
	layout := r.layoutForStep(args)
	var u *gads.User
	if v, ok := args["user"]; ok {
		u = r.user(v)
	} else {
		u = r.firstSheet().User()
	}
	n := 10
	if v, ok := args["repeat"]; ok {
		n, _ = strconv.Atoi(str(v))
	}
	seen := map[string]map[string]bool{}
	cols := strList(args["columns"])
	for i := 0; i < n; i++ {
		recs := r.e.NewRecords(layout, u)
		recs.View = r.buildView(args, layout)
		rec := recs.Single()
		for _, c := range cols {
			if seen[c] == nil {
				seen[c] = map[string]bool{}
			}
			seen[c][rec.Fields()[r.col(layout, c).ID].AsString()] = true
		}
	}
	obs := map[string]any{}
	for _, c := range cols {
		if len(seen[c]) > 1 {
			obs[c] = "many"
		} else {
			for v := range seen[c] {
				obs[c] = "one: " + v
			}
		}
	}
	return obs
}

// stepFileAccess retrieves the file of record current_id with
// FilevalFindWithPermission as user.
func (r *Runner) stepFileAccess(args map[string]any) any {
	id, ok := r.e.DB.QueryInt64(`SELECT "me"."id" FROM "fileval" "me" JOIN "file" "files" ON "files"."value" = "me"."id" JOIN "record" "record" ON "record"."id" = "files"."record_id" WHERE "record"."current_id" = ? ORDER BY "me"."id" LIMIT 1`, args["current_id"])
	if !ok {
		gads.Fail("No file for record %s", str(args["current_id"]))
	}
	return map[string]any{"found": boolInt(r.e.FilevalFindWithPermission(id, r.user(args["user"]), false, false))}
}

// stepSleep waits in real time (for values such as Lua's os.time).
func (r *Runner) stepSleep(args map[string]any) any {
	n, _ := strconv.Atoi(str(args["seconds"]))
	time.Sleep(time.Duration(n) * time.Second)
	return map[string]any{}
}

func (r *Runner) stepSearch(args map[string]any) any {
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
	if v, ok := args["rows"]; ok {
		recs.Rows, _ = strconv.Atoi(str(v))
	}
	if v, ok := args["page"]; ok {
		recs.Page, _ = strconv.Atoi(str(v))
	}
	if v, ok := args["search"]; ok && v != nil {
		recs.SetSearch(str(v))
	}
	if v, ok := args["rewind"]; ok && v != nil {
		t := parseDT(str(v))
		recs.Rewind = &t
	}
	if v, ok := args["from"]; ok && v != nil {
		t := parseDT(str(v))
		recs.From = &t
	}
	if v, ok := args["to"]; ok && v != nil {
		t := parseDT(str(v))
		recs.To = &t
	}
	if v, ok := args["exclusive"]; ok {
		recs.Exclusive = str(v)
	}
	if v, ok := args["include_children"]; ok {
		recs.SetIncludeChildren(truthy(v))
	}
	if truthy(args["is_deleted"]) {
		recs.IsDeleted = true
	}
	if truthy(args["include_approval"]) {
		recs.IncludeApproval = true
	}
	if v, ok := args["view_limit_extra"]; ok && v != nil {
		id := r.views[str(v)]
		recs.ViewLimitExtraID = &id
	}
	if v, ok := args["view_limit_override"]; ok && v != nil {
		id := r.views[str(v)]
		recs.ViewLimitOverrideID = &id
	}
	if v, ok := args["retrieve"]; ok {
		var ids []int64
		for _, n := range strList(v) {
			ids = append(ids, r.col(layout, n).ID)
		}
		recs.SetColumns(ids)
	}
	// retrieve_all: all the layout's non-internal columns
	if truthy(args["retrieve_all"]) {
		var ids []int64
		for _, c := range layout.All(gads.AllOptions{ExcludeInternal: true}) {
			ids = append(ids, c.ID)
		}
		recs.SetColumns(ids)
	}
	if s, ok := args["sort"].([]any); ok {
		var specs []gads.SortSpec
		for _, x := range s {
			m, _ := x.(map[string]any)
			var id int64
			// sheet: the column is in another sheet; parent: the curval
			// column it is a subfield of
			l := layout
			if sh, ok := m["sheet"]; ok && sh != nil {
				l = r.sheet(sh).Layout()
			}
			if v, ok := m["id"]; ok && v != nil {
				id, _ = strconv.ParseInt(str(v), 10, 64)
			} else {
				id, _ = strconv.ParseInt(r.filterID(l, m["name"]), 10, 64)
			}
			t := str(m["type"])
			if t == "" {
				t = "asc"
			}
			spec := gads.SortSpec{ID: id, Type: t}
			if p, ok := m["parent"]; ok && p != nil {
				spec.ParentID = r.col(layout, str(p)).ID
			}
			specs = append(specs, spec)
		}
		recs.SetSort(specs)
	}
	if ds, ok := args["default_sort"].(map[string]any); ok {
		spec := gads.SortSpec{Type: str(ds["type"])}
		if n, ok := ds["name"]; ok && n != nil {
			spec.ID = r.col(layout, n).ID
		} else {
			spec.ID, _ = strconv.ParseInt(str(ds["id"]), 10, 64)
		}
		recs.DefaultSortOverride = &spec
	}
	if af, ok := args["additional_filters"].([]any); ok {
		var filters []gads.AdditionalFilter
		for _, x := range af {
			m, _ := x.(map[string]any)
			filters = append(filters, gads.AdditionalFilter{ID: r.col(layout, m["name"]).ID, Value: yamlToPerl(m["value"]), IsText: truthy(m["is_text"])})
		}
		recs.AdditionalFilters = filters
	}
	// timeline: the records' data_timeline (with label, group and color
	// columns), calendar: data_calendar
	if tl, ok := args["timeline"]; ok && truthy(tl) {
		return r.observeTimeline(recs, layout, tl)
	}
	if truthy(args["calendar"]) {
		items := []any{}
		for _, it := range recs.DataCalendar() {
			items = append(items, timelineItemObs(it))
		}
		return map[string]any{"items": items}
	}
	obs := map[string]any{}
	if !truthy(args["no_count"]) {
		obs["count"] = recs.Count()
	}
	if _, ok := args["rows"]; ok {
		obs["pages"] = recs.Pages()
	}
	observe := strList(args["observe"])
	var ids []any
	var rows []any
	var recCols []any
	var results []*gads.Record
	if !truthy(args["no_results"]) {
		results = recs.Results()
	}
	for _, rec := range results {
		if cid := rec.CurrentID(); cid != 0 {
			ids = append(ids, cid)
		} else {
			ids = append(ids, nil)
		}
		// record_columns: the columns of each record shown (its
		// columns_render, as the presentation layer)
		if truthy(args["record_columns"]) {
			names := []any{}
			for _, c := range rec.ColumnsRender() {
				names = append(names, c.Name)
			}
			recCols = append(recCols, names)
		}
		if args["observe"] != nil {
			v := map[string]any{}
			for _, n := range observe {
				c := r.col(layout, n)
				d := rec.GetFieldValue(c)
				if d == nil {
					v[n] = nil
				} else {
					v[n] = d.AsString()
				}
			}
			rows = append(rows, v)
		}
	}
	if ids == nil {
		ids = []any{}
	}
	obs["ids"] = ids
	if truthy(args["record_columns"]) {
		if recCols == nil {
			recCols = []any{}
		}
		obs["record_columns"] = recCols
	}
	if len(ids) > 0 {
		obs["first"] = ids[0]
		obs["last"] = ids[len(ids)-1]
	}
	if args["observe"] != nil {
		if rows == nil {
			rows = []any{}
		}
		obs["rows"] = rows
	}
	if agg, ok := args["aggregate"]; ok {
		ar := recs.AggregateResults()
		if ar == nil {
			obs["aggregate"] = nil
		} else {
			m := map[string]any{}
			for _, n := range strList(agg) {
				m[n] = ar.GetFieldValue(r.col(layout, n)).AsString()
			}
			obs["aggregate"] = m
		}
	}
	if truthy(args["columns_render"]) {
		names := []any{}
		for _, c := range recs.ColumnsRender() {
			names = append(names, c.Name)
		}
		obs["columns_render"] = names
	}
	if truthy(args["group_counts"]) {
		counts := []any{}
		for _, rec := range results {
			counts = append(counts, rec.IDCount())
		}
		obs["id_counts"] = counts
	}
	if truthy(args["relations"]) {
		rel := []any{}
		for _, rec := range results {
			rel = append(rel, map[string]any{
				"parent_id":        parentObs(rec),
				"child_record_ids": childrenObs(rec),
			})
		}
		obs["relations"] = rel
	}
	if truthy(args["single"]) {
		recs.Clear()
		// single_dependent_not_shown: also each record's
		// dependent_not_shown for that column
		var dnsCol *gads.Column
		if c, ok := args["single_dependent_not_shown"]; ok && c != nil {
			dnsCol = r.col(layout, c)
		}
		single, dns := []any{}, []any{}
		for {
			rec := recs.Single()
			if rec == nil {
				break
			}
			single = append(single, rec.CurrentID())
			if dnsCol != nil {
				dns = append(dns, boolInt(gads.DependentNotShown(rec.GetFieldValue(dnsCol), false, "")))
			}
		}
		obs["single"] = single
		if dnsCol != nil {
			obs["single_dependent_not_shown"] = dns
		}
	}
	return obs
}

func (r *Runner) stepView(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "create"
	}
	ur := args["user"]
	if ur == nil {
		ur = "superadmin"
	}
	layout := r.layoutFor(args["sheet"], ur)
	switch op {
	case "create", "update":
		var view *gads.View
		if op == "update" {
			id, ok := r.views[str(args["ref"])]
			if !ok {
				gads.Fail("Unknown view %s", str(args["ref"]))
			}
			view = r.e.LoadView(id, layout)
		} else {
			view = r.e.NewView(layout)
		}
		if v, ok := args["name"]; ok {
			view.Name = str(v)
		}
		if v, ok := args["global"]; ok {
			view.Global = truthy(v)
		}
		if v, ok := args["is_admin"]; ok {
			view.IsAdmin = truthy(v)
		}
		if v, ok := args["group"]; ok {
			if v == nil {
				view.GroupID = nil
			} else {
				g := r.groupID(v)
				view.GroupID = &g
			}
		}
		if f, ok := args["filter"]; ok {
			h := r.convertFilter(layout, f)
			if h == nil {
				h = map[string]any{}
			}
			view.SetFilter(gads.NewFilterHash(h, layout))
		}
		// Set the view's existing filter (so that the change is tracked), as
		// a hash or as JSON
		if f, ok := args["set_filter"]; ok {
			h := r.convertFilter(layout, f)
			if h == nil {
				h = map[string]any{}
			}
			view.Filter().SetHash(h)
		}
		if f, ok := args["set_filter_json"]; ok {
			h := r.convertFilter(layout, f)
			if h == nil {
				h = map[string]any{}
			}
			view.Filter().SetJSON(gads.EncodeJSON(h))
		}
		if c, ok := args["columns"]; ok {
			var ids []int64
			if c == "@all" {
				for _, col := range layout.All(gads.AllOptions{}) {
					ids = append(ids, col.ID)
				}
			} else {
				for _, n := range strList(c) {
					ids = append(ids, r.col(layout, n).ID)
				}
			}
			view.SetColumns(ids)
		}
		if sorts, ok := args["sorts"].([]any); ok {
			var fields, types []string
			for _, s := range sorts {
				m, _ := s.(map[string]any)
				fields = append(fields, r.filterID(layout, m["name"]))
				t := str(m["type"])
				if t == "" {
					t = "asc"
				}
				types = append(types, t)
			}
			view.SetSorts(fields, types)
		}
		if g, ok := args["groups"]; ok {
			var fields []string
			for _, x := range strList(g) {
				fields = append(fields, r.filterID(layout, x))
			}
			view.SetGroups(fields)
		}
		force, noErrors := false, false
		if wo, ok := args["write_options"].(map[string]any); ok {
			force = truthy(wo["force"])
			noErrors = truthy(wo["no_errors"])
		}
		view.Write(force, noErrors)
		if a, ok := args["as"]; ok {
			r.views[str(a)] = view.ID
		}
		return map[string]any{"id": view.ID}
	case "delete":
		id, ok := r.views[str(args["ref"])]
		if !ok {
			gads.Fail("Unknown view %s", str(args["ref"]))
		}
		r.e.LoadView(id, layout).Delete()
		return map[string]any{}
	case "get":
		id, ok := r.views[str(args["ref"])]
		if !ok {
			gads.Fail("Unknown view %s", str(args["ref"]))
		}
		v := r.e.LoadView(id, layout)
		cols := v.Columns()
		if cols == nil {
			cols = []int64{}
		}
		return map[string]any{
			"id":       v.ID,
			"name":     v.Name,
			"global":   boolInt(v.Global),
			"is_admin": boolInt(v.IsAdmin),
			"filter":   v.Filter().AsHash(),
			"columns":  cols,
			"writable": boolInt(v.Writable()),
			"exists":   boolInt(v.Exists()),
		}
	}
	gads.Fail("Unknown view op %s", op)
	return nil
}

func (r *Runner) stepUser(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "create"
	}
	sheet := r.firstSheet()
	switch op {
	case "create":
		var uid, team int64
		if v, ok := args["user_id"]; ok {
			uid, _ = strconv.ParseInt(str(v), 10, 64)
		}
		if v, ok := args["team_id"]; ok {
			team, _ = strconv.ParseInt(str(v), 10, 64)
		}
		u := sheet.CreateUser(strList(args["permissions"]), uid, team, truthy(args["no_group"]), 0)
		if a, ok := args["as"]; ok {
			r.users[str(a)] = u
		}
		return map[string]any{"id": u.ID}
	case "view_limits":
		u := r.user(args["user"])
		var ids []int64
		for _, v := range strList(args["views"]) {
			id, ok := r.views[v]
			if !ok {
				gads.Fail("Unknown view %s", v)
			}
			ids = append(ids, id)
		}
		u.SetViewLimits(ids)
		return map[string]any{}
	case "group":
		id := r.e.CreateGroup(str(args["name"]))
		r.groups[str(args["name"])] = id
		for _, u := range strList(args["users"]) {
			r.e.DB.InsertMap("user_group", map[string]any{"user_id": r.user(u).ID, "group_id": id})
		}
		return map[string]any{"id": id}
	case "create_user":
		// ResultSet::User->create_user, as the user admin pages (params as
		// the form, with group names and view aliases; welcome emails are
		// not sent)
		params := map[string]any{}
		if p, ok := args["params"].(map[string]any); ok {
			for k, v := range p {
				params[k] = v
			}
		}
		if g, ok := params["groups"].([]any); ok {
			ids := []any{}
			for _, x := range g {
				ids = append(ids, r.groupID(x))
			}
			params["groups"] = ids
		}
		if vl, ok := params["view_limits"].([]any); ok {
			ids := []any{}
			for _, x := range vl {
				id, ok := r.views[str(x)]
				if !ok {
					gads.Fail("Unknown view %s", str(x))
				}
				ids = append(ids, id)
			}
			params["view_limits"] = ids
		}
		cu := args["current_user"]
		if cu == nil {
			cu = "superadmin"
		}
		params["current_user"] = r.user(cu)
		u := r.e.CreateUser(params)
		if a, ok := args["as"]; ok {
			r.users[str(a)] = u
		}
		return map[string]any{"id": u.ID, "value": u.Value, "username": u.Username}
	case "update_row":
		// Update the user row directly (with its validation)
		set, _ := args["set"].(map[string]any)
		r.e.UpdateUserFields(r.user(args["user"]).ID, set)
		return map[string]any{}
	case "insert_row":
		// Create a user row directly (with its validation)
		set, _ := args["set"].(map[string]any)
		id := r.e.InsertUser(set)
		if a := args["as"]; a != nil {
			r.users[str(a)] = r.e.FindUser(id)
		}
		return map[string]any{"id": id}
	case "set_groups":
		// As $user->groups($logged_in_user, [...])
		by := args["by"]
		if by == nil {
			by = "superadmin"
		}
		// (save: name keeps the user's current groups, which restore: name
		// sets again)
		u := r.user(args["user"])
		if sv, ok := args["save"]; ok {
			r.savedGroups[str(sv)] = r.e.DB.QueryInt64s(`SELECT "group_id" FROM "user_group" WHERE "user_id" = ?`, u.ID)
		}
		var ids []int64
		if rs, ok := args["restore"]; ok {
			saved, ok := r.savedGroups[str(rs)]
			if !ok {
				gads.Fail("Unknown saved groups %s", str(rs))
			}
			ids = saved
		} else {
			for _, g := range strList(args["groups"]) {
				ids = append(ids, r.groupID(g))
			}
		}
		u.SetGroups(r.user(by), ids)
		return map[string]any{}
	case "groups_viewable":
		names := r.user(args["user"]).GroupsViewable()
		sort.Strings(names)
		out := []any{}
		var re *regexp.Regexp
		if m, ok := args["match"]; ok && m != nil {
			re = regexp.MustCompile(str(m))
		}
		for _, n := range names {
			if re == nil || re.MatchString(n) {
				out = append(out, n)
			}
		}
		return out
	case "remove_from_group":
		gid := sheet.Group()
		if str(args["group"]) != "default" {
			gid = r.groupID(args["group"])
		}
		r.e.DB.Exec(`DELETE FROM "user_group" WHERE "user_id" = ? AND "group_id" = ?`, r.user(args["user"]).ID, gid)
		return map[string]any{}
	}
	gads.Fail("Unknown user op %s", op)
	return nil
}

func (r *Runner) stepLua(args map[string]any) any {
	layout := r.sheet(args["sheet"]).Layout()
	c := layout.NewColumn("calc")
	c.ReturnType = str(args["return_type"])
	if c.ReturnType == "" {
		c.ReturnType = "string"
	}
	c.Multivalue = truthy(args["multivalue"])
	vars := map[string]any{}
	if v, ok := args["vars"].(map[string]any); ok {
		for k, x := range v {
			vars[k] = yamlToPerl(x)
		}
	}
	ret := c.Eval(str(args["code"]), vars)
	var e any
	if ret.HasError() {
		e = normaliseError(ret.Error())
	}
	return map[string]any{"return": ret.Return(), "error": e}
}

func (r *Runner) stepDB(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "update"
	}
	table := str(args["table"])
	where, _ := args["where"].(map[string]any)
	conv := func(m map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			if s, ok := v.(string); ok && s == "@now" {
				out[k] = r.e.Now()
			} else if s, ok := v.(string); ok && strings.HasPrefix(s, "@") && r.dbRef(s) != nil {
				out[k] = r.dbRef(s)
			} else {
				out[k] = yamlToPerl(v)
			}
		}
		return out
	}
	switch op {
	case "update":
		set, _ := args["set"].(map[string]any)
		r.e.DB.UpdateMap(table, conv(set), conv(where))
		if truthy(args["clear_layouts"]) {
			r.clearLayouts()
		}
	case "count", "max":
		w := conv(where)
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		agg := "COUNT(*)"
		if op == "max" {
			agg = "MAX(" + db.Q(str(args["column"])) + ")"
		}
		q := "SELECT " + agg + " FROM " + db.Q(table)
		var vals []any
		for i, k := range keys {
			if i == 0 {
				q += " WHERE "
			} else {
				q += " AND "
			}
			var sb strings.Builder
			vals = db.WhereTerm(&sb, k, w[k], vals)
			q += sb.String()
		}
		n, ok := r.e.DB.QueryInt64(q, vals...)
		if op == "max" {
			if !ok {
				return map[string]any{"max": nil}
			}
			return map[string]any{"max": n}
		}
		return map[string]any{"count": n}
	case "delete":
		r.e.DB.DeleteWhere(table, conv(where))
	case "insert":
		set, _ := args["set"].(map[string]any)
		return map[string]any{"id": r.e.DB.InsertMap(table, conv(set))}
	case "select":
		cols := strList(args["columns"])
		w := conv(where)
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var conds []string
		var vals []any
		for _, k := range keys {
			if w[k] == nil {
				conds = append(conds, db.Q("me")+"."+db.Q(k)+" IS NULL")
			} else if m, ok := w[k].(map[string]any); ok {
				// An operator of SQL::Abstract, as { ">": 35 }
				ops := make([]string, 0, len(m))
				for o := range m {
					ops = append(ops, o)
				}
				sort.Strings(ops)
				for _, o := range ops {
					switch o {
					case ">", "<", ">=", "<=", "!=":
					default:
						panic("Unsupported operator " + o)
					}
					conds = append(conds, db.Q("me")+"."+db.Q(k)+" "+o+" ?")
					vals = append(vals, yamlToPerl(m[o]))
				}
			} else {
				conds = append(conds, db.Q("me")+"."+db.Q(k)+" = ?")
				vals = append(vals, w[k])
			}
		}
		var sel []string
		for _, c := range cols {
			// The stored text, as HashRefInflator returns it (the driver
			// would otherwise parse dates; on PostgreSQL the database layer
			// gives them as text, and a cast would give binary as hex)
			if r.e.DB.Dialect == db.Postgres {
				sel = append(sel, db.Q("me")+"."+db.Q(c))
			} else {
				sel = append(sel, "CAST("+db.Q("me")+"."+db.Q(c)+" AS TEXT) AS "+db.Q(c))
			}
		}
		q := "SELECT " + strings.Join(sel, ", ") + " FROM " + db.Q(table) + " " + db.Q("me")
		if len(conds) > 0 {
			q += " WHERE " + strings.Join(conds, " AND ")
		}
		// Tables without an id column name their own order
		var order []string
		if ob, ok := args["order_by"].([]any); ok {
			for _, o := range ob {
				order = append(order, db.Q("me")+"."+db.Q(str(o)))
			}
		} else {
			order = []string{db.Q("me") + "." + db.Q("id")}
		}
		q += " ORDER BY " + strings.Join(order, ", ")
		// Columns whose value is reported only as whether it matches a
		// regex (for values such as JSON with an unpredictable key order)
		match := map[string]*regexp.Regexp{}
		if cm, ok := args["columns_match"].(map[string]any); ok {
			for c, re := range cm {
				match[c] = regexp.MustCompile(str(re))
			}
		}
		out := []any{}
		for _, row := range r.e.DB.Query(q, vals...) {
			m := map[string]any{}
			for _, c := range cols {
				m[c] = row[c]
				if re, ok := match[c]; ok {
					m[c] = boolInt(row[c] != nil && re.MatchString(str(row[c])))
				}
			}
			out = append(out, m)
		}
		return out
	}
	return map[string]any{}
}

func (r *Runner) stepSetMultivalue(args map[string]any) any {
	s := r.sheet(args["sheet"])
	s.SetMultivalue(truthy(args["value"]))
	r.clearLayouts()
	return map[string]any{}
}

var reDBRef = regexp.MustCompile(`^@(user|group|column|instance|record|view):(.+)$`)

// dbRef resolves references to runner objects in values for db steps.
func (r *Runner) dbRef(s string) any {
	m := reDBRef.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	switch m[1] {
	case "user":
		return r.user(m[2]).ID
	case "group":
		if m[2] == "default" {
			return r.firstSheet().Group()
		}
		return r.groupID(m[2])
	case "column":
		return r.col(nil, m[2]).ID
	case "instance":
		return r.sheet(m[2]).Layout().InstanceID
	case "view":
		id, ok := r.views[m[2]]
		if !ok {
			gads.Fail("Unknown view %s", m[2])
		}
		return id
	case "record":
		id, ok := r.records[m[2]]
		if !ok {
			gads.Fail("Unknown record alias %s", m[2])
		}
		return id
	}
	return nil
}

// timelineItemObs is an item of a timeline or calendar, with its style (a
// random colour for most values) only as whether it has one.
func timelineItemObs(it gads.TimelineItem) map[string]any {
	m := map[string]any{}
	for k, v := range it.Fields {
		m[k] = v
	}
	m["style"] = boolInt(it.Fields["style"] != nil)
	if c, ok := it.Fields["color"]; ok {
		m["color"] = boolInt(c != nil)
	}
	if h, ok := it.Fields["has_time"]; ok {
		m["has_time"] = boolInt(truthy(h))
	}
	if vs, ok := it.Fields["values"].([]map[string]any); ok {
		out := []any{}
		for _, v := range vs {
			out = append(out, str(v["name"])+": "+str(v["value"]))
		}
		m["values"] = out
	}
	return m
}

func (r *Runner) observeTimeline(recs *gads.Records, layout *gads.Layout, opts any) any {
	var o gads.TimelineOptions
	if m, ok := opts.(map[string]any); ok {
		id := func(k string) int64 {
			v, ok := m[k]
			if !ok || !truthy(v) {
				return 0
			}
			if n, err := strconv.ParseInt(str(v), 10, 64); err == nil {
				return n
			}
			return r.col(layout, v).ID
		}
		o.Label, o.Group, o.Color = id("label"), id("group"), id("color")
		o.AllGroupValues = truthy(m["all_group_values"])
	}
	tl := recs.DataTimeline(o)
	items := []any{}
	for _, it := range tl.Items {
		items = append(items, timelineItemObs(it))
	}
	groups := []any{}
	for _, g := range tl.Groups {
		groups = append(groups, map[string]any{"content": g.Content, "order": g.Order})
	}
	dt := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return t.UTC().Format("2006-01-02T15:04:05")
	}
	return map[string]any{
		"items":  items,
		"groups": groups,
		"colors": len(tl.Colors),
		"min":    dt(tl.Min),
		"max":    dt(tl.Max),
	}
}
