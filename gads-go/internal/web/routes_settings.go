package web

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// The system settings pages of lib/GADS.pm

// any '/settings/?' (GADS.pm:901)
func (s *Server) hSettings(c *Ctx) string {
	return c.Template("admin/admin_settings", tt.HashOf("page", "system_settings"), nil)
}

// any '/settings/default_welcome_email/' (GADS.pm:907)
func (s *Server) hSettingsWelcomeEmail(c *Ctx) string {
	if !c.LoggedInUser().Permission()["superadmin"] {
		c.ForwardHome("danger", "You do not have permission to manage system settings", "")
	}
	site := c.Site()
	if truthy(c.ParamS("update")) {
		vals := map[string]any{}
		for _, k := range []string{"email_welcome_subject", "email_welcome_text", "name"} {
			if v, ok := c.Param(k); ok {
				vals[k] = v
			} else {
				vals[k] = nil
			}
		}
		if c.Process(func() { s.E.DB.UpdateMap("site", vals, map[string]any{"id": site.ID()}) }) {
			c.ForwardHome("success", "Configuration settings have been updated successfully", "settings/")
		}
		// The site object keeps the submitted values
		for k, v := range vals {
			site.Row()[k] = v
		}
	}
	return c.Template("admin/default_welcome_email", tt.HashOf(
		"instance", c.ttSite(site),
		"page", "manage_default_welcome_email",
	), nil)
}

// any '/settings/user_editable_personal_details/' (GADS.pm:932)
func (s *Server) hSettingsUserEditable(c *Ctx) string {
	if !c.LoggedInUser().Permission()["superadmin"] {
		c.ForwardHome("danger", "You do not have permission to manage system settings", "")
	}
	site := c.Site()
	if truthy(c.ParamS("update")) {
		if c.Process(func() { s.E.UpdateUserEditableFields(site, c.body.getAll("user_editable")) }) {
			c.ForwardHome("success", "Configuration settings have been updated successfully", "settings/")
		}
	}
	return c.Template("admin/user_editable_personal_details", tt.HashOf(
		"instance", c.ttSite(site),
		"page", "manage_user_editable_personal_details",
	), nil)
}

// userList is a list of values for users administered by the settings
// pages (titles, organisations, departments and teams)
type userList struct {
	table   string // the table of the items
	path    string // the name in the paths (settings/<path>_overview/ etc.)
	page    string
	userCol string // the user column referring to the items
	// the name of an item (lower case first) and in its deletion error
	name, errName func(site *gads.Site) string
	plural        func(site *gads.Site) string
	description   func(name string) string // of the overview
	edited        string                   // the word of the edit success message
	deleteFirst   bool                     // edit deletes before it saves
}

var userLists = []*userList{
	{
		table: "title", path: "title", page: "manage_titles", userCol: "title",
		name:    func(*gads.Site) string { return "title" },
		errName: func(*gads.Site) string { return "title" },
		plural:  func(*gads.Site) string { return "titles" },
		description: func(n string) string {
			return "In this window you can list the " + n + "s that you want to assign users to. You can update the existing items or add new ones. Changes in here will impact all users currently assigned if you delete or edit a value."
		},
		edited: "updated",
	},
	{
		table: "organisation", path: "organisation", page: "manage_organisations", userCol: "organisation",
		name:    func(s *gads.Site) string { return lcfirst(s.OrganisationName()) },
		errName: func(s *gads.Site) string { return s.OrganisationName() },
		plural:  func(s *gads.Site) string { return lcfirst(gads.Plural(s.OrganisationName())) },
		description: func(n string) string {
			return "In this window you can list the parts of the " + n + " that you want to assign users to. You can update the existing items or add new ones. Changes in here will impact all users currently assigned if you delete or edit a value."
		},
		edited: "edited", deleteFirst: true,
	},
	{
		table: "department", path: "department", page: "manage_departments", userCol: "department_id",
		name:    func(s *gads.Site) string { return lcfirst(s.DepartmentName()) },
		errName: func(s *gads.Site) string { return s.DepartmentName() },
		plural:  func(s *gads.Site) string { return lcfirst(gads.Plural(s.DepartmentName())) },
		description: func(n string) string {
			return "In this window you can list the " + n + " that you want to assign users to. You can update the existing items or add new ones. Changes in here will impact all users currently assigned if you delete or edit a value."
		},
		edited: "updated",
	},
	{
		table: "team", path: "team", page: "manage_teams", userCol: "team_id",
		name:    func(s *gads.Site) string { return lcfirst(s.TeamName()) },
		errName: func(s *gads.Site) string { return s.TeamName() },
		plural:  func(s *gads.Site) string { return lcfirst(gads.Plural(s.TeamName())) },
		description: func(n string) string {
			return "In this window you can list the " + n + " that you want to assign users to. You can update the existing items or add new ones. Changes in here will impact all users currently assigned if you delete or edit a value."
		},
		edited: "updated",
	},
}

func lcfirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// deleteItem deletes an item (delete_title etc.) of the row found, which
// dies as Perl calling a method of undef if there is none
func (s *Server) deleteItem(l *userList, row db.Row, site *gads.Site) {
	if row == nil {
		gads.Panic(`Can't call method "delete_%s" on an undefined value`, l.table)
	}
	s.E.DeleteListItem(l.table, l.userCol, row["id"], l.errName(site))
}

// any '/settings/<list>_overview/' (GADS.pm:953 etc.)
func (s *Server) hListOverview(l *userList) Handler {
	return func(c *Ctx) string {
		site := c.Site()
		name := l.name(site)
		if id := c.ParamS("delete"); truthy(id) {
			row := s.E.FindListItem(l.table, id)
			if c.Process(func() { s.deleteItem(l, row, site) }) {
				c.ForwardHome("success", "The "+name+" has been deleted successfully", "settings/"+l.path+"_overview/")
			}
		}
		items := tt.NewList()
		for _, r := range s.E.OrderedList(l.table) {
			items.Items = append(items.Items, newRow(l.table, r))
		}
		plural := l.plural(site)
		return c.Template("layouts/page_overview_name_only", tt.HashOf(
			"page", l.page,
			"page_title", "Manage "+plural,
			"page_description", l.description(name),
			"table_title", "Manage "+plural+" table",
			"table_column_label", "Name",
			"item_type", name,
			"add_path", "settings/"+l.path+"_add",
			"edit_path", "settings/"+l.path+"_edit",
			"back_url", "/settings/",
			"items", items,
		), nil)
	}
}

// listItem is the item of the add and edit pages: the row with the keys
// set by the route (a plain hash for a row not found, as Perl
// autovivifies one)
func (c *Ctx) listItem(l *userList, row db.Row, found bool, name, description string) any {
	back := c.Base() + "settings/" + l.path + "_overview/"
	if !found {
		return tt.HashOf("type", name, "description", description, "back_url", back, "field_label", ucfirst(name))
	}
	r := newRow(l.table, row)
	r.set("type", func([]any) any { return name })
	r.set("description", func([]any) any { return description })
	r.set("back_url", func([]any) any { return back })
	r.set("field_label", func([]any) any { return ucfirst(name) })
	return r
}

// any '/settings/<list>_add/' (GADS.pm:982 etc.)
func (s *Server) hListAdd(l *userList) Handler {
	return func(c *Ctx) string {
		site := c.Site()
		name := l.name(site)
		row := db.Row{}
		if v, _ := c.BodyParam("submit"); truthy(v) {
			n, ok := c.BodyParam("name")
			var nv any
			if ok {
				nv = n
			}
			row["name"] = nv
			if c.Process(func() { s.E.SaveListItem(l.table, nil, nv) }) {
				c.ForwardHome("success", "The "+name+" has been created successfully", "settings/"+l.path+"_overview/")
			}
		}
		return c.Template("layouts/page_save_name_only", tt.HashOf(
			"page", l.page,
			"item", c.listItem(l, row, true, name, "In this window you can add a "+name+" to assign to users."),
		), nil)
	}
}

// any '/settings/<list>_edit/:id' (GADS.pm:1009 etc.)
func (s *Server) hListEdit(l *userList) Handler {
	return func(c *Ctx) string {
		site := c.Site()
		name := l.name(site)
		id, _ := c.RouteParam("id")
		row := s.E.FindListItem(l.table, id)
		overview := "settings/" + l.path + "_overview/"
		del := func() {
			if truthy(c.ParamS("delete")) {
				if c.Process(func() { s.deleteItem(l, row, site) }) {
					c.ForwardHome("success", "The "+name+" has been deleted successfully", overview)
				}
			}
		}
		if l.deleteFirst {
			del()
		}
		if v, _ := c.BodyParam("submit"); truthy(v) {
			if row == nil {
				gads.Panic(`Can't call method "name" on an undefined value`)
			}
			n, ok := c.BodyParam("name")
			var nv any
			if ok {
				nv = n
			}
			row["name"] = nv
			if c.Process(func() { s.E.SaveListItem(l.table, row, nv) }) {
				c.ForwardHome("success", "The "+name+" has been "+l.edited+" successfully", overview)
			}
		}
		if !l.deleteFirst {
			del()
		}
		return c.Template("layouts/page_save_name_only", tt.HashOf(
			"page", l.page,
			"item", c.listItem(l, row, row != nil, name, "In this window you can edit a "+name+". Changes will impact all users currently assigned if you delete or edit a value."),
		), nil)
	}
}
