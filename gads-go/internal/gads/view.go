package gads

import (
	"github.com/ctrlo/gads-go/internal/db"
	"regexp"
)

// ViewSort is a sort of a view (sort table row).
type ViewSort struct {
	ID       int64
	LayoutID int64
	ParentID int64
	Type     string
	Order    int64
}

// ViewGroup is a grouping of a view (view_group table row).
type ViewGroup struct {
	ID       int64
	LayoutID int64
	ParentID int64
	Order    int64
}

// View is a saved or temporary view (GADS::View).
type View struct {
	e      *Engine
	layout *Layout

	ID         int64
	InstanceID int64
	row        db.Row
	rowLoaded  bool

	Name        string
	Global      bool
	IsAdmin     bool
	GroupID     *int64
	Owner       *int64
	OtherUserID *int64

	filter     *Filter
	columns    []int64
	columnsSet bool
	sorts      []ViewSort
	sortsSet   bool
	groups     []ViewGroup
	groupsSet  bool
	setSorts   *struct {
		fields []string
		types  []string
	}
	setGroups    []string
	hasSetGroups bool

	UserPermissionOverride bool
}

// LoadView loads a saved view.
func (e *Engine) LoadView(id int64, layout *Layout) *View {
	v := &View{e: e, layout: layout, ID: id, InstanceID: layout.InstanceID}
	v.load()
	return v
}

// NewView creates a new (unsaved) view.
func (e *Engine) NewView(layout *Layout) *View {
	return &View{e: e, layout: layout, InstanceID: layout.InstanceID, rowLoaded: true, columnsSet: true, sortsSet: true, groupsSet: true}
}

// NewTempView creates a temporary view with a filter (not written).
func (e *Engine) NewTempView(layout *Layout, filter *Filter) *View {
	v := e.NewView(layout)
	if filter == nil {
		filter = NewFilter(nil)
	}
	v.filter = filter
	return v
}

func (v *View) load() {
	if v.rowLoaded {
		return
	}
	v.rowLoaded = true
	if v.ID == 0 {
		return
	}
	v.row = v.e.DB.QueryRow(`SELECT * FROM "view" WHERE "id" = ? AND "instance_id" = ?`, v.ID, v.InstanceID)
	if v.row == nil {
		v.ID = 0
		return
	}
	v.Name, _ = db.AsString(v.row["name"])
	v.Global = rowBool(v.row["global"])
	v.IsAdmin = rowBool(v.row["is_admin"])
	v.GroupID = optInt(v.row["group_id"])
	if v.GroupID != nil && *v.GroupID == 0 {
		v.GroupID = nil
	}
	v.Owner = optInt(v.row["user_id"])
	if !v.CurrentUserHasAccess() {
		Fail("User %d does not have access to view %d", v.layout.User().ID, v.ID)
	}
}

// Exists reports whether the view exists in the database.
func (v *View) Exists() bool {
	v.load()
	return v.row != nil && v.ID != 0
}

// Layout returns the view layout.
func (v *View) Layout() *Layout { return v.layout }

// IsLimitExtra reports the is_limit_extra flag.
func (v *View) IsLimitExtra() bool {
	v.load()
	return v.row != nil && rowBool(v.row["is_limit_extra"])
}

// CurrentUserHasAccess reports whether the layout user can access the view.
func (v *View) CurrentUserHasAccess() bool {
	if v.UserPermissionOverride {
		return true
	}
	v.load()
	user := v.layout.User()
	if user == nil {
		return true
	}
	noAccess := v.ID != 0 && !v.Global && !v.IsAdmin && !v.IsLimitExtra() && !v.layout.UserCan("layout") && (v.Owner == nil || *v.Owner != user.ID)
	if !noAccess && v.Global && v.GroupID != nil {
		// The user is retrieved afresh, so its groups are current
		if !v.e.FindUser(user.ID).HasGroup()[*v.GroupID] {
			noAccess = true
		}
	}
	if user.Permission()["superadmin"] {
		noAccess = false
	}
	return !noAccess
}

