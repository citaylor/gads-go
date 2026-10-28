package gads

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// Port of GADS::Globe and GADS::RecordsGlobe.

// GlobeOptions are the options of a globe (group/color/label column IDs,
// which can be "parent_child" for curval sub-fields, or -1 for count).
type GlobeOptions struct {
	Layout     *Layout
	User       *User
	View       *View
	GroupColID string
	ColorColID string
	LabelColID string
}

// Globe is a globe (map) of records.
type Globe struct {
	e    *Engine
	o    GlobeOptions
	recs *Records

	groupCol, groupColParent *Column
	colorCol, colorColParent *Column
	labelCol, labelColParent *Column

	usedColorKeys map[string]bool
	graph         *graphBuilder
	data          *GlobeData
}

// GlobeData is the globe data (traces and params).
type GlobeData struct {
	Data   []map[string]any
	Params map[string]any
}

// NewGlobe creates a globe.
func (e *Engine) NewGlobe(o GlobeOptions) *Globe {
	g := &Globe{e: e, o: o, usedColorKeys: map[string]bool{}}
	l := o.Layout
	g.groupCol = l.Column(globeParseCol(o.GroupColID, false))
	g.groupColParent = l.Column(globeParseCol(o.GroupColID, true))
	g.colorCol = l.Column(globeParseCol(o.ColorColID, false))
	g.colorColParent = l.Column(globeParseCol(o.ColorColID, true))
	g.labelCol = l.Column(globeParseCol(o.LabelColID, false))
	g.labelColParent = l.Column(globeParseCol(o.LabelColID, true))
	return g
}

var reGlobeCol = regexp.MustCompile(`^([0-9]+)_([0-9]+)$`)

func globeParseCol(in string, parent bool) int64 {
	if in == "" || in == "0" {
		return 0
	}
	var p, c string
	if m := reGlobeCol.FindStringSubmatch(in); m != nil {
		p, c = m[1], m[2]
	} else {
		c = in
	}
	s := c
	if parent {
		s = p
	}
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}

func globeOperator(c *Column) string {
	if c != nil && c.Numeric() {
		return "sum"
	}
	return "max"
}

func (g *Globe) colorColIsCount() bool { return g.o.ColorColID == "-1" }

func (g *Globe) hasColorCol() bool { return g.colorCol != nil || g.colorColIsCount() }

func (g *Globe) hasLabelCol() bool {
	if g.labelCol != nil {
		return true
	}
	return g.o.LabelColID != "" && perlNumber(g.o.LabelColID) < 0
}

func (g *Globe) isChoropleth() bool {
	if !g.hasColorCol() {
		return false
	}
	if g.colorColIsCount() {
		return true
	}
	return g.colorCol != nil && g.colorCol.Numeric()
}

// IsGroup returns whether the globe's records are grouped.
func (g *Globe) IsGroup() bool {
	return g.hasColorCol() || g.groupCol != nil || g.hasLabelCol()
}

// Records returns the globe's records (RecordsGlobe).
func (g *Globe) Records() *Records {
	if g.recs == nil {
		r := g.e.NewRecords(g.o.Layout, g.o.User)
		r.isGlobe = true
		r.globeGroup = g.IsGroup()
		r.groupValuesNotIndex = true
		r.MaxResults = 1000
		r.View = g.o.View
		g.recs = r
	}
	return g.recs
}

// Clear clears the globe's records and data.
func (g *Globe) Clear() {
	if g.recs != nil {
		g.recs.Clear()
	}
	g.data = nil
}

func (g *Globe) columns() []*Column {
	cols := append([]*Column(nil), g.Records().ColumnsRender()...)
	for _, c := range append([]*Column(nil), cols...) {
		if c.IsCurcommon() {
			for _, f := range c.CurvalFields() {
				if f != nil {
					cols = append(cols, f)
				}
			}
		}
	}
	return cols
}

func (g *Globe) columnsGlobe() []*Column {
	var out []*Column
	for _, c := range g.columns() {
		if c.ReturnTypeFn() == "globe" {
			out = append(out, c)
		}
	}
	return out
}

func (g *Globe) getColor(value string) string {
	if g.graph == nil {
		g.graph = &graphBuilder{g: &Graph{e: g.e}, colors: map[string]bool{}, inUse: map[string]string{}}
		for _, c := range graphColors {
			g.graph.colors[c] = true
		}
	}
	return g.graph.getColor(value)
}

// Colors returns the colours used (key/color pairs).
func (g *Globe) Colors() []map[string]string {
	var keys []string
	for k := range g.usedColorKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []map[string]string
	for _, k := range keys {
		out = append(out, map[string]string{"key": k, "color": g.getColor(k)})
	}
	return out
}

