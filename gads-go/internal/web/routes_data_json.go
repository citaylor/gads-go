package web

import (
	"math"
	"time"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// epochParam is DateTime->from_epoch(epoch => param / 1000) of a parameter
// in milliseconds (the fraction kept unless truncate)
func (c *Ctx) epochParam(name string, truncate bool) time.Time {
	ms := perlNum(c.ParamS(name))
	secs := ms / 1000
	if truncate {
		secs = math.Trunc(secs)
	}
	whole := math.Floor(secs)
	return time.Unix(int64(whole), int64(math.Round((secs-whole)*1e9))).UTC()
}

// dtDuration is DateTime subtract_datetime: the difference of two times in
// months, days, minutes, seconds and nanoseconds (all of the same sign)
type dtDuration struct{ months, days, minutes, seconds, nanos int64 }

func subtractDatetime(dt1, dt2 time.Time) dtDuration {
	bigger, smaller, negative := dt1, dt2, false
	if dt1.Before(dt2) {
		bigger, smaller, negative = dt2, dt1, true
	}
	month1 := int64(bigger.Year()*12 + int(bigger.Month()))
	month2 := int64(smaller.Year()*12 + int(smaller.Month()))
	day1, day2 := int64(bigger.Day()), int64(smaller.Day())
	min1 := int64(bigger.Hour()*60 + bigger.Minute())
	min2 := int64(smaller.Hour()*60 + smaller.Minute())
	sec1, sec2 := int64(bigger.Second()), int64(smaller.Second())
	nano1, nano2 := int64(bigger.Nanosecond()), int64(smaller.Nanosecond())
	// The length of the month of the smaller date
	monthLength := int64(time.Date(smaller.Year(), smaller.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day())
	if nano1 < nano2 {
		sec1--
		nano1 += 1e9
	}
	if sec1 < sec2 {
		min1--
		sec1 += 60
	}
	if min1 < min2 {
		day1--
		min1 += 24 * 60
	}
	if day1 < day2 {
		month1--
		day1 += monthLength
	}
	d := dtDuration{month1 - month2, day1 - day2, min1 - min2, sec1 - sec2, nano1 - nano2}
	if negative {
		d = dtDuration{-d.months, -d.days, -d.minutes, -d.seconds, -d.nanos}
	}
	return d
}

func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// The DateTime::Duration accessors
func (d dtDuration) years() int64   { return absInt(d.months / 12) }
func (d dtDuration) monthsR() int64 { return absInt(d.months % 12) }
func (d dtDuration) weeks() int64   { return absInt(d.days / 7) }
func (d dtDuration) daysR() int64   { return absInt(d.days % 7) }

// calendarView is the period of the calendar requested, from the difference
// of its start and end
func calendarView(from, to time.Time) string {
	diff := subtractDatetime(to, from)
	switch {
	case diff.monthsR() >= 11 || diff.years() != 0:
		return "year"
	case diff.weeks() > 1 || diff.monthsR() != 0:
		return "month"
	case diff.daysR() >= 6 || diff.weeks() != 0:
		return "week"
	}
	return "day"
}

// get '/:layout_name/data_calendar/:time' (GADS.pm:2158)
func (s *Server) hDataCalendar(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	from := c.epochParam("from", false)
	to := c.epochParam("to", false)
	c.SessionSet("calendar", map[string]any{
		"day":  &ttDateTime{t: to.AddDate(0, 0, -1)},
		"view": calendarView(from, to),
	})
	// Epochs from the calendar are in the browser's timezone, so from is
	// rounded down to the day and to rounded up
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	if to.Hour() != 0 || to.Minute() != 0 || to.Second() != 0 {
		to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, to.Nanosecond(), time.UTC).AddDate(0, 0, 1)
	}
	user := c.LoggedInUser()
	records := s.E.NewRecords(layout, user)
	records.From, records.To = &from, &to
	records.MaxResults = 1000
	records.View = c.CurrentView(layout)
	if search, ok := sessionSearch(c); ok {
		records.SetSearch(search)
	}
	records.ViewLimitExtraID = c.currentViewLimitExtraID(layout)
	records.ViewLimitOverrideID = c.currentViewLimitOverrideID(layout)
	c.Header("Cache-Control", "max-age=0, must-revalidate, private")
	c.ContentType("application/json")
	items := []any{}
	for _, it := range records.DataCalendar() {
		items = append(items, it.Fields)
	}
	return gads.EncodeJSON(map[string]any{"success": int64(1), "result": items})
}

// get '/:layout_name/data_timeline/:time' (GADS.pm:2227)
func (s *Server) hDataTimelineJSON(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	// For the dashboard the normal records settings are ignored
	isDashboard := truthy(c.ParamS("dashboard"))
	from := c.epochParam("from", true)
	to := c.epochParam("to", true)
	user := c.LoggedInUser()
	var viewID int64
	if isDashboard {
		viewID, _ = toInt64(c.ParamS("view"))
	}
	records := s.E.NewRecords(layout, user)
	records.From, records.To = &from, &to
	if v, ok := c.Param("exclusive"); ok {
		records.Exclusive = v
	}
	records.View = c.CurrentViewID(layout, viewID)
	if !isDashboard {
		if search, ok := sessionSearch(c); ok {
			records.SetSearch(search)
		}
		records.Rewind = sessionRewind(c)
	}
	records.ViewLimitExtraID = c.currentViewLimitExtraID(layout)
	records.ViewLimitOverrideID = c.currentViewLimitOverrideID(layout)
	c.Header("Cache-Control", "max-age=0, must-revalidate, private")
	c.ContentType("application/json")
	var o gads.TimelineOptions
	if !isDashboard {
		if tl, ok := c.persistent()["tl_options"].(map[string]any); ok {
			if opts, ok := tl[fmtInt(layout.InstanceID)].(map[string]any); ok {
				num := func(k string) int64 { n, _ := toInt64(opts[k]); return n }
				o = gads.TimelineOptions{Label: num("label"), Group: num("group"), Color: num("color"), AllGroupValues: truthy(opts["all_group_values"])}
				if ov := num("overlay"); ov != 0 && ov != layout.InstanceID {
					gads.Panic("Timeline overlays are not yet supported")
				}
			}
		}
	}
	data := records.DataTimeline(o)
	items := []any{}
	for _, it := range data.Items {
		items = append(items, it.Fields)
	}
	return gads.EncodeJSON(items)
}

// post '/:layout_name/data_timeline' (GADS.pm:2265): remembers the range
// of the timeline being viewed
func (s *Server) hDataTimelinePost(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	tl := autoviv(autoviv(c.persistent(), "tl_options"), fmtInt(layout.InstanceID))
	if v := c.ParamS("from"); truthy(v) {
		tl["from"] = int64(perlNum(v) / 1000)
	}
	if v := c.ParamS("to"); truthy(v) {
		tl["to"] = int64(perlNum(v) / 1000)
	}
	if view := c.CurrentView(layout); view != nil {
		tl["view_id"] = view.ID
	} else {
		tl["view_id"] = nil
	}
	// The current time is noted so that it can be decided later whether
	// these settings are relevant
	tl["now"] = s.E.Now().Unix()
	return c.Template("index", tt.HashOf(), nil)
}

// get '/:layout_name/data_graph/:id/:time' (GADS.pm:2283)
func (s *Server) hDataGraphJSON(c *Ctx) string {
	idS, _ := c.RouteParam("id")
	id, _ := toInt64(idS)
	layout := c.Layout()
	user := c.LoggedInUser()
	view := c.CurrentView(layout)
	records := s.E.NewRecordsGraph(layout, user)
	if search, ok := sessionSearch(c); ok {
		records.SetSearch(search)
	}
	records.ViewLimitExtraID = c.currentViewLimitExtraID(layout)
	records.ViewLimitOverrideID = c.currentViewLimitOverrideID(layout)
	records.Rewind = sessionRewind(c)
	data := s.E.LoadGraph(id, layout, user).Data(records, view)
	c.Header("Cache-Control", "max-age=0, must-revalidate, private")
	c.ContentType("application/json")
	return gads.EncodeJSON(map[string]any{
		"points":  data.Points,
		"labels":  data.LabelsEncoded(),
		"xlabels": data.XLabels,
		"options": data.Options,
	})
}

// post '/print' (GADS.pm:2084): a PDF of the HTML (rendered by Chrome)
func (s *Server) hPrint(c *Ctx) string {
	html, _ := c.BodyParam("html")
	if !truthy(html) {
		gads.Fail("Missing body parameter: html")
	}
	gads.Panic("PDF printing is not supported")
	return ""
}
