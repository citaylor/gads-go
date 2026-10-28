package web

import (
	"encoding/base64"
	"regexp"
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// permissionMapping is GADS::Type::Permissions->permission_mapping
func permissionMapping() *tt.Hash {
	return tt.HashOf(
		"read", "Read",
		"write_new", "Write new",
		"write_existing", "Edit",
		"approve_new", "Approve new",
		"approve_existing", "Approve existing",
		"write_new_no_approval", "Write without approval",
		"write_existing_no_approval", "Edit without approval",
	)
}

// permissionTypes is GADS::Type::Permissions->all (objects stringifying as
// their long description)
func permissionTypes() *tt.List {
	l := tt.NewList()
	long := permissionInputs()
	medium := permissionMapping()
	for _, p := range []string{"read", "write_new", "write_existing", "approve_new", "approve_existing", "write_new_no_approval", "write_existing_no_approval"} {
		lv, _ := long.Get(p)
		mv, _ := medium.Get(p)
		l.Items = append(l.Items, tt.HashOf("short", p, "long", lv, "medium", mv))
	}
	return l
}

// ttInstancesObj is GADS::Instances
type ttInstancesObj struct {
	c  *Ctx
	is *gads.Instances
}

func (o *ttInstancesObj) TTMethod(name string, args []any) ([]any, bool, error) {
	switch name {
	case "all":
		l := tt.NewList()
		for _, lay := range o.is.All() {
			l.Items = append(l.Items, o.c.ttLayout(lay))
		}
		return []any{l}, true, nil
	case "layout":
		id, _ := toInt64(argAt(args, 0))
		if lay := o.is.Layout(id); lay != nil {
			return []any{o.c.ttLayout(lay)}, true, nil
		}
		return []any{nil}, true, nil
	}
	return nil, false, nil
}

// ttGroupsObj is GADS::Groups
type ttGroupsObj struct{ s *Server }

func (o *ttGroupsObj) TTMethod(name string, args []any) ([]any, bool, error) {
	if name == "all" {
		l := tt.NewList()
		for _, g := range o.s.E.Groups() {
			l.Items = append(l.Items, newRow("group", g))
		}
		return []any{l}, true, nil
	}
	return nil, false, nil
}

var rePermissionParam = regexp.MustCompile(`^permission_(.*?)_(\d+)$`)

// any ['get','post'] => /:layout_name/layout/?:id? (GADS.pm:3663)
func (s *Server) hLayout(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	if !layout.UserCan("layout") {
		c.ForwardHome("danger", "You do not have permission to manage fields", "")
	}
	idS, hasID := c.Param("id")
	params := tt.HashOf("page", "layouts")
	if hasID {
		params.Set("page", "layout")
		is := c.Instances()
		instLayouts := tt.NewList()
		for _, l := range is.All() {
			instLayouts.Items = append(instLayouts.Items, c.ttLayout(l))
		}
		params.Set("instance_layouts", instLayouts)
		params.Set("instances_object", &ttInstancesObj{c: c, is: is})
	}
	lo := layout.Identifier()
	var column *gads.Column
	submit, _ := c.Param("submit")
	updatePerms, _ := c.Param("update_perms")
	if truthy(idS) || truthy(submit) || truthy(updatePerms) {
		if truthy(idS) {
			id, _ := toInt64(idS)
			column = layout.Column(id)
			if column == nil {
				gads.Fail("Column ID %s not found", idS)
			}
			if column.InstanceID != layout.InstanceID {
				gads.Fail("Column belongs to instance ID %d but currently editing instance ID %d", column.InstanceID, layout.InstanceID)
			}
		} else {
			class := c.ParamS("type")
			valid := false
			for _, t := range gads.ColumnTypes {
				if t == class {
					valid = true
				}
			}
			if !valid {
				gads.Fail("Invalid column type %s", class)
			}
			column = layout.NewColumn(class)
		}
		if v, _ := c.Param("delete"); truthy(v) {
			// Logged in case of repercussions of the deletion
			s.E.Audit(map[string]any{
				"user_id":     user.ID,
				"description": `User "` + user.Username + `" deleted field "` + column.Name + `"`,
				"type":        "user_action",
				"method":      nil,
				"url":         nil,
				"instance_id": nil,
			})
			if c.Process(column.Delete) {
				c.ForwardHome("success", "The item has been deleted successfully", lo+"/layout")
			}
		}
		if truthy(submit) {
			s.layoutSubmit(c, layout, column, truthy(idS))
		}
		params.Set("column", c.ttColumn(column))
	} else if hasID {
		params.Set("column", int64(0))
	}
	base := c.Base()
	var crumbs *tt.List
	headerType, backURL := "table_tabs", base+"table"
	if hasID {
		last := "Add a field"
		if truthy(idS) && column != nil {
			last = "Edit field: " + column.Name
		}
		crumbs = tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb(base+lo+"/layout", "Fields"),
			crumb("", last),
		)
		headerType, backURL = "table_title", base+lo+"/layout"
	} else {
		crumbs = tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb("", "Fields"),
		)
	}
	// The user fields of the site, to filter people fields by
	var userFields []any
	for _, f := range c.Site().UserFields() {
		userFields = append(userFields, map[string]any{"id": f.Name, "label": f.Description, "type": "string"})
	}
	if userFields == nil {
		userFields = []any{}
	}
	params.Set("people_fields", perlEncodeBase64(gads.EncodeJSON(userFields)))
	params.Set("groups", &ttGroupsObj{s: s})
	params.Set("permissions", permissionTypes())
	params.Set("permission_mapping", permissionMapping())
	params.Set("permission_inputs", permissionInputs())
	topics := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT * FROM "topic" "me" WHERE "me"."instance_id" = ?`, layout.InstanceID) {
		topics.Items = append(topics.Items, newRow("topic", r))
	}
	params.Set("topics", topics)
	params.Set("content_block_custom_classes", "content-block--footer")
	params.Set("header_type", headerType)
	params.Set("header_back_url", backURL)
	params.Set("layout_obj", c.ttLayout(layout))
	params.Set("breadcrumbs", crumbs)
	if v, _ := c.Param("saveposition"); truthy(v) {
		position := c.body.getAll("position")
		if c.Process(func() { s.E.SetColumnPositions(position) }) {
			c.ForwardHome("success", "The ordering has been saved successfully", lo+"/layout")
		}
	}
	page := "layouts"
	if hasID {
		page = "layout"
	}
	return c.Template(page, params, nil)
}

// layoutSubmit sets the values of a column from the field form and writes
// it (forwarding to the fields on success)
func (s *Server) layoutSubmit(c *Ctx, layout *gads.Layout, column *gads.Column, existing bool) {
	perms := map[int64][]string{}
	var keys []string
	for k := range c.allParamKeys() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if m := rePermissionParam.FindStringSubmatch(k); m != nil {
			gid, _ := toInt64(m[2])
			perms[gid] = append(perms[gid], m[1])
		}
	}
	column.SetPermissions(perms)

	str := func(k string) string { v, _ := c.Param(k); return v }
	intOrNil := func(k string) *int64 {
		// coerce => sub { $_[0] || undef }
		v, _ := c.Param(k)
		if !truthy(v) {
			return nil
		}
		n, _ := toInt64(v)
		return &n
	}
	column.Name = str("name")
	column.NameShort = str("name_short")
	column.Description = str("description")
	column.Helptext = str("helptext")
	column.Optional = truthy(str("optional"))
	column.IsUnique = truthy(str("isunique"))
	column.SetCanChild(truthy(str("set_can_child")))
	column.Multivalue = truthy(str("multivalue"))
	column.Remember = truthy(str("remember"))
	column.LinkParentID = intOrNil("link_parent_id")
	column.TopicID = intOrNil("topic_id")
	column.Width, _ = toInt64(str("width"))
	column.Aggregate = str("aggregate")
	column.SetGroupDisplay(str("group_display"))
	for _, opt := range column.UserOptions() {
		if v, ok := c.Param(opt); ok {
			column.SetOption(opt, v)
		} else {
			column.SetOption(opt, nil)
		}
	}
	df := gads.NewFilterJSON(str("display_fields"), layout)
	column.SetDisplayFields(df)
	notes, _ := c.BodyParam("notes")
	column.Notes = notes

	var noAlerts bool
	switch column.Type {
	case "file":
		column.Filesize = intOrNil("filesize")
	case "rag":
		column.SetCode(str("code_rag"))
		noAlerts = truthy(str("no_alerts_rag"))
	case "enum":
		values := c.body.getAll("enumval")
		ids := c.body.getAll("enumval_id")
		var enumvals []map[string]any
		for _, v := range values {
			var id string
			if len(ids) > 0 {
				id, ids = ids[0], ids[1:]
			}
			if !truthy(id) {
				enumvals = append(enumvals, map[string]any{"value": v})
			} else {
				enumvals = append(enumvals, map[string]any{"id": id, "value": v})
			}
		}
		column.SetEnumvals(enumvals)
		column.Ordering = str("ordering")
	case "calc":
		column.SetCode(str("code_calc"))
		column.SetReturnType(str("return_type"))
		noAlerts = truthy(str("no_alerts_calc"))
	case "tree":
		column.EndNodeOnly = truthy(str("end_node_only"))
	case "string":
		column.Textbox = truthy(str("textbox"))
		column.ForceRegex = str("force_regex")
	case "curval":
		rti := intOrNil("refers_to_instance_id")
		if rti == nil {
			column.SetRefersToInstanceID(0)
		} else {
			column.SetRefersToInstanceID(*rti)
		}
		f := column.FilterFn()
		f.SetJSON(str("filter"))
		column.SetFilter(f)
		column.SetCurvalFieldIDs(int64List(c.body.getAll("curval_field_ids")))
	case "person":
		f := column.FilterFn()
		f.SetJSON(str("people-display"))
		column.SetFilter(f)
	case "autocur":
		column.SetCurvalFieldIDs(int64List(c.body.getAll("autocur_field_ids")))
		column.RelatedFieldID = intOrNil("related_field_id")
	case "filval":
		column.SetCurvalFieldIDs(int64List(c.body.getAll("filval_field_ids")))
		column.RelatedFieldID = intOrNil("filval_related_field_id")
	}
	noCache := str("no_cache_update_calc")
	if column.Type == "rag" {
		noCache = str("no_cache_update_rag")
	}
	o := gads.ColumnWriteOptions{NoAlerts: noAlerts}
	if truthy(noCache) {
		t := true
		o.NoCacheUpdate = &t
	}
	if c.Process(func() { column.Write(o) }) {
		msg := "Your field has been created successfully"
		if existing {
			msg = "Your field has been updated successfully"
		}
		c.ForwardHome("success", msg, layout.Identifier()+"/layout")
	}
}

func int64List(vals []string) []int64 {
	out := []int64{}
	for _, v := range vals {
		n, _ := toInt64(v)
		out = append(out, n)
	}
	return out
}

// perlEncodeBase64 is MIME::Base64 encode_base64 with its default line
// ending: lines of 76 characters, each ended by a newline
func perlEncodeBase64(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\n")
		enc = enc[76:]
	}
	if enc != "" {
		b.WriteString(enc)
		b.WriteString("\n")
	}
	return b.String()
}

// allParamKeys are the names of the parameters of the request (params)
func (c *Ctx) allParamKeys() map[string]bool {
	keys := map[string]bool{}
	for k := range c.query.vals {
		keys[k] = true
	}
	for k := range c.body.vals {
		keys[k] = true
	}
	for k := range c.route.params {
		keys[k] = true
	}
	return keys
}
