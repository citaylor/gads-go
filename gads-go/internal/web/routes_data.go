package web

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// Process is Dancer2::Plugin::LogReport process: f runs with its errors
// reported to the user as non-fatal messages; it returns whether f
// succeeded
func (c *Ctx) Process(f func()) (ok bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		switch r.(type) {
		case haltSignal, passSignal:
			panic(r)
		}
		c.flushReports()
		var msg *Message
		switch x := r.(type) {
		case *gads.Error:
			if x.Level == gads.LevelError {
				msg = &Message{Reason: "ERROR", Text: x.Msg}
			} else {
				msg = &Message{Reason: "PANIC", Text: x.Msg}
			}
		case *db.Error:
			msg = &Message{Reason: "PANIC", Text: x.Error()}
		default:
			msg = &Message{Reason: "PANIC", Text: errText(r)}
		}
		c.addMessage(msg)
		ok = false
	}()
	f()
	c.flushReports()
	return true
}

func errText(r any) string {
	if e, ok := r.(error); ok {
		return e.Error()
	}
	return fmt.Sprint(r)
}

// autoviv returns the hash m[k], creating it if needed (Perl
// autovivification of $m->{k}->{...})
func autoviv(m map[string]any, k string) map[string]any {
	h, ok := m[k].(map[string]any)
	if !ok {
		h = map[string]any{}
		m[k] = h
	}
	return h
}

// perlInt is Perl int() of a string
var leadingNum = regexp.MustCompile(`^\s*[-+]?\d+`)

func perlInt(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(leadingNum.FindString(s)), 10, 64)
	return n
}

// currentViewLimitExtra is GADS.pm current_view_limit_extra
func (c *Ctx) currentViewLimitExtra(layout *gads.Layout) db.Row {
	extra := autoviv(c.persistent(), "view_limit_extra")[fmtInt(layout.InstanceID)]
	id, _ := toInt64(extra)
	if id == 0 {
		id, _ = toInt64(layout.Row()["default_view_limit_extra_id"])
	}
	if id != 0 {
		row := c.E.DB.QueryRow(`SELECT "me".* FROM "view" "me" WHERE "me"."id" = ?`, id)
		if row != nil {
			if iid, _ := toInt64(row["instance_id"]); iid == layout.InstanceID {
				return row
			}
		}
	}
	return nil
}

func (c *Ctx) currentViewLimitExtraID(layout *gads.Layout) *int64 {
	if row := c.currentViewLimitExtra(layout); row != nil {
		id, _ := toInt64(row["id"])
		return &id
	}
	return nil
}

// currentViewLimitOverrideID is GADS.pm current_view_limit_override_id: the
// view limit override of the session, if it is a limit override view (the
// instance check of Perl compares the view with itself, so a view of any
// table is accepted)
func (c *Ctx) currentViewLimitOverrideID(layout *gads.Layout) *int64 {
	id, _ := toInt64(autoviv(c.persistent(), "view_limit_override")[fmtInt(layout.InstanceID)])
	if id == 0 {
		return nil
	}
	row := c.E.DB.QueryRow(`SELECT "me"."id", "me"."is_limit_override" FROM "view" "me" WHERE "me"."id" = ?`, id)
	if row == nil || !truthy(row["is_limit_override"]) {
		return nil
	}
	return &id
}

func sessionRewind(c *Ctx) *time.Time {
	if t, ok := c.SessionGet("rewind").(time.Time); ok {
		return &t
	}
	return nil
}

func sessionSearch(c *Ctx) (string, bool) {
	s, ok := c.SessionGet("search").(string)
	return s, ok
}

// sessionSort is session('sort') as a sort of Records
func sessionSort(c *Ctx) []gads.SortSpec {
	m, ok := c.SessionGet("sort").(map[string]any)
	if !ok {
		return nil
	}
	id, _ := toInt64(m["id"])
	return []gads.SortSpec{{ID: id, Type: tt.Str(m["type"])}}
}

