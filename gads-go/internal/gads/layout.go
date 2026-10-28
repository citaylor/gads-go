package gads

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// Layout is a table definition and its columns (GADS::Layout).
type Layout struct {
	e          *Engine
	user       *User
	InstanceID int64
	parent     *Layout // set when cloned (use_layout)
	record     *Record

	instanceRow db.Row

	columns      []*Column
	columnsIndex map[int64]*Column
	nameHash     map[string]*Column
	shortHash    map[string]*Column
	mycols       []*Column

	userPermsColumns map[int64]map[int64]map[string]bool
	userPermsTable   map[string]bool
	userPermsTableOK bool
	overall          map[string]bool
	groupPerms       map[int64]map[string]bool

	cachedRecords        map[int64]*Record
	cachedRecordsAutocur map[int64]*Record
	referredBy           []int64
	referredByLoaded     bool

	// set_groups for table permissions
	setGroups    []string
	hasSetGroups bool
}

// NewLayout creates a layout object for an instance and user.
func (e *Engine) NewLayout(instanceID int64, user *User) *Layout {
	return &Layout{e: e, user: user, InstanceID: instanceID}
}

// User returns the layout user.
func (l *Layout) User() *User { return l.user }

// SetUser changes the layout user (as $layout->user(...); callers clear the
// layout afterwards).
func (l *Layout) SetUser(u *User) { l.user = u }

// Engine returns the engine.
func (l *Layout) Engine() *Engine { return l.e }

// Record returns the record associated with the layout (for filtered
// curvals).
func (l *Layout) Record() *Record { return l.record }

// SetRecord associates a record with the layout.
func (l *Layout) SetRecord(r *Record) { l.record = r }

// UseLayout returns the parent layout if cloned.
func (l *Layout) UseLayout() *Layout {
	if l.parent != nil {
		return l.parent
	}
	return l
}

// Clone creates a layout for another instance sharing the same columns.
func (l *Layout) Clone(instanceID int64) *Layout {
	return &Layout{e: l.e, user: l.user, InstanceID: instanceID, parent: l.UseLayout()}
}

func (l *Layout) rset() db.Row {
	if l.instanceRow == nil {
		l.instanceRow = l.e.DB.QueryRow(`SELECT * FROM "instance" WHERE "id" = ?`, l.InstanceID)
		if l.instanceRow == nil {
			l.instanceRow = db.Row{}
		}
	}
	return l.instanceRow
}

func (l *Layout) rsetStr(k string) string {
	s, _ := db.AsString(l.rset()[k])
	return s
}

func (l *Layout) rsetInt(k string) *int64 { return optInt(l.rset()[k]) }

// Name returns the table name.
func (l *Layout) Name() string { return l.rsetStr("name") }

// NameShort returns the table short name.
func (l *Layout) NameShort() string { return l.rsetStr("name_short") }

// Identifier returns the table identifier.
func (l *Layout) Identifier() string {
	if ns := l.rsetStr("name_short"); ns != "" {
		return ns
	}
	return "table" + strconv.FormatInt(l.InstanceID, 10)
}

// RecordName returns the record name ("record" by default).
func (l *Layout) RecordName() string {
	if s := l.rsetStr("record_name"); s != "" {
		return s
	}
	return "record"
}

// ForgetHistory reports whether history is disabled.
func (l *Layout) ForgetHistory() bool { return rowBool(l.rset()["forget_history"]) }

// SortLayoutID returns the default sort column.
func (l *Layout) SortLayoutID() *int64 { return l.rsetInt("sort_layout_id") }

// SortType returns the default sort type.
func (l *Layout) SortType() string { return l.rsetStr("sort_type") }

// ViewLimitID returns the default view limit.
func (l *Layout) ViewLimitID() *int64 { return l.rsetInt("view_limit_id") }

// DefaultViewLimitExtraID returns the default extra view limit.
func (l *Layout) DefaultViewLimitExtraID() *int64 { return l.rsetInt("default_view_limit_extra_id") }

// Clear clears all caches.
func (l *Layout) Clear() {
	l.instanceRow = nil
	l.columns = nil
	l.columnsIndex = nil
	l.nameHash = nil
	l.shortHash = nil
	l.mycols = nil
	l.ClearPermissions()
	l.cachedRecords = nil
	l.cachedRecordsAutocur = nil
	l.referredBy = nil
	l.referredByLoaded = false
}

// ClearPermissions clears permission caches.
func (l *Layout) ClearPermissions() {
	l.userPermsColumns = nil
	l.userPermsTable = nil
	l.userPermsTableOK = false
	l.overall = nil
	l.groupPerms = nil
}

// ClearIndexes clears name indexes.
func (l *Layout) ClearIndexes() {
	l.instanceRow = nil
	l.columnsIndex = nil
	l.nameHash = nil
	l.shortHash = nil
}

