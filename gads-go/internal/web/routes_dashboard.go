package web

import (
	"encoding/base64"
	"regexp"

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
	d := c.E.FindDashboard(id, user, layout)
	if d == nil {
		d = c.E.SharedDashboard(user, layout, c.Site())
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
		"dashboards_json", c.E.DashboardsJSON(user, nil, c.Site()),
		"page", "index",
		"content_block_main_custom_classes", "pt-0",
		"content_block_custom_classes", "ps-0",
	)
	if dl, _ := c.Param("download"); truthy(dl) {
		params.Set("readonly", int64(1))
		if dl == "pdf" {
			c.sendPDF(c.pageAsMech("index", params, mechOptions{PDF: true}))
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
		"dashboards_json", c.E.DashboardsJSON(user, layout, c.Site()),
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
			c.sendPDF(c.pageAsMech("index", params, mechOptions{PDF: true}))
		}
	}
	return c.Template("index", params, nil)
}

// GET /table/? (GADS.pm:1415)
func (s *Server) hTables(c *Ctx) string {
	instances := tt.NewList()
	for _, r := range c.E.DB.Query(`SELECT * FROM "instance" "me"` + siteWhereMe(c.E)) {
		instances.Items = append(instances.Items, newRow("instance", r))
	}
	layouts := tt.NewList()
	for _, l := range c.Instances().All() {
		layouts.Items = append(layouts.Items, c.ttLayout(l))
	}
	groups := tt.NewList()
	for _, r := range c.E.DB.Query(`SELECT "me"."id", "me"."name" FROM "group" "me"` + siteWhereMe(c.E) + ` ORDER BY "me"."name"`) {
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
	case "is_record":
		return one(ttBoolEmpty(d.IsRecord()))
	case "type":
		return one(ttValue(d.Type))
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
		content := ttValue(w.Row["content"])
		// Hardened (H-5): Perl renders the HTML of a notice as given, so
		// that whoever can edit a dashboard can add scripts to it
		if c.E.Config.Hardened {
			content = noticePolicy.Sanitize(fmtAny(w.Row["content"]))
		}
		params.Set("content", content)
	} else if out, done := c.widgetParams(w, typ, params); done {
		return out
	}
	out, err := c.s.widgetTT().Process("snippets/widget_content.tt", params)
	if err != nil {
		gads.Panic("%s", err.Error())
	}
	return out
}