func globeToSVG(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

type globeExtra struct {
	col, parent *Column
	operator    string
	group       bool
}

type globeTitle struct {
	col   *Column
	value string
}

type globeItem struct {
	idCount    any
	valueColor any
	color      string
	valueLabel any
	valueGroup any
	currentID  int64
	titles     []globeTitle
}

type globeGroupSum struct{ text, sum any }

type globeValues struct {
	labelSum, colorSum, groupSum *float64
	groupSums                    []globeGroupSum
	hasGroupSums                 bool
	labelText, colorText         map[string]float64
	groupText                    map[string]float64
	color                        string
	hover                        []string
}

func addTo(p **float64, v float64) {
	if *p == nil {
		z := 0.0
		*p = &z
	}
	**p += v
}

func globeSortedKeys(m map[string]float64) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// getColumn is DBIx::Class get_column of a result row: a column that was
// not selected is an error.
func getColumn(row db.Row, key string) any {
	v, ok := row[key]
	if !ok {
		Fail("No such column '%s' on GADS::Schema::Result::Current", key)
	}
	return v
}

// formatCurcommon is _format_curcommon. The sub-field values are those of
// the parent given (that of the globe's label or group column, a Perl quirk).
func (g *Globe) formatCurcommon(col, parent *Column, row db.Row) any {
	records := g.Records()
	field := records.aggregateName(col, nil, "max")
	if !perlTrue(row[field]) {
		return nil
	}
	var vals []string
	for _, f := range col.CurvalFields() {
		if f == nil {
			continue
		}
		v := row[records.aggregateName(f, parent, "max")]
		if !perlTrue(v) {
			vals = append(vals, "")
		} else {
			vals = append(vals, graphStr(v))
		}
	}
	return `<a href="/record/` + graphStr(row[field]) + `">` + FormatValue(vals) + `</a>`
}

// Data returns the globe data (_build_data).
func (g *Globe) Data() *GlobeData {
	if g.data != nil {
		return g.data
	}
	records := g.Records()
	layout := records.Layout
	isGroup := g.IsGroup()

	var extra []globeExtra
	if g.groupCol != nil {
		extra = append(extra, globeExtra{col: g.groupCol, parent: g.groupColParent, operator: globeOperator(g.groupCol), group: !g.groupCol.Numeric()})
	}
	if g.colorCol != nil {
		extra = append(extra, globeExtra{col: g.colorCol, parent: g.colorColParent, operator: globeOperator(g.colorCol), group: !g.colorCol.Numeric()})
	}
	if g.labelCol != nil {
		extra = append(extra, globeExtra{col: g.labelCol, parent: g.labelColParent, group: !g.labelCol.Numeric()})
	}
	if records.View != nil && records.View.ID != 0 {
		existing := map[int64]bool{}
		for _, x := range extra {
			existing[x.col.ID] = true
		}
		if !isGroup {
			for _, c := range records.ColumnsRender() {
				if !existing[c.ID] {
					extra = append(extra, globeExtra{col: c})
				}
			}
		}
		gc := layout.All(AllOptions{IsGlobe: true, UserCanRead: true})
		hasGlobe := false
		for _, x := range extra {
			if x.col.ReturnTypeFn() == "globe" {
				hasGlobe = true
			}
		}
		if len(gc) == 1 && !hasGlobe {
			extra = append(extra, globeExtra{col: gc[0], group: isGroup})
		}
	} else {
		for _, c := range layout.All(AllOptions{UserCanRead: true}) {
			extra = append(extra, globeExtra{col: c})
		}
	}
	if isGroup {
		for i := range extra {
			if extra[i].col.ReturnTypeFn() == "globe" {
				extra[i].group = true
			}
		}
	}
	var gcols []*groupCol
	for _, x := range extra {
		gc := &groupCol{id: x.col.ID, operator: x.operator, group: x.group}
		if gc.operator == "" {
			gc.operator = "max"
		}
		if x.parent != nil {
			gc.parentID = x.parent.ID
		}
		gcols = append(gcols, gc)
	}
	records.setGraphColumns(gcols)

	countries := map[string][]*globeItem{}
	var countryOrder []string
	addCountry := func(c string, item *globeItem) {
		if _, ok := countries[c]; !ok {
			countryOrder = append(countryOrder, c)
		}
		countries[c] = append(countries[c], item)
	}

	if isGroup {
		records.resultsBuilt = false
		rows := records.graphResults()
		for _, row := range rows {
			var thisCountries []string
			var valueColor, valueLabel, valueGroup any
			var color string
			if g.hasColorCol() {
				if g.colorColIsCount() {
					valueColor = getColumn(row, "id_count")
				} else {
					field := records.aggregateName(g.colorCol, g.colorColParent, globeOperator(g.colorCol))
					valueColor = getColumn(row, field)
					if !g.colorCol.Numeric() {
						color = g.getColor(graphStr(valueColor))
						g.usedColorKeys[graphStr(valueColor)] = true
					}
				}
			}
			if g.labelCol != nil {
				if g.labelCol.Type == "curval" {
					valueLabel = g.formatCurcommon(g.labelCol, g.labelColParent, row)
				} else {
					valueLabel = getColumn(row, records.aggregateName(g.labelCol, g.labelColParent, "max"))
				}
				if !perlTrue(valueLabel) {
					valueLabel = "<blank>"
				}
			}
			if g.groupCol != nil {
				field := records.aggregateName(g.groupCol, g.groupColParent, "max")
				if globeOperator(g.groupCol) == "sum" {
					field += "_sum"
				}
				if g.groupCol.Type == "curval" {
					valueGroup = g.formatCurcommon(g.groupCol, g.groupColParent, row)
				} else {
					valueGroup = getColumn(row, field)
					if !perlTrue(valueGroup) {
						valueGroup = "<blank>"
					}
				}
			}
			for _, c := range g.columnsGlobe() {
				v := getColumn(row, records.aggregateName(c, nil, "max"))
				if !perlTrue(v) {
					continue
				}
				thisCountries = append(thisCountries, graphStr(v))
			}
			for _, c := range thisCountries {
				addCountry(c, &globeItem{idCount: row["id_count"], valueColor: valueColor, color: color, valueLabel: valueLabel, valueGroup: valueGroup})
			}
		}
	} else {
		for {
			rec := records.Single()
			if rec == nil {
				break
			}
			var titles []globeTitle
			var thisCountries []string
			for _, c := range g.columns() {
				d := rec.Fields()[c.ID]
				if d == nil {
					continue
				}
				if rec.ParentID() != 0 && !datumChildUnique(d) {
					continue
				}
				if c.ReturnTypeFn() == "globe" {
					thisCountries = append(thisCountries, d.AsString())
				} else {
					if c.Type == "rag" {
						continue
					}
					if s := d.AsString(); s != "" {
						titles = append(titles, globeTitle{col: c, value: s})
					}
				}
			}
			for _, c := range thisCountries {
				addCountry(c, &globeItem{currentID: rec.CurrentID(), titles: titles})
			}
		}
	}

	type itemReturn struct {
		hover    string
		location string
		color    any
		z        any
		label    any
		hasLabel bool
	}
	var itemReturns []itemReturn
	for _, country := range countryOrder {
		items := countries[country]
		v := &globeValues{labelText: map[string]float64{}, colorText: map[string]float64{}, groupText: map[string]float64{}}
		for _, item := range items {
			if g.hasLabelCol() {
				if g.labelCol != nil && g.labelCol.Numeric() {
					addTo(&v.labelSum, 0)
					if perlTrue(item.valueLabel) {
						addTo(&v.labelSum, perlNumber(item.valueLabel))
					}
					if g.groupCol != nil {
						v.hasGroupSums = true
						v.groupSums = append(v.groupSums, globeGroupSum{text: item.valueGroup, sum: item.valueLabel})
					}
				} else {
					label := ""
					if item.valueLabel != nil {
						label = globeToSVG(graphStr(item.valueLabel))
					}
					if label == "" || label == "0" {
						label = "_count"
					}
					v.labelText[label] += perlNumber(item.idCount)
				}
			}
			if g.hasColorCol() {
				if g.isChoropleth() {
					addTo(&v.colorSum, 0)
					if perlTrue(item.valueColor) {
						addTo(&v.colorSum, perlNumber(item.valueColor))
					}
					v.hasGroupSums = true
					if g.groupCol != nil && !(g.labelCol != nil && g.o.ColorColID == g.o.LabelColID) {
						v.groupSums = append(v.groupSums, globeGroupSum{text: item.valueGroup, sum: item.valueColor})
					}
				} else {
					v.colorText[graphStr(item.valueColor)] += perlNumber(item.idCount)
					if v.color == "" || v.color == item.color {
						v.color = item.color
					} else {
						v.color = "grey"
					}
				}
			}
			if g.groupCol != nil {
				if g.groupCol.Numeric() {
					addTo(&v.groupSum, 0)
					if perlTrue(item.valueGroup) {
						addTo(&v.groupSum, perlNumber(item.valueGroup))
					}
				} else {
					v.groupText[graphStr(item.valueGroup)] += perlNumber(item.idCount)
				}
			}
			if !isGroup {
				var sb strings.Builder
				for _, t := range item.titles {
					if t.col.Type == "file" {
						continue
					}
					sb.WriteString("<b>" + t.col.Name + ":</b> " + globeToSVG(t.value) + "<br>")
				}
				t := sb.String()
				if len(items) > 1 {
					t = "<i>Record ID " + strconv.FormatInt(item.currentID, 10) + "</i><br>" + t
				}
				v.hover = append(v.hover, t)
			}
		}
		var groupSums any
		if v.hasGroupSums {
			var parts []string
			for _, gs := range v.groupSums {
				parts = append(parts, globeToSVG(graphStr(gs.text)+": "+graphStr(gs.sum)))
			}
			groupSums = strings.Join(parts, "<br>")
		}
		numStr := func(p *float64) string {
			if p == nil {
				return ""
			}
			return perlFloatString(*p)
		}
		textJoin := func(m map[string]float64, count bool) string {
			var parts []string
			for _, k := range globeSortedKeys(m) {
				if count && k == "_count" {
					parts = append(parts, perlFloatString(m[k]))
				} else {
					parts = append(parts, k+": "+perlFloatString(m[k]))
				}
			}
			return strings.Join(parts, "<br>")
		}
		var hover string
		switch {
		case g.isChoropleth() && g.groupCol != nil:
			hover = graphStr(groupSums)
		case g.isChoropleth():
			hover = "Total: " + numStr(v.colorSum)
		case g.labelCol != nil && g.labelCol.Numeric() && g.groupCol != nil:
			hover = graphStr(groupSums)
		case g.labelCol != nil && g.labelCol.Numeric():
			hover = "Total: " + numStr(v.labelSum)
		case g.colorCol != nil:
			hover = textJoin(v.colorText, false)
		case g.groupCol != nil && g.groupCol.Numeric():
			hover = "Total: " + numStr(v.groupSum)
		case g.groupCol != nil:
			hover = textJoin(v.groupText, false)
		case g.hasLabelCol():
			hover = textJoin(v.labelText, true)
		default:
			hover = strings.Join(v.hover, "<br>")
		}
		hover = country + "<br>" + hover
		ir := itemReturn{hover: hover, location: country}
		if v.color != "" {
			ir.color = v.color
		}
		if v.colorSum != nil {
			ir.z = *v.colorSum
		}
		if g.hasLabelCol() {
			ir.hasLabel = true
			switch {
			case g.groupCol != nil && g.labelCol != nil && g.labelCol.Numeric():
				ir.label = groupSums
			case g.labelCol != nil && g.labelCol.Numeric():
				if v.labelSum != nil {
					ir.label = *v.labelSum
				}
			default:
				ir.label = textJoin(v.labelText, true)
			}
		}
		itemReturns = append(itemReturns, ir)
	}

	if len(itemReturns) == 0 && len(g.columnsGlobe()) == 0 {
		g.e.Mistake("There are no globe fields in this view to display")
	}

	z := []any{}
	text := []any{}
	locations := []any{}
	for _, ir := range itemReturns {
		z = append(z, ir.z)
		text = append(text, ir.hover)
		locations = append(locations, ir.location)
	}
	typ := "scattergeo"
	if isGroup {
		typ = "choropleth"
	}
	tr := map[string]any{
		"z":            z,
		"text":         text,
		"locations":    locations,
		"showscale":    g.isChoropleth(),
		"type":         typ,
		"hoverinfo":    "text",
		"locationmode": "country names",
	}
	switch {
	case g.isChoropleth():
		tr["countrycolors"] = nil
	case g.colorCol != nil:
		cc := []any{}
		for _, ir := range itemReturns {
			cc = append(cc, ir.color)
		}
		tr["countrycolors"] = cc
	default:
		cc := []any{}
		for range itemReturns {
			cc = append(cc, "#D3D3D3")
		}
		tr["countrycolors"] = cc
	}
	if !isGroup {
		tr["marker"] = map[string]any{"size": 15, "line": map[string]any{"width": 2}}
	}
	data := []map[string]any{tr}
	if g.hasLabelCol() {
		lt := []any{}
		ht := []any{}
		for _, ir := range itemReturns {
			lt = append(lt, ir.label)
			ht = append(ht, ir.hover)
		}
		data = append(data, map[string]any{
			"text":         lt,
			"locations":    locations,
			"hoverinfo":    "text",
			"hovertext":    ht,
			"mode":         "text",
			"type":         "scattergeo",
			"locationmode": "country names",
		})
	}
	globeFields := []any{}
	for _, x := range extra {
		if x.col.ReturnTypeFn() == "globe" {
			globeFields = append(globeFields, x.col.Field())
		}
	}
	var viewID any
	if g.o.View != nil {
		viewID = g.o.View.ID
	}
	var dvle any
	if d := g.o.Layout.DefaultViewLimitExtraID(); d != nil {
		dvle = *d
	}
	g.data = &GlobeData{
		Data: data,
		Params: map[string]any{
			"view_id":                     viewID,
			"layout_identifier":           g.o.Layout.Identifier(),
			"globe_fields":                globeFields,
			"default_view_limit_extra_id": dvle,
		},
	}
	return g.data
}