var sortParamRe = regexp.MustCompile(`^([0-9]+)(asc|desc)$`)
var viewtypeRe = regexp.MustCompile(`^(graph|table|calendar|timeline|globe)$`)
var numericKeyRe = regexp.MustCompile(`^([0-9]+)$`)

// newRecordsFor builds the records of the data page (search, rewind and
// view limit extra of the session)
func (c *Ctx) newRecordsFor(layout *gads.Layout, user *gads.User, filters []gads.AdditionalFilter) *gads.Records {
	records := c.E.NewRecords(layout, user)
	if s, ok := sessionSearch(c); ok {
		records.SetSearch(s)
	}
	records.Rewind = sessionRewind(c)
	records.AdditionalFilters = filters
	records.ViewLimitExtraID = c.currentViewLimitExtraID(layout)
	records.ViewLimitOverrideID = c.currentViewLimitOverrideID(layout)
	return records
}

// any ['get', 'post'] => /:layout_name/data (GADS.pm:2298)
func (s *Server) hData(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	iid := fmtInt(layout.InstanceID)

	var additionalFilters []gads.AdditionalFilter
	for _, key := range c.query.keys {
		if !numericKeyRe.MatchString(key) {
			continue
		}
		fid, _ := strconv.ParseInt(key, 10, 64)
		col := layout.Column(fid)
		if col == nil {
			gads.Panic(`Can't call method "is_curcommon" on an undefined value`)
		}
		var vals []any
		for _, v := range c.query.getAll(key) {
			vals = append(vals, v)
		}
		additionalFilters = append(additionalFilters, gads.AdditionalFilter{
			ID:     fid,
			Value:  vals,
			IsText: !(col.IsCurcommon() || col.Type == "id"),
		})
	}

	// Check for bulk delete
	if truthy(c.ParamS("modal_delete")) {
		if !layout.UserCan("bulk_delete") {
			c.ForwardHome("danger", "You do not have permission to bulk delete records", layout.Identifier()+"/data")
		}
		records := c.newRecordsFor(layout, user, additionalFilters)
		records.View = c.CurrentView(layout)
		if ids := c.body.getAll("delete_id"); len(ids) > 0 {
			var cids []int64
			for _, id := range ids {
				cids = append(cids, perlInt(id))
			}
			records.SetLimitCurrentIDs(cids)
		}
		count := ""
		n := 0
		for {
			record := records.Single()
			if record == nil {
				break
			}
			if c.Process(func() { record.DeleteCurrent(false, 0) }) {
				n++
				count = strconv.Itoa(n)
			}
		}
		c.ForwardHome("success", count+" records successfully deleted", layout.Identifier()+"/data")
	}

	// Check for rewind configuration
	if truthy(c.ParamS("modal_rewind")) || truthy(c.ParamS("modal_rewind_reset")) {
		if truthy(c.ParamS("modal_rewind_reset")) || !truthy(c.ParamS("rewind_date")) {
			c.SessionSet("rewind", nil)
		} else {
			input := c.ParamS("rewind_date")
			if t := c.ParamS("rewind_time"); truthy(t) {
				input += " " + t
			} else {
				input += " 23:59:59"
			}
			dt, ok := c.E.ParseDateTime(input)
			if !ok {
				gads.Fail("Invalid date or time: %s", input)
			}
			c.SessionSet("rewind", dt)
		}
	}

	// Setting a new view limit extra
	if layout.UserCan("view_limit_extra") {
		if extra := c.ParamS("extra"); truthy(extra) {
			autoviv(c.persistent(), "view_limit_extra")[iid] = extra
		}
	}

	// Setting a new view limit override
	if override, ok := c.Param("view_limit_override"); ok {
		autoviv(c.persistent(), "view_limit_override")[iid] = override
	}

	newViewID := c.ParamS("view")
	if truthy(c.ParamS("views_other_user_clear")) {
		c.SessionSet("views_other_user_id", nil)
		if v := c.E.NewViews(layout).DefaultView(); v != nil {
			newViewID = fmtInt(v.ID)
		} else {
			gads.Panic(`Can't call method "id" on an undefined value`)
		}
	} else if uid := c.ParamS("views_other_user_id"); truthy(uid) {
		c.SessionSet("views_other_user_id", uid)
	}

	// Deal with any alert requests
	if truthy(c.ParamS("modal_alert")) || truthy(c.ParamS("modal_remove")) {
		var success string
		var frequency any = ""
		if truthy(c.ParamS("modal_remove")) {
			success = "The alert has been removed successfully"
		}
		if truthy(c.ParamS("modal_alert")) {
			if f, ok := c.Param("frequency"); ok {
				frequency = f
			} else {
				frequency = nil
			}
			success = "The alert has been saved successfully"
		}
		alertUser := user
		if ou := c.SessionGet("views_other_user_id"); truthy(ou) {
			id, _ := toInt64(ou)
			alertUser = c.E.FindUser(id)
		}
		viewID, _ := toInt64(c.ParamS("view_id"))
		if c.Process(func() {
			alert := c.E.NewAlert(layout, alertUser, viewID)
			alert.SetFrequency(frequency)
			alert.Write()
		}) {
			c.ForwardHome("success", success, layout.Identifier()+"/data")
		}
	}

	if truthy(newViewID) {
		autoviv(c.persistent(), "view")[iid] = newViewID
		// Check that it's valid
		c.CurrentView(layout)
		c.SessionSet("sort", nil)
		c.SessionSet("page", nil)
		c.SessionSet("search", "")
	}

	if rows := c.ParamS("rows"); truthy(rows) {
		c.SessionSet("rows", s.clampRows(perlInt(rows)))
	}
	if page := c.ParamS("page"); truthy(page) {
		c.SessionSet("page", perlInt(page))
	}

	viewtype := c.ParamS("viewtype")
	if truthy(viewtype) {
		if viewtypeRe.MatchString(viewtype) {
			autoviv(c.persistent(), "viewtype")[iid] = viewtype
		}
	} else {
		viewtype = tt.Str(autoviv(c.persistent(), "viewtype")[iid])
		if !truthy(viewtype) {
			viewtype = "table"
		}
	}

	view := c.CurrentView(layout)
	params := tt.HashOf("layout", c.ttLayout(layout))

	switch viewtype {
	case "graph":
		s.dataGraph(c, layout, user, params)
	case "timeline":
		s.dataTimeline(c, layout, user, view, params)
	case "calendar", "globe":
		gads.Panic("The %s view type is not yet supported", viewtype)
	default:
		s.dataTable(c, layout, user, view, additionalFilters, params)
	}

	// Get all alerts
	alerts := s.E.UserAlerts(user)
	views := c.E.NewViews(layout)
	if ou := c.SessionGet("views_other_user_id"); truthy(ou) {
		id, _ := toInt64(ou)
		views.OtherUserID = &id
	}
	userViews := views.UserViews()
	viewList := func(vs []gads.ViewSummary) *tt.List {
		l := tt.NewList()
		for _, v := range vs {
			l.Items = append(l.Items, tt.HashOf("id", v.ID, "name", v.Name))
		}
		return l
	}
	params.Set("user_views", tt.HashOf(
		"admin", viewList(userViews.Admin),
		"shared", viewList(userViews.Shared),
		"personal", viewList(userViews.Personal),
	))
	limitExtra := tt.NewList()
	for _, r := range views.LimitExtra() {
		limitExtra.Items = append(limitExtra.Items, newRow("view", r))
	}
	params.Set("views_limit_extra", limitExtra)
	limitOverride := tt.NewList()
	for _, r := range views.LimitOverride() {
		limitOverride.Items = append(limitOverride.Items, newRow("view", r))
	}
	params.Set("views_limit_override", limitOverride)
	if r := c.currentViewLimitExtra(layout); r != nil {
		params.Set("current_view_limit_extra", newRow("view", r))
	} else if id, ok := toInt64(layout.Row()["default_view_limit_extra_id"]); ok && id != 0 {
		if r := c.E.DB.QueryRow(`SELECT "me".* FROM "view" "me" WHERE "me"."id" = ?`, id); r != nil {
			params.Set("current_view_limit_extra", newRow("view", r))
		} else {
			params.Set("current_view_limit_extra", nil)
		}
	} else {
		params.Set("current_view_limit_extra", nil)
	}
	if alerts == nil {
		params.Set("alerts", nil)
	} else {
		h := tt.NewHash()
		for vid, a := range alerts {
			h.Set(fmtInt(vid), tt.HashOf("id", ttValue(a["id"]), "view_id", ttValue(a["view_id"]), "frequency", ttValue(a["frequency"])))
		}
		params.Set("alerts", h)
	}
	if ou := c.SessionGet("views_other_user_id"); truthy(ou) {
		id, _ := toInt64(ou)
		if u := c.E.FindUser(id); u != nil {
			params.Set("views_other_user", c.ttUser(u))
		} else {
			params.Set("views_other_user", nil)
		}
	} else {
		params.Set("views_other_user", ou)
	}
	base := c.Base()
	params.Set("content_block_custom_classes", "content-block--lg-aside")
	params.Set("header_type", "table_tabs")
	params.Set("header_back_url", base+"table")
	params.Set("layout_obj", c.ttLayout(layout))
	params.Set("breadcrumbs", tt.NewList(
		crumb(base+"table/", "Tables"),
		crumb("", "Table: "+layout.Name()),
	))
	return c.Template("data", params, nil)
}

