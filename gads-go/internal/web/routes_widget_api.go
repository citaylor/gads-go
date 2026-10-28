package web

import (
	"bytes"
	"encoding/json"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// The dashboard and widget routes of lib/GADS/API.pm, for site dashboards
// and (with the table prefix) table dashboards

// widgetAPI returns the handler of a widget route: with prefix, the route
// passes unless the table of the prefix exists (var('layout') or pass)
func (s *Server) widgetAPI(prefix bool, h func(c *Ctx, layout *gads.Layout) string) Handler {
	return func(c *Ctx) string {
		var layout *gads.Layout
		if prefix {
			if layout = c.Layout(); layout == nil {
				c.Pass()
			}
		}
		return h(c, layout)
	}
}

// apiSuccess is API.pm _success
func (c *Ctx) apiSuccess(msg string) string {
	c.ContentType("application/json; charset=UTF-8")
	return gads.EncodeJSON(map[string]any{"is_error": int64(0), "message": msg})
}

// getDashboard is API.pm _get_dashboard
func (s *Server) getDashboard(c *Ctx, id string) *gads.Dashboard {
	n, ok := toInt64(id)
	var d *gads.Dashboard
	if ok {
		d = s.E.DashboardByID(n)
	}
	if d == nil {
		c.Status(404)
		gads.Fail("Dashboard %s not found", id)
	}
	// Hardened (M-2): a personal dashboard is only its owner's
	if s.E.Config.Hardened && d.UserID != nil && *d.UserID != c.LoggedInUser().ID {
		c.Status(403)
		gads.Fail("User does not have access to dashboard ID %s", id)
	}
	return d
}

// getDashboardWrite is API.pm _get_dashboard_write
func (s *Server) getDashboardWrite(c *Ctx, id string, layout *gads.Layout) *gads.Dashboard {
	d := s.getDashboard(c, id)
	user := c.LoggedInUser()
	if s.E.Config.Hardened {
		if s.canWriteDashboard(c, d) {
			return d
		}
	} else if user.Permission()["superadmin"] || (layout != nil && layout.UserCan("layout")) {
		return d
	}
	if d.UserID != nil && *d.UserID == user.ID {
		return d
	}
	c.Status(403)
	gads.Fail("User does not have write access to dashboard ID %s", id)
	return nil
}

// canWriteDashboard is whether the user can change a dashboard, by the
// dashboard's own scope (hardened, H-5). Perl allows anyone with the
// layout permission of the table in the URL to change any dashboard,
// including the site's home dashboard.
func (s *Server) canWriteDashboard(c *Ctx, d *gads.Dashboard) bool {
	user := c.LoggedInUser()
	switch {
	case d.UserID != nil: // personal
		return *d.UserID == user.ID
	case d.InstanceID == nil: // the site's
		return user.Permission()["superadmin"]
	default: // a table's
		if user.Permission()["superadmin"] {
			return true
		}
		l := c.Instances().Layout(*d.InstanceID)
		return l != nil && l.UserCan("layout")
	}
}

// getWidget is API.pm _get_widget
func (s *Server) getWidget(c *Ctx, layout *gads.Layout) (*gads.Widget, *gads.Dashboard) {
	dashboardID, _ := c.RouteParam("dashboard_id")
	gridID, _ := c.RouteParam("id")
	row := s.E.DB.QueryRow(`SELECT "me".* FROM "widget" "me" WHERE "me"."dashboard_id" = ? AND "me"."grid_id" = ? ORDER BY "me"."id" LIMIT 1`, dashboardID, gridID)
	if row == nil {
		c.Status(404)
		gads.Fail("Widget ID %s not found", gridID)
	}
	// Check access. Borks if no access
	d := s.getDashboard(c, fmtAny(row["dashboard_id"]))
	return &gads.Widget{Row: row, Layout: layout}, d
}

// getWidgetWrite is API.pm _get_widget_write
func (s *Server) getWidgetWrite(c *Ctx, layout *gads.Layout) (*gads.Widget, *gads.Dashboard) {
	w, d := s.getWidget(c, layout)
	// Check write access to dashboard, borks on unauth
	s.getDashboardWrite(c, fmtAny(w.Row["dashboard_id"]), layout)
	return w, d
}

// post '/api/dashboard/:dashboard_id/widget' (API.pm:511, 478)
func (s *Server) hWidgetCreate(c *Ctx, layout *gads.Layout) string {
	typ, _ := c.QueryParam("type")
	id, _ := c.RouteParam("dashboard_id")
	d := s.getDashboardWrite(c, id, layout)
	m := map[string]any{"type": emptyNil(typ)}
	if typ == "notice" {
		m["content"] = "This is a new notice widget - click edit to update the contents"
	} else {
		m["content"] = nil
	}
	return c.apiSuccess(d.CreateWidget(m))
}

// put '/api/dashboard/:dashboard_id/dashboard/:id' (API.pm:515, 483):
// _update_dashboard
func (s *Server) hDashboardUpdate(c *Ctx, layout *gads.Layout) string {
	id, _ := c.RouteParam("id")
	d := s.getDashboardWrite(c, id, layout)
	var widgets []map[string]any
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.UseNumber()
	if err := dec.Decode(&widgets); err != nil {
		gads.Panic("malformed JSON string: %s", err)
	}
	for _, w := range widgets {
		// Static widgets added to personal dashboards will be passed in, but
		// are added anyway on dashboard render
		if truthy(apiValue(w["static"])) && !d.IsShared() {
			continue
		}
		vals := map[string]any{}
		for _, k := range []string{"h", "w", "x", "y"} {
			vals[k] = apiValue(w[k])
		}
		grid := apiValue(w["i"])
		if row := s.E.DB.QueryRow(`SELECT "id" FROM "widget" WHERE "dashboard_id" = ? AND "grid_id" = ?`, d.ID, grid); row != nil {
			s.E.DB.UpdateMap("widget", vals, map[string]any{"id": row["id"]})
		} else {
			vals["dashboard_id"] = d.ID
			vals["grid_id"] = grid
			s.E.DB.InsertMap("widget", vals)
		}
	}
	return c.apiSuccess("Dashboard updated successfully")
}

// get '/api/dashboard/:dashboard_id/widget/:id' (API.pm:519, 488)
func (s *Server) hWidgetGet(c *Ctx, layout *gads.Layout) string {
	w, _ := s.getWidget(c, layout)
	return c.widgetHTML(w)
}

// get '/api/dashboard/:dashboard_id/widget/:id/edit' (API.pm:523, 493)
func (s *Server) hWidgetEditForm(c *Ctx, layout *gads.Layout) string {
	w, d := s.getWidgetWrite(c, layout)
	typ, _ := db.AsString(w.Row["type"])
	widget := ttValue(map[string]any(w.Row)).(*tt.Hash)
	widget.Set("dashboard", tt.HashOf("is_shared", ttBoolEmpty(d.IsShared())))
	inflate := func(k string) any {
		v, _ := db.AsString(w.Row[k])
		if v == "" {
			return nil
		}
		x, err := gads.DecodeJSON(v)
		if err != nil {
			gads.Panic("malformed JSON string: %s", err)
		}
		return ttValue(x)
	}
	params := tt.HashOf("widget", widget, "tl_options", inflate("tl_options"), "globe_options", inflate("globe_options"))
	if typ != "notice" {
		if layout == nil {
			gads.Panic(`Can't call method "instance_id" on an undefined value`)
		}
		views := c.E.NewViews(layout)
		uv := views.UserViews()
		list := func(vs []gads.ViewSummary) *tt.List {
			l := tt.NewList()
			for _, v := range vs {
				l.Items = append(l.Items, tt.HashOf("id", v.ID, "name", v.Name))
			}
			return l
		}
		params.Set("user_views", tt.HashOf("admin", list(uv.Admin), "shared", list(uv.Shared), "personal", list(uv.Personal)))
		params.Set("columns_read", c.ttColumns(layout.All(gads.AllOptions{UserCanRead: true})))
	}
	switch typ {
	case "notice", "table":
	default:
		gads.Panic("The edit form of %s widgets is not yet supported", typ)
	}
	content := c.Template("widget", params, &TemplateOptions{NoLayout: true})
	// Keep consistent with return type generated on error
	return gads.EncodeJSON(map[string]any{"is_error": int64(0), "content": content})
}

// put '/api/dashboard/:dashboard_id/widget/:id/edit' (API.pm:527, 498)
func (s *Server) hWidgetEdit(c *Ctx, layout *gads.Layout) string {
	w, d := s.getWidgetWrite(c, layout)
	var body map[string]any
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		gads.Panic("malformed JSON string: %s", err)
	}
	set := map[string]any{"title": apiValue(body["title"])}
	if d.IsShared() {
		if v, _ := c.QueryParam("static"); truthy(v) {
			set["static"] = int64(1)
		} else {
			set["static"] = int64(0)
		}
	}
	typ, _ := db.AsString(w.Row["type"])
	switch typ {
	case "notice":
		set["content"] = apiValue(body["content"])
	case "graph":
		set["graph_id"] = apiValue(body["graph_id"])
		set["view_id"] = apiValue(body["view_id"])
	case "table":
		set["rows"] = apiValue(body["rows"])
		set["view_id"] = apiValue(body["view_id"])
	case "timeline":
		set["tl_options"] = gads.EncodeJSON(map[string]any{
			"label": apiValue(body["tl_label"]), "group": apiValue(body["tl_group"]),
			"color": apiValue(body["tl_color"]), "overlay": apiValue(body["tl_overlay"]),
		})
		set["view_id"] = apiValue(body["view_id"])
	case "globe":
		set["globe_options"] = gads.EncodeJSON(map[string]any{
			"label": apiValue(body["globe_label"]), "group": apiValue(body["globe_group"]),
			"color": apiValue(body["globe_color"]),
		})
		set["view_id"] = apiValue(body["view_id"])
	}
	s.E.DB.UpdateMap("widget", set, map[string]any{"id": w.Row["id"]})
	return c.apiSuccess("Widget updated successfully")
}

// del '/api/dashboard/:dashboard_id/widget/:id' (API.pm:531, 503)
func (s *Server) hWidgetDelete(c *Ctx, layout *gads.Layout) string {
	w, _ := s.getWidgetWrite(c, layout)
	s.E.DB.Exec(`DELETE FROM "widget" WHERE "id" = ?`, w.Row["id"])
	return c.apiSuccess("Widget deleted successfully")
}

func fmtAny(v any) string {
	if n, ok := toInt64(v); ok {
		return fmtInt(n)
	}
	s, _ := db.AsString(v)
	return s
}
