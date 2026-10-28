package gads

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
)

// Port of GADS::Graph, GADS::Graph::Data and GADS::RecordsGraph.

// GraphTypes are the valid graph types (GADS::Graphs->types).
var GraphTypes = []string{"bar", "line", "donut", "scatter", "pie"}

// Graph is a graph definition.
type Graph struct {
	e      *Engine
	Layout *Layout
	User   *User

	ID            int64
	Title         string
	Description   string
	XAxis         int64
	XAxisLink     int64
	XAxisGrouping string
	YAxis         int64
	YAxisLink     int64 // the curval of a Y-axis field within a curval
	YAxisStack    string
	YAxisLabel    string
	GroupBy       int64
	Stackseries   bool
	Trend         string
	From, To      *time.Time
	XAxisRange    string
	IsShared      bool
	UserID        int64
	GroupID       int64
	AsPercent     bool
	Type          string
	MetricGroupID int64
	// Selected is whether the user has selected the graph (GADS::Graphs)
	Selected bool

	exists bool
	// raw are values from a form, written as given (Perl keeps empty
	// strings of these, where a graph built otherwise has none)
	raw map[string]any
	// loadErr is the error of a graph that could not be retrieved, raised
	// where Perl first needs the graph's row (it is built lazily)
	loadErr    string
	groupIDSet bool
	rawID      string
}

// LoadGraphLazy is GADS::Graph->new with an ID from a route: a graph not
// found (or an invalid ID) is only an error once its row is needed
func (e *Engine) LoadGraphLazy(id string, layout *Layout, user *User) *Graph {
	if !reDigits.MatchString(id) {
		return &Graph{e: e, Layout: layout, User: user, loadErr: "Invalid graph ID " + id, rawID: id}
	}
	gid, _ := strconv.ParseInt(id, 10, 64)
	row := e.DB.QueryRow(`SELECT "id", "instance_id" FROM "graph" WHERE "id" = ?`, gid)
	// Hardened (M-1): a graph of another table is not found, and has no ID
	// to delete or update. Perl edits it, and moves it to this table.
	if e.Config.Hardened && row != nil && layout != nil {
		if inst, _ := db.AsInt64(row["instance_id"]); inst != layout.InstanceID {
			return &Graph{e: e, Layout: layout, User: user, loadErr: "Requested graph ID " + id + " not found"}
		}
	}
	if row == nil {
		return &Graph{e: e, Layout: layout, User: user, ID: gid, loadErr: "Requested graph ID " + id + " not found"}
	}
	return e.LoadGraph(gid, layout, user)
}

// RawID is the ID of the graph as given, if not a valid ID
func (g *Graph) RawID() string { return g.rawID }

// LoadErr is the error of a graph that could not be retrieved
func (g *Graph) LoadErr() string { return g.loadErr }

// NewGraph creates a new graph definition.
func (e *Engine) NewGraph(layout *Layout, user *User) *Graph {
	return &Graph{e: e, Layout: layout, User: user}
}

// LoadGraph retrieves a graph from the database.
func (e *Engine) LoadGraph(id int64, layout *Layout, user *User) *Graph {
	row := e.DB.QueryRow(`SELECT * FROM "graph" WHERE "id" = ?`, id)
	if row == nil {
		Fail("Requested graph ID %d not found", id)
	}
	g := &Graph{e: e, Layout: layout, User: user, ID: id, exists: true}
	str := func(k string) string { s, _ := db.AsString(row[k]); return s }
	num := func(k string) int64 { n, _ := db.AsInt64(row[k]); return n }
	g.Title = str("title")
	g.Description = str("description")
	g.XAxis = num("x_axis")
	g.XAxisLink = num("x_axis_link")
	g.XAxisGrouping = str("x_axis_grouping")
	g.YAxis = num("y_axis")
	g.YAxisLink = num("y_axis_link")
	g.YAxisStack = str("y_axis_stack")
	g.YAxisLabel = str("y_axis_label")
	g.GroupBy = num("group_by")
	g.Stackseries = num("stackseries") != 0
	g.Trend = str("trend")
	if t, ok := db.AsTime(row["from"]); ok {
		g.From = &t
	}
	if t, ok := db.AsTime(row["to"]); ok {
		g.To = &t
	}
	g.XAxisRange = str("x_axis_range")
	g.IsShared = num("is_shared") != 0
	g.UserID = num("user_id")
	g.GroupID = num("group_id")
	g.AsPercent = num("as_percent") != 0
	g.Type = str("type")
	g.MetricGroupID = num("metric_group")
	return g
}

var reXAxisFull = regexp.MustCompile(`^([0-9]+)_([0-9]+)$`)

// SetXAxis sets the x-axis from a form value, which may be "link_field".
func (g *Graph) SetXAxis(v string) {
	if m := reXAxisFull.FindStringSubmatch(v); m != nil {
		g.XAxis, _ = strconv.ParseInt(m[2], 10, 64)
		g.XAxisLink, _ = strconv.ParseInt(m[1], 10, 64)
		return
	}
	g.XAxisLink = 0
	g.XAxis, _ = strconv.ParseInt(v, 10, 64)
}

// XAxisFull returns the x-axis including any link field.
func (g *Graph) XAxisFull() string {
	if g.XAxisLink != 0 {
		return itoa(g.XAxisLink) + "_" + itoa(g.XAxis)
	}
	if g.XAxis == 0 {
		return ""
	}
	return itoa(g.XAxis)
}

// Showlegend reports whether a legend is shown.
func (g *Graph) Showlegend() bool {
	return g.GroupBy != 0 || g.Type == "pie" || g.Type == "donut" || g.Trend != ""
}

func (g *Graph) writableShared() bool {
	if g.Layout.UserCan("layout") {
		return true
	}
	if g.loadErr != "" && !g.groupIDSet {
		Fail("%s", g.loadErr)
	}
	return g.GroupID != 0 && g.Layout.UserCan("view_group")
}

