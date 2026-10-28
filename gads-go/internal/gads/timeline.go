package gads

import (
	"sort"
	"strings"
	"time"
)

// Timeline is a port of GADS::Timeline: the items of records plotted on a
// timeline or calendar.
type Timeline struct {
	Type           string // timeline or calendar
	Records        *Records
	LabelColID     int64
	GroupColID     int64
	ColorColID     int64
	AllGroupValues bool

	usedColorKeys map[string]bool
	usedColorList []string
	groups        map[string]int
	groupCount    int
	// display_from/display_to (nil when unset or infinite)
	displayFrom, displayTo *time.Time
	allItemsIndex          map[string]bool
	items                  []TimelineItem
	itemsBuilt             bool
	graph                  *graphBuilder
}

// TimelineItem is an item of a timeline (or calendar). DT and DTTo are the
// item's from and to dates (removed by Perl before JSON encoding).
type TimelineItem struct {
	Fields   map[string]any
	DT, DTTo time.Time
}

// TimelineGroup is a group of a timeline.
type TimelineGroup struct {
	ID      int
	Content string
	Order   int
	Style   string
}

// TimelineColor is a colour used in a timeline.
type TimelineColor struct {
	Key, Color string
}

// TimelineData is the result of Records.DataTimeline.
type TimelineData struct {
	Items    []TimelineItem
	Groups   []TimelineGroup
	Colors   []TimelineColor
	Min, Max *time.Time
}

// TimelineOptions are the options to Records.DataTimeline.
type TimelineOptions struct {
	Label, Group, Color int64
	AllGroupValues      bool
}

// NewTimeline creates a timeline of the records.
func NewTimeline(typ string, records *Records) *Timeline {
	return &Timeline{Type: typ, Records: records}
}

func (t *Timeline) init() {
	if t.usedColorKeys == nil {
		t.usedColorKeys = map[string]bool{}
	}
	if t.groups == nil {
		t.groups = map[string]int{}
	}
	if t.allItemsIndex == nil {
		t.allItemsIndex = map[string]bool{}
	}
}

// Colors returns the colours used (Timeline::colors).
func (t *Timeline) Colors() []TimelineColor {
	var out []TimelineColor
	for _, k := range t.usedColorList {
		out = append(out, TimelineColor{Key: k, Color: t.getColor(k)})
	}
	return out
}

func (t *Timeline) useColorKey(k string) {
	t.init()
	if !t.usedColorKeys[k] {
		t.usedColorKeys[k] = true
		t.usedColorList = append(t.usedColorList, k)
	}
}

// getColor uses a Graph::Data instance to get the relevant colours.
func (t *Timeline) getColor(value string) string {
	if t.graph == nil {
		t.graph = &graphBuilder{g: &Graph{e: t.Records.e}, colors: map[string]bool{}, inUse: map[string]string{}}
		for _, c := range graphColors {
			t.graph.colors[c] = true
		}
	}
	return t.graph.getColor(value)
}

// Clear clears the timeline and its records (Timeline::clear).
func (t *Timeline) Clear() {
	t.Records.Clear()
	t.items = nil
	t.itemsBuilt = false
	t.displayFrom = nil
	t.displayTo = nil
	t.groups = nil
	t.groupCount = 0
	t.allItemsIndex = nil
}

// DisplayFrom returns where to show the timeline from by default.
func (t *Timeline) DisplayFrom() *time.Time { return t.displayFrom }

// DisplayTo returns where to show the timeline to by default.
func (t *Timeline) DisplayTo() *time.Time { return t.displayTo }

// Groups returns the group values and their numbers.
func (t *Timeline) Groups() map[string]int {
	t.init()
	return t.groups
}

// tick converts a time to milliseconds since the epoch.
func tick(t time.Time) int64 { return t.Unix() * 1000 }

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

type timelineDate struct {
	from, to  time.Time
	color     string
	column    int64
	daterange bool
	hasTime   bool
	// hasTimeValue is has_time as the Perl value (0/1, or ""/1 for a calc)
	hasTimeValue any
	currentID    int64
}

type timelineValue struct {
	col   *Column
	value Datum
}

