package gads

import (
	"regexp"

	"github.com/ctrlo/gads-go/internal/db"
)

// Dashboard is a dashboard of the home page or of a table
// (GADS::Schema::Result::Dashboard and its resultset).
type Dashboard struct {
	e          *Engine
	ID         int64
	InstanceID *int64
	UserID     *int64
	// Type is table (a table's or the site's) or record (the overview of
	// a record, GADS 115); NULL in a database of before 115 that was not
	// upgraded
	Type *string
	// Layout is the layout the dashboard is displayed for (may be nil)
	Layout *Layout
	// CurrentID is the record a record dashboard is displayed for
	CurrentID string
}

// Widget is a widget of a dashboard (GADS::Schema::Result::Widget)
type Widget struct {
	Row db.Row
	// Layout is the layout of the dashboard displaying the widget
	Layout *Layout
	// CurrentID is the record a widget of a record dashboard shows (the
	// current_id of the request), 0 if none
	CurrentID string
}

func (e *Engine) dashboardFromRow(row db.Row) *Dashboard {
	d := &Dashboard{e: e}
	d.ID, _ = db.AsInt64(row["id"])
	d.InstanceID = optInt(row["instance_id"])
	d.UserID = optInt(row["user_id"])
	if t, ok := db.AsString(row["type"]); ok && row["type"] != nil {
		d.Type = &t
	}
	return d
}

// IsRecord is Dashboard->is_record: whether this is a record dashboard
func (d *Dashboard) IsRecord() bool { return d.Type != nil && *d.Type == "record" }

func (e *Engine) dashboardSite() string {
	if e.SiteID == 0 {
		return ""
	}
	return ` AND "me"."site_id" = ` + itoa(e.SiteID)
}

func instanceCond(layout *Layout) (string, []any) {
	if layout == nil {
		return `"me"."instance_id" IS NULL`, nil
	}
	return `"me"."instance_id" = ?`, []any{layout.InstanceID}
}

// FindDashboard is ResultSet::Dashboard->dashboard: the dashboard with the
// ID if it is shared or the user's, nil if there is no such dashboard, or
// an error if it belongs to another user.
func (e *Engine) FindDashboard(id int64, user *User, layout *Layout) *Dashboard {
	if id == 0 {
		return nil
	}
	if _, ok := e.DB.QueryInt64(`SELECT "me"."id" FROM "dashboard" "me" WHERE "me"."id" = ?`+e.dashboardSite(), id); !ok {
		return nil
	}
	row := e.DB.QueryRow(`SELECT "me".* FROM "dashboard" "me" WHERE "me"."id" = ? AND ("me"."user_id" IS NULL OR "me"."user_id" = ?) AND "me"."type" = 'table'`+e.dashboardSite(), id, user.ID)
	if row == nil {
		Fail("Dashboard %d not found for this user", id)
	}
	d := e.dashboardFromRow(row)
	d.Layout = layout
	return d
}

// SharedDashboard is ResultSet::Dashboard->shared_dashboard: the shared
// dashboard of the table (or the site if layout is nil), created if needed
func (e *Engine) SharedDashboard(user *User, layout *Layout, site *Site) *Dashboard {
	cond, args := instanceCond(layout)
	row := e.DB.QueryRow(`SELECT "me".* FROM "dashboard" "me" WHERE `+cond+` AND "me"."user_id" IS NULL AND "me"."type" = 'table'`+e.dashboardSite()+` LIMIT 1`, args...)
	var d *Dashboard
	if row != nil {
		d = e.dashboardFromRow(row)
	} else {
		d = e.CreateDashboard("shared", user, layout, site)
	}
	d.Layout = layout
	return d
}

func (e *Engine) personalDashboard(user *User, layout *Layout, site *Site) *Dashboard {
	cond, args := instanceCond(layout)
	args = append(args, user.ID)
	row := e.DB.QueryRow(`SELECT "me".* FROM "dashboard" "me" WHERE `+cond+` AND "me"."user_id" = ?`+e.dashboardSite()+` LIMIT 1`, args...)
	if row != nil {
		return e.dashboardFromRow(row)
	}
	return e.CreateDashboard("personal", user, layout, site)
}

