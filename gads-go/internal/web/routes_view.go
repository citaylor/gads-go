package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// any ['get','post'] => /:layout_name/view/:id (GADS.pm:3468)
func (s *Server) hView(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	lo := layout.Identifier()
	if !layout.UserCan("view_create") {
		c.ForwardHome("danger", "You do not have permission to edit views", lo+"/data")
	}
	viewIDS, _ := c.Param("id")
	if !reDigitsOnly.MatchString(viewIDS) {
		gads.Fail("Invalid view ID: %s", viewIDS)
	}
	clone, _ := c.Param("clone")
	if truthy(clone) && !c.IsPost() {
		viewIDS = clone
	}
	update, _ := c.Param("update")
	var view *gads.View
	if truthy(viewIDS) {
		id, _ := toInt64(viewIDS)
		// The view is retrieved when first needed: for the page (and a
		// deletion) once the messages have been taken for the template
		if err := gads.Try(func() { view = s.E.LoadView(id, layout) }); err != nil {
			if !truthy(update) && !truthy(clone) {
				c.SessionSet("messages", []*Message{})
			}
			panic(err)
		}
	} else {
		view = s.E.NewView(layout)
	}
	if ou, ok := toInt64(c.SessionGet("views_other_user_id")); ok && truthy(c.SessionGet("views_other_user_id")) {
		view.OtherUserID = &ou
	}
	// A clone of a full shared view by a user who can only share views
	// with groups is not shared by default
	if truthy(clone) && view.GroupID == nil && !layout.UserCan("layout") {
		view.Global = false
	}
	iid := fmtInt(layout.InstanceID)
	if truthy(update) {
		var columns []int64
		for _, v := range c.ParamAll("column") {
			id, _ := toInt64(v)
			columns = append(columns, id)
		}
		view.SetColumns(columns)
		view.Global = c.ParamS("viewType") == "global"
		view.IsAdmin = c.ParamS("viewType") == "admin"
		view.GroupID = nil
		if g, ok := toInt64(c.ParamS("group_id")); ok && g != 0 {
			view.GroupID = &g
		}
		view.Name = c.ParamS("name")
		view.Filter().SetJSON(c.ParamS("filter"))
		view.SetSorts(c.body.getAll("sortfield"), c.body.getAll("sorttype"))
		view.SetGroups(c.body.getAll("groupfield"))
		if c.Process(func() { view.Write(false, false) }) {
			// The view created or edited becomes the current view, without
			// any search or custom sort
			autoviv(c.persistent(), "view")[iid] = view.ID
			c.SessionSet("search", "")
			c.SessionSet("sort", nil)
			c.ForwardHome("success", "The view has been updated successfully", lo+"/data?table_clear_state=1")
		}
	}
	if v, _ := c.Param("delete"); truthy(v) {
		autoviv(c.persistent(), "view")[iid] = nil
		if c.Process(view.Delete) {
			c.ForwardHome("success", "The view has been deleted successfully", lo+"/data?table_clear_state=1")
		}
	}
	page := "view"
	if truthy(clone) {
		page = "view/clone"
	} else if !truthy(c.ParamS("id")) {
		page = "view/0"
	}
	last := "Add view"
	if truthy(clone) {
		last = "Clone view"
	} else if truthy(viewIDS) {
		last = "Edit view: " + view.Name
	}
	base := c.Base()
	var cloneTok any
	if v, ok := c.Param("clone"); ok {
		cloneTok = v
	}
	sortTypes := tt.NewList()
	for _, st := range [][2]string{{"asc", "Ascending"}, {"desc", "Descending"}, {"random", "Random"}} {
		sortTypes.Items = append(sortTypes.Items, tt.HashOf("name", st[0], "description", st[1]))
	}
	return c.Template("view", tt.HashOf(
		"layout", c.ttLayout(layout),
		"sort_types", sortTypes,
		"view_edit", c.ttView(view),
		"clone", cloneTok,
		"page", page,
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/data",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb("", last),
		),
	), nil)
}