// Columns returns all columns of all instances (site wide).
func (l *Layout) Columns() []*Column {
	ul := l.UseLayout()
	if ul != l {
		return ul.Columns()
	}
	if l.columns != nil {
		return l.columns
	}
	rows := l.e.DB.Query(`SELECT "me".* FROM "layout" "me" WHERE "me"."instance_id" IN (SELECT "id" FROM "instance"` + l.e.siteWhere("") + `) ORDER BY "me"."position", "me"."id"`)
	if l.e.pg() {
		rows = l.pgColumnOrder(rows)
	}
	cols := make([]*Column, 0, len(rows))
	for _, r := range rows {
		cols = append(cols, l.e.columnFromRow(l, r))
	}
	l.columns = cols
	return cols
}

// pgColumnOrder orders the columns as GADS's prefetching query of them
// does (in the order of their first rows), as PostgreSQL orders the
// columns of the same position by its plan rather than by ID
func (l *Layout) pgColumnOrder(rows []db.Row) []db.Row {
	byID := map[int64]db.Row{}
	for _, r := range rows {
		id, _ := db.AsInt64(r["id"])
		byID[id] = r
	}
	all := l.e.DB.Query(`SELECT "me"."id" AS "me_id", "me".*, "calcs".*, "rags".*, "link_parent".*, "display_fields".* FROM "layout" "me" LEFT JOIN "calc" "calcs" ON "calcs"."layout_id" = "me"."id" LEFT JOIN "rag" "rags" ON "rags"."layout_id" = "me"."id" LEFT JOIN "layout" "link_parent" ON "link_parent"."id" = "me"."link_parent" LEFT JOIN "display_field" "display_fields" ON "display_fields"."layout_id" = "me"."id" WHERE ( "me"."instance_id" IN ( SELECT "me"."id" FROM "instance" "me"` + l.e.siteWhere("me") + ` ) ) ORDER BY "me"."position", "display_fields"."id"`)
	out := make([]db.Row, 0, len(rows))
	for _, a := range all {
		id, _ := db.AsInt64(a["me_id"])
		if r, ok := byID[id]; ok {
			out = append(out, r)
			delete(byID, id)
		}
	}
	if len(out) != len(rows) {
		return rows
	}
	return out
}

func (e *Engine) siteWhere(alias string) string {
	if e.SiteID == 0 {
		return ""
	}
	p := ""
	if alias != "" {
		p = q(alias) + "."
	}
	return " WHERE " + p + `"site_id" = ` + itoa(e.SiteID)
}

func (l *Layout) index() map[int64]*Column {
	ul := l.UseLayout()
	if ul.columnsIndex == nil {
		ul.columnsIndex = map[int64]*Column{}
		for _, c := range ul.Columns() {
			ul.columnsIndex[c.ID] = c
		}
	}
	return ul.columnsIndex
}

// Column returns a column by ID (any instance), or nil.
func (l *Layout) Column(id int64) *Column {
	if id == 0 {
		return nil
	}
	return l.index()[id]
}

// ColumnPerm returns a column only if the user has the permission.
func (l *Layout) ColumnPerm(id int64, permission string) *Column {
	c := l.Column(id)
	if c == nil {
		return nil
	}
	if permission != "" && !c.UserCan(permission) {
		return nil
	}
	return c
}

// ColumnThisInstance reports whether the column belongs to this layout.
func (l *Layout) ColumnThisInstance(id int64) bool {
	c := l.Column(id)
	return c != nil && c.InstanceID == l.InstanceID
}

// ColumnByName returns a column of this instance by name.
func (l *Layout) ColumnByName(name string) *Column {
	if l.nameHash == nil {
		l.nameHash = map[string]*Column{}
		for _, c := range l.UseLayout().Columns() {
			if c.InstanceID != l.InstanceID {
				continue
			}
			if _, ok := l.nameHash[c.Name]; ok {
				Fail("Column %s exists twice - unable to find unique column", c.Name)
			}
			l.nameHash[c.Name] = c
		}
	}
	return l.nameHash[name]
}

// ColumnByNameShort returns a column by short name. Internal columns
// (starting "_") are resolved within this instance.
func (l *Layout) ColumnByNameShort(name string) *Column {
	ul := l.UseLayout()
	if ul.shortHash == nil {
		ul.shortHash = map[string]*Column{}
		for _, c := range ul.Columns() {
			if c.NameShort == "" {
				continue
			}
			if c.Internal {
				ul.shortHash[c.NameShort+itoa(c.InstanceID)] = c
			} else {
				ul.shortHash[c.NameShort] = c
			}
		}
	}
	key := name
	if strings.HasPrefix(name, "_") {
		key = name + itoa(l.InstanceID)
	}
	return ul.shortHash[key]
}