// Filter returns the view filter.
func (v *View) Filter() *Filter {
	if v.filter == nil {
		v.load()
		if v.row != nil {
			js, _ := db.AsString(v.row["filter"])
			v.filter = NewFilterJSON(js, v.layout)
		} else {
			v.filter = NewFilter(v.layout)
		}
	}
	return v.filter
}

// SetFilter sets the view filter.
func (v *View) SetFilter(f *Filter) { v.filter = f }

// Columns returns the column IDs of the view.
func (v *View) Columns() []int64 {
	if !v.columnsSet {
		v.columnsSet = true
		if v.ID != 0 {
			v.columns = v.e.DB.QueryInt64s(`SELECT "layout_id" FROM "view_layout" WHERE "view_id" = ? ORDER BY "id"`, v.ID)
		}
	}
	return v.columns
}

// HasColumnID reports whether the view includes the column.
func (v *View) HasColumnID(id int64) bool {
	for _, c := range v.Columns() {
		if c == id {
			return true
		}
	}
	return false
}

// SetColumns sets the view columns.
func (v *View) SetColumns(ids []int64) {
	v.columns = ids
	v.columnsSet = true
}

// Sorts returns the view sorts.
func (v *View) Sorts() []ViewSort {
	if v.setSorts != nil {
		return v.buildSetSorts()
	}
	if !v.sortsSet {
		v.sortsSet = true
		if v.ID != 0 {
			for _, r := range v.e.DB.Query(`SELECT * FROM "sort" WHERE "view_id" = ? ORDER BY "order", "id"`, v.ID) {
				s := ViewSort{}
				s.ID, _ = db.AsInt64(r["id"])
				s.LayoutID, _ = db.AsInt64(r["layout_id"])
				s.ParentID, _ = db.AsInt64(r["parent_id"])
				s.Type, _ = db.AsString(r["type"])
				s.Order, _ = db.AsInt64(r["order"])
				v.sorts = append(v.sorts, s)
			}
		}
	}
	return v.sorts
}

// SetSorts sets the sorts from filter IDs ("parent_child" or "id").
func (v *View) SetSorts(fields []string, types []string) {
	v.setSorts = &struct {
		fields []string
		types  []string
	}{fields, types}
}

func (v *View) parseSortID(id string) (int64, int64) {
	if m := reParentChild.FindStringSubmatch(id); m != nil {
		p, _ := anyToInt64(m[1])
		c, _ := anyToInt64(m[2])
		return c, p
	}
	c, _ := anyToInt64(id)
	return c, 0
}

func (v *View) buildSetSorts() []ViewSort {
	var out []ViewSort
	types := append([]string(nil), v.setSorts.types...)
	last := ""
	var order int64
	for _, f := range v.setSorts.fields {
		if f == "" || f == "0" {
			continue
		}
		lid, pid := v.parseSortID(f)
		t := last
		if len(types) > 0 {
			t = types[0]
			types = types[1:]
			if t == "" {
				t = last
			}
		}
		if t != "asc" && t != "desc" && t != "random" {
			Fail("Invalid type %s", t)
		}
		v.checkSortField(lid, pid, "sorts")
		order++
		out = append(out, ViewSort{LayoutID: lid, ParentID: pid, Type: t, Order: order})
		last = t
	}
	return out
}

// checkSortField checks the field (and parent field) of a sort or
// grouping can be read by the user (a field that does not exist is an
// unexpected error, as Perl calls user_can on undef)
func (v *View) checkSortField(lid, pid int64, typ string) {
	for _, id := range []int64{lid, pid} {
		if id == 0 {
			continue
		}
		c := v.layout.Column(id)
		if c == nil {
			Panic(`Can't call method "user_can" on an undefined value`)
		}
		if !c.UserCan("read") {
			Fail("Invalid field ID %d in %s", id, typ)
		}
	}
}

