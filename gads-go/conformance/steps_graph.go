package conformance

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
)

// stepGraph creates a graph (or uses an existing one) and observes its
// data, as GADS::Graph and GADS::Graph::Data.
func (r *Runner) stepGraph(args map[string]any) any {
	layout := r.layoutForStep(args)
	var u *gads.User
	if v, ok := args["user"]; ok {
		u = r.user(v)
	} else {
		u = r.firstSheet().User()
	}
	obs := map[string]any{}
	var graphID int64
	if ref, ok := args["graph_ref"]; ok && ref != nil {
		id, ok := r.graphs[str(ref)]
		if !ok {
			gads.Fail("Unknown graph %s", str(ref))
		}
		graphID = id
	} else {
		g := r.e.NewGraph(layout, u)
		g.Title = str(args["title"])
		g.Type = str(args["type"])
		if v := args["x_axis"]; v != nil {
			g.XAxis = r.col(layout, v).ID
		}
		if v := args["x_axis_link"]; truthy(v) {
			g.XAxisLink = r.col(layout, v).ID
		}
		if v := args["x_axis_grouping"]; truthy(v) {
			g.XAxisGrouping = str(v)
		}
		if v := args["x_axis_range"]; truthy(v) {
			g.XAxisRange = str(v)
		}
		if v := args["from"]; truthy(v) {
			t := parseDT(str(v))
			g.From = &t
		}
		if v := args["to"]; truthy(v) {
			t := parseDT(str(v))
			g.To = &t
		}
		if v := args["y_axis"]; v != nil {
			g.YAxis = r.col(layout, v).ID
		}
		// y_axis_link: the curval of a Y-axis field within a curval
		if v := args["y_axis_link"]; v != nil {
			g.YAxisLink = r.col(layout, v).ID
		}
		g.YAxisStack = str(args["y_axis_stack"])
		if v := args["group_by"]; truthy(v) {
			g.GroupBy = r.col(layout, v).ID
		}
		g.AsPercent = truthy(args["as_percent"])
		if v := args["trend"]; truthy(v) {
			g.Trend = str(v)
		}
		if v, ok := args["is_shared"]; ok {
			g.IsShared = truthy(v)
		}
		if v := args["group"]; truthy(v) {
			if str(v) == "default" {
				g.GroupID = r.firstSheet().Group()
			} else {
				g.GroupID = r.groupID(v)
			}
		}
		g.Write()
		graphID = g.ID
		if as := str(args["as"]); as != "" {
			r.graphs[as] = graphID
		}
	}
	if truthy(args["no_data"]) {
		return obs
	}
	var view *gads.View
	if args["filter"] != nil || args["view"] != nil || args["view_ref"] != nil || args["columns"] != nil {
		view = r.buildView(args, layout)
	}
	recs := r.e.NewRecordsGraph(layout, u)
	if v := args["rewind"]; truthy(v) {
		t := parseDT(str(v))
		recs.Rewind = &t
	}
	if v, ok := args["search"]; ok && v != nil {
		recs.SetSearch(str(v))
	}
	data := r.e.LoadGraph(graphID, layout, u).Data(recs, view)
	obs["points"] = graphPoints(data.Points)
	obs["xlabels"] = data.XLabels
	labels := []any{}
	for _, l := range data.Labels {
		labels = append(labels, l.Label)
	}
	obs["labels"] = labels
	obs["options"] = data.Options
	return obs
}

func graphPoints(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = graphPoints(e)
		}
		return out
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return v
}