// Writable reports whether the current user can change the graph.
func (g *Graph) Writable() bool {
	if g.e.IgnorePermissions || g.writableShared() {
		return true
	}
	if g.ID == 0 && g.loadErr == "" && !g.IsShared {
		return true
	}
	if g.loadErr != "" {
		Fail("%s", g.loadErr)
	}
	return g.UserID != 0 && g.User != nil && g.User.ID == g.UserID
}

// Write validates and writes the graph.
func (g *Graph) Write() {
	if !g.Writable() {
		Fail("You do not have permission to write to this graph")
	}
	l := g.Layout
	if g.Title == "" {
		Fail("Please enter a title")
	}
	if g.YAxis != 0 {
		if g.YAxisLink != 0 {
			if !l.ColumnThisInstance(g.YAxisLink) {
				Fail("Invalid Y-axis parent %d", g.YAxisLink)
			}
		} else if !l.ColumnThisInstance(g.YAxis) {
			Fail("Invalid Y-axis %d", g.YAxis)
		}
	}
	if g.YAxisStack == "" {
		Fail("A valid value is required for Y-axis stacking")
	}
	if g.YAxisStack != "count" && g.YAxisStack != "sum" {
		Fail("%s is an invalid value for Y-axis", g.YAxisStack)
	}
	if g.YAxisStack == "sum" && g.YAxis == 0 {
		Fail("Please select a Y-axis")
	}
	if g.XAxis != 0 && l.Column(g.XAxis) == nil {
		Fail("Invalid X-axis value %d", g.XAxis)
	}
	if g.From != nil && g.To != nil && g.From.After(*g.To) {
		Fail("Start date of custom range must be before end of custom range")
	}
	if g.XAxisRange == "" && g.Trend != "" {
		Fail("An x-axis range must be selected when plotting historical trends")
	}
	if g.XAxisGrouping != "" && g.XAxisGrouping != "day" && g.XAxisGrouping != "month" && g.XAxisGrouping != "year" {
		Fail("%s is an invalid value for X-axis grouping", g.XAxisGrouping)
	}
	if xc := l.Column(g.XAxis); xc != nil && strings.Contains(xc.ReturnTypeFn(), "date") && g.XAxisRange == "custom" &&
		(g.From == nil || g.To == nil || g.XAxisGrouping == "") {
		Fail("Dates and grouping must be entered for a custom range")
	}
	if g.XAxis == 0 && g.Trend != "" {
		Fail("A single field must be selected for the x-axis when using the historic trend option")
	}
	if g.GroupBy != 0 && !l.ColumnThisInstance(g.GroupBy) {
		Fail("Invalid group by value %d", g.GroupBy)
	}
	if g.MetricGroupID != 0 {
		if row := g.e.DB.QueryRow(`SELECT "id" FROM "metric_group" WHERE "id" = ?`, g.MetricGroupID); row == nil {
			Fail("Invalid metric group ID %d", g.MetricGroupID)
		}
	}
	if g.GroupBy != 0 && g.Trend != "" {
		Fail("Historical trends cannot be used with y-axis grouping (Group by)")
	}
	if g.IsShared && !g.writableShared() && !g.e.IgnorePermissions {
		Fail("You do not have permission to create shared graphs")
	}
	valid := false
	for _, t := range GraphTypes {
		if g.Type == t {
			valid = true
		}
	}
	if !valid {
		Fail("Invalid graph type %s", g.Type)
	}
	if g.YAxisStack == "sum" && !l.Column(g.YAxis).Numeric() {
		Fail("For graph %s, a field returning a numeric value must be used for the Y-axis when calculating the sum of values", g.Title)
	}
	nz := func(v int64) any {
		if v == 0 {
			return nil
		}
		return v
	}
	ns := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	nd := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return db.FormatDate(*t)
	}
	m := map[string]any{
		"title":           g.Title,
		"description":     ns(g.Description),
		"y_axis":          nz(g.YAxis),
		"y_axis_link":     nz(g.YAxisLink),
		"y_axis_stack":    g.YAxisStack,
		"y_axis_label":    ns(g.YAxisLabel),
		"x_axis":          nz(g.XAxis),
		"x_axis_link":     nz(g.XAxisLink),
		"trend":           ns(g.Trend),
		"from":            nd(g.From),
		"to":              nd(g.To),
		"x_axis_range":    ns(g.XAxisRange),
		"x_axis_grouping": ns(g.XAxisGrouping),
		"group_by":        nz(g.GroupBy),
		"metric_group":    nz(g.MetricGroupID),
		"stackseries":     boolToInt(g.Stackseries),
		"is_shared":       boolToInt(g.IsShared),
		"group_id":        nz(g.GroupID),
		"as_percent":      boolToInt(g.AsPercent),
		"type":            g.Type,
		"instance_id":     l.InstanceID,
	}
	if !g.IsShared {
		uid := g.UserID
		if uid == 0 && g.User != nil {
			uid = g.User.ID
		}
		m["user_id"] = nz(uid)
	}
	for k, v := range g.raw {
		m[k] = v
	}
	if g.loadErr != "" {
		Fail("%s", g.loadErr)
	}
	if g.exists {
		g.e.DB.UpdateMap("graph", m, map[string]any{"id": g.ID})
	} else {
		g.ID = g.e.DB.InsertMap("graph", m)
		g.exists = true
	}
}

// SetYAxis is set_y_axis: a Y-axis as a column ID, or the full ID
// (parent_child) of a field within a curval
func (g *Graph) SetYAxis(value string) {
	if m := regexp.MustCompile(`^([0-9]+)_([0-9]+)$`).FindStringSubmatch(value); m != nil {
		g.YAxis, _ = strconv.ParseInt(m[2], 10, 64)
		g.YAxisLink, _ = strconv.ParseInt(m[1], 10, 64)
		return
	}
	g.YAxisLink = 0
	g.YAxis, _ = anyToInt64(value)
}

