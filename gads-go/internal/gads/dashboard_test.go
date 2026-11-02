package gads

import (
	"strings"
	"testing"

	"github.com/ctrlo/gads-go/internal/db"
)

func newDashboardTestEngine(t *testing.T) *Engine {
	t.Helper()
	d, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	e, err := NewEngine(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO "site" ("id", "host") VALUES (1, 'localhost')`)
	d.Exec(`INSERT INTO "instance" ("id", "name", "site_id") VALUES (1, 'Table1', 1)`)
	return e
}

// Widget->validate, with the messages of GADS
func TestValidateWidget(t *testing.T) {
	table := &Dashboard{Type: strPtr("table")}
	record := &Dashboard{Type: strPtr("record")}
	cases := []struct {
		d    *Dashboard
		row  map[string]any
		want string // the error, or "" for none
	}{
		{table, map[string]any{"type": "notice"}, ""},
		{table, map[string]any{"type": "graph"}, ""},
		{table, map[string]any{"type": "globe"}, ""},
		{table, map[string]any{"type": "field"}, "Invalid widget type: field"},
		{table, map[string]any{"type": "chart"}, "Invalid widget type: chart"},
		{record, map[string]any{"type": "field"}, ""},
		{record, map[string]any{"type": "graph"}, "Invalid widget type: graph"},
		{record, map[string]any{"type": "globe"}, "Invalid widget type: globe"},
		{table, map[string]any{"type": "table", "h": "2", "w": int64(4), "x": "0", "y": nil, "rows": "10"}, ""},
		{table, map[string]any{"type": "table", "h": "1.5"}, "Invalid h value 1.5"},
		{table, map[string]any{"type": "table", "x": "left"}, "Invalid x value left"},
		{table, map[string]any{"type": "table", "w": "-1"}, "Invalid w value -1"},
		{table, map[string]any{"type": "table", "y": float64(2.5)}, "Invalid y value 2.5"},
		{table, map[string]any{"type": "table", "rows": "ten"}, "Invalid rows value ten"},
		// The options of a timeline are checked on a record dashboard only
		{table, map[string]any{"type": "timeline", "options": `{"label":"1"}`}, ""},
		{record, map[string]any{"type": "timeline"}, ""},
		{record, map[string]any{"type": "timeline", "options": `{"column_ids":["5","6_7"],"timeline_range":12}`}, ""},
		{record, map[string]any{"type": "timeline", "options": `{"column_ids":[],"timeline_range":"24"}`}, ""},
		{record, map[string]any{"type": "timeline", "options": `{"column_ids":"5"}`}, "Incorrect value type for columns"},
		{record, map[string]any{"type": "timeline", "options": `{"column_ids":["5","x"]}`}, "Invalid column ID in columns configuration"},
		{record, map[string]any{"type": "timeline", "options": `{"timeline_range":5}`}, "Invalid timeline range: 5"},
		{record, map[string]any{"type": "timeline", "options": `{"timeline_range":0}`}, ""},
		{record, map[string]any{"type": "timeline", "options": `{"label":"1"}`}, "Unexpected keys in widget configuration"},
	}
	for i, c := range cases {
		err := Try(func() { ValidateWidget(c.d, c.row) })
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%d: unexpected error %v", i, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%d: error %v, want %q", i, err, c.want)
		}
	}
	// An empty type becomes a notice (until a widget type can be defined)
	row := map[string]any{"type": nil}
	ValidateWidget(table, row)
	if row["type"] != "notice" {
		t.Errorf("empty type became %v", row["type"])
	}
}

func TestWidgetOptionsInflated(t *testing.T) {
	if m := (&Widget{Row: db.Row{"options": nil}}).OptionsInflated(); len(m) != 0 {
		t.Errorf("no options: %v", m)
	}
	m := (&Widget{Row: db.Row{"options": `{"column_ids":["3"],"show_field_labels":true}`}}).OptionsInflated()
	if ids, ok := m["column_ids"].([]any); !ok || len(ids) != 1 || m["show_field_labels"] != true {
		t.Errorf("options: %#v", m)
	}
	if err := Try(func() { (&Widget{Row: db.Row{"options": `[1]`}}).OptionsInflated() }); err == nil {
		t.Errorf("options that are not a hash: no error")
	}
}

// ResultSet::Dashboard record_dashboard: created on first use, then found;
// the table dashboards do not see it
func TestRecordDashboard(t *testing.T) {
	e := newDashboardTestEngine(t)
	layout := e.NewLayout(1, nil)
	d := e.RecordDashboard(layout, "3")
	if !d.IsRecord() || d.CurrentID != "3" || d.Name() != "Overview" {
		t.Fatalf("record dashboard %+v (%s)", d, d.Name())
	}
	row := e.DB.QueryRow(`SELECT * FROM "dashboard" WHERE "id" = ?`, d.ID)
	if s, _ := db.AsString(row["type"]); s != "record" {
		t.Errorf("type %q", s)
	}
	if n, _ := db.AsInt64(row["site_id"]); n != e.SiteID {
		t.Errorf("site_id %v, want %d", row["site_id"], e.SiteID)
	}
	again := e.RecordDashboard(layout, "4")
	if again.ID != d.ID || again.CurrentID != "4" {
		t.Errorf("second record dashboard %d (%s), want %d", again.ID, again.CurrentID, d.ID)
	}
	if n, _ := e.DB.QueryInt64(`SELECT COUNT(*) FROM "dashboard"`); n != 1 {
		t.Errorf("%d dashboards", n)
	}
	// The widgets of a record dashboard are shown for its record
	d.CreateWidget(map[string]any{"type": "field"})
	for _, w := range d.DisplayWidgets() {
		if w.CurrentID != "3" {
			t.Errorf("widget current ID %q", w.CurrentID)
		}
		if !w.Dashboard(e).IsRecord() {
			t.Errorf("widget dashboard is not the record dashboard")
		}
	}
}