const personalDashboardText = `<p>Welcome to your personal dashboard</p>
            <p>Create widgets using the Add Widget menu. Edit widgets using their
            edit button (including this one). Drag and resize widgets as required.</p>`

// CreateDashboard is ResultSet::Dashboard->create_dashboard
func (e *Engine) CreateDashboard(typ string, user *User, layout *Layout, site *Site) *Dashboard {
	var d *Dashboard
	e.DB.Txn(func() {
		m := map[string]any{"instance_id": nil, "type": "table"}
		if layout != nil {
			m["instance_id"] = layout.InstanceID
		}
		if e.SiteID != 0 {
			m["site_id"] = e.SiteID
		}
		switch typ {
		case "shared":
			id := e.InsertSite("dashboard", m)
			d = &Dashboard{e: e, ID: id, InstanceID: optInt(m["instance_id"]), Type: strPtr("table")}
			var text, text2 string
			if layout != nil {
				text, _ = db.AsString(layout.Row()["homepage_text"])
				text2, _ = db.AsString(layout.Row()["homepage_text2"])
			} else if site != nil {
				text = site.str("homepage_text")
				text2 = site.str("homepage_text2")
			}
			if text2 != "" { // Assume 2 columns of homepage
				d.createWidget(map[string]any{"type": "notice", "h": 6, "w": 6, "x": 6, "y": 0, "content": text2})
			}
			if text != "" {
				w := 12
				if text2 != "" {
					w = 6
				}
				d.createWidget(map[string]any{"type": "notice", "h": 6, "w": w, "x": 0, "y": 0, "content": text})
			}
		case "personal":
			m["user_id"] = user.ID
			id := e.InsertSite("dashboard", m)
			d = &Dashboard{e: e, ID: id, InstanceID: optInt(m["instance_id"]), UserID: &user.ID, Type: strPtr("table")}
			d.createWidget(map[string]any{"type": "notice", "h": 6, "w": 6, "x": 0, "y": 0, "content": personalDashboardText})
		default:
			Panic("Unexpected dashboard type: %s", typ)
		}
	})
	return d
}

// createWidget creates a widget, generating its grid ID (Widget
// before_create)
func (d *Dashboard) createWidget(m map[string]any) int64 {
	m["dashboard_id"] = d.ID
	if !truthy(m["grid_id"]) {
		var grid string
		for {
			if grid != "" {
				if n, _ := d.e.DB.QueryInt64(`SELECT COUNT(*) FROM "widget" "me" WHERE "me"."dashboard_id" = ? AND "me"."grid_id" = ?`, d.ID, grid); n == 0 {
					break
				}
			}
			grid = d.e.NewToken(32)
		}
		m["grid_id"] = grid
	}
	return d.e.DB.InsertMap("widget", m)
}

// AllUserDashboards is ResultSet::Dashboard->_all_user: the dashboards
// available to the user (creating their personal dashboard if needed)
func (e *Engine) AllUserDashboards(user *User, layout *Layout, site *Site) []*Dashboard {
	var out []*Dashboard
	e.DB.Txn(func() {
		if layout != nil {
			d := e.SharedDashboard(user, layout, site)
			if !d.IsEmpty() || layout.UserCan("layout") {
				out = append(out, d)
			}
			out = append(out, e.personalDashboard(user, layout, site))
		} else {
			d := e.SharedDashboard(user, nil, site)
			if !d.IsEmpty() || user.Permission()["superadmin"] {
				out = append(out, d)
			}
			out = append(out, e.personalDashboard(user, nil, site))
		}
	})
	return out
}

// DashboardsJSON is ResultSet::Dashboard->dashboards_json
func (e *Engine) DashboardsJSON(user *User, layout *Layout, site *Site) string {
	e.AllUserDashboards(user, layout, site) // As Perl, called twice
	list := []any{}
	for _, d := range e.AllUserDashboards(user, layout, site) {
		list = append(list, map[string]any{"id": d.ID, "url": d.URL(), "name": d.Name()})
	}
	return EncodeJSON(list)
}