// AllShortNames returns all short names across instances.
func (l *Layout) AllShortNames() []string {
	l.ColumnByNameShort("_id")
	ul := l.UseLayout()
	out := make([]string, 0, len(ul.shortHash))
	for k := range ul.shortHash {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ColumnID returns the internal ID column.
func (l *Layout) ColumnID() *Column {
	c := l.ColumnByNameShort("_id")
	if c == nil {
		Panic("Internal _id column missing")
	}
	return c
}

func (l *Layout) myCols() []*Column {
	if l.mycols == nil {
		for _, c := range l.UseLayout().Columns() {
			if c.InstanceID == l.InstanceID {
				l.mycols = append(l.mycols, c)
			}
		}
		if l.mycols == nil {
			l.mycols = []*Column{}
		}
	}
	return l.mycols
}

// AllOptions are the filtering options of Layout::all.
type AllOptions struct {
	Type                     string
	IncludeHidden            bool
	TopicID                  *int64
	HasTopicFilter           bool
	OrderDependencies        bool
	ExcludeInternal          bool
	OnlyInternal             bool
	IncludeColumnIDs         map[int64]bool
	OnlyUnique               bool
	CanChild                 bool
	Linked                   bool
	IsGlobe                  bool
	Remember                 *bool
	Userinput                *bool
	HasCache                 bool
	Multivalue               *bool
	UserCanRead              bool
	UserCanWrite             bool
	UserCanWriteNew          bool
	UserCanReadwriteNew      bool
	UserCanWriteExisting     bool
	UserCanReadwriteExisting bool
	UserCanApproveNew        bool
	UserCanApproveExisting   bool
	SortByTopics             bool
	IncludeInternal          bool // no-op, as in GADS
}

// BoolP returns a pointer to b.
func BoolP(b bool) *bool { return &b }

// All returns the columns of this instance filtered by options.
func (l *Layout) All(o AllOptions) []*Column {
	cols := append([]*Column(nil), l.myCols()...)
	filter := func(f func(c *Column) bool) {
		out := cols[:0:0]
		for _, c := range cols {
			if f(c) {
				out = append(out, c)
			}
		}
		cols = out
	}
	if !o.IncludeHidden {
		filter(func(c *Column) bool { return !c.Hidden() })
	}
	if o.HasTopicFilter {
		filter(func(c *Column) bool {
			if o.TopicID != nil && *o.TopicID != 0 {
				return c.TopicID != nil && *c.TopicID == *o.TopicID
			}
			return c.TopicID == nil || *c.TopicID == 0
		})
	}
	if o.OrderDependencies {
		cols = l.orderDependencies(cols)
	}
	if o.ExcludeInternal {
		filter(func(c *Column) bool { return !c.Internal })
	}
	if o.OnlyInternal {
		filter(func(c *Column) bool { return c.Internal })
	}
	if o.IncludeColumnIDs != nil {
		filter(func(c *Column) bool { return o.IncludeColumnIDs[c.ID] })
	}
	if o.OnlyUnique {
		filter(func(c *Column) bool { return c.IsUnique })
	}
	if o.CanChild {
		filter(func(c *Column) bool { return c.CanChild() })
	}
	if o.Linked {
		filter(func(c *Column) bool { return c.LinkParent() != nil })
	}
	if o.Type != "" {
		filter(func(c *Column) bool { return c.Type == o.Type })
	}
	if o.IsGlobe {
		filter(func(c *Column) bool { return c.ReturnTypeFn() == "globe" })
	}
	if o.Remember != nil {
		filter(func(c *Column) bool { return c.Remember == *o.Remember })
	}
	if o.Userinput != nil {
		filter(func(c *Column) bool { return c.Userinput() == *o.Userinput })
	}
	if o.HasCache {
		filter(func(c *Column) bool { return c.HasCache() })
	}
	if o.Multivalue != nil {
		filter(func(c *Column) bool { return c.MultivalueFn() == *o.Multivalue })
	}
	if o.UserCanRead {
		filter(func(c *Column) bool { return c.UserCan("read") })
	}
	if o.UserCanWrite {
		filter(func(c *Column) bool { return c.UserCan("write") })
	}
	if o.UserCanWriteNew {
		filter(func(c *Column) bool { return c.UserCan("write_new") })
	}
	if o.UserCanReadwriteNew {
		filter(func(c *Column) bool { return c.UserCan("write_new") || c.UserCan("read") })
	}
	if o.UserCanWriteExisting {
		filter(func(c *Column) bool { return c.UserCan("write_existing") })
	}
	if o.UserCanReadwriteExisting {
		filter(func(c *Column) bool { return c.UserCan("write_existing") || c.UserCan("read") })
	}
	if o.UserCanApproveNew {
		filter(func(c *Column) bool { return c.UserCan("approve_new") })
	}
	if o.UserCanApproveExisting {
		filter(func(c *Column) bool { return c.UserCan("approve_existing") })
	}
	if o.SortByTopics {
		topics := map[int64][]*Column{}
		for _, c := range cols {
			if c.TopicID != nil && *c.TopicID != 0 {
				topics[*c.TopicID] = append(topics[*c.TopicID], c)
			}
		}
		var out []*Column
		var prev int64
		done := map[int64]bool{}
		for _, c := range cols {
			if done[c.ID] {
				continue
			}
			done[c.ID] = true
			tid := int64(0)
			if c.TopicID != nil {
				tid = *c.TopicID
			}
			if tid != 0 && tid != prev {
				for _, tc := range topics[tid] {
					out = append(out, tc)
					done[tc.ID] = true
				}
			} else {
				out = append(out, c)
			}
			prev = tid
		}
		cols = out
	}
	return cols
}

// AllWithInternal returns all columns including internal ones.
func (l *Layout) AllWithInternal() []*Column { return l.All(AllOptions{}) }

// AllUserRead returns columns the user can read.
func (l *Layout) AllUserRead() []*Column { return l.All(AllOptions{UserCanRead: true}) }

// AllIDs returns all column IDs of this instance.
func (l *Layout) AllIDs() []int64 {
	var out []int64
	for _, c := range l.UseLayout().Columns() {
		if c.InstanceID == l.InstanceID {
			out = append(out, c.ID)
		}
	}
	return out
}

// CheckRecursive checks for recursive dependencies, returning the path of
// any recursion found.
func (l *Layout) CheckRecursive(deps map[int64]map[int64]bool, key int64, path []int64) string {
	if path == nil {
		path = []int64{key}
	}
	search := map[int64]bool{}
	for _, p := range path[:len(path)-1] {
		search[p] = true
	}
	if search[key] {
		var names []string
		for _, p := range path {
			if c := l.Column(p); c != nil {
				names = append(names, c.Name)
			}
		}
		return strings.Join(names, " → ")
	}
	keys := make([]int64, 0, len(deps[key]))
	for k := range deps[key] {
		keys = append(keys, k)
	}
	sortInt64s(keys)
	for _, val := range keys {
		path = append(path, val)
		if ret := l.CheckRecursive(deps, val, path); ret != "" {
			return ret
		}
		path = path[:len(path)-1]
	}
	return ""
}

// orderDependencies orders columns so that code values follow the
// columns they depend on (Algorithm::Dependency::Ordered).
func (l *Layout) orderDependencies(cols []*Column) []*Column {
	if len(cols) == 0 {
		return nil
	}
	deps := map[int64]map[int64]bool{}
	for _, c := range cols {
		m := map[int64]bool{}
		var src []int64
		if c.HasDisplayField() {
			src = c.DisplayFieldColIDs()
		} else {
			src = c.DependsOn()
		}
		for _, d := range src {
			if d != c.ID {
				m[d] = true
			}
		}
		deps[c.ID] = m
	}
	keys := make([]int64, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sortInt64s(keys)
	for _, k := range keys {
		if ret := l.CheckRecursive(deps, k, nil); ret != "" {
			Fail("Calculated value or display condition recursive dependencies: %s", ret)
		}
	}
	order := scheduleAll(deps)
	idx := l.index()
	var out []*Column
	for _, id := range order {
		if c := idx[id]; c != nil {
			out = append(out, c)
		} else {
			out = append(out, nil)
		}
	}
	return out
}

// scheduleAll implements Algorithm::Dependency::Ordered->schedule_all: the
// items (sorted as strings) are processed as a queue, items whose
// dependencies are not yet scheduled being moved to the back. A full pass
// without progress is a circular reference (undef in Perl).
func scheduleAll(deps map[int64]map[int64]bool) []int64 {
	queue := make([]int64, 0, len(deps))
	for k := range deps {
		queue = append(queue, k)
	}
	sort.Slice(queue, func(i, j int) bool { return itoa(queue[i]) < itoa(queue[j]) })
	selected := map[int64]bool{}
	var schedule []int64
	var errorMarker int64
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == errorMarker {
			Panic("Can't use an undefined value as an ARRAY reference")
		}
		missing := false
		for d := range deps[id] {
			if !selected[d] {
				missing = true
				break
			}
		}
		if missing {
			if errorMarker == 0 {
				errorMarker = id
			}
			queue = append(queue, id)
			continue
		}
		schedule = append(schedule, id)
		selected[id] = true
		errorMarker = 0
	}
	return schedule
}

// Position sets column positions in the given order.
func (l *Layout) Position(ids []int64) {
	for i, id := range ids {
		if id == 0 {
			Fail("No ID supplied for position update")
		}
		if _, ok := l.e.DB.QueryInt64(`SELECT "id" FROM "layout" WHERE "id" = ?`, id); !ok {
			Fail("Column ID %d not found", id)
		}
		l.e.DB.Exec(`UPDATE "layout" SET "position" = ? WHERE "id" = ?`, i+1, id)
	}
}

// ---------------------------------------------------------------------
// Permissions
// ---------------------------------------------------------------------

func (l *Layout) getUserPermissions(userID int64) map[int64]map[string]bool {
	out := map[int64]map[string]bool{}
	rows := l.e.DB.Query(`SELECT "layout_groups"."layout_id", "layout_groups"."permission" FROM "user_group" "ug" JOIN "group" "g" ON "g"."id" = "ug"."group_id" JOIN "layout_group" "layout_groups" ON "layout_groups"."group_id" = "g"."id" WHERE "ug"."user_id" = ?`, userID)
	for _, r := range rows {
		lid, _ := db.AsInt64(r["layout_id"])
		p, _ := db.AsString(r["permission"])
		if out[lid] == nil {
			out[lid] = map[string]bool{}
		}
		out[lid][p] = true
	}
	return out
}

// UserCanColumn reports whether a user has a permission on a column.
func (l *Layout) UserCanColumn(userID, columnID int64, permission string) bool {
	ul := l.UseLayout()
	if ul.userPermsColumns == nil {
		ul.userPermsColumns = map[int64]map[int64]map[string]bool{}
	}
	cache, ok := ul.userPermsColumns[userID]
	if !ok {
		cache = l.getUserPermissions(userID)
		ul.userPermsColumns[userID] = cache
	}
	return cache[columnID][permission]
}

// CurrentUserCanColumn reports whether the layout user has the permission.
func (l *Layout) CurrentUserCanColumn(columnID int64, permission string) bool {
	u := l.user
	if u == nil {
		return false
	}
	return l.UserCanColumn(u.ID, columnID, permission)
}

func (l *Layout) tablePermissions() map[string]bool {
	if l.userPermsTableOK {
		return l.userPermsTable
	}
	out := map[string]bool{}
	if l.user != nil {
		rows := l.e.DB.Query(`SELECT "ig"."permission" FROM "user_group" "ug" JOIN "instance_group" "ig" ON "ig"."group_id" = "ug"."group_id" WHERE "ug"."user_id" = ? AND "ig"."instance_id" = ?`, l.user.ID, l.InstanceID)
		for _, r := range rows {
			p, _ := db.AsString(r["permission"])
			out[p] = true
		}
	}
	l.userPermsTable = out
	l.userPermsTableOK = true
	return out
}

func (l *Layout) overallPermissions() map[string]bool {
	if l.overall != nil {
		return l.overall
	}
	out := map[string]bool{}
	if l.user == nil {
		l.overall = out
		return out
	}
	ul := l.UseLayout()
	if ul.userPermsColumns == nil {
		ul.userPermsColumns = map[int64]map[int64]map[string]bool{}
	}
	perms, ok := ul.userPermsColumns[l.user.ID]
	if !ok {
		perms = l.getUserPermissions(l.user.ID)
		ul.userPermsColumns[l.user.ID] = perms
	}
	for _, id := range l.AllIDs() {
		for p := range perms[id] {
			out[p] = true
		}
	}
	for p := range l.tablePermissions() {
		out[p] = true
	}
	if l.user.Permission()["superadmin"] {
		out["layout"] = true
		out["view_create"] = true
	}
	l.overall = out
	return out
}

// UserCan reports a table-level permission.
func (l *Layout) UserCan(permission string) bool {
	return l.overallPermissions()[permission]
}

// UserCanAnything reports whether the user has any access.
func (l *Layout) UserCanAnything() bool {
	if l.e.IgnorePermissions {
		return true
	}
	return len(l.overallPermissions()) > 0
}

// GroupHas reports whether a group has a table permission.
func (l *Layout) GroupHas(groupID int64, permission string) bool {
	if l.groupPerms == nil {
		l.groupPerms = map[int64]map[string]bool{}
		for _, r := range l.e.DB.Query(`SELECT "group_id", "permission" FROM "instance_group" WHERE "instance_id" = ?`, l.InstanceID) {
			g, _ := db.AsInt64(r["group_id"])
			p, _ := db.AsString(r["permission"])
			if l.groupPerms[g] == nil {
				l.groupPerms[g] = map[string]bool{}
			}
			l.groupPerms[g][p] = true
		}
	}
	return l.groupPerms[groupID][permission]
}

// ---------------------------------------------------------------------
// Instance management
// ---------------------------------------------------------------------

var internalColumns = []struct {
	name, typ, short string
	unique           bool
}{
	{"ID", "id", "_id", true},
	{"Last edited time", "createddate", "_version_datetime", false},
	{"Last edited by", "createdby", "_version_user", false},
	{"Created by", "createdby", "_created_user", false},
	{"Deleted by", "deletedby", "_deleted_by", false},
	{"Created time", "createddate", "_created", false},
	{"Serial", "serial", "_serial", true},
}

// CreateInternalColumns creates the internal columns for the instance.
func (l *Layout) CreateInternalColumns() {
	for _, ic := range internalColumns {
		if _, ok := l.e.DB.QueryInt64(`SELECT "id" FROM "layout" WHERE "instance_id" = ? AND "name_short" = ? LIMIT 1`, l.InstanceID, ic.short); ok {
			continue
		}
		l.e.DB.InsertMap("layout", map[string]any{
			"name":        ic.name,
			"type":        ic.typ,
			"name_short":  ic.short,
			"isunique":    boolInt(ic.unique),
			"can_child":   0,
			"internal":    1,
			"instance_id": l.InstanceID,
		})
	}
}

// SetGroups sets table permissions as "<group>_<permission>" strings.
func (l *Layout) SetGroups(perms []string) {
	l.setGroups = perms
	l.hasSetGroups = true
}

var reTableShort = regexp.MustCompile(`(?i)^[-_a-z0-9]+$`)
var reGroupPerm = regexp.MustCompile(`^([0-9]+)\_(.*)`)

var validTablePerms = map[string]bool{
	"delete": true, "purge": true, "download": true, "layout": true, "message": true,
	"view_create": true, "view_group": true, "create_child": true, "bulk_update": true,
	"bulk_delete": true, "bulk_import": true, "link": true, "view_limit_extra": true,
}

// Write writes the table properties and any set table permissions.
func (l *Layout) Write(set map[string]any) {
	if l.InstanceID == 0 {
		// A new table: create it (through the site's resultset) with its
		// internal columns
		m := map[string]any{}
		if l.e.SiteID != 0 {
			m["site_id"] = l.e.SiteID
		}
		l.InstanceID = l.e.DB.InsertMap("instance", m)
		l.CreateInternalColumns()
	}
	if ns, ok := set["name_short"]; ok && ns != nil && anyToString(ns) != "" {
		s := anyToString(ns)
		if !reTableShort.MatchString(s) {
			Fail(`Invalid short name for table "%s": short names can only contain alphanumeric characters and the underscore`, s)
		}
		row := l.e.DB.QueryRow(`SELECT "name" FROM "instance" WHERE "name_short" = ? AND "id" != ?`, s, l.InstanceID)
		if row != nil {
			n, _ := db.AsString(row["name"])
			Fail("The short name %s already exists for the table %s", s, n)
		}
	}
	// Result::Instance validate, of the values written
	row := l.e.DB.QueryRow(`SELECT "sort_layout_id", "sort_type" FROM "instance" WHERE "id" = ?`, l.InstanceID)
	value := func(k string) any {
		if v, ok := set[k]; ok {
			return v
		}
		if row != nil {
			return row[k]
		}
		return nil
	}
	if v := value("sort_layout_id"); v != nil && !reDigits.MatchString(anyToString(v)) {
		Fail("Invalid sort_layout_id %s", anyToString(v))
	}
	if v := value("sort_type"); v != nil && anyToString(v) != "asc" && anyToString(v) != "desc" {
		Fail("Invalid sort type %s", anyToString(v))
	}
	if len(set) > 0 {
		l.e.DB.UpdateMap("instance", set, map[string]any{"id": l.InstanceID})
	}
	if l.hasSetGroups {
		existing := map[int64]map[string]bool{}
		for _, r := range l.e.DB.Query(`SELECT "group_id", "permission" FROM "instance_group" WHERE "instance_id" = ?`, l.InstanceID) {
			g, _ := db.AsInt64(r["group_id"])
			p, _ := db.AsString(r["permission"])
			if existing[g] == nil {
				existing[g] = map[string]bool{}
			}
			existing[g][p] = true
		}
		type gp struct {
			g int64
			p string
		}
		var create []gp
		for _, perm := range l.setGroups {
			m := reGroupPerm.FindStringSubmatch(perm)
			if m == nil {
				Panic("Invalid permission %s", perm)
			}
			gid, _ := anyToInt64(m[1])
			if gid == 0 || !validTablePerms[m[2]] {
				Panic("Invalid permission %s", perm)
			}
			if existing[gid][m[2]] {
				delete(existing[gid], m[2])
			} else {
				create = append(create, gp{gid, m[2]})
			}
		}
		var pop []map[string]any
		for _, c := range create {
			pop = append(pop, map[string]any{"instance_id": l.InstanceID, "group_id": c.g, "permission": c.p})
		}
		l.e.DB.Populate("instance_group", pop)
		for g, ps := range existing {
			for p := range ps {
				l.e.DB.Exec(`DELETE FROM "instance_group" WHERE "instance_id" = ? AND "group_id" = ? AND "permission" = ?`, l.InstanceID, g, p)
			}
		}
		l.hasSetGroups = false
	}
	l.Clear()
}

// ReferredBy returns the IDs of curval columns referring to this table.
func (l *Layout) ReferredBy() []*Column {
	if !l.referredByLoaded {
		l.referredBy = l.e.DB.QueryInt64s(`SELECT DISTINCT "me"."id" FROM "layout" "me" JOIN "curval_fields" "cf" ON "cf"."parent_id" = "me"."id" JOIN "layout" "child" ON "child"."id" = "cf"."child_id" WHERE "child"."instance_id" = ? ORDER BY "me"."id"`, l.InstanceID)
		l.referredByLoaded = true
	}
	var out []*Column
	for _, id := range l.referredBy {
		if c := l.Column(id); c != nil {
			out = append(out, c)
		}
	}
	return out
}

// CachedRecords is a per-request record cache used by curval fields.
func (l *Layout) CachedRecords() map[int64]*Record {
	if l.cachedRecords == nil {
		l.cachedRecords = map[int64]*Record{}
	}
	return l.cachedRecords
}

// CachedRecordsAutocur is the record cache used by autocur fields.
func (l *Layout) CachedRecordsAutocur() map[int64]*Record {
	if l.cachedRecordsAutocur == nil {
		l.cachedRecordsAutocur = map[int64]*Record{}
	}
	return l.cachedRecordsAutocur
}

// HasChildren reports whether any column can have child values.
func (l *Layout) HasChildren() bool {
	for _, c := range l.All(AllOptions{}) {
		if c.CanChild() {
			return true
		}
	}
	return false
}

// AllPeopleNotify returns person columns with notifications.
func (l *Layout) AllPeopleNotify() []*Column {
	var out []*Column
	for _, c := range l.All(AllOptions{}) {
		if c.Type == "person" && c.NotifyOnSelection() {
			out = append(out, c)
		}
	}
	return out
}

// ColIDsForCacheUpdate returns all columns needed to update calc fields.
func (l *Layout) ColIDsForCacheUpdate() []int64 {
	cols := map[int64]bool{}
	for _, id := range l.e.DB.QueryInt64s(`SELECT "me"."layout_id" FROM "layout_depend" "me" JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE "instance_id" = ? GROUP BY "me"."layout_id"`, l.InstanceID) {
		cols[id] = true
	}
	for _, id := range l.e.DB.QueryInt64s(`SELECT "me"."depends_on" FROM "layout_depend" "me" JOIN "layout" "depend_on" ON "depend_on"."id" = "me"."depends_on" WHERE "instance_id" = ? GROUP BY "me"."depends_on"`, l.InstanceID) {
		cols[id] = true
	}
	for _, c := range l.All(AllOptions{HasCache: true}) {
		cols[c.ID] = true
	}
	var out []int64
	for k := range cols {
		out = append(out, k)
	}
	sortInt64s(out)
	return out
}

// Delete deletes the table (GADS::Layout::delete).
func (l *Layout) Delete() {
	if _, ok := l.e.DB.QueryInt64(`SELECT "id" FROM "current" WHERE "instance_id" = ? LIMIT 1`, l.InstanceID); ok {
		Fail("This table cannot be deleted as it contains records")
	}
	l.e.DB.Txn(func() {
		for _, c := range l.All(AllOptions{OnlyInternal: true, IncludeHidden: true}) {
			c.Delete()
		}
		l.e.DB.Exec(`UPDATE "layout" SET "topic_id" = NULL WHERE "topic_id" IN (SELECT "id" FROM "topic" WHERE "instance_id" = ?)`, l.InstanceID)
		l.e.DB.Exec(`DELETE FROM "topic" WHERE "instance_id" = ?`, l.InstanceID)
		l.e.DB.Exec(`DELETE FROM "instance_group" WHERE "instance_id" = ?`, l.InstanceID)
		l.e.DB.Exec(`DELETE FROM "widget" WHERE "dashboard_id" IN (SELECT "id" FROM "dashboard" WHERE "instance_id" = ?)`, l.InstanceID)
		l.e.DB.Exec(`DELETE FROM "dashboard" WHERE "instance_id" = ?`, l.InstanceID)
		l.e.DB.Exec(`DELETE FROM "instance_rag" WHERE "instance_id" = ?`, l.InstanceID)
		l.e.DB.Exec(`UPDATE "audit" SET "instance_id" = NULL WHERE "instance_id" = ?`, l.InstanceID)
		for _, vid := range l.e.DB.QueryInt64s(`SELECT "id" FROM "view" WHERE "instance_id" = ?`, l.InstanceID) {
			l.e.DB.Exec(`DELETE FROM "view_limit" WHERE "view_id" = ?`, vid)
			v := l.e.LoadView(vid, l)
			v.Delete()
		}
		// Result::Instance delete
		if n, _ := l.e.DB.QueryInt64(`SELECT COUNT(*) FROM "layout" WHERE "instance_id" = ?`, l.InstanceID); n > 0 {
			Fail("All fields must be deleted from this table before it can be deleted")
		}
		l.e.DB.Exec(`DELETE FROM "instance" WHERE "id" = ?`, l.InstanceID)
	})
}

// Purge deletes the table together with all its graphs, metrics, views,
// columns and records (GADS::Layout::purge).
func (l *Layout) Purge() {
	e := l.e
	for _, gid := range e.DB.QueryInt64s(`SELECT "id" FROM "graph" WHERE "instance_id" = ?`, l.InstanceID) {
		e.LoadGraph(gid, l, l.user).Delete()
	}
	for _, mg := range e.DB.QueryInt64s(`SELECT "id" FROM "metric_group" WHERE "instance_id" = ?`, l.InstanceID) {
		if rows := e.DB.Query(`SELECT "title" FROM "graph" WHERE "metric_group" = ?`, mg); len(rows) > 0 {
			var names []string
			for _, r := range rows {
				s, _ := db.AsString(r["title"])
				names = append(names, s)
			}
			Fail("The metric is being used in the following graphs: %s. Please remove it first.", strings.Join(names, ", "))
		}
		e.DB.Exec(`DELETE FROM "metric" WHERE "metric_group" = ?`, mg)
		e.DB.Exec(`DELETE FROM "metric_group" WHERE "id" = ?`, mg)
	}
	for _, vid := range e.DB.QueryInt64s(`SELECT "id" FROM "view" WHERE "instance_id" = ?`, l.InstanceID) {
		e.DB.Exec(`DELETE FROM "view_limit" WHERE "view_id" = ?`, vid)
		e.LoadView(vid, l).Delete()
	}
	cols := l.All(AllOptions{OrderDependencies: true, IncludeHidden: true})
	for i := len(cols) - 1; i >= 0; i-- {
		cols[i].Delete()
	}
	e.DB.Exec(`DELETE FROM "user_lastrecord"`)
	e.DB.Exec(`UPDATE "current" SET "current_version_id" = NULL WHERE "instance_id" = ?`, l.InstanceID)
	e.DB.Exec(`DELETE FROM "record" WHERE "id" IN (SELECT "me"."id" FROM "record" "me" JOIN "current" "current" ON "current"."id" = "me"."current_id" WHERE "current"."instance_id" = ?)`, l.InstanceID)
	e.DB.Exec(`DELETE FROM "current" WHERE "instance_id" = ?`, l.InstanceID)
	e.DB.Exec(`DELETE FROM "instance_group" WHERE "instance_id" = ?`, l.InstanceID)
	e.DB.Exec(`DELETE FROM "widget" WHERE "dashboard_id" IN (SELECT "id" FROM "dashboard" WHERE "instance_id" = ?)`, l.InstanceID)
	e.DB.Exec(`DELETE FROM "audit" WHERE "instance_id" = ?`, l.InstanceID)
	e.DB.Exec(`DELETE FROM "dashboard" WHERE "instance_id" = ?`, l.InstanceID)
	e.DB.Exec(`DELETE FROM "instance" WHERE "id" = ?`, l.InstanceID)
}

// allRags are the RAG grades in their fixed order
var allRags = []string{"danger", "attention", "warning", "advisory", "success", "undefined", "unexpected", "complete"}

// Rags is Layout->rags: the instance_rag rows of the table, creating any
// that are missing (attention and complete are disabled by default)
func (l *Layout) Rags() []db.Row {
	have := map[string]bool{}
	for _, r := range l.e.DB.Query(`SELECT "rag" FROM "instance_rag" WHERE "instance_id" = ?`, l.InstanceID) {
		s, _ := db.AsString(r["rag"])
		have[s] = true
	}
	for _, rag := range allRags {
		if have[rag] {
			continue
		}
		enabled := 1
		if rag == "attention" || rag == "complete" {
			enabled = 0
		}
		l.e.DB.Exec(`INSERT INTO "instance_rag" ("instance_id", "rag", "enabled", "description") VALUES (?, ?, ?, ?)`, l.InstanceID, rag, enabled, rag)
	}
	return l.e.DB.Query(`SELECT * FROM "instance_rag" WHERE "instance_id" = ?`, l.InstanceID)
}

// EnabledRags is Layout->enabled_rags: the enabled RAG rows in the fixed
// order of the grades (a blank description is the name of the grade)
func (l *Layout) EnabledRags() []db.Row {
	index := map[string]db.Row{}
	for _, r := range l.Rags() {
		s, _ := db.AsString(r["rag"])
		if d, _ := db.AsString(r["description"]); d == "" {
			r["description"] = s
		}
		index[s] = r
	}
	var out []db.Row
	for _, rag := range allRags {
		if r := index[rag]; r != nil && truthy(r["enabled"]) {
			out = append(out, r)
		}
	}
	return out
}

// WriteRags is Layout->write_rags with set_rags: the enabled flag and
// description of each RAG grade, from the "<rag>_selected" and
// "<rag>_description" parameters
func (l *Layout) WriteRags(params map[string]any) {
	l.Rags()
	for _, rag := range allRags {
		enabled := 0
		if truthy(params[rag+"_selected"]) {
			enabled = 1
		}
		l.e.DB.Exec(`UPDATE "instance_rag" SET "enabled" = ?, "description" = ? WHERE "instance_id" = ? AND "rag" = ?`,
			enabled, params[rag+"_description"], l.InstanceID, rag)
	}
}

// SetColumnPositions is Layout->position: the columns given in order
func (e *Engine) SetColumnPositions(ids []string) {
	for i, id := range ids {
		if id == "" || id == "0" {
			Fail("No ID supplied for position update")
		}
		if row := e.DB.QueryRow(`SELECT "id" FROM "layout" WHERE "id" = ?`, id); row == nil {
			Fail("Column ID %s not found", id)
		}
		e.DB.UpdateMap("layout", map[string]any{"position": int64(i + 1)}, map[string]any{"id": id})
	}
}

// ClearCachedRecords is Layout->clear_cached_records
func (l *Layout) ClearCachedRecords() {
	l.cachedRecords = nil
	l.cachedRecordsAutocur = nil
}
