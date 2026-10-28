package web

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/perl"
)

var (
	dtColumnNameRe   = regexp.MustCompile(`^columns\[([0-9]+)\]\[name\]$`)
	dtColumnSearchRe = regexp.MustCompile(`^columns\[([0-9]+)\]\[search\]\[value\]$`)
	hSpaceRe         = regexp.MustCompile(`^[ \t]+|[ \t]+$`)
)

// allParams is Hash::MultiValue->new(query_parameters->flatten,
// body_parameters->flatten)
func (c *Ctx) allParams() *multiValue {
	m := newMultiValue()
	for _, src := range []*multiValue{c.query, c.body} {
		for _, k := range src.keys {
			for _, v := range src.vals[k] {
				m.add(k, v)
			}
		}
	}
	return m
}

// mvKeys is Hash::MultiValue keys: a key for each value
func mvKeys(m *multiValue) []string {
	var out []string
	for _, k := range m.keys {
		for range m.vals[k] {
			out = append(out, k)
		}
	}
	return out
}

// perlNum is a parameter used as a number in Perl
func perlNum(s string) float64 {
	return perl.Numify(s)
}

// any ['get', 'post'] => /api/:sheet/records (API.pm:545, _get_records)
func (s *Server) hAPIRecords(c *Ctx) string {
	sheetname, _ := c.Param("sheet")
	user := c.LoggedInUser()
	layout := c.Instances().LayoutByShortname(sheetname)
	if layout == nil {
		gads.Fail("Table not found: %s", sheetname)
	}
	var view *gads.View
	if rid, _ := c.QueryParam("curval_record_id"); !truthy(rid) {
		view = c.CurrentView(layout)
	}

	params := c.allParams()
	get := func(k string) string { v, _ := params.get(k); return v }
	start := perlNum(get("start"))
	length := perlNum(get("length"))
	if length == 0 {
		length = 25
	}
	length = float64(s.clampRows(int64(length)))

	records := s.E.NewRecords(layout, user)
	records.View = view
	records.Rows = int(length)
	records.Page = int(1 + math.Ceil(start/length))
	records.Rewind = sessionRewind(c)
	records.ViewLimitExtraID = c.currentViewLimitExtraID(layout)
	records.ViewLimitOverrideID = c.currentViewLimitOverrideID(layout)
	if gf, _ := c.QueryParam("group_filter"); truthy(gf) {
		f := false
		records.IsGroupOverride = &f
	}
	if rid, _ := c.QueryParam("curval_record_id"); truthy(rid) {
		lid, _ := c.QueryParam("curval_layout_id")
		records.ForCurval = &struct{ RecordID, LayoutID int64 }{RecordID: perlInt(rid), LayoutID: perlInt(lid)}
	}

	// Map rendered columns to IDs
	mapping := map[int64]string{}
	for _, key := range mvKeys(params) {
		if m := dtColumnNameRe.FindStringSubmatch(key); m != nil {
			idx, _ := strconv.ParseInt(m[1], 10, 64)
			mapping[idx] = get(key)
		}
	}

	// Look for column filters
	var filters []gads.AdditionalFilter
	for _, key := range mvKeys(params) {
		m := dtColumnSearchRe.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		idx, _ := strconv.ParseInt(m[1], 10, 64)
		if records.IsGroup() {
			idx--
		}
		search := get(key)
		if !truthy(search) {
			continue
		}
		id, _ := strconv.ParseInt(mapping[idx], 10, 64)
		col := layout.Column(id)
		if col == nil {
			continue
		}
		filters = append(filters, gads.AdditionalFilter{
			ID:     col.ID,
			Value:  []any{search},
			IsText: !(col.IsCurcommon() || col.Type == "id"),
		})
	}
	records.AdditionalFilters = filters

	if search := get("search[value]"); truthy(search) {
		records.SetSearch(hSpaceRe.ReplaceAllString(search, ""))
	}

	// Configure table sort, but only if ordering column submitted
	if oi, ok := params.get("order[0][column]"); ok {
		idx := int(perlNum(oi))
		if records.IsGroup() {
			idx--
		}
		cols := records.ColumnsRender()
		if idx < 0 {
			idx += len(cols)
		}
		var colOrder *gads.Column
		if idx >= 0 && idx < len(cols) {
			colOrder = cols[idx]
		}
		if colOrder == nil || !colOrder.UserCan("read") {
			gads.Fail("Invalid column ID for sort")
		}
		records.SetSort([]gads.SortSpec{{ID: colOrder.ID, ParentID: colOrder.ParentID, Type: get("order[0][dir]")}})
	}

	var draw any
	if d, ok := params.get("draw"); ok {
		draw = d
	}
	count := records.Count()
	ret := map[string]any{
		"draw":            draw,
		"recordsTotal":    count,
		"recordsFiltered": count,
	}
	data := []any{}
	for _, rec := range records.Results() {
		row := map[string]any{}
		if records.IsGroup() {
			var fs []string
			for _, gcol := range records.GroupCols() {
				fs = append(fs, fmtInt(gcol.ID)+"="+uriEscapeUTF8(rec.GetFieldValue(gcol).FilterValue()))
			}
			desc := "records"
			if rec.IDCount() == 1 {
				desc = "record"
			}
			row["_count"] = map[string]any{
				"column_id": "_count",
				"values":    []any{fmtInt(rec.IDCount()) + " " + desc},
				"type":      "string",
				"url":       "group_filter=1&" + strings.Join(fs, "&"),
			}
		} else {
			row["_id"] = rec.CurrentID()
		}
		for _, col := range records.ColumnsRender() {
			row[col.FullID()] = gads.ForTable(rec.GetFieldValue(col))
		}
		data = append(data, row)
	}
	ret["data"] = data
	if records.IsGroup() {
		ret["recordsTotalUngrouped"] = count
		ret["recordsTotal"] = len(data)
		ret["recordsFiltered"] = len(data)
	}
	if agg := records.AggregateResults(); agg != nil {
		var a map[string]any
		for _, col := range records.ColumnsRender() {
			if col.Aggregate == "" {
				continue
			}
			if a == nil {
				a = map[string]any{}
			}
			a[fmtInt(col.ID)] = gads.ForTable(agg.GetFieldValue(col))
		}
		ret["aggregate"] = a
	}
	c.ctype = "application/json; charset=UTF-8"
	return gads.EncodeJSON(ret)
}
