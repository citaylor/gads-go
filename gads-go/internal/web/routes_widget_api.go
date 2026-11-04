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
		d = c.E.DashboardByID(n)
	}
	if d == nil {
		c.Status(404)
		gads.Fail("Dashboard %s not found", id)
	}
	// Hardened (M-2): a personal dashboard is only its owner's
	if c.E.Config.Hardened && d.UserID != nil && *d.UserID != c.LoggedInUser().ID {
		c.Status(403)
		gads.Fail("User does not have access to dashboard ID %s", id)
	}
	return d
}

// getDashboardWrite is API.pm _get_dashboard_write
func (s *Server) getDashboardWrite(c *Ctx, id string, layout *gads.Layout) *gads.Dashboard {
	d := s.getDashboard(c, id)
	user := c.LoggedInUser()
	if c.E.Config.Hardened {
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
	row := c.E.DB.QueryRow(`SELECT "me".* FROM "widget" "me" WHERE "me"."dashboard_id" = ? AND "me"."grid_id" = ? ORDER BY "me"."id" LIMIT 1`, dashboardID, gridID)
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

// post '/api/dashboard/:dashboard_id/widget' (API.pm:493, 526)
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
	gads.ValidateWidget(d, m)
	return c.apiSuccess(d.CreateWidget(m))
}

// put '/api/dashboard/:dashboard_id/dashboard/:id' (API.pm:498, 530):
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
		// (update_or_create: the widget is validated with its new values,
		// so a new one becomes a notice)
		if row := c.E.DB.QueryRow(`SELECT * FROM "widget" WHERE "dashboard_id" = ? AND "grid_id" = ?`, d.ID, grid); row != nil {
			for k, v := range vals {
				row[k] = v
			}
			gads.ValidateWidget(d, row)
			c.E.DB.UpdateMap("widget", vals, map[string]any{"id": row["id"]})
		} else {
			vals["dashboard_id"] = d.ID
			vals["grid_id"] = grid
			gads.ValidateWidget(d, vals)
			c.E.DB.InsertMap("widget", vals)
		}
	}
	return c.apiSuccess("Dashboard updated successfully")
}

// get '/api/dashboard/:dashboard_id/widget/:id' (API.pm:502, 534): with the
// table prefix, the record of a record dashboard is query current_id
func (s *Server) hWidgetGet(c *Ctx, layout *gads.Layout) string {
	w, _ := s.getWidget(c, layout)
	if layout != nil {
		if v, ok := c.QueryParam("current_id"); ok {
			w.CurrentID = v
		}
	}
	return c.widgetHTML(w)
}

// get '/api/dashboard/:dashboard_id/widget/:id/edit' (API.pm:508, 539)
func (s *Server) hWidgetEditForm(c *Ctx, layout *gads.Layout) string {
	w, _ := s.getWidgetWrite(c, layout)
	d := w.Dashboard(c.E)
	typ, _ := db.AsString(w.Row["type"])
	widget := ttValue(map[string]any(w.Row)).(*tt.Hash)
	widget.Set("dashboard", c.ttDashboard(d))
	params := tt.HashOf("widget", widget, "widget_options", ttValue(w.OptionsInflated()))
	if typ != "notice" {
		if layout == nil {
			gads.Panic(`Can't call method "instance_id" on an undefined value`)
		}
		params.Set("user_views", ttUserViews(c.E.NewViews(layout)))
		params.Set("columns_read", c.ttColumns(layout.All(gads.AllOptions{UserCanRead: true})))
	}
	if typ == "graph" {
		var personal, shared []any
		for _, g := range c.E.AllGraphs(layout, c.LoggedInUser()) {
			if g.IsShared {
				shared = append(shared, c.ttGraph(g))
			} else {
				personal = append(personal, c.ttGraph(g))
			}
		}
		params.Set("graphs", tt.HashOf("all_personal", tt.NewList(personal...), "all_shared", tt.NewList(shared...)))
	}
	content := c.Template("widget", params, &TemplateOptions{NoLayout: true})
	// Keep consistent with return type generated on error
	return gads.EncodeJSON(map[string]any{"is_error": int64(0), "content": content})
}

// put '/api/dashboard/:dashboard_id/widget/:id/edit' (API.pm:513, 543)
func (s *Server) hWidgetEdit(c *Ctx, layout *gads.Layout) string {
	w, _ := s.getWidgetWrite(c, layout)
	d := w.Dashboard(c.E)
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
	// The options keep the JSON values as sent (numbers stay numbers)
	columnIDs := func() any {
		if l, ok := body["layout_id"].([]any); ok {
			return l
		}
		return []any{body["layout_id"]}
	}
	typ, _ := db.AsString(w.Row["type"])
	switch {
	case typ == "notice":
		set["content"] = apiValue(body["content"])
	case typ == "graph":
		set["graph_id"] = apiValue(body["graph_id"])
		set["view_id"] = apiValue(body["view_id"])
	case typ == "table" && d.IsRecord():
		columnID := body["layout_id"]
		set["options"] = gads.EncodeJSON(map[string]any{"column_id": columnID})
		set["rows"] = apiValue(body["rows"])
		set["view_id"] = apiValue(body["view_id_"+fmtAny(apiValue(columnID))])
	case typ == "table":
		set["rows"] = apiValue(body["rows"])
		set["view_id"] = apiValue(body["view_id"])
	case typ == "timeline" && d.IsRecord():
		set["options"] = gads.EncodeJSON(map[string]any{
			"column_ids": columnIDs(), "timeline_range": body["timeline_range"],
		})
	case typ == "timeline":
		set["options"] = gads.EncodeJSON(map[string]any{
			"label": body["tl_label"], "group": body["tl_group"],
			"color": body["tl_color"], "overlay": body["tl_overlay"],
		})
		set["view_id"] = apiValue(body["view_id"])
	case typ == "globe":
		set["options"] = gads.EncodeJSON(map[string]any{
			"label": body["globe_label"], "group": body["globe_group"],
			"color": body["globe_color"],
		})
		set["view_id"] = apiValue(body["view_id"])
	case typ == "field":
		set["options"] = gads.EncodeJSON(map[string]any{
			"column_ids": columnIDs(), "show_field_labels": truthy(apiValue(body["show_field_labels"])),
		})
	}
	row := map[string]any{}
	for k, v := range w.Row {
		row[k] = v
	}
	for k, v := range set {
		row[k] = v
	}
	gads.ValidateWidget(d, row)
	c.E.DB.UpdateMap("widget", set, map[string]any{"id": w.Row["id"]})
	return c.apiSuccess("Widget updated successfully")
}

// del '/api/dashboard/:dashboard_id/widget/:id' (API.pm:531, 503)
func (s *Server) hWidgetDelete(c *Ctx, layout *gads.Layout) string {
	w, _ := s.getWidgetWrite(c, layout)
	c.E.DB.Exec(`DELETE FROM "widget" WHERE "id" = ?`, w.Row["id"])
	return c.apiSuccess("Widget deleted successfully")
}

func fmtAny(v any) string {
	if n, ok := toInt64(v); ok {
		return fmtInt(n)
	}
	s, _ := db.AsString(v)
	return s
}
