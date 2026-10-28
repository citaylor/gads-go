package gads

import (
	"regexp"
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// NewColumn creates a new column object for writing.
func (l *Layout) NewColumn(typ string) *Column {
	valid := false
	for _, t := range ColumnTypes {
		if t == typ {
			valid = true
		}
	}
	if !valid {
		Fail("Invalid field type %s", typ)
	}
	c := &Column{e: l.e, layout: l, Type: typ, Optional: true, writeCache: true, Options: map[string]any{}, InstanceID: l.InstanceID}
	c.codeLoaded = true
	c.enumvalsLoaded = true
	c.curvalFieldsLoaded = true
	c.dependsLoaded = true
	return c
}

// SetPermissions sets the group permissions to write.
func (c *Column) SetPermissions(p map[int64][]string) {
	c.setPermissions = p
	c.hasSetPerms = true
}

// SetOption sets a column option.
func (c *Column) SetOption(name string, v any) {
	c.setOpt(name, v)
	// The trigger of Curval show_add: a curval with added values and no
	// value selector is multivalue
	if name == "show_add" && c.Type == "curval" && truthy(v) && anyToString(c.opt("value_selector")) == "noshow" {
		c.Multivalue = true
	}
}

// ColumnWriteOptions are options to Column.Write.
type ColumnWriteOptions struct {
	Force                bool
	AllowInternalWrite   bool
	ReportOnly           bool
	NoCacheUpdate        *bool
	UpdateDependents     *bool
	Override             bool
	IncludeApprovalPerms bool
	NoDBAdd              bool
	NoAlerts             bool
}

var reShortName = regexp.MustCompile(`(?i)^[a-z][_0-9a-z]*$`)
var reEnumValid = regexp.MustCompile(`^[ \S]+$`)

// Write writes the column (GADS::Column::write).
func (c *Column) Write(o ColumnWriteOptions) {
	if !c.layout.UserCan("layout") && !c.e.IgnorePermissions && !o.Force {
		Fail("You do not have permission to manage fields")
	}
	c.layout.ClearPermissions()
	if c.Internal && !o.AllowInternalWrite {
		Fail("Internal fields cannot be edited")
	}
	var newID int64
	var specialOpts map[string]bool
	c.e.DB.Txn(func() {
		item := map[string]any{}
		if c.Name == "" {
			Fail("Please enter a name for item")
		}
		item["name"] = c.Name
		if c.Type == "" {
			Fail("Please select a type for the item")
		}
		item["type"] = c.Type
		if c.NameShort != "" {
			item["name_short"] = c.NameShort
		} else {
			item["name_short"] = nil
		}
		if c.NameShort != "" && !c.Internal {
			if !reShortName.MatchString(c.NameShort) {
				Fail("Short names must begin with a letter and can only contain letters, numbers and underscores")
			}
			args := []any{c.NameShort}
			sql := `SELECT "me"."name" FROM "layout" "me" JOIN "instance" "instance" ON "instance"."id" = "me"."instance_id" WHERE "me"."name_short" = ?`
			if c.e.SiteID != 0 {
				sql += ` AND "instance"."site_id" = ?`
				args = append(args, c.e.SiteID)
			} else {
				sql += ` AND "instance"."site_id" IS NULL`
			}
			if c.ID != 0 {
				sql += ` AND "me"."id" != ?`
				args = append(args, c.ID)
			}
			if row := c.e.DB.QueryRow(sql+" LIMIT 1", args...); row != nil {
				n, _ := db.AsString(row["name"])
				Fail(`Short name %s must be unique but already exists for field "%s"`, c.NameShort, n)
			}
		}
		if c.LinkParentID != nil && *c.LinkParentID != 0 {
			lpRow := c.e.DB.QueryRow(`SELECT * FROM "layout" WHERE "id" = ?`, *c.LinkParentID)
			lpi, _ := db.AsInt64(lpRow["instance_id"])
			if lpi == c.InstanceID {
				Fail("Cannot link to field within the same table")
			}
			if t, _ := db.AsString(lpRow["type"]); t == "curval" {
				for _, ci := range c.e.DB.QueryInt64s(`SELECT "child"."instance_id" FROM "curval_fields" "me" JOIN "layout" "child" ON "child"."id" = "me"."child_id" WHERE "me"."parent_id" = ?`, *c.LinkParentID) {
					if ci == c.InstanceID {
						n, _ := db.AsString(lpRow["name"])
						Fail(`Cannot link to column "%s" which contains columns from this table`, n)
					}
				}
			}
		}
		item["topic_id"] = derefInt(c.TopicID)
		item["optional"] = boolInt(c.Optional)
		item["remember"] = boolInt(c.Remember)
		item["isunique"] = boolInt(c.IsUnique)
		if c.setCanChild != nil {
			item["can_child"] = boolInt(*c.setCanChild)
		}
		item["filter"] = c.FilterFn().AsJSON()
		if c.CanMultivalue() {
			item["multivalue"] = boolInt(c.MultivalueFn())
		}
		item["description"] = nullStrVal(c.Description)
		width := c.Width
		if width == 0 {
			width = 50
		}
		item["width"] = width
		item["helptext"] = nullStrVal(c.Helptext)
		item["options"] = EncodeJSON(c.optionsForWrite())
		item["link_parent"] = derefInt(c.LinkParentID)
		if cond, ok := c.DisplayFields().AsHash()["condition"]; ok {
			item["display_condition"] = cond
		} else {
			item["display_condition"] = nil
		}
		item["instance_id"] = c.layout.InstanceID
		item["aggregate"] = nullStrVal(c.Aggregate)
		item["notes"] = nullStrVal(c.Notes)
		if c.Aggregate != "" && c.Aggregate != "sum" && c.Aggregate != "recalc" {
			Fail("Invalid aggregate value %s", c.Aggregate)
		}
		if c.Aggregate == "recalc" {
			for _, dep := range c.DependsOn() {
				if dc := c.layout.Column(dep); dc != nil && dc.Aggregate == "" {
					Fail("In order to recalculate this field from other aggregate values, all columns in its calculation need to have a defined aggregate. Currently column %s does not have an aggregate defined.", dc.Name)
				}
			}
		} else if c.Aggregate == "" && c.ID != 0 {
			var names []string
			for _, r := range c.e.DB.Query(`SELECT "layout"."name", "layout"."aggregate" FROM "layout_depend" "me" JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE "me"."depends_on" = ?`, c.ID) {
				if a, _ := db.AsString(r["aggregate"]); a == "recalc" {
					n, _ := db.AsString(r["name"])
					names = append(names, n)
				}
			}
			if len(names) > 0 {
				Fail("The aggregate on this column cannot be removed, as the following columns have an aggregate type that recalculates based on the aggregate value of this column: %s", strings.Join(names, ", "))
			}
		}
		if c.Numeric() {
			item["group_display"] = "sum"
		} else if c.GroupDisplay() == "unique" {
			item["group_display"] = "unique"
		} else {
			item["group_display"] = nil
		}
		if c.Position != nil && *c.Position != 0 {
			item["position"] = *c.Position
		}
		if !o.ReportOnly {
			if c.ID == 0 {
				if c.setID != 0 {
					item["id"] = c.setID
				}
				if _, ok := item["position"]; !ok {
					max, _ := c.e.DB.QueryInt64(`SELECT MAX( "position" ) FROM "layout" "me"`)
					item["position"] = max + 1
				}
				newID = c.e.DB.InsertMap("layout", item)
			} else {
				iid, ok := c.e.DB.QueryInt64(`SELECT "instance_id" FROM "layout" WHERE "id" = ?`, c.ID)
				if ok {
					if iid != c.layout.InstanceID {
						Panic("Attempt to move column between instances")
					}
					c.oldMultivalue = rowBoolQ(c.e, `SELECT "multivalue" FROM "layout" WHERE "id" = ?`, c.ID)
					c.e.DB.UpdateMap("layout", item, map[string]any{"id": c.ID})
				} else {
					item["id"] = c.ID
					c.e.DB.InsertMap("layout", item)
				}
			}
			id := newID
			if id == 0 {
				id = c.ID
			}
			specialOpts = c.writeSpecial(id, newID != 0, o)
		}
		id := newID
		if id == 0 {
			id = c.ID
		}
		c.writePermissions(id, o)
		if c.ID != 0 {
			c.e.DB.Exec(`DELETE FROM "display_field" WHERE "layout_id" = ?`, c.ID)
		}
		for _, cond := range c.DisplayFields().Filters() {
			c.e.DB.InsertMap("display_field", map[string]any{
				"layout_id":        id,
				"display_field_id": cond["column_id"],
				"regex":            cond["value"],
				"operator":         cond["operator"],
			})
		}
	})
	if o.ReportOnly {
		return
	}
	if newID != 0 {
		c.ID = newID
		if !o.NoDBAdd {
			c.layout.Clear()
		}
	}
	c.afterWriteSpecial(specialOpts)
	c.layout.ClearIndexes()
}

func rowBoolQ(e *Engine, sql string, args ...any) bool {
	v, _ := e.DB.QueryInt64(sql, args...)
	return v != 0
}

func nullStrVal(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// writeSpecial writes the column-type specific data.
func (c *Column) writeSpecial(id int64, isNew bool, o ColumnWriteOptions) map[string]bool {
	out := map[string]bool{}
	switch c.Type {
	case "string":
		c.e.DB.Exec(`UPDATE "layout" SET "force_regex" = ?, "textbox" = ? WHERE "id" = ?`, nullStrVal(c.ForceRegex), boolInt(c.Textbox), id)
	case "enum":
		c.ID = id
		c.writeEnumvals()
		c.e.DB.Exec(`UPDATE "layout" SET "ordering" = ? WHERE "id" = ?`, nullStrVal(c.Ordering), id)
		if isNew {
			c.ID = 0
		}
	case "tree":
		c.e.DB.Exec(`UPDATE "layout" SET "end_node_only" = ? WHERE "id" = ?`, boolInt(c.EndNodeOnly), id)
	case "file":
		if _, ok := c.e.DB.QueryInt64(`SELECT "id" FROM "file_option" WHERE "layout_id" = ?`, id); ok {
			c.e.DB.Exec(`UPDATE "file_option" SET "filesize" = ? WHERE "layout_id" = ?`, derefInt(c.Filesize), id)
		} else {
			c.e.DB.InsertMap("file_option", map[string]any{"filesize": derefInt(c.Filesize), "layout_id": id})
		}
	case "curval":
		if !o.Override {
			if c.LayoutParent() == nil {
				Fail("Please select a table to link to")
			}
			c.updateCurvals(id)
		}
		c.e.DB.Exec(`UPDATE "layout" SET "typeahead" = 0 WHERE "id" = ?`, id)
		c.ClearCurval()
	case "autocur", "filval":
		if o.Override {
			return out
		}
		if c.RelatedFieldID == nil || *c.RelatedFieldID == 0 {
			if c.Type == "autocur" {
				Fail("Please select a field that refers to this table")
			}
			Fail("Please select a field to store the filtered values from")
		}
		c.e.DB.Exec(`UPDATE "layout" SET "related_field" = ? WHERE "id" = ?`, *c.RelatedFieldID, id)
		c.updateCurvals(id)
		c.ClearCurval()
	case "calc", "rag":
		newCode := id == 0 || !c.codeInStorageFor(id)
		if m := reExtended.FindString(c.Code); m != "" {
			Fail("Extended characters are not supported in calculated fields (found here: %s)", m)
		}
		changed := c.writeCode(id)
		update := changed
		if o.UpdateDependents != nil {
			update = *o.UpdateDependents
		}
		if update {
			if newCode {
				out["no_alerts"] = true
			}
			deps := map[int64]bool{}
			var depOrder []int64
			saved := c.ID
			c.ID = id
			for _, pc := range c.ParamColumns(!o.Override) {
				if pc.Internal {
					continue
				}
				if !deps[pc.ID] {
					deps[pc.ID] = true
					depOrder = append(depOrder, pc.ID)
				}
			}
			if isNew {
				c.ID = saved
			}
			c.e.DB.Exec(`DELETE FROM "layout_depend" WHERE "layout_id" = ?`, id)
			for _, d := range depOrder {
				c.e.DB.InsertMap("layout_depend", map[string]any{"layout_id": id, "depends_on": d})
			}
			c.ClearDependsOn()
		} else if o.NoCacheUpdate == nil {
			out["no_cache_update"] = true
		}
		if o.NoCacheUpdate != nil && *o.NoCacheUpdate {
			out["no_cache_update"] = true
		}
	}
	return out
}

var reExtended = regexp.MustCompile(`.....[^\x00-\x7F]+.....`)

func (c *Column) codeInStorageFor(id int64) bool {
	table := "calc"
	if c.Type == "rag" {
		table = "rag"
	}
	row := c.e.DB.QueryRow(`SELECT "code" FROM `+q(table)+` WHERE "layout_id" = ?`, id)
	if row == nil {
		return false
	}
	s, _ := db.AsString(row["code"])
	return s != ""
}

func (c *Column) writeCode(id int64) bool {
	// The code is lazily loaded: make sure an update of other attributes
	// does not overwrite it
	c.loadCode()
	table := "calc"
	if c.Type == "rag" {
		table = "rag"
	}
	row := c.e.DB.QueryRow(`SELECT * FROM `+q(table)+` WHERE "layout_id" = ? ORDER BY "id" LIMIT 1`, id)
	need := row == nil
	if row != nil {
		code, _ := db.AsString(row["code"])
		if code != c.Code {
			need = true
		}
		if c.Type == "calc" {
			rf, _ := db.AsString(row["return_format"])
			if rf != c.ReturnTypeFn() {
				need = true
			}
			if c.oldMultivalue != c.MultivalueFn() {
				need = true
			}
		}
	}
	if c.Type == "calc" {
		vals := map[string]any{"layout_id": id, "code": c.Code, "return_format": c.ReturnTypeFn(), "decimal_places": derefInt(c.DecimalPlaces)}
		if row != nil {
			rid, _ := db.AsInt64(row["id"])
			c.e.DB.UpdateMap("calc", vals, map[string]any{"id": rid})
		} else {
			c.e.DB.InsertMap("calc", vals)
		}
	} else {
		vals := map[string]any{"layout_id": id, "code": c.Code}
		if row != nil {
			rid, _ := db.AsInt64(row["id"])
			c.e.DB.UpdateMap("rag", vals, map[string]any{"id": rid})
		} else {
			c.e.DB.InsertMap("rag", vals)
		}
	}
	c.codeInStorage = true
	return need
}

func (c *Column) afterWriteSpecial(opts map[string]bool) {
	if c.Type == "calc" || c.Type == "rag" {
		if !opts["no_cache_update"] {
			// Perl passes no_alerts, but update_cached only checks
			// no_alert_send, so alerts are always sent
			c.UpdateCached(false)
		}
	}
}

// updateCurvals writes curval_fields (Curcommon::_update_curvals).
func (c *Column) updateCurvals(id int64) {
	if len(c.CurvalFieldIDs()) == 0 && !c.e.AllowBlankCurval {
		Fail("Please select some fields to use from the other table")
	}
	lp := c.LayoutParent()
	var keep []int64
	for _, f := range c.CurvalFieldIDs() {
		ff := lp.Column(f)
		if ff == nil || ff.InstanceID != lp.InstanceID {
			continue
		}
		if ff.Type == "curval" {
			continue
		}
		if n, _ := c.e.DB.QueryInt64(`SELECT COUNT(*) FROM "curval_fields" WHERE "parent_id" = ? AND "child_id" = ?`, id, f); n == 0 {
			c.e.DB.Insert("curval_fields", []string{"child_id", "parent_id"}, f, id)
		}
		keep = append(keep, f)
	}
	if len(keep) > 0 {
		args := []any{}
		for _, k := range keep {
			args = append(args, k)
		}
		args = append(args, id)
		c.e.DB.Exec(`DELETE FROM "curval_fields" WHERE ( "child_id" NOT IN (`+db.Placeholders(len(keep))+`) AND "parent_id" = ? )`, args...)
	} else {
		c.e.DB.Exec(`DELETE FROM "curval_fields" WHERE "parent_id" = ?`, id)
	}
}

func (c *Column) writePermissions(id int64, o ColumnWriteOptions) {
	if !c.hasSetPerms {
		return
	}
	perms := map[int64][]string{}
	for g, p := range c.setPermissions {
		perms[g] = append([]string(nil), p...)
	}
	var groups []int64
	for g := range perms {
		groups = append(groups, g)
	}
	sql := `SELECT MAX( "group_id" ) AS "group_id" FROM "layout_group" "me" WHERE "layout_id" = ?`
	args := []any{id}
	if len(groups) > 0 {
		var conds []string
		for _, g := range groups {
			conds = append(conds, `"group_id" != ?`)
			args = append(args, g)
		}
		sql += " AND ( " + strings.Join(conds, " AND ") + " )"
	}
	sql += ` GROUP BY "group_id"`
	for _, g := range c.e.DB.QueryInt64s(sql, args...) {
		perms[g] = []string{}
	}
	groups = nil
	for g := range perms {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i] < groups[j] })
	for _, g := range groups {
		newPerms := append([]string(nil), perms[g]...)
		if !o.IncludeApprovalPerms {
			if containsStr(newPerms, "write_existing") {
				newPerms = append(newPerms, "write_existing_no_approval")
			}
			if containsStr(newPerms, "write_new") {
				newPerms = append(newPerms, "write_new_no_approval")
			}
		}
		var existing []string
		for _, r := range c.e.DB.Query(`SELECT "permission" FROM "layout_group" WHERE "layout_id" = ? AND "group_id" = ? ORDER BY "id"`, id, g) {
			s, _ := db.AsString(r["permission"])
			existing = append(existing, s)
		}
		// List::Compare: complement = in existing not new; unique = in new not existing
		var removed, added []string
		for _, e := range uniqueSorted(existing) {
			if !containsStr(newPerms, e) {
				removed = append(removed, e)
			}
		}
		for _, n := range uniqueSorted(newPerms) {
			if !containsStr(existing, n) {
				added = append(added, n)
			}
		}
		readRemoved := containsStr(removed, "read")
		if o.ReportOnly && len(removed) > 0 {
			c.e.Notice("Removing the following permissions from %s for group ID %d: %s", c.Name, g, strings.Join(removed, ", "))
		} else if len(removed) > 0 {
			a := []any{id, g}
			var conds []string
			for _, p := range removed {
				conds = append(conds, `"permission" = ?`)
				a = append(a, p)
			}
			c.e.DB.Exec(`DELETE FROM "layout_group" WHERE "layout_id" = ? AND "group_id" = ? AND ( `+strings.Join(conds, " OR ")+` )`, a...)
		}
		if o.ReportOnly && len(added) > 0 {
			c.e.Notice("Adding the following permissions to %s for group ID %d: %s", c.Name, g, strings.Join(added, ", "))
		} else {
			for _, p := range added {
				c.e.DB.Insert("layout_group", []string{"group_id", "layout_id", "permission"}, g, id, p)
			}
		}
		if readRemoved && !o.ReportOnly {
			c.layout.ClearPermissions()
			for _, r := range c.e.DB.Query(`SELECT "me"."id", "view"."user_id" FROM "sort" "me" JOIN "view" "view" ON "view"."id" = "me"."view_id" WHERE "me"."layout_id" = ? AND "view"."user_id" IS NOT NULL`, id) {
				uid, _ := db.AsInt64(r["user_id"])
				if !c.layout.UserCanColumn(uid, id, "read") {
					sid, _ := db.AsInt64(r["id"])
					c.e.DB.Exec(`DELETE FROM "sort" WHERE "id" = ?`, sid)
				}
			}
			for _, r := range c.e.DB.Query(`SELECT "me"."id", "me"."view_id", "view"."user_id", "view"."filter" FROM "filter" "me" JOIN "view" "view" ON "view"."id" = "me"."view_id" WHERE "me"."layout_id" = ? AND "view"."user_id" IS NOT NULL`, id) {
				uid, _ := db.AsInt64(r["user_id"])
				if c.layout.UserCanColumn(uid, id, "read") {
					continue
				}
				fid, _ := db.AsInt64(r["id"])
				vid, _ := db.AsInt64(r["view_id"])
				c.e.DB.Exec(`DELETE FROM "filter" WHERE "id" = ?`, fid)
				c.e.DB.Exec(`DELETE FROM "alert_cache" WHERE "layout_id" = ? AND "view_id" = ?`, id, vid)
				c.e.DB.Exec(`DELETE FROM "view_layout" WHERE "layout_id" = ? AND "view_id" = ?`, id, vid)
				js, _ := db.AsString(r["filter"])
				c.e.DB.Exec(`UPDATE "view" SET "filter" = ? WHERE "id" = ?`, filterRemoveColID(js, id), vid)
			}
		}
	}
	c.permissions = nil
}