func (d *Dashboard) instance() *Layout {
	if d.InstanceID == nil {
		return nil
	}
	if d.Layout != nil && d.Layout.InstanceID == *d.InstanceID {
		return d.Layout
	}
	return d.e.NewLayout(*d.InstanceID, nil)
}

// Name is the dashboard's name
func (d *Dashboard) Name() string {
	switch {
	case d.IsRecord():
		return "Overview"
	case d.UserID != nil && d.InstanceID != nil:
		return d.instance().Name() + " dashboard (personal)"
	case d.InstanceID != nil:
		return d.instance().Name() + " dashboard (shared)"
	case d.UserID != nil:
		return "Home dashboard (personal)"
	}
	return "Home dashboard (shared)"
}

// URL is the URL of the dashboard
func (d *Dashboard) URL() string {
	if l := d.instance(); l != nil {
		return "/" + l.Identifier() + "?did=" + itoa(d.ID)
	}
	return "/?did=" + itoa(d.ID)
}

// DownloadURL is the URL of the PDF of the dashboard
func (d *Dashboard) DownloadURL() string { return d.URL() + "&download=pdf" }

// IsShared reports whether the dashboard is shared (not a user's)
func (d *Dashboard) IsShared() bool { return d.UserID == nil }

// IsEmpty reports whether the dashboard has no widgets
func (d *Dashboard) IsEmpty() bool {
	n, _ := d.e.DB.QueryInt64(`SELECT COUNT(*) FROM "widget" "me" WHERE "me"."dashboard_id" = ?`, d.ID)
	return n == 0
}

// AsJSON is Dashboard->as_json
func (d *Dashboard) AsJSON() string {
	return EncodeJSON(map[string]any{"name": d.Name(), "download_url": d.DownloadURL()})
}

// DisplayWidgets returns the widgets to display: the static widgets of the
// shared dashboard first (for personal dashboards), then the dashboard's
func (d *Dashboard) DisplayWidgets() []*Widget {
	var rows []db.Row
	if !d.IsShared() {
		cond := `"dashboard"."instance_id" IS NULL`
		var args []any
		if d.InstanceID != nil {
			cond = `"dashboard"."instance_id" = ?`
			args = append(args, *d.InstanceID)
		}
		rows = append(rows, d.e.DB.Query(`SELECT "me".* FROM "widget" "me" LEFT JOIN "dashboard" "dashboard" ON "dashboard"."id" = "me"."dashboard_id" WHERE `+cond+` AND "dashboard"."user_id" IS NULL AND "me"."static" = 1`, args...)...)
	}
	rows = append(rows, d.e.DB.Query(`SELECT "me".* FROM "widget" "me" WHERE "me"."dashboard_id" = ?`, d.ID)...)
	var out []*Widget
	for _, r := range rows {
		out = append(out, &Widget{Row: r, Layout: d.Layout, CurrentID: d.CurrentID})
	}
	return out
}

// Grid is the JSON of the widget's grid position
func (w *Widget) Grid(dashboardShared bool) string {
	static := !dashboardShared && rowBool(w.Row["static"])
	num := func(k string) any {
		if v, ok := db.AsInt64(w.Row[k]); ok && w.Row[k] != nil {
			return v
		}
		return nil
	}
	return EncodeJSON(map[string]any{
		"i":      w.Row["grid_id"],
		"static": static,
		"h":      num("h"),
		"w":      num("w"),
		"x":      num("x"),
		"y":      num("y"),
	})
}

// DashboardByID is the dashboard with the ID (schema->resultset('Dashboard')
// ->find, site filtered), or nil
func (e *Engine) DashboardByID(id int64) *Dashboard {
	row := e.DB.QueryRow(`SELECT "me".* FROM "dashboard" "me" WHERE "me"."id" = ?`+e.dashboardSite(), id)
	if row == nil {
		return nil
	}
	return e.dashboardFromRow(row)
}

// CreateWidget creates a widget of the dashboard, returning its grid ID
func (d *Dashboard) CreateWidget(m map[string]any) string {
	id := d.createWidget(m)
	g, _ := db.AsString(d.e.DB.QueryRow(`SELECT "grid_id" FROM "widget" WHERE "id" = ?`, id)["grid_id"])
	return g
}

