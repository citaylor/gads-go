package web

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/ctrlo/gads-go/internal/gads"
)

// post '/api/table_request' (API.pm:540): _post_table_request, creating a
// table from the table wizard
func (s *Server) hAPITableRequest(c *Ctx) string {
	if c.RequestContentType() != "application/json" {
		gads.Fail("Body content must be application/json")
	}
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil || !truthy(apiValue(decoded)) {
		gads.Fail("No body content received")
	}
	if !c.LoggedInUser().Permission()["superadmin"] {
		gads.Fail("You must be a super-administrator to create tables")
	}
	body, ok := wizardValue(decoded).(map[string]any)
	if !ok {
		gads.Panic("Not a HASH reference")
	}
	s.E.DB.Txn(func() { s.createTable(c, body) })
	return c.apiSuccess("Table created successfully")
}

// wizardValue converts a decoded JSON value to the values of Perl's
// decode_json: integers as numbers, booleans as 1 and 0
func wizardValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := strconv.ParseInt(x.String(), 10, 64); err == nil {
			return n
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	case bool:
		if x {
			return int64(1)
		}
		return int64(0)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = wizardValue(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = wizardValue(e)
		}
		return out
	}
	return v
}

// The table permissions of the wizard: key, type and permission name
var wizardTablePerms = []struct{ key, typ, name string }{
	{"delete_records", "records", "delete"},
	{"purge_deleted_records", "records", "purge"},
	{"download_records", "records", "download"},
	{"bulk_import_records", "records", "bulk_import"},
	{"bulk_update_records", "records", "bulk_update"},
	{"bulk_delete_records", "records", "bulk_delete"},
	{"manage_linked_records", "records", "link"},
	{"manage_child_records", "records", "create_child"},
	{"manage_views", "views", "view_create"},
	{"manage_group_views", "views", "view_group"},
	{"select_extra_view_limits", "views", "view_limit_extra"},
	{"manage_fields", "fields", "layout"},
}

func wizardMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func wizardList(v any) []map[string]any {
	var out []map[string]any
	l, _ := v.([]any)
	for _, e := range l {
		out = append(out, wizardMap(e))
	}
	return out
}

func wizardIDs(v any) []int64 {
	var out []int64
	l, _ := v.([]any)
	for _, e := range l {
		if n, ok := toInt64(fmtAny(e)); ok {
			out = append(out, n)
		}
	}
	return out
}