func uniqueSorted(in []string) []string {
	m := map[string]bool{}
	for _, s := range in {
		m[s] = true
	}
	var out []string
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// filterRemoveColID removes rules for a column from a filter JSON.
func filterRemoveColID(js string, colID int64) string {
	v, err := DecodeJSON(js)
	if err != nil {
		return js
	}
	m, _ := v.(map[string]any)
	if m == nil {
		return "{}"
	}
	filterRemoveDecoded(m, colID)
	if rules, ok := m["rules"].([]any); !ok || len(rules) == 0 {
		m = map[string]any{}
	}
	return EncodeJSON(m)
}

func filterRemoveDecoded(f map[string]any, colID int64) bool {
	if rules, ok := f["rules"].([]any); ok {
		var keep []any
		for _, r := range rules {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if filterRemoveDecoded(rm, colID) {
				if sub, ok := rm["rules"].([]any); ok && len(sub) == 0 {
					continue
				}
				keep = append(keep, rm)
			}
		}
		if keep == nil {
			keep = []any{}
		}
		f["rules"] = keep
	}
	if id, ok := anyToInt64(f["id"]); ok && truthy(f["id"]) && id == colID {
		return false
	}
	return true
}

// Delete deletes the column (GADS::Column::delete).
func (c *Column) Delete() {
	e := c.e
	e.DB.Txn(func() {
		if rows := e.DB.Query(`SELECT "layout"."name" FROM "display_field" "me" JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE "me"."display_field_id" = ?`, c.ID); len(rows) > 0 {
			var names []string
			for _, r := range rows {
				s, _ := db.AsString(r["name"])
				names = append(names, s)
			}
			Fail("The following fields are conditional on this field: %s.\n            Please remove these conditions before deletion.", strings.Join(names, ", "))
		}
		if dep := c.DependedBy(); len(dep) > 0 {
			var names []string
			for _, d := range dep {
				if dc := c.layout.Column(d); dc != nil {
					names = append(names, dc.Name)
				}
			}
			Fail("The following fields contain this field in their formula: %s.\n            Please remove these before deletion.", strings.Join(names, ", "))
		}
		if rows := e.DB.Query(`SELECT "me"."name", "instance"."name" AS "iname" FROM "layout" "me" JOIN "instance" "instance" ON "instance"."id" = "me"."instance_id" WHERE "me"."related_field" = ?`, c.ID); len(rows) > 0 {
			var names []string
			for _, r := range rows {
				n, _ := db.AsString(r["name"])
				i, _ := db.AsString(r["iname"])
				names = append(names, n+" ("+i+")")
			}
			Fail("The following fields in another table refer to this field: %s.\n            Please remove these references before deletion of this field.", strings.Join(names, ", "))
		}
		if rows := e.DB.Query(`SELECT "parent"."name", "instance"."name" AS "iname" FROM "curval_fields" "me" JOIN "layout" "parent" ON "parent"."id" = "me"."parent_id" JOIN "instance" "instance" ON "instance"."id" = "parent"."instance_id" WHERE "me"."child_id" = ?`, c.ID); len(rows) > 0 {
			var names []string
			for _, r := range rows {
				n, _ := db.AsString(r["name"])
				i, _ := db.AsString(r["iname"])
				names = append(names, n+" ("+i+")")
			}
			Fail("The following fields in another table refer to this field: %s.\n            Please remove these references before deletion of this field.", strings.Join(names, ", "))
		}
		if rows := e.DB.Query(`SELECT "me"."name", "instance"."name" AS "iname" FROM "layout" "me" JOIN "instance" "instance" ON "instance"."id" = "me"."instance_id" WHERE "me"."link_parent" = ?`, c.ID); len(rows) > 0 {
			var names []string
			for _, r := range rows {
				n, _ := db.AsString(r["name"])
				i, _ := db.AsString(r["iname"])
				names = append(names, n+" ("+i+")")
			}
			Fail("The following fields in another table are linked to this field: %s.\n            Please remove these links before deletion of this field.", strings.Join(names, ", "))
		}
		if rows := e.DB.Query(`SELECT "title" FROM "graph" WHERE "x_axis" = ? OR "y_axis" = ? OR "group_by" = ?`, c.ID, c.ID, c.ID); len(rows) > 0 {
			var names []string
			for _, r := range rows {
				s, _ := db.AsString(r["title"])
				names = append(names, s)
			}
			Fail("The following graphs references this field: %s. Please update them before deletion.", strings.Join(names, ", "))
		}
		if rows := e.DB.Query(`SELECT DISTINCT "report"."name" FROM "report_layout" "me" JOIN "report" "report" ON "report"."id" = "me"."report_id" WHERE "me"."layout_id" = ? AND "report"."deleted" IS NULL`, c.ID); len(rows) > 0 {
			var names []string
			for _, r := range rows {
				s, _ := db.AsString(r["name"])
				names = append(names, s)
			}
			Fail(`The following reports reference this field: "%s". Please update them before deletion.`, strings.Join(names, ", "))
		}
		e.DB.Exec(`DELETE FROM "report_layout" WHERE "layout_id" = ?`, c.ID)
		for _, r := range e.DB.Query(`SELECT DISTINCT "view"."id", "view"."filter" FROM "filter" "me" JOIN "view" "view" ON "view"."id" = "me"."view_id" WHERE "me"."layout_id" = ?`, c.ID) {
			vid, _ := db.AsInt64(r["id"])
			js, _ := db.AsString(r["filter"])
			e.DB.Exec(`UPDATE "view" SET "filter" = ? WHERE "id" = ?`, filterRemoveColID(js, c.ID), vid)
		}
		for _, r := range e.DB.Query(`SELECT "id", "filter" FROM "layout" WHERE "filter" != '{}'`) {
			js, _ := db.AsString(r["filter"])
			if js == "" {
				continue
			}
			lid, _ := db.AsInt64(r["id"])
			e.DB.Exec(`UPDATE "layout" SET "filter" = ? WHERE "id" = ?`, filterRemoveColID(js, c.ID), lid)
		}
		// Clean up type specific data for all types
		for _, t := range []string{"string", "intgr", "date", "daterange", "person", "file", "file_option", "calc", "calcval", "calc_unique", "rag", "ragval", "curval", "filtered_value"} {
			e.DB.Exec(`DELETE FROM `+q(t)+` WHERE "layout_id" = ?`, c.ID)
		}
		e.DB.Exec(`DELETE FROM "curval_fields" WHERE "parent_id" = ?`, c.ID)
		e.DB.Exec(`DELETE FROM "enum" WHERE "layout_id" = ?`, c.ID)
		e.DB.Exec(`UPDATE "enumval" SET "parent" = NULL WHERE "layout_id" = ?`, c.ID)
		e.DB.Exec(`DELETE FROM "enumval" WHERE "layout_id" = ?`, c.ID)
		for _, t := range []string{"view_layout", "view_group", "filter", "alert_cache", "alert_send", "sort", "layout_depend", "layout_group", "display_field", "alert_column"} {
			e.DB.Exec(`DELETE FROM `+q(t)+` WHERE "layout_id" = ?`, c.ID)
		}
		e.DB.Exec(`DELETE FROM "sort" WHERE "parent_id" = ?`, c.ID)
		e.DB.Exec(`UPDATE "instance" SET "sort_layout_id" = NULL WHERE "sort_layout_id" = ?`, c.ID)
		e.DB.Exec(`DELETE FROM "layout" WHERE "id" = ?`, c.ID)
	})
}

func regexpMust(s string) *regexp.Regexp { return regexp.MustCompile(s) }
