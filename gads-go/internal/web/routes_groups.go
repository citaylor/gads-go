package web

import (
	"github.com/ctrlo/gads-go/internal/tt"
)

// The group administration pages of lib/GADS.pm

const groupDescription = "In this window you can create a permission group. Under table and field management you can define permissions and functions available to this group."

// deleteGroup is GADS::Group delete of the group of an ID (from_id): a
// group not found is created and deleted
func (s *Server) deleteGroup(id string) {
	if g := s.E.FindGroup(id); g != nil && truthy(id) {
		n, _ := toInt64(fmtAny(g["id"]))
		s.E.DeleteGroup(n)
		return
	}
	s.E.DeleteMissingGroup()
}

// any '/group_overview/' (GADS.pm:808)
func (s *Server) hGroupOverview(c *Ctx) string {
	if id := c.ParamS("delete"); truthy(id) {
		if c.Process(func() { s.deleteGroup(id) }) {
			c.ForwardHome("success", "The group has been deleted successfully", "group_overview/")
		}
	}
	items := tt.NewList()
	for _, g := range s.E.Groups() {
		items.Items = append(items.Items, tt.HashOf("id", ttValue(g["id"]), "name", ttValue(g["name"])))
	}
	return c.Template("layouts/page_overview_name_only", tt.HashOf(
		"page", "group",
		"page_title", "Group",
		"page_description", "Groups are the basis for LinkSpace’s fine-grained access control. Users can be allocated to any number of groups. You then set the group permissions for every field you create to control what fields users can view or edit.",
		"table_title", "Groups table",
		"table_column_label", "Group",
		"item_type", "group",
		"add_path", "group_add",
		"edit_path", "group_edit",
		"items", items,
	), nil)
}

// any '/group_add/' (GADS.pm:836)
func (s *Server) hGroupAdd(c *Ctx) string {
	if truthy(c.ParamS("submit")) {
		if c.Process(func() { s.E.CreateGroup(c.ParamS("name")) }) {
			c.ForwardHome("success", "Group has been created successfully", "group_overview/")
		}
	}
	return c.Template("layouts/page_save_name_only", tt.HashOf(
		"page", "group",
		"item", tt.HashOf(
			"type", "group",
			"description", groupDescription,
			"back_url", c.Base()+"group_overview/",
			"field_label", "Group",
		),
	), nil)
}

// any '/group_edit/:id' (GADS.pm:863)
func (s *Server) hGroupEdit(c *Ctx) string {
	id := c.ParamS("id")
	var g map[string]any
	if truthy(id) {
		g = s.E.FindGroup(id)
	}
	if truthy(c.ParamS("delete")) {
		if c.Process(func() { s.deleteGroup(id) }) {
			c.ForwardHome("success", "The group has been deleted successfully", "group_overview/")
		}
	}
	name := any(nil)
	if g != nil {
		name = g["name"]
	}
	if truthy(c.ParamS("submit")) {
		name = c.ParamS("name")
		if c.Process(func() {
			// A group not found is created (GADS::Group _rset)
			if g == nil {
				s.E.CreateGroup(c.ParamS("name"))
				return
			}
			n, _ := toInt64(fmtAny(g["id"]))
			s.E.WriteGroup(n, c.ParamS("name"))
		}) {
			c.ForwardHome("success", "Group has been updated successfully", "group_overview/")
		}
	}
	var gid any
	if g != nil {
		gid = ttValue(g["id"])
	}
	return c.Template("layouts/page_save_name_only", tt.HashOf(
		"page", "group",
		"item", tt.HashOf(
			"id", gid,
			"name", ttValue(name),
			"type", "group",
			"description", groupDescription,
			"back_url", c.Base()+"group_overview/",
			"field_label", "Group",
		),
	), nil)
}