// YAxisFull is y_axis_full
func (g *Graph) YAxisFull() string {
	if g.YAxisLink != 0 {
		return itoa(g.YAxisLink) + "_" + itoa(g.YAxis)
	}
	return itoa(g.YAxis)
}

// SetForm sets the values of the graph from the parameters of the graph
// form, as the setters of GADS::Graph (each of them set, a missing
// parameter as undef)
func (g *Graph) SetForm(param func(string) any) {
	str := func(k string) string { return anyToString(param(k)) }
	id := func(k string) int64 { n, _ := anyToInt64(param(k)); return n }
	date := func(k string) *time.Time {
		if t, ok := g.e.ParseDateTime(str(k)); ok {
			return &t
		}
		return nil
	}
	g.Title = str("title")
	g.Description = str("description")
	g.Type = str("type")
	g.SetXAxis(str("set_x_axis"))
	g.XAxisGrouping = str("x_axis_grouping")
	g.YAxis = id("y_axis")
	g.YAxisLabel = str("y_axis_label")
	g.YAxisStack = str("y_axis_stack")
	g.GroupBy = id("group_by")
	g.Stackseries = perlTrue(param("stackseries"))
	g.MetricGroupID = id("metric_group_id")
	g.AsPercent = perlTrue(param("as_percent"))
	g.IsShared = perlTrue(param("is_shared"))
	g.GroupID = id("group_id")
	g.groupIDSet = true
	g.Trend = str("trend")
	g.From = date("from")
	g.To = date("to")
	g.XAxisRange = str("x_axis_range")
	g.raw = map[string]any{
		"description":  param("description"),
		"y_axis_label": param("y_axis_label"),
		"trend":        param("trend"),
		"x_axis_range": param("x_axis_range"),
	}
}