// stepGraphs lists the graphs that a user can see (GADS::Graphs->all).
func (r *Runner) stepGraphs(args map[string]any) any {
	layout := r.layoutForStep(args)
	var u *gads.User
	if v, ok := args["user"]; ok {
		u = r.user(v)
	} else {
		u = r.firstSheet().User()
	}
	all := r.e.VisibleGraphs(layout, u)
	var ids []int64
	for _, g := range all {
		ids = append(ids, g.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []any{}
	for _, id := range ids {
		out = append(out, id)
	}
	return map[string]any{"count": len(all), "ids": out}
}

var reImportUser = regexp.MustCompile(`\{user:(\w+)\}`)

var reGlobeLiteral = regexp.MustCompile(`^-?[0-9]+$`)

// stepGlobe observes a globe (GADS::Globe) of a sheet's records. Items are
// sorted by location and the <br>-separated parts of hover texts and labels
// sorted, as Perl's countries are a hash.
func (r *Runner) stepGlobe(args map[string]any) any {
	layout := r.layoutForStep(args)
	var u *gads.User
	if v, ok := args["user"]; ok {
		u = r.user(v)
	} else {
		u = r.firstSheet().User()
	}
	var view *gads.View
	if args["view_ref"] != nil || args["view"] != nil || args["columns"] != nil {
		view = r.buildView(args, layout)
	}
	o := gads.GlobeOptions{Layout: layout, User: u, View: view}
	colOpt := func(v any) string {
		if v == nil {
			return ""
		}
		if l, ok := v.([]any); ok {
			var ids []string
			for _, c := range l {
				ids = append(ids, strconv.FormatInt(r.col(layout, c).ID, 10))
			}
			return strings.Join(ids, "_")
		}
		if s := str(v); reGlobeLiteral.MatchString(s) {
			return s
		}
		return strconv.FormatInt(r.col(layout, v).ID, 10)
	}
	o.GroupColID = colOpt(args["group"])
	o.ColorColID = colOpt(args["color"])
	o.LabelColID = colOpt(args["label"])
	globe := r.e.NewGlobe(o)
	data := globe.Data()
	canon := func(v any, keepFirst bool) any {
		if v == nil {
			return nil
		}
		parts := strings.Split(str(v), "<br>")
		var first []string
		if keepFirst {
			first, parts = parts[:1], parts[1:]
		}
		sort.Strings(parts)
		return strings.Join(append(first, parts...), "<br>")
	}
	traces := []any{}
	for _, tr := range data.Data {
		locs := tr["locations"].([]any)
		text, hasText := tr["text"].([]any)
		hover, hasHover := tr["hovertext"].([]any)
		z, hasZ := tr["z"].([]any)
		cc, hasCC := tr["countrycolors"].([]any)
		var items []map[string]any
		for i, l := range locs {
			item := map[string]any{"location": l}
			if hasText {
				item["text"] = canon(text[i], !hasHover)
			}
			if hasHover {
				item["hovertext"] = canon(hover[i], true)
			}
			if hasZ {
				item["z"] = z[i]
			}
			if hasCC {
				c := cc[i]
				if s, ok := c.(string); ok && strings.HasPrefix(s, "#") && s != "#D3D3D3" {
					c = "color"
				}
				item["countrycolor"] = c
			}
			items = append(items, item)
		}
		sort.SliceStable(items, func(i, j int) bool { return str(items[i]["location"]) < str(items[j]["location"]) })
		itemsAny := []any{}
		for _, it := range items {
			itemsAny = append(itemsAny, it)
		}
		var showscale, countrycolors any
		if s, ok := tr["showscale"].(bool); ok {
			showscale = boolInt(s)
		}
		if c, ok := tr["countrycolors"]; ok {
			countrycolors = boolInt(c != nil)
		}
		traces = append(traces, map[string]any{
			"type":          tr["type"],
			"hoverinfo":     tr["hoverinfo"],
			"locationmode":  tr["locationmode"],
			"mode":          tr["mode"],
			"showscale":     showscale,
			"marker":        tr["marker"],
			"countrycolors": countrycolors,
			"items":         itemsAny,
		})
	}
	return map[string]any{
		"traces": traces,
		"params": map[string]any{
			"has_view":                    boolInt(data.Params["view_id"] != nil),
			"layout_identifier":           data.Params["layout_identifier"],
			"globe_fields":                data.Params["globe_fields"],
			"default_view_limit_extra_id": data.Params["default_view_limit_extra_id"],
		},
		"colors": len(globe.Colors()),
	}
}

// stepImport imports CSV content into a sheet (GADS::Import) and observes the
// import status and its rows.
func (r *Runner) stepImport(args map[string]any) any {
	layout := r.layoutForStep(args)
	u := r.user(args["user"])
	opts, _ := args["options"].(map[string]any)
	o := gads.ImportOptions{
		TakeFirstEnum:       truthy(opts["take_first_enum"]),
		DryRun:              truthy(opts["dry_run"]),
		IgnoreStringZeros:   truthy(opts["ignore_string_zeros"]),
		ForceMandatory:      truthy(opts["force_mandatory"]),
		SplitMultiple:       truthy(opts["split_multiple"]),
		ShortNames:          truthy(opts["short_names"]),
		BlankInvalidEnum:    truthy(opts["blank_invalid_enum"]),
		RoundIntegers:       truthy(opts["round_integers"]),
		UpdateOnly:          truthy(opts["update_only"]),
		NoChangeUnlessBlank: str(opts["no_change_unless_blank"]),
	}
	if v, ok := opts["report_changes"]; ok && !truthy(v) {
		o.NoReportChanges = true
	}
	colID := func(v any) int64 {
		if !truthy(v) {
			return 0
		}
		if str(v) == "ID" {
			return layout.ColumnID().ID
		}
		return r.col(layout, v).ID
	}
	o.UpdateUnique = colID(opts["update_unique"])
	o.SkipExistingUnique = colID(opts["skip_existing_unique"])
	if l, ok := opts["append"].([]any); ok {
		for _, c := range l {
			o.Append = append(o.Append, r.col(layout, c).ID)
		}
	}
	// {user:<name>} is replaced by that user's ID
	content := reImportUser.ReplaceAllStringFunc(str(args["content"]), func(m string) string {
		return strconv.FormatInt(r.user(reImportUser.FindStringSubmatch(m)[1]).ID, 10)
	})
	id := r.e.NewImport(layout, u, []byte(content), o).Process()
	st := r.e.DB.QueryRow(`SELECT * FROM "import" WHERE "id" = ?`, id)
	rows := []any{}
	for _, row := range r.e.DB.Query(`SELECT * FROM "import_row" WHERE "import_id" = ? ORDER BY "id"`, id) {
		rows = append(rows, map[string]any{
			"status":  row["status"],
			"content": row["content"],
			"errors":  row["errors"],
			"changes": row["changes"],
		})
	}
	return map[string]any{
		"row_count":     st["row_count"],
		"written_count": st["written_count"],
		"error_count":   st["error_count"],
		"skipped_count": st["skipped_count"],
		"result":        st["result"],
		"completed":     boolInt(st["completed"] != nil),
		"rows":          rows,
	}
}