// Groups returns the view groupings.
func (v *View) Groups() []ViewGroup {
	if v.hasSetGroups {
		var out []ViewGroup
		var order int64
		for _, f := range v.setGroups {
			if f == "" || f == "0" {
				continue
			}
			lid, pid := v.parseSortID(f)
			v.checkSortField(lid, pid, "groups")
			order++
			out = append(out, ViewGroup{LayoutID: lid, ParentID: pid, Order: order})
		}
		return out
	}
	if !v.groupsSet {
		v.groupsSet = true
		if v.ID != 0 {
			for _, r := range v.e.DB.Query(`SELECT * FROM "view_group" WHERE "view_id" = ? ORDER BY "order", "id"`, v.ID) {
				g := ViewGroup{}
				g.ID, _ = db.AsInt64(r["id"])
				g.LayoutID, _ = db.AsInt64(r["layout_id"])
				g.ParentID, _ = db.AsInt64(r["parent_id"])
				g.Order, _ = db.AsInt64(r["order"])
				v.groups = append(v.groups, g)
			}
		}
	}
	return v.groups
}

// SetGroups sets the groupings.
func (v *View) SetGroups(fields []string) {
	v.setGroups = fields
	v.hasSetGroups = true
}

// IsGroup reports whether the view is grouped.
func (v *View) IsGroup() bool { return len(v.Groups()) > 0 }

// HasCuruser reports whether the filter uses [CURUSER].
func (v *View) HasCuruser() bool {
	for _, f := range v.Filter().Filters() {
		cid, _ := anyToInt64(f["column_id"])
		c := v.layout.Column(cid)
		if c == nil {
			continue
		}
		if s, ok := f["value"].(string); ok && s == "[CURUSER]" {
			if c.Type == "person" || c.ReturnTypeFn() == "string" {
				return true
			}
		}
	}
	return false
}

// Writable reports whether the layout user can modify the view.
func (v *View) Writable() bool {
	user := v.layout.User()
	if user == nil {
		return true
	}
	if v.IsAdmin {
		return v.layout.UserCan("layout")
	}
	if v.Global {
		if v.GroupID == nil && v.layout.UserCan("layout") {
			return true
		}
		if v.GroupID != nil && (v.layout.UserCan("view_group") || v.layout.UserCan("layout")) {
			return true
		}
		return false
	}
	if v.ID == 0 {
		return v.layout.UserCan("view_create")
	}
	if v.Owner != nil && *v.Owner == user.ID {
		return v.layout.UserCan("view_create")
	}
	return v.layout.UserCan("layout")
}

// HasAlerts reports whether any alerts exist for the view.
func (v *View) HasAlerts() bool {
	if v.ID == 0 {
		return false
	}
	n, _ := v.e.DB.QueryInt64(`SELECT COUNT(*) FROM "alert" WHERE "view_id" = ?`, v.ID)
	return n > 0
}

var reAllSpace = regexp.MustCompile(`^\s*$`)