// Items returns the items of the timeline (Timeline::_build_items).
func (t *Timeline) Items() []TimelineItem {
	if t.itemsBuilt {
		return t.items
	}
	t.init()
	records := t.Records
	to, from := records.To, records.From
	layout := records.Layout

	colOrNil := func(id int64) *Column {
		if id == 0 {
			return nil
		}
		return layout.Column(id)
	}
	groupCol := colOrNil(t.GroupColID)
	colorCol := colOrNil(t.ColorColID)
	labelCol := colOrNil(t.LabelColID)

	// Add on any extra required columns for labelling etc
	var extra []SortSpec
	for _, c := range []*Column{labelCol, groupCol, colorCol} {
		if c != nil {
			extra = append(extra, SortSpec{ID: c.ID})
		}
	}
	records.ColumnsExtra = extra
	records.clearColumns()

	// Don't show the ID by default on the timeline as it clutters each
	// item. It can be added as a field if required
	var columns []*Column
	for _, c := range records.ColumnsRender() {
		if c.Type != "id" {
			columns = append(columns, c)
		}
	}

	isDateRT := func(rt string) bool { return rt == "daterange" || rt == "date" }
	dateColumnCount := 0
	for _, column := range columns {
		cols := []*Column{column}
		if column.IsCurcommon() {
			cols = append(cols, column.CurvalFields()...)
		}
		for _, col := range cols {
			if isDateRT(col.ReturnTypeFn()) {
				dateColumnCount++
			}
		}
	}

	if t.AllGroupValues && groupCol != nil && groupCol.Fixedvals() {
		for _, val := range groupCol.valuesForTimeline() {
			t.groupCount++
			t.groups[val] = t.groupCount
		}
	}

	var items []TimelineItem
	for {
		record := records.Single()
		if record == nil {
			break
		}
		var groupsToAdd []string
		if groupCol != nil && groupCol.UserCan("read") {
			if d := record.GetFieldValue(groupCol); d != nil {
				groupsToAdd = append(groupsToAdd, d.TextAll()...)
			}
		}
		hasGroups := len(groupsToAdd) > 0
		if !hasGroups {
			groupsToAdd = []string{""}
		}
		if t.GroupColID != 0 {
			// If the grouping value is blank for this record, then set it
			// to a suitable textual value, otherwise it won't be rendered
			for i := range groupsToAdd {
				if groupsToAdd[i] == "" || groupsToAdd[i] == "0" {
					groupsToAdd[i] = "<blank>"
				}
			}
		}

		seqnr := 0
		// AT_BIGCHILL / AT_BIGBANG
		var oldest, newest *time.Time

		for _, groupToAdd := range groupsToAdd {
			var dates []timelineDate
			var values []timelineValue

			for _, column := range columns {
				var ds []Datum
				if d := record.GetFieldValue(column); d != nil {
					ds = append(ds, d)
				}
				if column.IsCurcommon() {
					// The main value (for the pop-up) and also any dates
					// within it to add to the timeline separately
					var dateCols []*Column
					for _, c := range column.CurvalFields() {
						if isDateRT(c.ReturnTypeFn()) {
							dateCols = append(dateCols, c)
						}
					}
					if cd, ok := record.GetFieldValue(column).(*CurcommonDatum); ok {
						for _, v := range cd.AllRecords() {
							for _, dc := range dateCols {
								if sd := v.Record.GetFieldValue(dc); sd != nil {
									ds = append(ds, sd)
								}
							}
						}
					}
				}

				for _, d := range ds {
					// Only show unique items of children, otherwise will
					// be a lot of repeated entries
					if record.ParentID() != 0 && !d.base().childUnique {
						continue
					}
					columnDatum := d.Column()
					rt := columnDatum.ReturnTypeFn()
					if !isDateRT(rt) {
						// Not a date value, push onto labels
						if d.AsString() != "" {
							values = append(values, timelineValue{col: columnDatum, value: d})
						}
						continue
					}

					// Create colour if need be
					var color string
					if t.Type == "calendar" || (colorCol == nil && dateColumnCount > 1) {
						color = t.getColor(column.Name)
						t.useColorKey(column.Name)
					}

					var spans []DateSpan
					isRange := false
					if rt == "daterange" {
						spans = datumSpans(d)
						isRange = true
					} else {
						for _, v := range datumDates(d) {
							spans = append(spans, DateSpan{Start: v, End: v})
						}
					}

					for _, span := range spans {
						start, end := span.Start, span.End
						// Truncate the from of a date-only field, as that
						// is what will have been retrieved from the database
						var fromCheck *time.Time
						if from != nil {
							fc := *from
							if !column.HasTime() {
								fc = truncateDay(fc)
							}
							fromCheck = &fc
						}
						var fromTest, toTest bool
						if records.Exclusive == "from" {
							fromTest = fromCheck == nil || end.After(*fromCheck)
						} else {
							fromTest = fromCheck == nil || !end.Before(*fromCheck)
						}
						if records.Exclusive == "to" {
							toTest = to == nil || start.Before(*to)
						} else {
							toTest = to == nil || !start.After(*to)
						}
						if !fromTest || !toTest {
							continue
						}
						seqnr++
						var cid int64
						if r := d.base().record; r != nil {
							cid = r.CurrentID()
						}
						dates = append(dates, timelineDate{
							from: start, to: end, color: color, column: columnDatum.ID,
							daterange: isRange, hasTime: column.HasTime(), hasTimeValue: perlHasTime(column), currentID: cid,
						})
					}
				}
			}

			for _, dd := range dates {
				if oldest == nil || dd.from.Before(*oldest) {
					f := dd.from
					oldest = &f
				}
				if newest == nil || dd.to.After(*newest) {
					tt := dd.to
					newest = &tt
				}
			}

			var titles []timelineValue
			if labelCol == nil {
				for _, v := range values {
					// RAG colours are not much use on a label; don't add
					// grouping or colour text to the title
					if v.col.Type == "rag" {
						continue
					}
					if groupCol != nil && groupCol.ID == v.col.ID {
						continue
					}
					if colorCol != nil && colorCol.ID == v.col.ID {
						continue
					}
					titles = append(titles, v)
				}
			} else if label := record.GetFieldValue(labelCol); label != nil {
				if label.AsString() != "" {
					titles = append(titles, timelineValue{col: layout.Column(labelCol.ID), value: label})
				}
			}

			// If a specific field is set to colour-code by, then use that
			// and override any previous colours set for multiple date fields
			var itemColor string
			if colorCol != nil {
				if c := record.GetFieldValue(colorCol); c != nil {
					if key := c.AsString(); key != "" {
						itemColor = t.getColor(key)
						t.useColorKey(key)
					}
				}
			}

			var itemGroup any
			if groupToAdd != "" && groupToAdd != "0" {
				g, ok := t.groups[groupToAdd]
				if !ok || g == 0 {
					t.groupCount++
					g = t.groupCount
					t.groups[groupToAdd] = g
				}
				itemGroup = g
			}

			// Create title label, filenames are ugly
			var parts []string
			for _, ti := range titles {
				if ti.col.Type != "file" {
					parts = append(parts, ti.value.AsString())
				}
			}
			title := strings.Join(parts, " - ")
			titleAbr := title
			if len([]rune(title)) > 50 {
				titleAbr = string([]rune(title)[:45]) + "..."
			}

			for _, dd := range dates {
				add := ""
				if dateColumnCount > 1 {
					add = " (" + layout.Column(dd.column).Name + ")"
				}
				cid := dd.currentID
				if cid == 0 {
					cid = record.CurrentID()
				}

				if t.Type == "calendar" {
					items = append(items, TimelineItem{Fields: map[string]any{
						"url":   "/record/" + itoa(cid),
						"color": nullIfEmptyStr(dd.color),
						"title": titleAbr + add,
						"id":    record.CurrentID(),
						"start": tick(dd.from),
						"end":   tick(dd.to),
					}, DT: dd.from, DTTo: dd.to})
					continue
				}

				// Additional items for the same record and field can appear
				// at any time, in particular if grouped by the field. Ensure
				// that exactly the same item is not added twice.
				uid := itoa(cid) + "+" + itoa(dd.column)
				if groupToAdd != "" && groupToAdd != "0" {
					uid += "+" + groupToAdd
				}
				if t.allItemsIndex[uid] {
					continue
				}
				t.allItemsIndex[uid] = true

				// Exclude ID for pop-up values as it's included in the
				// pop-up title
				popup := []map[string]any{}
				for _, v := range values {
					if v.col.NameShort == "_id" {
						continue
					}
					var value any = v.value.HTML()
					if sd, ok := v.value.(*SerialDatum); ok {
						// The serial is a number in Perl (html of the
						// integer from the database leaves it numeric)
						if n := sd.value(); n != nil {
							value = *n
						} else {
							value = nil
						}
					}
					popup = append(popup, map[string]any{
						"name":  htmlEscape(v.col.Name),
						"value": value,
					})
				}

				item := map[string]any{
					"content":    title + add,
					"id":         uid,
					"current_id": cid,
					"start":      tick(dd.from),
					"group":      itemGroup,
					"column":     dd.column,
					"values":     popup,
				}

				// Set to date field colour unless specific colour field chosen
				if t.ColorColID == 0 && dateColumnCount > 1 {
					itemColor = dd.color
				}
				if itemColor != "" {
					item["style"] = "background-color: " + itemColor
				}

				if dd.daterange {
					// Add one day, otherwise ends at 00:00:00, looking like
					// day is not included
					v := dd.to
					if !dd.hasTime {
						v = v.AddDate(0, 0, 1)
					}
					item["end"] = tick(v)
					item["has_time"] = dd.hasTimeValue
				} else {
					item["single"] = tick(dd.from)
				}
				items = append(items, TimelineItem{Fields: item, DT: dd.from, DTTo: dd.to})
			}
		}

		// Infinite values (no dates in the record) leave the display range
		// undefined
		if newest == nil {
			t.displayFrom = nil
		} else if t.displayFrom == nil || newest.Before(*t.displayFrom) {
			n := *newest
			t.displayFrom = &n
		}
		if oldest == nil {
			t.displayTo = nil
		} else if t.displayTo == nil || oldest.After(*t.displayTo) {
			o := *oldest
			t.displayTo = &o
		}
	}

	if len(items) == 0 && dateColumnCount == 0 {
		t.Records.e.Mistake("There are no date fields in this view to display")
	}

	t.items = items
	t.itemsBuilt = true
	return items
}

func nullIfEmptyStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// datumDates returns the dates of a date (or date calc) datum.
func datumDates(d Datum) []time.Time {
	switch x := d.(type) {
	case *DateDatum:
		return x.Values()
	case *CalcDatum:
		var out []time.Time
		for _, v := range x.Values() {
			if t, ok := v.(time.Time); ok {
				out = append(out, t)
			}
		}
		return out
	}
	return nil
}

// datumSpans returns the spans of a daterange (or daterange calc) datum.
func datumSpans(d Datum) []DateSpan {
	switch x := d.(type) {
	case *DaterangeDatum:
		return x.Values()
	case *CalcDatum:
		var out []DateSpan
		for _, v := range x.Values() {
			if s, ok := v.(DateSpan); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// valuesForTimeline returns the possible values of a fixed-value column
// (values_for_timeline).
func (c *Column) valuesForTimeline() []string {
	var out []string
	if c.IsCurcommon() {
		for _, v := range c.FilteredValues("") {
			out = append(out, v.Value)
		}
		return out
	}
	if c.Type == "person" || c.Type == "createdby" || c.Type == "deletedby" {
		for _, u := range c.People() {
			out = append(out, u.Value)
		}
		return out
	}
	// Enum enumvals exclude deleted values, tree ones (dev) include them
	for _, e := range c.Enumvals() {
		out = append(out, anyToString(e["value"]))
	}
	return out
}

func minTime(items []TimelineItem) *time.Time {
	var m *time.Time
	for _, i := range items {
		if m == nil || i.DT.Before(*m) {
			t := i.DT
			m = &t
		}
	}
	return m
}

func maxTime(items []TimelineItem) *time.Time {
	var m *time.Time
	for _, i := range items {
		if m == nil || i.DT.After(*m) {
			t := i.DT
			m = &t
		}
	}
	return m
}

// DataCalendar returns the items of the records for a calendar.
func (r *Records) DataCalendar() []TimelineItem {
	return NewTimeline("calendar", r).Items()
}

// DataTimeline returns the timeline of the records (Records::data_timeline).
func (r *Records) DataTimeline(o TimelineOptions) TimelineData {
	originalFrom, originalTo := r.From, r.To
	limitQty := originalFrom != nil && originalTo == nil

	timeline := &Timeline{Type: "timeline", Records: r, LabelColID: o.Label, GroupColID: o.Group,
		ColorColID: o.Color, AllGroupValues: o.AllGroupValues}
	r.SeparateRecordsForMulticol = o.Group

	var items []TimelineItem
	var min, max *time.Time

	cmpFail := func() {
		Panic("A DateTime object can only be compared to another DateTime object")
	}

	if limitQty {
		// We may have retrieved values other than the ones we want, for
		// example additional date fields in records where we wanted the
		// other one. Normally we don't want these. However, we will want to
		// add them on if there are not many records in the original set
		r.MaxResults = 100
		retrieved := timeline.Items()
		retrievedCount := r.RecordsRetrievedCount()

		// AFTER
		max = timeline.DisplayTo()
		firstFrom := timeline.DisplayFrom()
		var after []TimelineItem
		for _, it := range retrieved {
			if max == nil {
				cmpFail()
			}
			if it.DT.Before(*max) {
				items = append(items, it)
			} else {
				after = append(after, it)
			}
		}
		// If we still have "room" after taking the ones we want, start
		// taking other values that were retrieved at the same time
		if retrievedCount < 100 {
			over := append([]TimelineItem(nil), after...)
			sort.SliceStable(over, func(i, j int) bool { return over[i].DT.Before(over[j].DT) })
			for len(items) < 100 && len(over) > 0 {
				items = append(items, over[0])
				over = over[1:]
			}
		}
		timeline.Clear()

		// BEFORE: retrieve up to but not including the previous retrieval
		f := *originalFrom
		r.To = &f
		r.Exclusive = "to"
		r.From = nil
		r.MaxResults = 50
		r.sorts = nil
		r.sortsBuilt = false

		retrieved = timeline.Items()
		retrievedCount = r.RecordsRetrievedCount()
		min = timeline.DisplayFrom()
		if min == nil {
			min = firstFrom
		}

		// Don't include retrieved items that are the same as the min
		var before []TimelineItem
		for _, it := range retrieved {
			if min == nil {
				cmpFail()
			}
			if min.Before(it.DT) {
				items = append(items, it)
			} else {
				before = append(before, it)
			}
		}
		if retrievedCount < 50 {
			over := append([]TimelineItem(nil), before...)
			sort.SliceStable(over, func(i, j int) bool { return over[i].DTTo.After(over[j].DTTo) })
			for i, j := 0, len(over)-1; i < j; i, j = i+1, j-1 {
				over[i], over[j] = over[j], over[i]
			}
			for len(items) <= 50 && len(over) > 0 {
				it := over[len(over)-1]
				over = over[:len(over)-1]
				items = append(items, it)
				if it.DT.Before(*min) {
					d := it.DT
					min = &d
				}
			}
		}

		timeline.Clear()
		r.MaxResults = 0

		min = minTime(items)
		if min != nil {
			m := min.UTC()
			min = &m
		}
		max = maxTime(items)
		if max != nil {
			m := max.UTC()
			max = &m
		}

		// Set from and to so that a re-retrieval retrieves the same results
		if max != nil {
			t := max.AddDate(0, 0, 1)
			r.To = &t
		}
		if min != nil {
			t := *min
			r.From = &t
		}

		// Set the display range of the timeline
		originalFrom = nil
		if min != nil {
			m := min.AddDate(0, 0, -1)
			min = &m
			originalFrom = min
		}
		if max != nil {
			m := max.AddDate(0, 0, 2)
			max = &m
		}
		originalTo = max
	}
	// If the limit_qty routine above was used, re-retrieve all of the
	// items to ensure correct sorting (in the case of grouping)
	if !limitQty || len(items) > 0 {
		items = timeline.Items()
		if originalFrom != nil && originalTo != nil {
			min, max = originalFrom, originalTo
		}
	}

	var groups []TimelineGroup
	for content, id := range timeline.Groups() {
		groups = append(groups, TimelineGroup{ID: id, Content: htmlEscape(content), Order: id,
			Style: "font-weight: bold"})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Order < groups[j].Order })

	r.From, r.To = originalFrom, originalTo

	return TimelineData{Items: items, Groups: groups, Colors: timeline.Colors(), Min: min, Max: max}
}

// perlHasTime is the has_time of a column as Perl returns it: 0 or 1, or
// the result of a string comparison ("" or 1) for a calc
func perlHasTime(c *Column) any {
	if c.HasTime() {
		return int64(1)
	}
	if c.Type == "calc" {
		return ""
	}
	return int64(0)
}
