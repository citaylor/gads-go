package gads

import (
	"encoding/base64"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// The export hashes of GADS (export_hash and Layout->export), used by
// bin/export.pl and bin/import.pl. Values are as Perl has them: database
// integers as int64, text as string, nulls as nil and DateTimes in ISO
// 8601 (DateTime->datetime).

// int01 is a Perl boolean attribute as an integer
func int01(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// isoDateTime is DateTime->datetime of a database value (nil if null)
func isoDateTime(v any) any {
	if v == nil {
		return nil
	}
	if t, ok := db.AsTime(v); ok {
		return t.Format("2006-01-02T15:04:05")
	}
	return v
}

// encodeBase64 is MIME::Base64 encode_base64: lines of 76 characters,
// each ended by a newline
func encodeBase64(b []byte) string {
	enc := base64.StdEncoding.EncodeToString(b)
	var s strings.Builder
	for len(enc) > 76 {
		s.WriteString(enc[:76] + "\n")
		enc = enc[76:]
	}
	if enc != "" {
		s.WriteString(enc + "\n")
	}
	return s.String()
}

// Export is Layout->export: the settings of the table
func (l *Layout) Export() map[string]any {
	row := l.e.DB.QueryRow(`SELECT * FROM "instance" WHERE "id" = ?`, l.InstanceID)
	perms := []any{}
	for _, p := range l.e.DB.Query(`SELECT "group_id", "permission" FROM "instance_group" WHERE "instance_id" = ? ORDER BY "id"`, l.InstanceID) {
		perms = append(perms, map[string]any{"group_id": p["group_id"], "permission": p["permission"]})
	}
	return map[string]any{
		"name":                        row["name"],
		"name_short":                  row["name_short"],
		"hide_in_selector":            row["hide_in_selector"],
		"homepage_text":               row["homepage_text"],
		"homepage_text2":              row["homepage_text2"],
		"sort_layout_id":              row["sort_layout_id"],
		"sort_type":                   row["sort_type"],
		"view_limit_id":               row["view_limit_id"],
		"default_view_limit_extra_id": row["default_view_limit_extra_id"],
		"permissions":                 perms,
	}
}

// optionNames are the options of each type of column (option_names)
var optionNames = map[string][]string{
	"daterange": {"show_datepicker"},
	"autocur":   {"override_permissions", "limit_rows"},
	"curval":    {"override_permissions", "value_selector", "show_add", "delete_not_used", "limit_rows", "show_view_all"},
	"string":    {"max_length"},
	"file":      {"override_types"},
	"calc":      {"show_in_edit"},
	"date":      {"show_datepicker", "default_today"},
	"person":    {"default_to_login", "notify_on_selection", "notify_on_selection_message", "notify_on_selection_subject"},
	"intgr":     {"show_calculator"},
	"id":        {"show_calculator"},
}

func init() {
	// The internal columns have the options of the classes they extend
	optionNames["createddate"] = optionNames["date"]
	optionNames["createdby"] = optionNames["person"]
	optionNames["deletedby"] = optionNames["person"]
}

// OptionValue is the accessor of an option
func (c *Column) OptionValue(name string) any { return c.optionValue(name) }

// optionValue is the accessor of an option
func (c *Column) optionValue(name string) any {
	switch name {
	case "show_datepicker":
		return int01(c.ShowDatepicker())
	case "default_today":
		return int01(c.DefaultToday())
	case "show_calculator":
		return int01(c.ShowCalculator())
	case "show_in_edit":
		return c.ShowInEdit()
	case "override_permissions":
		return int01(c.OverridePermissions())
	case "value_selector":
		return c.ValueSelector()
	case "show_add":
		return int01(c.ShowAdd())
	case "delete_not_used":
		return int01(c.DeleteNotUsed())
	case "limit_rows":
		if n := c.LimitRows(); n != 0 {
			return n
		}
		return nil
	case "show_view_all":
		return int01(c.ShowViewAll())
	case "max_length":
		if m := c.MaxLength(); m != nil {
			return *m
		}
		return nil
	case "override_types":
		// options->{override_types} || []
		if v := c.opt("override_types"); truthy(v) {
			return v
		}
		return []any{}
	case "default_to_login":
		return int01(c.DefaultToLogin())
	case "notify_on_selection":
		return int01(c.NotifyOnSelection())
	}
	return c.opt(name)
}

// ExportHash is Column->export_hash
func (c *Column) ExportHash() map[string]any {
	d := c.e.DB
	row := d.QueryRow(`SELECT * FROM "layout" WHERE "id" = ?`, c.ID)
	var permissions any
	perms := map[string]any{}
	for _, p := range d.Query(`SELECT "group_id", "permission" FROM "layout_group" WHERE "layout_id" = ? ORDER BY "id"`, c.ID) {
		g := anyToString(p["group_id"])
		l, _ := perms[g].([]any)
		perms[g] = append(l, p["permission"])
	}
	if len(perms) > 0 {
		permissions = perms
	}
	var groupDisplay any
	if gd := c.GroupDisplay(); gd != "" {
		groupDisplay = gd
	}
	h := map[string]any{
		"id":                c.ID,
		"type":              c.Type,
		"internal":          row["internal"],
		"name":              row["name"],
		"name_short":        row["name_short"],
		"topic_id":          row["topic_id"],
		"optional":          row["optional"],
		"remember":          row["remember"],
		"isunique":          row["isunique"],
		"can_child":         int01(c.CanChild()),
		"position":          row["position"],
		"description":       row["description"],
		"width":             row["width"],
		"helptext":          row["helptext"],
		"display_condition": row["display_condition"],
		"lookup_endpoint":   row["lookup_endpoint"],
		"lookup_group":      row["lookup_group"],
		"link_parent":       row["link_parent"],
		"multivalue":        row["multivalue"],
		"filter":            c.FilterFn().AsJSON(),
		"aggregate":         row["aggregate"],
		"group_display":     groupDisplay,
		"permissions":       permissions,
	}
	displayFields := []any{}
	for _, f := range c.DisplayFields().Filters() {
		displayFields = append(displayFields, map[string]any{
			"id":       f["column_id"],
			"value":    f["value"],
			"operator": f["operator"],
		})
	}
	h["display_fields"] = displayFields
	for _, o := range optionNames[c.Type] {
		h[o] = c.optionValue(o)
	}
	switch c.Type {
	case "string":
		h["textbox"] = row["textbox"]
		h["force_regex"] = row["force_regex"]
	case "enum":
		vals := []any{}
		for _, v := range c.Enumvals() {
			ev := map[string]any{}
			for k, x := range v {
				if k == "deleted" {
					x = int01(x.(bool))
				}
				ev[k] = x
			}
			vals = append(vals, ev)
		}
		h["enumvals"] = vals
		h["ordering"] = row["ordering"]
	case "tree":
		h["end_node_only"] = row["end_node_only"]
		h["tree"] = c.TreeJSON()
	case "file":
		h["filesize"] = derefInt(c.Filesize)
	case "calc":
		h["code"] = c.CodeFn()
		h["return_type"] = c.ReturnTypeFn()
		h["decimal_places"] = derefInt(c.DecimalPlaces)
	case "rag":
		h["code"] = c.CodeFn()
	}
	switch c.Type {
	case "curval", "autocur", "filval":
		h["refers_to_instance_id"] = derefInt(c.RefersToInstanceID())
		ids := []any{}
		for _, id := range c.CurvalFieldIDs() {
			ids = append(ids, id)
		}
		h["curval_field_ids"] = ids
		if c.Type != "curval" {
			h["related_field_id"] = row["related_field"]
		}
	}
	return h
}

// ExportHash is MetricGroup->export_hash
func (e *Engine) MetricGroupExportHash(row db.Row) map[string]any {
	metrics := []any{}
	for _, m := range e.DB.Query(`SELECT "me".* FROM "metric" "me" JOIN "metric_group" "metric_group" ON "metric_group"."id" = "me"."metric_group" WHERE "me"."metric_group" = ? AND "metric_group"."instance_id" = ? ORDER BY "me"."x_axis_value", "me"."y_axis_grouping_value"`, row["id"], row["instance_id"]) {
		metrics = append(metrics, map[string]any{
			"id":                    m["id"],
			"x_axis_value":          m["x_axis_value"],
			"target":                m["target"],
			"y_axis_grouping_value": m["y_axis_grouping_value"],
		})
	}
	return map[string]any{"id": row["id"], "name": row["name"], "metrics": metrics}
}

// GraphExportHash is Graph->export_hash of a graph row
func GraphExportHash(g db.Row) map[string]any {
	return map[string]any{
		"title":           g["title"],
		"description":     g["description"],
		"y_axis":          g["y_axis"],
		"y_axis_link":     g["y_axis_link"],
		"y_axis_stack":    g["y_axis_stack"],
		"y_axis_label":    g["y_axis_label"],
		"x_axis":          g["x_axis"],
		"x_axis_link":     g["x_axis_link"],
		"x_axis_grouping": g["x_axis_grouping"],
		"group_by":        g["group_by"],
		"stackseries":     g["stackseries"],
		"trend":           g["trend"],
		"from":            isoDateTime(g["from"]),
		"to":              isoDateTime(g["to"]),
		"x_axis_range":    g["x_axis_range"],
		"is_shared":       g["is_shared"],
		"user_id":         g["user_id"],
		"group_id":        g["group_id"],
		"as_percent":      g["as_percent"],
		"type":            g["type"],
		"metric_group_id": g["metric_group"],
	}
}

// ExportHash is View->export_hash
func (v *View) ExportHash() map[string]any {
	d := v.e.DB
	row := d.QueryRow(`SELECT * FROM "view" WHERE "id" = ?`, v.ID)
	sorts := []any{}
	for _, s := range d.Query(`SELECT * FROM "sort" WHERE "view_id" = ? ORDER BY "id"`, v.ID) {
		sorts = append(sorts, map[string]any{"layout_id": s["layout_id"], "parent_id": s["parent_id"], "type": s["type"], "order": s["order"]})
	}
	groups := []any{}
	for _, g := range d.Query(`SELECT * FROM "view_group" WHERE "view_id" = ? ORDER BY "id"`, v.ID) {
		groups = append(groups, map[string]any{"layout_id": g["layout_id"], "parent_id": g["parent_id"], "order": g["order"]})
	}
	columns := []any{}
	for _, c := range v.Columns() {
		columns = append(columns, c)
	}
	return map[string]any{
		"id":       v.ID,
		"global":   row["global"],
		"is_admin": row["is_admin"],
		"group_id": row["group_id"],
		"name":     row["name"],
		"filter":   v.Filter().AsHash(),
		"sorts":    sorts,
		"groups":   groups,
		"columns":  columns,
	}
}

// CurrentExportHash is the export_hash of a record (current) row: its
// versions with their values
func (e *Engine) CurrentExportHash(cur db.Row) map[string]any {
	d := e.DB
	h := map[string]any{
		"id":        cur["id"],
		"serial":    cur["serial"],
		"parent_id": cur["parent_id"],
		"linked_id": cur["linked_id"],
		"deleted":   isoDateTime(cur["deleted"]),
		"deletedby": cur["deletedby"],
	}
	records := []any{}
	for _, rec := range d.Query(`SELECT * FROM "record" WHERE "current_id" = ? ORDER BY "id"`, cur["id"]) {
		values := []any{}
		for _, table := range []string{"curval", "date", "daterange", "enum", "intgr", "person", "string", "file"} {
			for _, v := range d.Query(`SELECT * FROM "`+table+`" WHERE "record_id" = ? ORDER BY "id"`, rec["id"]) {
				val := map[string]any{"layout_id": v["layout_id"], "child_unique": v["child_unique"]}
				switch table {
				case "date":
					val["value"] = isoDateTime(v["value"])
				case "daterange":
					val["from"] = isoDateTime(v["from"])
					val["to"] = isoDateTime(v["to"])
					val["value"] = v["value"]
				case "string":
					val["value"] = v["value"]
					val["value_index"] = v["value_index"]
				case "file":
					val["content"], val["name"], val["mimetype"] = nil, nil, nil
					if f := d.QueryRow(`SELECT * FROM "fileval" WHERE "id" = ?`, v["value"]); f != nil {
						fid, _ := db.AsInt64(f["id"])
						val["content"] = encodeBase64(e.FilevalContent(fid))
						val["name"] = f["name"]
						val["mimetype"] = f["mimetype"]
					}
				default:
					val["value"] = v["value"]
				}
				values = append(values, val)
			}
		}
		records = append(records, map[string]any{
			"created":    isoDateTime(rec["created"]),
			"createdby":  rec["createdby"],
			"approvedby": rec["approvedby"],
			"record_id":  rec["record_id"],
			"approval":   rec["approval"],
			"values":     values,
		})
	}
	h["records"] = records
	return h
}

// UserExportHash is the export_hash of a user row
func (e *Engine) UserExportHash(u db.Row) map[string]any {
	d := e.DB
	groups := []any{}
	for _, g := range d.QueryInt64s(`SELECT "group_id" FROM "user_group" WHERE "user_id" = ? ORDER BY "id"`, u["id"]) {
		groups = append(groups, g)
	}
	perms := []any{}
	for _, p := range d.Query(`SELECT "permission"."name" FROM "user_permission" "me" JOIN "permission" "permission" ON "permission"."id" = "me"."permission_id" WHERE "me"."user_id" = ? ORDER BY "me"."id"`, u["id"]) {
		perms = append(perms, p["name"])
	}
	return map[string]any{
		"id":                    u["id"],
		"firstname":             u["firstname"],
		"surname":               u["surname"],
		"value":                 u["value"],
		"email":                 u["email"],
		"username":              u["username"],
		"freetext1":             u["freetext1"],
		"freetext2":             u["freetext2"],
		"password":              u["password"],
		"pwchanged":             isoDateTime(u["pwchanged"]),
		"deleted":               isoDateTime(u["deleted"]),
		"lastlogin":             isoDateTime(u["lastlogin"]),
		"account_request":       u["account_request"],
		"account_request_notes": u["account_request_notes"],
		"created":               isoDateTime(u["created"]),
		"created_by_id":         u["created_by_id"],
		"groups":                groups,
		"permissions":           perms,
	}
}