// Write saves the view (GADS::View::write).
func (v *View) Write(force, noErrors bool) {
	fatal := !noErrors
	if v.Name == "" {
		Fail("Please enter a name for the view")
	}
	if len([]rune(v.Name)) >= 128 {
		Fail("View name must be less than 128 characters")
	}
	// Names consisting of just whitespace characters cause issues when
	// displaying a view
	if reAllSpace.MatchString(v.Name) {
		Fail("View name must not contain only whitespace characters")
	}
	user := v.layout.User()
	global := v.Global
	if user == nil {
		global = true
	}
	vu := map[string]any{
		"name":        v.Name,
		"filter":      v.Filter().AsJSON(),
		"instance_id": v.InstanceID,
		"global":      boolInt(global),
		"is_admin":    boolInt(v.IsAdmin),
		"group_id":    derefInt(v.GroupID),
	}
	v.load()
	if global || v.IsAdmin {
		vu["user_id"] = nil
	} else if v.row == nil || v.Owner == nil {
		if v.layout.UserCan("layout") && v.OtherUserID != nil {
			vu["user_id"] = *v.OtherUserID
		} else {
			vu["user_id"] = user.ID
		}
	}
	for _, f := range v.Filter().Filters() {
		cid, _ := anyToInt64(f["column_id"])
		col := v.layout.Column(cid)
		if col == nil {
			Fail("Field ID %s does not exist", anyToString(f["column_id"]))
		}
		val := f["value"]
		op := anyToString(f["operator"])
		rtype := col.ReturnTypeFn()
		if op == "changed_after" {
			col.validateSearchDate(anyToString(val), fatal)
		} else if rtype == "daterange" {
			if op == "equal" || op == "not_equal" {
				col.ValidateSearch(anyToString(val), fatal, map[string]bool{"full_only": true})
			} else {
				col.ValidateSearch(anyToString(val), fatal, map[string]bool{"single_only": true})
			}
		} else if op != "is_empty" && op != "is_not_empty" {
			col.ValidateSearch(val, fatal, nil)
		}
		hasValue := truthy(val)
		if arr, ok := val.([]any); ok && len(arr) == 0 {
			hasValue = false
		}
		if (op == "is_empty" || op == "is_not_empty") && hasValue {
			Fail("No value can be entered for empty and not empty operators")
		}
		if !col.UserCan("read") {
			Fail("Invalid field ID %d in filter", cid)
		}
	}
	if !v.Writable() && !force {
		if v.ID != 0 {
			Fail("User %d does not have access to modify view %d", user.ID, v.ID)
		}
		Fail("User %d does not have permission to create new views", user.ID)
	}
	if v.HasAlerts() && v.IsGroup() {
		Fail("It is not possible to have alerts on a grouped view. Please either remove the grouping or disable the alerts")
	}
	if v.ID != 0 {
		v.e.DB.UpdateMap("view", vu, map[string]any{"id": v.ID})
		// Update any alert caches for the new filter
		if v.Filter().Changed() && v.HasAlerts() {
			v.e.NewAlert(v.layout, user, v.ID).UpdateCache(false)
		}
	} else {
		vu["created"] = v.e.Now()
		if user != nil {
			vu["createdby"] = user.ID
		} else {
			vu["createdby"] = nil
		}
		v.ID = v.e.DB.InsertMap("view", vu)
	}
	// Sorts and groups
	sorts := v.Sorts()
	groups := v.Groups()
	v.e.DB.Exec(`DELETE FROM "sort" WHERE "view_id" = ?`, v.ID)
	for _, s := range sorts {
		v.e.DB.InsertMap("sort", map[string]any{"view_id": v.ID, "layout_id": nullID(s.LayoutID), "parent_id": nullID(s.ParentID), "type": s.Type, "order": s.Order})
	}
	v.e.DB.Exec(`DELETE FROM "view_group" WHERE "view_id" = ?`, v.ID)
	for _, g := range groups {
		v.e.DB.InsertMap("view_group", map[string]any{"view_id": v.ID, "layout_id": nullID(g.LayoutID), "parent_id": nullID(g.ParentID), "order": g.Order})
	}
	v.setSorts = nil
	v.hasSetGroups = false
	v.sortsSet = false
	v.sorts = nil
	v.groupsSet = false
	v.groups = nil
	// Columns
	colviews := v.Columns()
	inView := map[int64]bool{}
	for _, c := range colviews {
		inView[c] = true
	}
	for _, c := range v.layout.All(AllOptions{UserCanRead: true}) {
		if !inView[c.ID] {
			continue
		}
		if _, ok := v.e.DB.QueryInt64(`SELECT "id" FROM "view_layout" WHERE "view_id" = ? AND "layout_id" = ?`, v.ID, c.ID); !ok {
			v.e.DB.Insert("view_layout", []string{"layout_id", "view_id"}, c.ID, v.ID)
			// Update the alert cache with the new column: one row for each
			// record already in the cache (without a user, as Perl's
			// grouped search by current_id)
			var pop []map[string]any
			for _, cid := range v.e.DB.QueryInt64s(`SELECT MAX( "alert_caches"."current_id" ) FROM "view" "me" LEFT JOIN "alert_cache" "alert_caches" ON "alert_caches"."view_id" = "me"."id" WHERE ( "me"."id" = ? ) GROUP BY "current_id"`, v.ID) {
				pop = append(pop, map[string]any{"layout_id": c.ID, "view_id": v.ID, "current_id": cid})
			}
			v.e.DB.Populate("alert_cache", pop)
		}
	}
	if len(colviews) > 0 {
		args := []any{v.ID}
		for _, c := range colviews {
			args = append(args, c)
		}
		v.e.DB.Exec(`DELETE FROM "view_layout" WHERE "view_id" = ? AND NOT "layout_id" IN (`+db.Placeholders(len(colviews))+`)`, args...)
		v.e.DB.Exec(`DELETE FROM "alert_cache" WHERE "view_id" = ? AND NOT "layout_id" IN (`+db.Placeholders(len(colviews))+`)`, args...)
	} else {
		v.e.DB.Exec(`DELETE FROM "view_layout" WHERE "view_id" = ?`, v.ID)
		v.e.DB.Exec(`DELETE FROM "alert_cache" WHERE "view_id" = ?`, v.ID)
	}
	// Filter table
	existing := map[int64]bool{}
	for _, id := range v.e.DB.QueryInt64s(`SELECT "layout_id" FROM "filter" WHERE "view_id" = ?`, v.ID) {
		existing[id] = true
	}
	var all []any
	for _, f := range v.Filter().Filters() {
		cid, _ := anyToInt64(f["column_id"])
		all = append(all, cid)
		if existing[cid] || cid <= 0 {
			continue
		}
		v.e.DB.Insert("filter", []string{"layout_id", "view_id"}, cid, v.ID)
		existing[cid] = true
	}
	if len(all) > 0 {
		args := append([]any{}, all...)
		args = append(args, v.ID)
		v.e.DB.Exec(`DELETE FROM "filter" WHERE ( "layout_id" NOT IN (`+db.Placeholders(len(all))+`) AND "view_id" = ? )`, args...)
	} else {
		v.e.DB.Exec(`DELETE FROM "filter" WHERE "view_id" = ?`, v.ID)
	}
	v.rowLoaded = false
	v.load()
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// Delete deletes the view.
func (v *View) Delete() {
	if !v.Writable() {
		Fail("You do not have permission to delete %d", v.ID)
	}
	if n, _ := v.e.DB.QueryInt64(`SELECT COUNT(*) FROM "view_limit" WHERE "view_id" = ?`, v.ID); n > 0 {
		var users []string
		for _, r := range v.e.DB.Query(`SELECT "user"."value" FROM "view_limit" "me" JOIN "user" "user" ON "user"."id" = "me"."user_id" WHERE "me"."view_id" = ?`, v.ID) {
			s, _ := db.AsString(r["value"])
			users = append(users, s)
		}
		Fail("This view cannot be deleted as it is used to limit user data. Remove the view from the limited views of the following users before deleting: %s", joinWith(users, "; "))
	}
	if w, ok := v.e.DB.QueryInt64(`SELECT "id" FROM "widget" WHERE "view_id" = ? LIMIT 1`, v.ID); ok {
		Fail("This view cannot be used as it is used in widget ID %d", w)
	}
	if !v.Exists() {
		return
	}
	for _, t := range []string{"sort", "view_layout", "view_group", "filter", "alert_cache"} {
		v.e.DB.Exec(`DELETE FROM `+q(t)+` WHERE "view_id" = ?`, v.ID)
	}
	alerts := v.e.DB.QueryInt64s(`SELECT "id" FROM "alert" WHERE "view_id" = ?`, v.ID)
	for _, a := range alerts {
		v.e.DB.Exec(`DELETE FROM "alert_send" WHERE "alert_id" = ?`, a)
		v.e.DB.Exec(`DELETE FROM "alert" WHERE "id" = ?`, a)
	}
	v.e.DB.Exec(`UPDATE "user" SET "lastview" = NULL WHERE "lastview" = ?`, v.ID)
	v.e.DB.Exec(`DELETE FROM "view" WHERE "id" = ?`, v.ID)
}

func joinWith(s []string, sep string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}