// dataTable is the table view type of the data page
func (s *Server) dataTable(c *Ctx, layout *gads.Layout, user *gads.User, view *gads.View, additionalFilters []gads.AdditionalFilter, params *tt.Hash) {
	if !truthy(c.SessionGet("rows")) {
		c.SessionSet("rows", int64(50))
	}
	if !truthy(c.SessionGet("page")) {
		c.SessionSet("page", int64(1))
	}
	_, download := c.Param("download")
	var rows, page int64
	if !download {
		rows, _ = toInt64(c.SessionGet("rows"))
		page, _ = toInt64(c.SessionGet("page"))
	}

	records := c.newRecordsFor(layout, user, additionalFilters)
	groupFilter, hasGroupFilter := c.QueryParam("group_filter")
	disableGroup := truthy(groupFilter)
	if disableGroup {
		f := false
		records.IsGroupOverride = &f
	}
	curvalRecordID, _ := c.QueryParam("curval_record_id")
	if truthy(curvalRecordID) {
		lid, _ := c.QueryParam("curval_layout_id")
		var curvalName any
		if n, ok := toInt64(lid); ok {
			if r := s.E.DB.QueryRow(`SELECT "name" FROM "instance" WHERE "id" = ?`, n); r != nil {
				curvalName = r["name"]
			}
		}
		if curvalName == nil {
			gads.Panic(`Can't call method "name" on an undefined value`)
		}
		params.Set("curval_layout_id", lid)
		params.Set("curval_record_id", curvalRecordID)
		prid, _ := c.QueryParam("parent_record_id")
		params.Set("parent_record_id", emptyNil(prid))
		params.Set("parent_field_name", ttValue(curvalName))
		params.Set("hide_view_menu", int64(1))
	}
	if truthy(curvalRecordID) {
		records.View = nil
	} else {
		records.View = view
	}
	records.Rows = int(rows)
	records.Page = int(page)
	records.SetSort(sessionSort(c))

	if m := sortParamRe.FindStringSubmatch(c.ParamS("sort")); m != nil {
		sortcol, _ := strconv.ParseInt(m[1], 10, 64)
		if col := layout.Column(sortcol); col == nil || !col.UserCan("read") {
			c.ForwardHome("danger", "Invalid column ID for sort", layout.Identifier()+"/data")
		}
		records.SortFirst()
		c.SessionSet("sort", map[string]any{"type": m[2], "id": m[1]})
		records.SetSort(sessionSort(c))
	}

	if truthy(c.ParamS("modal_sendemail")) {
		gads.Panic("Sending messages to the users of records is not yet supported")
	}

	if download {
		if records.Count() == 0 {
			c.ForwardHome("danger", "There are no records to download in this view", layout.Identifier()+"/data")
		}
		header := tt.Str(s.Config.Gads["header"])
		if header != "" {
			header = "-" + header
		}
		now := s.E.Now().Format("2006-01-02T15:04:05")
		c.headers.Set("Content-Disposition", `attachment; filename="`+now+header+`.csv"`)
		c.ctype = `text/csv; charset="utf-8"`
		var b strings.Builder
		b.WriteString(records.CSVHeader())
		for {
			line, ok := records.CSVLine()
			if !ok {
				break
			}
			b.WriteString(line)
		}
		c.content = b.String()
		panic(haltSignal{})
	}

	columns := records.ColumnsRender()
	sortFirst := records.SortFirst()
	params.Set("user_can_edit", ttBoolEmpty(layout.UserCan("write_existing")))
	// The sort of the records (the session sort, as a list)
	if ss := c.SessionGet("sort"); tt.True(ss) {
		params.Set("sort", tt.NewList(ttValue(ss)))
	} else {
		params.Set("sort", nil)
	}
	params.Set("aggregate", s.aggregatePresentation(c, records))
	cols := tt.NewList()
	ragCount := int64(0)
	for _, col := range columns {
		cols.Items = append(cols.Items, c.columnPresentation(col, presentOpts{sort: sortFirst}))
		if col.Type == "rag" {
			ragCount++
		}
	}
	params.Set("columns", cols)
	params.Set("is_group", ttBoolEmpty(records.IsGroup()))
	params.Set("has_rag_column", ragCount)
	params.Set("viewtype", "table")
	params.Set("group_filter", optString(groupFilter, hasGroupFilter))
	params.Set("page", "data_table")
	params.Set("search_limit_reached", int64(records.SearchLimitReached()))
	if truthy(c.ParamS("table_clear_state")) {
		params.Set("table_clear_state", int64(1))
	}
	if len(additionalFilters) > 0 {
		var filters []string
		for _, add := range additionalFilters {
			for _, v := range add.Value.([]any) {
				filters = append(filters, "field"+fmtInt(add.ID)+"="+uriEscapeUTF8(tt.Str(v)))
			}
		}
		if disableGroup {
			filters = append(filters, "group_filter")
		}
		params.Set("filter_url", strings.Join(filters, "&"))
	}
}