// Delete removes the graph.
func (g *Graph) Delete() {
	if !g.Writable() {
		Fail("You do not have permission to delete this graph")
	}
	g.e.DB.Exec(`UPDATE "widget" SET "graph_id" = NULL WHERE "graph_id" = ?`, g.ID)
	g.e.DB.Exec(`DELETE FROM "user_graph" WHERE "graph_id" = ?`, g.ID)
	g.e.DB.Exec(`DELETE FROM "graph" WHERE "id" = ?`, g.ID)
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// NewRecordsGraph creates a records search for graphs (GADS::RecordsGraph).
func (e *Engine) NewRecordsGraph(layout *Layout, user *User) *Records {
	r := e.NewRecords(layout, user)
	r.isGraph = true
	r.groupValuesNotIndex = true
	return r
}

// GraphSeriesLabel is the label of a series (jqplot options).
type GraphSeriesLabel struct {
	Color         string
	Showlabel     string
	Showline      string
	Markeroptions string
	Label         string
}

// GraphData is the plotted data of a graph.
type GraphData struct {
	XLabels []string
	Points  []any
	Labels  []GraphSeriesLabel
	Options map[string]any
}

type graphBuilder struct {
	g       *Graph
	records *Records
	view    *View
	colors  map[string]bool
	inUse   map[string]string
}

var graphDateFormats = map[string]string{
	"day":   "02 January 2006",
	"month": "January 2006",
	"year":  "2006",
}

const (
	colorRed    = "D9534F"
	colorAmber  = "F0AD4E"
	colorYellow = "FCFC4B"
	colorGreen  = "5CB85C"
	colorGrey   = "8C8C8C"
	colorPurple = "4B0F44"
	colorBlue   = "3B3AF2"
)

var graphColors = []string{"7A221B", "D1D3D4", "34C3E0", "FFDD00", "9F6512", "F0679E", "2C4269", "7F3F98",
	"1C75BC", "51417B", "F26522", "BDE0E9", "B0B11A", "4D4C4C", "007B45", "F37970", "EE2D72", "F9DDB6",
	"97C9B3", "FFED7D", colorRed, colorAmber, colorYellow, colorGreen, colorGrey, colorPurple, colorBlue}

// Data builds the graph's data from the records, optionally limited by a view.
func (g *Graph) Data(records *Records, view *View) *GraphData {
	b := &graphBuilder{g: g, records: records, view: view, colors: map[string]bool{}, inUse: map[string]string{}}
	for _, c := range graphColors {
		b.colors[c] = true
	}
	return b.build()
}

func (b *graphBuilder) xAxisCol() *Column {
	if b.g.XAxis == 0 {
		return nil
	}
	return b.records.Layout.Column(b.g.XAxis)
}

func (b *graphBuilder) yAxisCol() *Column {
	if b.g.YAxis == 0 {
		return nil
	}
	return b.records.Layout.Column(b.g.YAxis)
}

func (b *graphBuilder) groupByCol() *Column {
	if b.g.Type == "pie" || b.g.GroupBy == 0 {
		return nil
	}
	return b.records.Layout.Column(b.g.GroupBy)
}

// rangeNum returns x_axis_range as a Perl number.
func (b *graphBuilder) rangeNum() float64 { return perlNumber(b.g.XAxisRange) }

func (b *graphBuilder) trendRangeAmount() (float64, bool) {
	if b.g.XAxisRange == "" || b.g.XAxisRange == "0" {
		return 0, false
	}
	n := b.rangeNum()
	if n < 0 {
		n = -n
	}
	return n, true
}

func (b *graphBuilder) groupingCalculated() string {
	g := b.g
	if g.XAxis == 0 {
		return ""
	}
	if g.Trend == "" {
		rt := b.xAxisCol().ReturnTypeFn()
		if rt != "date" && rt != "daterange" {
			return ""
		}
	}
	if g.XAxisRange == "custom" || (g.Trend == "" && g.XAxisGrouping != "") {
		return g.XAxisGrouping
	}
	if g.From == nil && g.To == nil && (g.XAxisRange == "" || g.XAxisRange == "0") {
		return ""
	}
	amount, _ := b.trendRangeAmount()
	switch {
	case amount == 1:
		return "day"
	case amount <= 24:
		return "month"
	}
	return "year"
}

func truncateTo(t time.Time, interval string) time.Time {
	switch interval {
	case "year":
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "day":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return t
}

func addInterval(t time.Time, interval string, n int) time.Time {
	switch interval {
	case "year":
		return addMonthsDT(t, 12*n)
	case "month":
		return addMonthsDT(t, n)
	case "day":
		return t.AddDate(0, 0, n)
	}
	return t
}

// addMonthsDT adds months as DateTime does (end_of_month => 'wrap').
func addMonthsDT(t time.Time, n int) time.Time { return t.AddDate(0, n, 0) }

func (b *graphBuilder) fromCalculated() *time.Time {
	interval := b.groupingCalculated()
	if interval == "" {
		return nil
	}
	g := b.g
	if g.From != nil && g.Trend != "" {
		t := truncateTo(*g.From, interval)
		return &t
	}
	if g.From != nil {
		t := *g.From
		return &t
	}
	amount, ok := b.trendRangeAmount()
	if !ok {
		return nil
	}
	from := truncateTo(b.g.e.Now(), interval)
	if b.rangeNum() < 0 {
		from = addMonthsDT(from, -int(amount))
	}
	return &from
}

func (b *graphBuilder) toCalculated() *time.Time {
	interval := b.groupingCalculated()
	if interval == "" {
		return nil
	}
	g := b.g
	if g.To != nil && g.Trend != "" {
		t := truncateTo(*g.To, interval)
		return &t
	}
	if g.To != nil {
		t := *g.To
		return &t
	}
	amount, ok := b.trendRangeAmount()
	if !ok {
		return nil
	}
	t := addMonthsDT(*b.fromCalculated(), int(amount))
	return &t
}

// graphResults is the results hash: x value -> series key -> value.
type graphResults struct {
	keys   []string
	values map[string]map[string]float64
	totals map[string]float64 // values_only
}

func newGraphResults() *graphResults {
	return &graphResults{values: map[string]map[string]float64{}, totals: map[string]float64{}}
}

func (gr *graphResults) ensure(x string) map[string]float64 {
	m, ok := gr.values[x]
	if !ok {
		m = map[string]float64{}
		gr.values[x] = m
		gr.keys = append(gr.keys, x)
	}
	return m
}

type graphX struct {
	col *Column
	dt  time.Time
}

func (b *graphBuilder) build() *GraphData {
	g := b.g
	records := b.records
	layout := records.Layout
	var colIDs []int64
	switch {
	case g.XAxis != 0:
		colIDs = []int64{g.XAxis}
	case b.view != nil:
		colIDs = b.view.Columns()
	default:
		for _, c := range layout.All(AllOptions{UserCanRead: true}) {
			colIDs = append(colIDs, c.ID)
		}
	}
	var columns []*groupCol
	for _, id := range colIDs {
		gc := &groupCol{id: id, operator: "max"}
		if g.XAxis != 0 && id == g.XAxis {
			gc.parentID = g.XAxisLink
		}
		columns = append(columns, gc)
	}
	xAxis := b.xAxisCol()
	xDaterange := xAxis != nil && xAxis.ReturnTypeFn() == "daterange"
	if !xDaterange && g.XAxis != 0 {
		columns = append(columns, &groupCol{id: g.XAxis, group: true, pluck: b.groupingCalculated(), parentID: g.XAxisLink})
	}
	if b.groupByCol() != nil {
		columns = append(columns, &groupCol{id: g.GroupBy, group: true})
	}
	if g.Trend == "" {
		records.From = b.fromCalculated()
		records.To = b.toCalculated()
	}
	var link *Column
	if g.XAxisLink != 0 {
		link = layout.Column(g.XAxisLink)
	}
	if xDaterange {
		records.drInterval = b.groupingCalculated()
		records.drColumn = xAxis.ID
		records.drColumnParent = link
		records.drYAxisID = g.YAxis
	}
	records.View = b.view
	if g.YAxis != 0 {
		// The parent is the curval of a field picked from within a curval
		columns = append(columns, &groupCol{id: g.YAxis, operator: g.YAxisStack, parentID: g.YAxisLink})
	}
	records.setGraphColumns(columns)
	if xDaterange {
		records.graphResults()
	}
	view := records.View

	var xs []graphX
	switch {
	case xDaterange:
		if records.drFrom != nil && records.drTo != nil {
			p := *records.drFrom
			for p.Unix() <= records.drTo.Unix() {
				xs = append(xs, graphX{dt: p})
				p = addInterval(p, b.groupingCalculated(), 1)
			}
		}
	case g.XAxisRange != "" && g.XAxisRange != "0" && b.groupingCalculated() != "":
		interval := b.groupingCalculated()
		p := *b.fromCalculated()
		to := *b.toCalculated()
		for !p.After(to) {
			xs = append(xs, graphX{dt: p})
			p = addInterval(p, interval, 1)
		}
	case xAxis != nil:
		xs = append(xs, graphX{col: xAxis})
	case view != nil:
		for _, c := range records.ColumnsRender() {
			xs = append(xs, graphX{col: c})
		}
	default:
		for _, c := range layout.All(AllOptions{UserCanRead: true}) {
			xs = append(xs, graphX{col: c})
		}
	}

	var xlabels []string
	var results *graphResults
	seriesKeys := map[string]bool{}
	var datemin, datemax *time.Time
	if g.Trend != "" {
		// Retain quick search across historical queries
		search, hasSearch := records.Search, records.hasSearch
		results = newGraphResults()
		interval := b.groupingCalculated()
		for _, x := range xs {
			records.Clear()
			records.Search, records.hasSearch = search, hasSearch
			// The end of the period: only the values are rewound, not the
			// set of records
			rewind := addInterval(x.dt, interval, 1).Add(-time.Second)
			records.SetRewindValues(&rewind)
			thisResults, thisKeys, dmin, dmax := b.recordsToResults(xDaterange, []graphX{{col: xAxis}}, true)
			datemin, datemax = dmin, dmax
			label := x.dt.Format(graphDateFormats[interval])
			xlabels = append(xlabels, label)
			m := results.ensure(label)
			for k, v := range thisResults.totals {
				m[k] = v
			}
			for k := range thisKeys {
				seriesKeys[k] = true
			}
		}
	} else {
		results, seriesKeys, datemin, datemax = b.recordsToResults(xDaterange, xs, false)
	}

	switch {
	case b.groupingCalculated() != "" && datemin != nil && datemax != nil:
		xlabels = nil
		inc := *datemin
		interval := b.groupingCalculated()
		for inc.Unix() <= datemax.Unix() {
			xlabels = append(xlabels, inc.Format(graphDateFormats[interval]))
			inc = addInterval(inc, interval, 1)
		}
	case g.XAxis == 0:
		xlabels = nil
		for _, x := range xs {
			xlabels = append(xlabels, x.col.Name)
		}
	case g.Trend != "":
	default:
		xlabels = sortedStrings(results.keys)
	}

	series := map[string][]float64{}
	for serial := range seriesKeys {
		if g.XAxis != 0 {
			for _, x := range xlabels {
				series[serial] = append(series[serial], math.Trunc(results.values[x][serial]))
			}
		} else {
			for _, x := range xs {
				y := math.Trunc(results.values[x.col.Name][serial])
				if !x.col.Numeric() {
					y = 0
				}
				series[serial] = append(series[serial], y)
			}
		}
	}

	if g.AsPercent && g.Type != "pie" && g.Type != "donut" {
		if b.groupByCol() != nil {
			count := 0
			for _, s := range series {
				count = len(s)
				break
			}
			for i := 0; i < count; i++ {
				var sum float64
				for _, s := range series {
					sum += s[i]
				}
				for k := range series {
					series[k][i] = toPercent(sum, series[k][i])
				}
			}
		} else {
			s := series["1"]
			var sum float64
			for _, v := range s {
				sum += v
			}
			for i := range s {
				s[i] = toPercent(sum, s[i])
			}
		}
	}

	var metricMax *float64
	if g.MetricGroupID != 0 {
		metrics := map[string]map[string]float64{}
		for _, m := range g.e.DB.Query(`SELECT * FROM "metric" WHERE "metric_group" = ?`, g.MetricGroupID) {
			ygv, _ := db.AsString(m["y_axis_grouping_value"])
			if ygv == "" || ygv == "0" {
				ygv = "1"
			}
			xv, _ := db.AsString(m["x_axis_value"])
			ygv, xv = strings.ToLower(ygv), strings.ToLower(xv)
			if metrics[ygv] == nil {
				metrics[ygv] = map[string]float64{}
			}
			metrics[ygv][xv] = perlNumber(m["target"])
		}
		for line, data := range series {
			for i := range data {
				var x string
				if i < len(xlabels) {
					x = xlabels[i]
				}
				target := metrics[strings.ToLower(line)][strings.ToLower(x)]
				var val float64
				if target != 0 {
					val = math.Trunc(data[i] * 100 / target)
				}
				data[i] = val
				if metricMax == nil || val > *metricMax {
					v := val
					metricMax = &v
				}
			}
		}
	}

	markeroptions := "{ show: false }"
	if g.Type == "scatter" {
		markeroptions = `{ size: 7, style:"x" }`
	}
	out := &GraphData{XLabels: xlabels, Options: map[string]any{}}
	if xlabels == nil {
		out.XLabels = []string{}
	}
	if g.Type == "pie" || g.Type == "donut" {
		var rings [][]any
		for _, k := range sortedKeys(series) {
			data := series[k]
			if g.AsPercent {
				var sum float64
				for _, v := range data {
					sum += v
				}
				for i := range data {
					data[i] = toPercent(sum, data[i])
				}
			}
			var ps []any
			for i, x := range xlabels {
				var v float64
				if i < len(data) {
					v = data[i]
				}
				ps = append(ps, []any{htmlEscape(x), graphNum(v)})
			}
			rings = append(rings, ps)
		}
		sort.SliceStable(rings, func(i, j int) bool { return len(rings[i]) > len(rings[j]) })
		for _, r := range rings {
			if r == nil {
				r = []any{}
			}
			out.Points = append(out.Points, r)
		}
	} else {
		labels := map[string]GraphSeriesLabel{}
		for _, k := range sortedKeys(series) {
			showline := "true"
			if g.Type == "scatter" {
				showline = "false"
			}
			labels[k] = GraphSeriesLabel{Color: b.getColor(k), Showlabel: "true", Showline: showline, Markeroptions: markeroptions, Label: k}
		}
		keys := sortedKeys(series)
		for i := len(keys) - 1; i >= 0; i-- {
			var pts []any
			for _, v := range series[keys[i]] {
				pts = append(pts, graphNum(v))
			}
			if pts == nil {
				pts = []any{}
			}
			out.Points = append(out.Points, pts)
			out.Labels = append(out.Labels, labels[keys[i]])
		}
	}
	if out.Points == nil {
		out.Points = []any{}
	}
	if metricMax != nil && *metricMax < 100 {
		out.Options["y_max"] = int64(100)
	}
	if metricMax != nil {
		out.Options["is_metric"] = int64(1)
	}
	if link != nil {
		link.ClearCurval()
	}
	return out
}

func sortedKeys(m map[string][]float64) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// graphNum returns a whole number as int64, otherwise the float.
func graphNum(v float64) any {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return int64(v)
	}
	return v
}

// toPercent as Graph::Data::_to_percent (Math::Round::round).
func toPercent(sum, value float64) float64 {
	if sum == 0 {
		Fail("Illegal division by zero")
	}
	v := value / sum * 100
	if v < 0 {
		return -math.Floor(-v + 0.5)
	}
	return math.Floor(v + 0.5)
}

// getColor allocates a colour to a series. Perl picks a random unused colour
// for names that are not RAG values; this picks the first unused in order.
func (b *graphBuilder) getColor(value string) string {
	if r := []rune(value); len(r) > 127 {
		value = string(r[:127])
	}
	if c, ok := b.inUse[value]; ok {
		return "#" + c
	}
	e := b.g.e
	var color string
	existing := e.DB.QueryRow(`SELECT "color" FROM "graph_color" WHERE "name" = ?`, value)
	if existing != nil {
		if c, _ := db.AsString(existing["color"]); b.colors[c] {
			color = c
		}
	}
	if color == "" {
		switch value {
		case "a_grey":
			color = colorGrey
		case "b_red", "b_attention":
			color = colorRed
		case "c_amber":
			color = colorAmber
		case "c_yellow":
			color = colorYellow
		case "d_green":
			color = colorGreen
		case "e_purple":
			color = colorPurple
		case "d_blue":
			color = colorBlue
		default:
			for _, c := range graphColors {
				if b.colors[c] {
					color = c
					break
				}
			}
		}
		if color != "" {
			if existing != nil {
				e.DB.UpdateMap("graph_color", map[string]any{"color": color}, map[string]any{"name": value})
			} else {
				e.DB.InsertMap("graph_color", map[string]any{"name": value, "color": color})
			}
		}
	}
	if color == "" {
		return ""
	}
	b.inUse[value] = color
	delete(b.colors, color)
	return "#" + color
}

func (b *graphBuilder) groupDate(t time.Time) time.Time {
	return truncateTo(t, b.groupingCalculated())
}

func perlTrue(v any) bool {
	if v == nil {
		return false
	}
	s, _ := db.AsString(v)
	return s != "" && s != "0"
}

func graphStr(v any) string {
	if f, ok := v.(float64); ok {
		return perlFloatString(f)
	}
	s, _ := db.AsString(v)
	return s
}

// perlFloatString formats a float as Perl does (%.15g).
func perlFloatString(f float64) string {
	return strconv.FormatFloat(f, 'g', 15, 64)
}

func (b *graphBuilder) formatCurcommon(col *Column, line db.Row) any {
	if !perlTrue(line[col.Field()]) {
		return nil
	}
	var vals []string
	for _, f := range col.CurvalFields() {
		if f == nil {
			continue
		}
		v := line[b.records.aggregateName(f, col, "max")]
		if !perlTrue(v) {
			vals = append(vals, "")
		} else {
			vals = append(vals, graphStr(v))
		}
	}
	return FormatValue(vals)
}

func (b *graphBuilder) recordsToResults(xDaterange bool, xs []graphX, valuesOnly bool) (*graphResults, map[string]bool, *time.Time, *time.Time) {
	g := b.g
	results := newGraphResults()
	seriesKeys := map[string]bool{}
	var datemin, datemax *time.Time
	rows := b.records.graphResults()
	interval := b.groupingCalculated()
	df := graphDateFormats[interval]
	xAxis := b.xAxisCol()
	rangeDate := g.XAxisRange != "" && g.XAxisRange != "0" && xAxis != nil && xAxis.Type == "date"
	if g.Trend == "" && rangeDate {
		for _, x := range xs {
			v := b.groupDate(x.dt)
			if datemin == nil {
				datemin = &v
			}
			if datemax == nil || datemax.Unix() < v.Unix() {
				datemax = &v
			}
			results.ensure(v.Format(df))
		}
	}
	yAxis := b.yAxisCol()
	groupBy := b.groupByCol()
	for _, line := range rows {
		forXs := xs
		if rangeDate {
			forXs = []graphX{{col: xAxis}}
		}
		for _, x := range forXs {
			var key string
			if xDaterange {
				key = itoa(x.dt.Unix())
			} else {
				key = b.records.aggregateName(x.col, b.records.Layout.Column(g.XAxisLink), "max")
			}
			xValue := line[key]
			if !xDaterange && x.col.LinkParent() != nil && !perlTrue(xValue) {
				xValue = line[key+"_link"]
			}
			if !xDaterange && x.col.IsCurcommon() && perlTrue(xValue) {
				xValue = b.formatCurcommon(x.col, line)
			}
			var xStr string
			if g.Trend == "" && interval != "" {
				if !perlTrue(xValue) {
					continue
				}
				var dt time.Time
				if xDaterange {
					dt = x.dt
				} else {
					var ok bool
					dt, ok = db.ParseDBDateTime(graphStr(xValue))
					if !ok {
						continue
					}
				}
				v := b.groupDate(dt)
				if datemin == nil || datemin.Unix() > v.Unix() {
					datemin = &v
				}
				if datemax == nil || datemax.Unix() < v.Unix() {
					datemax = &v
				}
				xStr = v.Format(df)
			} else if g.XAxis == 0 {
				xStr = x.col.Name
			} else if perlTrue(xValue) {
				xStr = graphStr(xValue)
			}
			if xStr == "" || xStr == "0" {
				xStr = "<no value>"
			}
			var fname string
			switch {
			case xDaterange:
				fname = itoa(x.dt.Unix())
			case g.XAxis == 0:
				fname = x.col.Field()
			case g.YAxisStack == "count":
				fname = "id_count"
			default:
				fname = b.records.aggregateName(yAxis, b.records.Layout.Column(g.YAxisLink), g.YAxisStack)
			}
			val := perlNumber(line[fname])
			if g.YAxisStack == "sum" && yAxis != nil && yAxis.LinkParent() != nil {
				val += perlNumber(line[fname+"_link"])
			}
			if valuesOnly {
				seriesKeys[xStr] = true
				if _, ok := results.totals[xStr]; !ok {
					results.keys = append(results.keys, xStr)
				}
				results.totals[xStr] += val
				continue
			}
			var k string
			switch {
			case groupBy != nil && groupBy.IsCurcommon():
				if v := b.formatCurcommon(groupBy, line); v != nil {
					k = v.(string)
				}
			case groupBy != nil:
				if v := line[groupBy.Field()]; perlTrue(v) {
					k = graphStr(v)
				}
			default:
				k = "1"
			}
			if (k == "" || k == "0") && groupBy != nil && groupBy.LinkParent() != nil {
				if v := line[groupBy.Field()+"_link"]; perlTrue(v) {
					k = graphStr(v)
				}
			}
			if k == "" || k == "0" {
				k = "<blank value>"
			}
			seriesKeys[k] = true
			results.ensure(xStr)[k] += val
		}
	}
	return results, seriesKeys, datemin, datemax
}

var reLeadingNumber = regexp.MustCompile(`^\s*[-+]?(\d+\.?\d*([eE][-+]?\d+)?|\.\d+([eE][-+]?\d+)?)`)

// perlNumber converts a value to a number as Perl does in numeric context.
func perlNumber(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	}
	s, _ := db.AsString(v)
	m := reLeadingNumber.FindString(s)
	if m == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(m), 64)
	return f
}