// createTable is API.pm _create_table
func (s *Server) createTable(c *Ctx, params map[string]any) {
	user := c.LoggedInUser()
	table := s.E.NewLayout(0, user)
	var groupPerms []string
	for _, perm := range wizardList(params["table_permissions"]) {
		gid := fmtAny(perm["group_id"])
		// Perl iterates a hash: the order of the permissions does not
		// matter as they are a set
		for _, p := range wizardTablePerms {
			if truthy(wizardMap(perm[p.typ])[p.key]) {
				groupPerms = append(groupPerms, gid+"_"+p.name)
			}
		}
	}
	table.SetGroups(groupPerms)
	// Undefined values are not written (DBIx::Class does not update a
	// column of the new instance from undef to undef)
	table.Write(definedValues(map[string]any{
		"name":                        params["name"],
		"name_short":                  params["shortName"],
		"hide_in_selector":            params["hide_in_selector"],
		"homepage_text":               nil,
		"homepage_text2":              nil,
		"sort_type":                   nil,
		"sort_layout_id":              nil,
		"view_limit_id":               nil,
		"default_view_limit_extra_id": nil,
	}))
	// Set default rag keys
	table.WriteRags(map[string]any{
		"danger_selected":   1,
		"warning_selected":  1,
		"advisory_selected": 1,
		"success_selected":  1,
	})
	table.Clear()

	topics := map[string]int64{}
	for _, top := range wizardList(params["topics"]) {
		state := "collapsed"
		if truthy(top["expanded"]) {
			state = "open"
		}
		topics[fmtAny(top["tempId"])] = s.E.DB.InsertMap("topic", map[string]any{
			"instance_id":   table.InstanceID,
			"name":          top["name"],
			"description":   top["description"],
			"initial_state": state,
		})
	}

	for _, f := range wizardList(params["fields"]) {
		typ := fmtAny(f["field_type"])
		valid := false
		for _, t := range []string{"string", "intgr", "date", "daterange", "enum", "tree", "file", "person", "rag", "calc", "curval", "autocur", "filval"} {
			valid = valid || typ == t
		}
		if !valid {
			gads.Fail("Invalid field type: %s", typ)
		}
		field := table.NewColumn(typ)
		name, ok := f["name"].(string)
		if !ok {
			// Moo's Str type check of the name dies
			gads.Panic(`isa check for "name" failed: undef is not a string!`)
		}
		field.Name = name
		field.Optional = truthy(f["optional"])
		if t := fmtAny(f["topic_tempid"]); truthy(f["topic_tempid"]) {
			id := topics[t]
			field.TopicID = &id
		}

		// Permissions: each one is added three times, as Perl loops over
		// create, read and update for each
		perms := map[int64][]string{}
		for _, perm := range wizardList(f["custom_field_permissions"]) {
			gid, _ := toInt64(fmtAny(perm["group_id"]))
			if perms[gid] == nil {
				perms[gid] = []string{}
			}
			p := wizardMap(perm["permissions"])
			for i := 0; i < 3; i++ {
				if truthy(p["create"]) {
					perms[gid] = append(perms[gid], "write_new")
				}
				if truthy(p["read"]) {
					perms[gid] = append(perms[gid], "read")
				}
				if truthy(p["update"]) {
					perms[gid] = append(perms[gid], "write_existing")
				}
			}
		}
		field.SetPermissions(perms)

		set := wizardMap(f["field_type_settings"])
		opts := func(names ...string) {
			for _, n := range names {
				field.SetOption(n, set[n])
			}
		}
		switch typ {
		case "string":
			field.Textbox = truthy(set["textbox"])
			field.ForceRegex = fmtAny(set["force_regex"])
		case "intgr":
			opts("show_calculator")
		case "date":
			opts("show_datepicker", "default_today")
		case "daterange":
			opts("show_datepicker")
		case "enum":
			var vals []map[string]any
			for _, v := range wizardList(set["dropdown_values"]) {
				vals = append(vals, map[string]any{"value": fmtAny(v["enumval"])})
			}
			field.SetEnumvals(vals)
			if truthy(set["ordering"]) {
				field.Ordering = fmtAny(set["ordering"])
			}
		case "tree":
			field.EndNodeOnly = truthy(set["end_node_only"])
		case "file":
			if truthy(set["filesize"]) {
				n, _ := toInt64(fmtAny(set["filesize"]))
				field.Filesize = &n
			}
		case "person":
			opts("default_to_login", "notify_on_selection", "notify_on_selection_message", "notify_on_selection_subject")
		case "rag":
			field.SetCode(fmtAny(set["code_rag"]))
		case "calc":
			field.SetCode(fmtAny(set["code_calc"]))
			field.SetReturnType(fmtAny(set["return_type"]))
			opts("show_in_edit")
		case "curval":
			if n, ok := toInt64(fmtAny(set["refers_to_instance_id"])); ok {
				field.SetRefersToInstanceID(n)
			}
			field.SetCurvalFieldIDs(wizardIDs(set["curval_field_ids"]))
			opts("override_permissions", "value_selector", "show_add", "delete_not_used", "limit_rows")
		case "autocur", "filval":
			field.SetCurvalFieldIDs(wizardIDs(set["curval_field_ids"]))
			key := "related_field_id"
			if typ == "filval" {
				key = "filval_related_field_id"
			}
			if n, ok := toInt64(fmtAny(set[key])); ok {
				field.RelatedFieldID = &n
			}
		}
		t := true
		field.Write(gads.ColumnWriteOptions{NoAlerts: true, NoCacheUpdate: &t})
		// ID needs to be set before writing tree
		if typ == "tree" {
			tree := wizardList(set["dataJson"])
			field.TreeUpdate(tree)
		}
	}
}

// definedValues returns the values of m that are not nil
func definedValues(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if v != nil {
			out[k] = v
		}
	}
	return out
}
