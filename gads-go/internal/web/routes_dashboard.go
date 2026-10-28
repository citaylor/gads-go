package web

import (
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// crumb is GADS::Helper::BreadCrumbs Crumb
func crumb(href, text string) *tt.Hash {
	isLink := int64(0)
	if href != "" {
		isLink = 1
	}
	return tt.HashOf("text", text, "href", href, "is_link", isLink)
}

// dashboardFor returns the selected (or shared) dashboard of the home page
// (layout nil) or a table, remembering a selection made with did
func (s *Server) dashboardFor(c *Ctx, layout *gads.Layout) *gads.Dashboard {
	user := c.LoggedInUser()
	key := "0"
	if layout != nil {
		key = fmtInt(layout.InstanceID)
	}
	persistent := c.persistent()
	if did, _ := c.QueryParam("did"); truthy(did) {
		dash, ok := persistent["dashboard"].(map[string]any)
		if !ok {
			dash = map[string]any{}
			persistent["dashboard"] = dash
		}
		dash[key] = did
	}
	var id int64
	if dash, ok := persistent["dashboard"].(map[string]any); ok {
		id, _ = toInt64(dash[key])
	}
	d := s.E.FindDashboard(id, user, layout)
	if d == nil {
		d = s.E.SharedDashboard(user, layout, c.Site())
	}
	return d
}

// GET / (GADS.pm:389)
func (s *Server) hIndex(c *Ctx) string {
	user := c.LoggedInUser()
	d := s.dashboardFor(c, nil)
	params := tt.HashOf(
		"readonly", ttBoolEmpty(d.IsShared() && !user.Permission()["superadmin"]),
		"dashboard", c.ttDashboard(d),
		"dashboards_json", s.E.DashboardsJSON(user, nil, c.Site()),
		"page", "index",
		"content_block_main_custom_classes", "pt-0",
		"content_block_custom_classes", "ps-0",
	)
	if dl, _ := c.Param("download"); truthy(dl) {
		params.Set("readonly", int64(1))
		if dl == "pdf" {
			gads.Panic("PDF download of dashboards is not supported")
		}
	}
	return c.Template("index", params, nil)
}

// GET /:layout_name/? (GADS.pm:2099)
func (s *Server) hTableIndex(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	d := s.dashboardFor(c, layout)
	base := c.Base()
	params := tt.HashOf(
		"readonly", ttBoolEmpty(d.IsShared() && !layout.UserCan("layout")),
		"dashboard", c.ttDashboard(d),
		"dashboards_json", s.E.DashboardsJSON(user, layout, c.Site()),
		"page", "table_index",
		"header_type", "table_tabs",
		"content_block_custom_classes", "ps-0",
		"content_block_main_custom_classes", "pt-0",
		"header_back_url", base+"table",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+layout.Identifier()+"/data", "Table: "+layout.Name()),
			crumb("", "Dashboard"),
		),
	)
	if dl, _ := c.Param("download"); truthy(dl) {
		params.Set("readonly", int64(1))
		if dl == "pdf" && !truthy(layout.Row()["no_download_pdf"]) {
			gads.Panic("PDF download of dashboards is not supported")
		}
	}
	return c.Template("index", params, nil)
}

// GET /table/? (GADS.pm:1415)
func (s *Server) hTables(c *Ctx) string {
	instances := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT * FROM "instance" "me"` + siteWhereMe(s.E)) {
		instances.Items = append(instances.Items, newRow("instance", r))
	}
	layouts := tt.NewList()
	for _, l := range c.Instances().All() {
		layouts.Items = append(layouts.Items, c.ttLayout(l))
	}
	groups := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT "me"."id", "me"."name" FROM "group" "me"` + siteWhereMe(s.E) + ` ORDER BY "me"."name"`) {
		groups.Items = append(groups.Items, tt.HashOf("id", ttValue(r["id"]), "name", ttValue(r["name"])))
	}
	return c.Template("tables", tt.HashOf(
		"page", "table",
		"instances", instances,
		"instance_layouts", layouts,
		"instances_object", nil,
		"groups", groups,
		"permission_inputs", permissionInputs(),
	), nil)
}

func siteWhereMe(e *gads.Engine) string {
	if e.SiteID == 0 {
		return ""
	}
	return ` WHERE "me"."site_id" = ` + itoa(e.SiteID)
}

// permissionInputs is GADS::Type::Permissions->permission_inputs
func permissionInputs() *tt.Hash {
	return tt.HashOf(
		"read", "Values can be read",
		"write_new", "Values can be written to new records",
		"write_existing", "Modifications can be made to existing records",
		"approve_new", "Values for new records can be approved",
		"approve_existing", "Modifications to existing records can be approved",
		"write_new_no_approval", "Values for new records do not require approval",
		"write_existing_no_approval", "Modifications to existing records do not require approval",
	)
}

// ttDashboardObj is GADS::Schema::Result::Dashboard
type ttDashboardObj struct {
	c *Ctx
	d *gads.Dashboard
}

func (c *Ctx) ttDashboard(d *gads.Dashboard) any { return &ttDashboardObj{c: c, d: d} }

func (o *ttDashboardObj) TTMethod(name string, args []any) ([]any, bool, error) {
	d := o.d
	one := func(v any) ([]any, bool, error) { return []any{v}, true, nil }
	switch name {
	case "id":
		return one(d.ID)
	case "instance_id":
		return one(ttValue(d.InstanceID))
	case "user_id":
		return one(ttValue(d.UserID))
	case "name":
		return one(d.Name())
	case "url":
		return one(d.URL())
	case "download_url":
		return one(d.DownloadURL())
	case "as_json":
		return one(d.AsJSON())
	case "is_shared":
		return one(ttBoolEmpty(d.IsShared()))
	case "is_empty":
		return one(ttBoolEmpty(d.IsEmpty()))
	case "display_widgets":
		l := tt.NewList()
		for _, w := range d.DisplayWidgets() {
			l.Items = append(l.Items, tt.HashOf(
				"html", o.c.widgetHTML(w),
				"grid", w.Grid(d.IsShared()),
			))
		}
		return one(l)
	}
	return nil, false, nil
}

// widgetHTML is Widget->html: the content rendered with
// snippets/widget_content.tt (by a Template object with default options)
func (c *Ctx) widgetHTML(w *gads.Widget) string {
	typ, _ := db.AsString(w.Row["type"])
	params := tt.HashOf("type", typ, "title", ttValue(w.Row["title"]))
	if typ == "notice" {
		params.Set("content", ttValue(w.Row["content"]))
	} else {
		if w.Layout == nil {
			gads.Panic("Missing layout")
		}
		gads.Panic("Widgets of type %s are not yet supported", typ)
	}
	out, err := c.s.widgetTT().Process("snippets/widget_content.tt", params)
	if err != nil {
		gads.Panic("%s", err.Error())
	}
	return out
}

func (s *Server) widgetTT() *tt.Engine {
	if s.ttDefault == nil {
		s.ttDefault = tt.NewEngine(s.Views)
		s.ttDefault.NoAnyCase = true
	}
	return s.ttDefault
}