// ---------------------------------------------------------------------
// Records support for graphs
// ---------------------------------------------------------------------

func (r *Records) setGraphColumns(cols []*groupCol) {
	r.graphColumns = cols
	var ids []int64
	for _, c := range cols {
		if c.id != 0 {
			ids = append(ids, c.id)
		}
	}
	r.ColumnIDs = ids
	r.hasColumnIDs = true
	r.clearColumns()
}

// graphResults returns the raw grouped rows of a graph records search.
func (r *Records) graphResults() []db.Row {
	if !r.resultsBuilt {
		r.buildGroupResults(groupOptions{})
		r.resultsBuilt = true
	}
	return r.graphRows
}

// drSelects builds the per-interval sums for daterange graph x-axes.
func (r *Records) drSelects(cols []*groupCol) ([]string, []any) {
	r.drFrom, r.drTo = nil, nil
	inc := r.drInterval
	drCol := r.Layout.Column(r.drColumn)
	field := drCol.Field()
	fieldLink := ""
	if lp := drCol.LinkParent(); lp != nil {
		fieldLink = lp.Field()
	}
	fromField, toField := drCol.FromField(), drCol.ToField()
	sel := []string{"MIN( " + q(field+"."+fromField) + ` ) AS "start_date"`, "MAX( " + q(field+"."+toField) + ` ) AS "end_date"`}
	st := r.store()
	cvo := r.cvoValues()
	search := r.searchQuery(searchOpts{joinOpts: joinOpts{search: true, prefetch: true, hasPrefetch: true, linked: false, hasLinked: true, currentVersionOnly: cvo}, rewind: r.RewindValues()})
	if fieldLink != "" {
		sel = append(sel, "MIN( "+q(fieldLink+"."+fromField)+` ) AS "start_date_link"`, "MAX( "+q(fieldLink+"."+toField)+` ) AS "end_date_link"`)
	}
	jr := newJoinRenderer(r.e, formatRewind(r.RewindValues()))
	jr.layout = r.Layout
	if l := r.linkedJoins(joinOpts{search: true, prefetch: true, hasPrefetch: true, currentVersionOnly: cvo}); l != nil {
		jr.render("current", "me", []*joinSpec{l})
	}
	jr.render("current", "me", []*joinSpec{r.versionJoin(cvo, st.jpfetch(joinOpts{search: true, prefetch: true, hasPrefetch: true, linked: false, hasLinked: true, currentVersionOnly: cvo}))})
	var wargs []any
	where := renderCond(condAnd(search), &wargs)
	sql := "SELECT " + strings.Join(sel, ", ") + ` FROM "current" "me"` + jr.sb.String()
	if where != "" {
		sql += " WHERE " + where
	}
	row := r.e.DB.QueryRow(sql, append(append([]any{}, jr.args...), wargs...)...)
	get := func(k string) any {
		if row == nil {
			return nil
		}
		return row[k]
	}
	var from, to *time.Time
	if r.From != nil {
		t := *r.From
		from = &t
	} else {
		from = minMaxDate("min", get("start_date"), get("start_date_link"))
	}
	if r.To != nil {
		t := *r.To
		to = &t
	} else {
		to = minMaxDate("max", get("end_date"), get("end_date_link"))
	}
	if from == nil || to == nil {
		return nil, nil
	}
	f, t := truncateTo(*from, inc), truncateTo(*to, inc)
	r.drFrom, r.drTo = &f, &t
	fromFull, toFull := q(field+"."+fromField), q(field+"."+toField)
	var fromLink, toLink string
	if fieldLink != "" {
		fromLink, toLink = q(fieldLink+".from"), q(fieldLink+".to")
	}
	var dy *groupCol
	for _, c := range cols {
		if c.id == r.drYAxisID {
			dy = c
			break
		}
	}
	if dy == nil {
		Panic("Y-axis column %d not found for daterange graph", r.drYAxisID)
	}
	colVal := q(st.fqvalue(dy.column, joinOpts{search: true, prefetch: true, hasPrefetch: true}, false))
	caseSQL := func(from, to, sum string) string {
		if fieldLink != "" {
			return "CASE WHEN (" + fromFull + " < " + from + " OR " + fromLink + " < " + from + ") AND (" +
				toFull + " >= " + to + " OR " + toLink + " >= " + to + ") THEN " + sum + " ELSE 0 END"
		}
		return "CASE WHEN " + fromFull + " < " + from + " AND " + toFull + " >= " + to + " THEN " + sum + " ELSE 0 END"
	}
	var selects []string
	p := f
	for p.Unix() <= t.Unix() {
		fromQ := sqlQuote(db.FormatDate(addInterval(p, inc, 1)))
		toQ := sqlQuote(db.FormatDate(p))
		sum := colVal
		if dy.operator == "count" {
			sum = "1"
		}
		selects = append(selects, "SUM( "+caseSQL(fromQ, toQ, sum)+" ) AS "+qa(itoa(p.Unix())))
		if lp := dy.column.LinkParent(); lp != nil {
			lsum := q(st.fqvalue(lp, joinOpts{linked: true, hasLinked: true, search: true, prefetch: true, hasPrefetch: true}, false))
			if dy.operator == "count" {
				lsum = "1"
			}
			selects = append(selects, "SUM( "+caseSQL(fromQ, toQ, lsum)+" ) AS "+qa(itoa(p.Unix())+"_link"))
		}
		p = addInterval(p, inc, 1)
	}
	return selects, nil
}

func sqlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func minMaxDate(action string, a, b any) *time.Time {
	parse := func(v any) *time.Time {
		if !perlTrue(v) {
			return nil
		}
		s, _ := db.AsString(v)
		t, ok := db.ParseDBDateTime(s)
		if !ok {
			return nil
		}
		return &t
	}
	d1, d2 := parse(a), parse(b)
	if d2 == nil {
		return d1
	}
	if d1 == nil {
		return d2
	}
	if action == "min" {
		if d1.Unix() < d2.Unix() {
			return d1
		}
	} else if d1.Unix() > d2.Unix() {
		return d1
	}
	return d2
}

// VisibleGraphs returns the graphs that a user can see in a layout, ordered
// by title (GADS::Graphs->all).
func (e *Engine) VisibleGraphs(layout *Layout, user *User) []*Graph {
	rows := e.DB.Query(`SELECT DISTINCT "me"."id", "me"."title" FROM "graph" "me" LEFT JOIN "group" "group" ON "group"."id" = "me"."group_id" LEFT JOIN "user_group" "user_groups" ON "user_groups"."group_id" = "group"."id" WHERE "me"."instance_id" = ? AND ( ( "me"."group_id" IS NULL AND "me"."is_shared" = 1 ) OR ( "me"."is_shared" = 1 AND "user_groups"."user_id" = ? ) OR "me"."user_id" = ? ) ORDER BY "me"."title", "me"."id"`,
		layout.InstanceID, user.ID, user.ID)
	var out []*Graph
	for _, r := range rows {
		id, _ := db.AsInt64(r["id"])
		out = append(out, e.LoadGraph(id, layout, user))
	}
	return out
}

