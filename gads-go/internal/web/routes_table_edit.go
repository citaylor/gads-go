package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// bodyLast is the body parameters as Hash::MultiValue get: the last value
// of each
func (c *Ctx) bodyLast() map[string]any {
	m := map[string]any{}
	for k, v := range c.body.vals {
		if len(v) > 0 {
			m[k] = v[len(v)-1]
		}
	}
	return m
}

// tableLayout is the table of the table settings routes, which need
// permission to administer it
func (c *Ctx) tableLayout() *gads.Layout {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	if !layout.UserCan("layout") {
		c.ForwardHome("danger", "You do not have permission to manage fields", "")
	}
	return layout
}

// any ['get','post'] => /:layout_name/edit (GADS.pm:3566)
func (s *Server) hTableEdit(c *Ctx) string {
	layout := c.tableLayout()
	param := func(k string) any {
		if v, ok := c.Param(k); ok {
			return v
		}
		return nil
	}
	orUndef := func(k string) any {
		if v := param(k); truthy(v) {
			return v
		}
		return nil
	}
	over := map[string]any{}
	name := layout.Name()
	if v, _ := c.Param("submit"); truthy(v) {
		if param("name") == nil {
			gads.Panic(`isa check for "name" failed: undef is not a string!`)
		}
		name = c.ParamS("name")
		set := map[string]any{
			"name":             param("name"),
			"name_short":       param("name_short"),
			"hide_in_selector": param("hide_in_selector"),
			"sort_layout_id":   orUndef("sort_layout_id"),
			"sort_type":        orUndef("sort_type"),
			"view_limit_id":    orUndef("view_limit_id"),
		}
		for k, v := range set {
			over[k] = ttValue(v)
		}
		// The alert columns are set immediately (whether or not the table
		// is then written)
		var alertColumns []int64
		for _, v := range c.body.getAll("alert_column") {
			id, _ := toInt64(v)
			alertColumns = append(alertColumns, id)
		}
		layout.SetAlertColumns(alertColumns)
		rags := c.bodyLast()
		if c.Process(func() {
			layout.Write(set)
			layout.WriteRags(rags)
		}) {
			msg := "Your new table has been created successfully"
			if truthy(param("id")) {
				msg = "The table has been updated successfully"
			}
			c.ForwardHome("success", msg, layout.Identifier()+"/edit")
		}
	}
	if v, _ := c.Param("delete"); truthy(v) {
		if c.Process(layout.Delete) {
			c.ForwardHome("success", "The table has been deleted successfully", "table")
		}
	}
	base := c.Base()
	lt := c.ttLayoutWith(layout, over)
	return c.Template("table", tt.HashOf(
		"page", "table_edit",
		"content_block_custom_classes", "content-block--footer",
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+layout.Identifier()+"/data", "Table: "+name),
			crumb("", "General settings"),
		),
		"header_type", "table_tabs",
		"layout", lt,
		"layout_obj", lt,
	), nil)
}

// any ['get','post'] => /:layout_name/permissions (GADS.pm:3627)
func (s *Server) hTablePermissions(c *Ctx) string {
	layout := c.tableLayout()
	if v, _ := c.Param("submit"); truthy(v) {
		layout.SetGroups(c.body.getAll("permissions"))
		if c.Process(func() { layout.Write(nil) }) {
			c.ForwardHome("success", "The table permissions have been updated successfully", layout.Identifier()+"/permissions")
		}
	}
	base := c.Base()
	groups := tt.NewList()
	for _, g := range s.E.Groups() {
		groups.Items = append(groups.Items, newRow("group", g))
	}
	return c.Template("table_permissions", tt.HashOf(
		"page", "table_permissions",
		"header_type", "table_tabs",
		"content_block_custom_classes", "content-block--footer",
		"layout", c.ttLayout(layout),
		"layout_obj", c.ttLayout(layout),
		"groups", groups,
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+layout.Identifier()+"/data", "Table: "+layout.Name()),
			crumb("", "Permissions"),
		),
	), nil)
}