// aggregatePresentation is Records->aggregate_presentation
func (s *Server) aggregatePresentation(c *Ctx, records *gads.Records) any {
	agg := records.AggregateResults()
	if agg == nil {
		return nil
	}
	cols := tt.NewList()
	for _, col := range records.ColumnsRender() {
		if col.Aggregate == "" {
			// No aggregate value for the column
			cols.Items = append(cols.Items, nil)
			continue
		}
		cols.Items = append(cols.Items, c.columnPresentation(col, presentOpts{datum: agg.GetFieldValue(col)}))
	}
	return tt.HashOf("columns", cols)
}

func nilIfZero(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}

func optString(s string, ok bool) any {
	if !ok {
		return nil
	}
	return s
}

// uriEscapeUTF8 is URI::Escape uri_escape_utf8 (unreserved characters are
// A-Za-z0-9 -._~)
func uriEscapeUTF8(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)+0x100, 16)[1:]))
		}
	}
	return b.String()
}

var _ = url.QueryEscape

// dataGraph is the graph view type of the data page
func (s *Server) dataGraph(c *Ctx, layout *gads.Layout, user *gads.User, params *tt.Hash) {
	params.Set("page", "data_graph")
	params.Set("viewtype", "graph")
	if png := c.ParamS("png"); truthy(png) {
		gads.Panic("PNG images of graphs are not supported")
	}
	if csv := c.ParamS("csv"); truthy(csv) {
		gads.Panic("CSV downloads of graphs are not yet supported")
	}
	graphs := tt.NewList()
	for _, g := range s.E.AllGraphs(layout, user) {
		graphs.Items = append(graphs.Items, c.ttGraph(g))
	}
	params.Set("graphs", graphs)
}