func strPtr(s string) *string { return &s }

// RecordDashboard is ResultSet::Dashboard->record_dashboard: the overview
// dashboard of the records of a table, created if needed, displayed for a
// record
func (e *Engine) RecordDashboard(layout *Layout, currentID string) *Dashboard {
	row := e.DB.QueryRow(`SELECT "me".* FROM "dashboard" "me" WHERE "me"."instance_id" = ? AND "me"."type" = 'record'`+e.dashboardSite()+` LIMIT 1`, layout.InstanceID)
	var d *Dashboard
	if row != nil {
		d = e.dashboardFromRow(row)
	} else {
		m := map[string]any{"instance_id": layout.InstanceID, "type": "record"}
		id := e.InsertSite("dashboard", m)
		d = &Dashboard{e: e, ID: id, InstanceID: &layout.InstanceID, Type: strPtr("record")}
	}
	d.Layout = layout
	d.CurrentID = currentID
	return d
}

// Dashboard is the dashboard of the widget
func (w *Widget) Dashboard(e *Engine) *Dashboard {
	id, _ := db.AsInt64(w.Row["dashboard_id"])
	row := e.DB.QueryRow(`SELECT "me".* FROM "dashboard" "me" WHERE "me"."id" = ?`, id)
	if row == nil {
		Panic(`Can't call method "is_record" on an undefined value`)
	}
	return e.dashboardFromRow(row)
}

// OptionsInflated is Widget->options_inflated: the options JSON, an empty
// hash if none
func (w *Widget) OptionsInflated() map[string]any {
	s, _ := db.AsString(w.Row["options"])
	if s == "" {
		return map[string]any{}
	}
	v, err := DecodeJSON(s)
	if err != nil {
		Panic("malformed JSON string: %s", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		Panic("Not a HASH reference")
	}
	return m
}

var (
	widgetTypesRecord = regexp.MustCompile(`^(notice|timeline|field|table)$`)
	widgetTypesTable  = regexp.MustCompile(`^(notice|table|graph|timeline|globe)$`)
	widgetInteger     = regexp.MustCompile(`^[0-9]+$`)
	widgetColumnID    = regexp.MustCompile(`^[_0-9]+$`)
	widgetRange       = regexp.MustCompile(`^(3|6|12|24)$`)
)

// ValidateWidget is Widget->validate, run (as GADS::DBIC) before a widget
// is inserted or updated with the values of row. An empty type becomes
// notice, as Perl does until a widget type can be defined.
func ValidateWidget(d *Dashboard, row map[string]any) {
	typ, _ := db.AsString(row["type"])
	if typ == "" {
		typ = "notice"
		row["type"] = typ
	}
	if d.IsRecord() {
		if !widgetTypesRecord.MatchString(typ) {
			Fail("Invalid widget type: %s", typ)
		}
	} else if !widgetTypesTable.MatchString(typ) {
		Fail("Invalid widget type: %s", typ)
	}
	for _, k := range []string{"h", "w", "x", "y", "rows"} {
		v, ok := row[k]
		if !ok || v == nil {
			continue
		}
		if s := perlStringify(v); !widgetInteger.MatchString(s) {
			Fail("Invalid %s value %s", k, s)
		}
	}
	if typ == "timeline" && d.IsRecord() {
		options := (&Widget{Row: row}).OptionsInflated()
		columnIDs := options["column_ids"]
		delete(options, "column_ids")
		if !truthy(columnIDs) {
			columnIDs = []any{}
		}
		ids, ok := columnIDs.([]any)
		if !ok {
			Fail("Incorrect value type for columns")
		}
		for _, id := range ids {
			if !widgetColumnID.MatchString(perlStringify(id)) {
				Fail("Invalid column ID in columns configuration")
			}
		}
		r := options["timeline_range"]
		delete(options, "timeline_range")
		if truthy(r) && !widgetRange.MatchString(perlStringify(r)) {
			Fail("Invalid timeline range: %s", perlStringify(r))
		}
		if len(options) > 0 {
			Fail("Unexpected keys in widget configuration")
		}
	}
}