// AllGraphs is GADS::Graphs->all: the graphs a user can see in a layout,
// each marked whether the user has selected it for display
func (e *Engine) AllGraphs(layout *Layout, user *User) []*Graph {
	selected := map[int64]bool{}
	for _, r := range e.DB.Query(`SELECT "me"."id" FROM "graph" "me" LEFT JOIN "user_graph" "user_graphs" ON "user_graphs"."graph_id" = "me"."id" WHERE "me"."instance_id" = ? AND "user_graphs"."user_id" = ?`, layout.InstanceID, user.ID) {
		id, _ := db.AsInt64(r["id"])
		selected[id] = true
	}
	graphs := e.VisibleGraphs(layout, user)
	for _, g := range graphs {
		g.Selected = selected[g.ID]
	}
	return graphs
}

// XAxisName is the name of the x-axis column ("" for a graph of all the
// columns of the view)
func (g *Graph) XAxisName() string {
	if g.XAxis == 0 {
		return ""
	}
	col := g.Layout.Column(g.XAxis)
	if col == nil {
		Panic(`Can't call method "name" on an undefined value`)
	}
	return col.Name
}

// LabelsEncoded is Graph::Data labels_encoded: the labels of the series with
// the label HTML-encoded
func (d *GraphData) LabelsEncoded() []any {
	out := []any{}
	for _, l := range d.Labels {
		out = append(out, map[string]any{
			"color": l.Color, "showlabel": l.Showlabel, "showline": l.Showline,
			"markeroptions": l.Markeroptions, "label": htmlEscape(l.Label),
		})
	}
	return out
}

// SetGraphs is Result::User->graphs: the graphs of a table the user has
// selected (added if not selected, others of the table removed)
func (u *User) SetGraphs(instanceID int64, graphs []string) {
	for _, g := range graphs {
		if n, _ := u.e.DB.QueryInt64(`SELECT COUNT(*) FROM "user_graph" WHERE "user_id" = ? AND "graph_id" = ?`, u.ID, g); n == 0 {
			u.e.DB.InsertMap("user_graph", map[string]any{"user_id": u.ID, "graph_id": g})
		}
	}
	sql := `DELETE FROM "user_graph" WHERE "id" IN (SELECT "me"."id" FROM "user_graph" "me" JOIN "graph" "graph" ON "graph"."id" = "me"."graph_id" WHERE "me"."user_id" = ? AND "graph"."instance_id" = ?`
	args := []any{u.ID, instanceID}
	for _, g := range graphs {
		sql += ` AND "me"."graph_id" != ?`
		args = append(args, g)
	}
	u.e.DB.Exec(sql+`)`, args...)
}