// dataTimeline is the timeline view type of the data page
func (s *Server) dataTimeline(c *Ctx, layout *gads.Layout, user *gads.User, view *gads.View, params *tt.Hash) {
	records := c.newRecordsFor(layout, user, nil)
	records.View = view
	now := s.E.Now()
	records.From = &now
	tl := autoviv(autoviv(c.persistent(), "tl_options"), fmtInt(layout.InstanceID))
	if truthy(c.ParamS("modal_timeline")) {
		for _, k := range []string{"label", "group", "all_group_values", "color", "overlay"} {
			if v, ok := c.Param("tl_" + k); ok {
				tl[k] = v
			} else {
				tl[k] = nil
			}
		}
	}
	// See whether to restore remembered range
	if tl["from"] != nil && tl["to"] != nil {
		viewID, _ := toInt64(tl["view_id"])
		sameView := (viewID == 0 && view == nil) || (view != nil && viewID == view.ID)
		tlNow, _ := toInt64(tl["now"])
		if sameView && tlNow > now.AddDate(0, 0, -7).Unix() {
			from, _ := toInt64(tl["from"])
			to, _ := toInt64(tl["to"])
			f, t := time.Unix(from, 0).UTC(), time.Unix(to, 0).UTC()
			records.From, records.To = &f, &t
		}
	}
	if o, _ := toInt64(tl["overlay"]); o != 0 && o != layout.InstanceID {
		gads.Panic("Timeline overlays are not yet supported")
	}
	num := func(k string) int64 { n, _ := toInt64(tl[k]); return n }
	data := records.DataTimeline(gads.TimelineOptions{
		Label:          num("label"),
		Group:          num("group"),
		Color:          num("color"),
		AllGroupValues: truthy(tl["all_group_values"]),
	})
	items := []any{}
	for _, it := range data.Items {
		items = append(items, it.Fields)
	}
	groups := []any{}
	for _, g := range data.Groups {
		groups = append(groups, map[string]any{"id": g.ID, "content": g.Content, "order": g.Order, "style": g.Style})
	}
	b64 := func(v any) string { return base64.StdEncoding.EncodeToString([]byte(gads.EncodeJSON(v))) }
	params.Set("records", b64(items))
	params.Set("groups", b64(groups))
	colors := tt.NewList()
	for _, col := range data.Colors {
		colors.Items = append(colors.Items, tt.HashOf("key", col.Key, "color", col.Color))
	}
	params.Set("colors", colors)
	tlHash := tt.NewHash()
	tlHash.Set("min", ttTime(data.Min))
	tlHash.Set("max", ttTime(data.Max))
	params.Set("timeline", tlHash)
	params.Set("tl_options", ttValue(tl))
	params.Set("columns_read", c.ttColumns(layout.All(gads.AllOptions{UserCanRead: true})))
	params.Set("page", "data_timeline")
	params.Set("viewtype", "timeline")
	params.Set("search_limit_reached", int64(records.SearchLimitReached()))
	if truthy(c.ParamS("png")) {
		gads.Panic("PNG images of timelines are not supported")
	}
	if truthy(c.ParamS("modal_pdf")) && !truthy(layout.Row()["no_download_pdf"]) {
		gads.Panic("PDF downloads of timelines are not supported")
	}
}

func ttTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return &ttDateTime{t: *t}
}