// widgetParams sets the template parameters of a widget other than a
// notice, or returns the content to return as it is
func (c *Ctx) widgetParams(w *gads.Widget, typ string, params *tt.Hash) (string, bool) {
	layout := w.Layout
	if layout == nil {
		gads.Panic("Missing layout")
	}
	user := layout.User()
	viewID, _ := toInt64(w.Row["view_id"])
	view := c.E.NewViewID(viewID, layout)
	rows, _ := toInt64(w.Row["rows"])
	graphID, _ := toInt64(w.Row["graph_id"])
	notice := func(content string) {
		params.Set("type", "notice")
		params.Set("content", content)
	}
	b64 := func(v any) string { return base64.StdEncoding.EncodeToString([]byte(gads.EncodeJSON(v))) }
	isRecord := w.Dashboard(c.E).IsRecord()
	switch {
	case typ == "table" && isRecord:
		// The field from the record from which the referenced table is
		// being rendered
		options := w.OptionsInflated()
		var source *gads.Column
		if id, ok := toInt64(apiValue(options["column_id"])); ok {
			source = layout.Column(id)
		}
		switch {
		case viewID == 0 || rows == 0 || source == nil:
			notice("Configuration required")
		case c.E.Config.Hardened && !c.E.NewViewID(viewID, source.LayoutParent()).CurrentUserHasAccess():
			// Hardened (M-1): the view is that of the table referred to,
			// found there and checked with its permissions
			notice("You do not have access to the configured view for this widget")
		case view.CurrentUserHasAccess():
			// The view of the table referred to, limited to the records
			// that refer to this record (or that it refers to)
			layoutParent := source.LayoutParent()
			if c.E.Config.Hardened {
				view = c.E.NewViewID(viewID, layoutParent)
			}
			var remote *gads.Column
			if source.Type == "curval" {
				for _, col := range layoutParent.All(gads.AllOptions{}) {
					if col.Type == "autocur" && col.RelatedField() != nil && col.RelatedField().ID == source.ID {
						remote = col
						break
					}
				}
			} else {
				remote = source.RelatedField()
			}
			if remote == nil {
				// Graceful error
				notice("The table that this value refers to requires an autocur field")
				break
			}
			// Temporarily set the new filter, but do not write it
			filter := map[string]any{
				"rules": []any{
					map[string]any{"id": remote.ID, "type": "string", "value": w.CurrentID, "operator": "equal"},
					view.Filter().AsHash(),
				},
				"operator": "AND",
			}
			view.SetFilter(gads.NewFilterHash(filter, layoutParent))
			records := c.E.NewRecords(layoutParent, user)
			records.Page = 1
			records.View = view
			records.Rows = int(rows)
			c.widgetTable(records, layoutParent, params)
		default:
			// Fail cleanly with a useful message
			notice("You do not have access to the configured view for this widget")
		}
	case typ == "table":
		switch {
		case viewID == 0 || rows == 0:
			notice("Configuration required")
		case view.CurrentUserHasAccess():
			records := c.E.NewRecords(layout, user)
			records.Page = 1
			records.View = view
			records.Rows = int(rows)
			c.widgetTable(records, layout, params)
		default:
			notice("You do not have access to the configured view for this widget")
		}
	case typ == "graph":
		if viewID == 0 || graphID == 0 {
			return "Configuration required", true
		}
		records := c.E.NewRecordsGraph(layout, user)
		data := c.E.LoadGraph(graphID, layout, user).Data(records, view)
		params.Set("plot_data", b64(map[string]any{
			"points":  data.Points,
			"labels":  data.LabelsEncoded(),
			"xlabels": data.XLabels,
			"options": data.Options,
		}))
		g := c.E.LoadGraph(graphID, layout, user)
		var yLabel any
		if g.YAxisLabel != "" {
			yLabel = g.YAxisLabel
		}
		params.Set("graph_id", graphID)
		params.Set("plot_options", b64(map[string]any{
			"type":         g.Type,
			"x_axis_name":  emptyNil(g.XAxisName()),
			"y_axis_label": yLabel,
			"stackseries":  g.Stackseries,
			"showlegend":   g.Showlegend(),
			"id":           g.ID,
		}))
	case typ == "timeline" && isRecord:
		options := w.OptionsInflated()
		rng, _ := toInt64(apiValue(options["timeline_range"]))
		if rng == 0 {
			rng = 12
		}
		var lower, upper int
		switch rng {
		case 3:
			lower, upper = 1, 2
		case 6:
			lower, upper = 1, 5
		case 12:
			lower, upper = 2, 10
		default: // 24
			lower, upper = 3, 21
		}
		// Columns can be IDs of this table, or parent_child IDs of
		// curcommons, which are retrieved as the parent only (so their
		// dates show only if the curcommon displays the field)
		columnIDs := []int64{}
		if l, ok := options["column_ids"].([]any); ok {
			for _, id := range l {
				s := tt.Str(apiValue(id))
				if m := reColumnParent.FindStringSubmatch(s); m != nil {
					s = m[1]
				}
				n, _ := toInt64(s)
				columnIDs = append(columnIDs, n)
			}
		}
		records := c.E.NewRecords(layout, user)
		cid, _ := toInt64(w.CurrentID)
		records.SetCurrentIDs([]int64{cid})
		records.SetColumns(columnIDs)
		now := c.E.Now()
		from, to := now.AddDate(0, -lower, 0), now.AddDate(0, upper, 0)
		records.From, records.To = &from, &to
		data := records.DataTimeline(gads.TimelineOptions{LabelAsField: true})
		params.Set("records", b64(timelineItems(data)))
		params.Set("dashboard", int64(1))
		params.Set("timeline", timelineParams(data))
	case typ == "timeline":
		if viewID == 0 {
			return "Configuration required", true
		}
		records := c.E.NewRecords(layout, user)
		records.View = view
		// No "to": the number of records is taken from today
		now := c.E.Now()
		records.From = &now
		options := w.OptionsInflated()
		num := func(k string) int64 { n, _ := toInt64(apiValue(options[k])); return n }
		data := records.DataTimeline(gads.TimelineOptions{
			Label:          num("label"),
			Group:          num("group"),
			Color:          num("color"),
			AllGroupValues: truthy(apiValue(options["all_group_values"])),
			Overlay:        num("overlay"),
		})
		params.Set("records", b64(timelineItems(data)))
		params.Set("groups", b64(timelineGroups(data)))
		params.Set("dashboard", int64(1))
		params.Set("view_id", view.ID)
		params.Set("timeline", timelineParams(data))
	case typ == "globe":
		if viewID == 0 {
			return "Configuration required", true
		}
		options := w.OptionsInflated()
		globe := c.E.NewGlobe(gads.GlobeOptions{
			Layout:     layout,
			User:       user,
			View:       view,
			GroupColID: fmtAny(apiValue(options["group"])),
			ColorColID: fmtAny(apiValue(options["color"])),
			LabelColID: fmtAny(apiValue(options["label"])),
		})
		data := globe.Data()
		params.Set("globe_data", b64(map[string]any{"data": data.Data, "params": data.Params}))
	case typ == "field":
		options := w.OptionsInflated()
		if !truthy(w.CurrentID) {
			gads.Fail("No record ID provided for field widget %s", fmtAny(w.Row["id"]))
		}
		records := c.E.NewRecords(layout, user)
		cid, _ := toInt64(w.CurrentID)
		records.SetCurrentIDs([]int64{cid})
		// (all the columns if none are configured)
		if l, ok := options["column_ids"].([]any); ok {
			columnIDs := []int64{}
			for _, id := range l {
				n, _ := toInt64(apiValue(id))
				columnIDs = append(columnIDs, n)
			}
			records.SetColumns(columnIDs)
		}
		record := records.Single()
		if record == nil {
			gads.Fail("Record ID %s not found", w.CurrentID)
		}
		params.Set("record", c.recordPresentation(record, recordPresentOpts{}))
		params.Set("show_field_labels", ttValue(options["show_field_labels"]))
	}
	return "", false
}

var reColumnParent = regexp.MustCompile(`([0-9]+)_`)

// widgetTable sets the parameters of a table widget: the first page of the
// records
func (c *Ctx) widgetTable(records *gads.Records, layout *gads.Layout, params *tt.Hash) {
	columns := records.ColumnsRender()
	list := tt.NewList()
	for _, r := range records.Results() {
		list.Items = append(list.Items, c.recordPresentation(r, recordPresentOpts{}))
	}
	params.Set("records", list)
	params.Set("layout", c.ttLayout(layout))
	sortFirst := records.SortFirst()
	cols := tt.NewList()
	for _, col := range columns {
		cols.Items = append(cols.Items, c.columnPresentation(col, presentOpts{sort: sortFirst}))
	}
	params.Set("columns", cols)
}

func (s *Server) widgetTT() *tt.Engine {
	s.ttDefaultOnce.Do(func() {
		s.ttDefault = tt.NewEngine(s.Views)
		s.ttDefault.NoAnyCase = true
	})
	return s.ttDefault
}
