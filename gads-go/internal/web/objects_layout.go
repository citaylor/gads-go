package web

import (
	"encoding/base64"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// ttLayoutObj is GADS::Layout
type ttLayoutObj struct {
	c *Ctx
	l *gads.Layout
	// over are attributes set on the object but not written (shown after
	// a failed write)
	over map[string]any
}

// ttLayoutWith is a layout object with attributes set
func (c *Ctx) ttLayoutWith(l *gads.Layout, over map[string]any) any {
	return &ttLayoutObj{c: c, l: l, over: over}
}

func (c *Ctx) ttLayout(l *gads.Layout) any {
	if l == nil {
		return nil
	}
	return &ttLayoutObj{c: c, l: l}
}

// article is Lingua::EN::Inflect A (for regular words)
func article(w string) string {
	if w != "" && strings.ContainsRune("aeiouAEIOU", rune(w[0])) {
		return "an " + w
	}
	return "a " + w
}

func (o *ttLayoutObj) recordName() string {
	if n := o.l.RecordName(); n != "" {
		return n
	}
	return "record"
}

func (o *ttLayoutObj) TTMethod(name string, args []any) ([]any, bool, error) {
	l := o.l
	one := func(v any) ([]any, bool, error) { return []any{v}, true, nil }
	if v, ok := o.over[name]; ok {
		return one(v)
	}
	switch name {
	case "identifier":
		return one(l.Identifier())
	case "referred_by":
		// The fields of other tables with curval fields of this table
		list := tt.NewList()
		for _, r := range o.c.s.E.DB.Query(`SELECT DISTINCT "me".* FROM "layout" "me" JOIN "curval_fields" "curval_fields_parents" ON "curval_fields_parents"."parent_id" = "me"."id" JOIN "layout" "child" ON "child"."id" = "curval_fields_parents"."child_id" WHERE "child"."instance_id" = ? ORDER BY "me"."id"`, l.InstanceID) {
			row := newRow("layout", r)
			iid := r["instance_id"]
			row.set("instance", func([]any) any {
				if ir := o.c.s.E.DB.QueryRow(`SELECT * FROM "instance" WHERE "id" = ?`, iid); ir != nil {
					return newRow("instance", ir)
				}
				return nil
			})
			list.Items = append(list.Items, row)
		}
		return one(list)
	case "filtered_curvals":
		list := tt.NewList()
		for _, col := range l.All(gads.AllOptions{}) {
			if col.Type == "curval" && len(col.FilterFn().ColumnsInSubs()) > 0 {
				list.Items = append(list.Items, o.c.ttColumn(col))
			}
		}
		return one(list)
	case "columns_for_display_condition":
		cols := []any{}
		for _, col := range l.All(gads.AllOptions{ExcludeInternal: true}) {
			cols = append(cols, map[string]any{"id": col.ID, "label": col.Name, "type": "string"})
		}
		return one(base64.StdEncoding.EncodeToString([]byte(gads.EncodeJSON(cols))))
	case "has_alert_column":
		id, _ := toInt64(argAt(args, 0))
		for _, col := range l.AlertColumns() {
			if col.ID == id {
				return one(int64(1))
			}
		}
		return one(nil)
	case "rag":
		rag := fmtAny(argAt(args, 0))
		for _, r := range l.Rags() {
			if fmtAny(r["rag"]) == rag {
				if fmtAny(r["description"]) == "" {
					r["description"] = rag
				}
				return one(newRow("instance_rag", r))
			}
		}
		return one(nil)
	case "group_has":
		id, _ := toInt64(argAt(args, 0))
		return one(ttBool01(l.GroupHas(id, fmtAny(argAt(args, 1)))))
	case "global_view_summary":
		// The admin views of the table in order of name
		out := tt.NewList()
		for _, r := range o.c.E.DB.Query(`SELECT "me".* FROM "view" "me" WHERE ( "is_admin" = ? AND "instance_id" = ? ) ORDER BY "me"."name"`, 1, l.InstanceID) {
			out.Items = append(out.Items, newRow("view", r))
		}
		return one(out)
	case "enabled_rags":
		out := tt.NewList()
		for _, r := range l.EnabledRags() {
			out.Items = append(out.Items, newRow("instance_rag", r))
		}
		return one(out)
	case "name":
		return one(l.Name())
	case "name_short":
		return one(ttValue(l.Row()["name_short"]))
	case "instance_id":
		return one(l.InstanceID)
	case "user_can":
		return one(ttBoolEmpty(l.UserCan(tt.Str(argAt(args, 0)))))
	case "record_name":
		return one(o.recordName())
	case "record_name_plural":
		return one(gads.Plural(o.recordName()))
	case "record_name_with_article":
		return one(article(o.recordName()))
	case "no_download_pdf", "no_hide_blank", "no_copy_record", "no_overnight_update", "forget_history",
		"homepage_text", "homepage_text2", "sort_type", "sort_layout_id", "view_limit_id",
		"default_view_limit_extra_id", "api_index_layout_id", "alert_column", "hide_in_selector":
		return one(ttValue(l.Row()[name]))
	case "has_topics":
		return one(ttBoolEmpty(l.HasTopics()))
	case "has_globe":
		return one(ttBoolEmpty(l.HasGlobe()))
	case "has_children":
		return one(ttBoolEmpty(l.HasChildren()))
	case "all":
		return one(o.c.ttColumns(l.All(allOptions(args))))
	case "all_user_read":
		return one(o.c.ttColumns(l.AllUserRead()))
	case "column":
		id, _ := toInt64(argAt(args, 0))
		return one(o.c.ttColumn(l.Column(id)))
	case "columns_for_filter":
		showAll := false
		for i := 0; i+1 < len(args); i += 2 {
			if tt.Str(args[i]) == "show_all_columns" {
				showAll = tt.True(args[i+1])
			}
		}
		if h, ok := argAt(args, len(args)-1).(*tt.Hash); ok {
			v, _ := h.Get("show_all_columns")
			showAll = showAll || tt.True(v)
		}
		return one(o.columnsForFilter(showAll))
	case "show_add_record":
		return one(ttBoolEmpty(l.ShowAddRecord()))
	}
	return nil, false, nil
}

// ttViewObj is GADS::View
type ttViewObj struct {
	c *Ctx
	v *gads.View
	// The lists of the view's sorts and groups are each built once, as
	// Perl's lazy attributes (the edit template changes them)
	sorts, groups *tt.List
}

// ttFilterObj is GADS::Filter
type ttFilterObj struct{ f *gads.Filter }

func (o *ttFilterObj) TTMethod(name string, args []any) ([]any, bool, error) {
	switch name {
	case "base64":
		return []any{o.f.Base64()}, true, nil
	case "as_json":
		return []any{o.f.AsJSON()}, true, nil
	}
	return nil, false, nil
}

// sortFilterID is the filter ID of a sort or grouping (Result::Sort
// filter_id): the parent ID and the field ID, or the field ID
func sortFilterID(layoutID, parentID int64) any {
	if parentID != 0 {
		return fmtInt(parentID) + "_" + fmtInt(layoutID)
	}
	return nilIfZero(layoutID)
}

func (c *Ctx) ttView(v *gads.View) any {
	if v == nil {
		return nil
	}
	return &ttViewObj{c: c, v: v}
}

func (o *ttViewObj) TTMethod(name string, args []any) ([]any, bool, error) {
	v := o.v
	one := func(x any) ([]any, bool, error) { return []any{x}, true, nil }
	switch name {
	case "id":
		return one(nilIfZero(v.ID))
	case "name":
		return one(v.Name)
	case "group_id":
		if v.GroupID == nil {
			return one(nil)
		}
		return one(*v.GroupID)
	case "filter":
		return one(&ttFilterObj{f: v.Filter()})
	case "columns":
		l := tt.NewList()
		for _, id := range v.Columns() {
			l.Items = append(l.Items, id)
		}
		return one(l)
	case "sorts":
		if o.sorts == nil {
			o.sorts = tt.NewList()
			for _, st := range v.Sorts() {
				o.sorts.Items = append(o.sorts.Items, tt.HashOf("filter_id", sortFilterID(st.LayoutID, st.ParentID), "type", st.Type))
			}
		}
		return one(o.sorts)
	case "groups":
		if o.groups == nil {
			o.groups = tt.NewList()
			for _, g := range v.Groups() {
				o.groups.Items = append(o.groups.Items, tt.HashOf("filter_id", sortFilterID(g.LayoutID, g.ParentID)))
			}
		}
		return one(o.groups)
	case "global":
		return one(ttBoolEmpty(v.Global))
	case "is_admin":
		return one(ttBoolEmpty(v.IsAdmin))
	case "is_group":
		return one(ttBoolEmpty(v.IsGroup()))
	case "writable":
		return one(ttBoolEmpty(v.Writable()))
	case "has_alerts":
		return one(ttBoolEmpty(v.HasAlerts()))
	case "instance_id":
		return one(v.InstanceID)
	case "alert":
		// The alert of the layout's user
		if u := o.c.LoggedInUser(); u != nil {
			if r := o.c.E.DB.QueryRow(`SELECT "me".* FROM "alert" "me" WHERE "me"."view_id" = ? AND "me"."user_id" = ? LIMIT 1`, v.ID, u.ID); r != nil {
				return one(newRow("alert", r))
			}
		}
		return one(nil)
	}
	return nil, false, nil
}

// CurrentView is GADS current_view: the view of the session, or the
// user's default view (errors loading the view are reported)
func (c *Ctx) CurrentView(layout *gads.Layout) *gads.View {
	return c.CurrentViewID(layout, 0)
}

// CurrentViewID is current_view with a view ID (used instead of the
// session's view if set)
func (c *Ctx) CurrentViewID(layout *gads.Layout, viewID int64) *gads.View {
	if layout == nil {
		return nil
	}
	views := c.E.NewViews(layout)
	var view *gads.View
	id := viewID
	if id == 0 {
		// session('persistent')->{view}->{$id} autovivifies the view hash
		p := c.persistent()
		pv, ok := p["view"].(map[string]any)
		if !ok {
			pv = map[string]any{}
			p["view"] = pv
		}
		id, _ = toInt64(pv[fmtInt(layout.InstanceID)])
	}
	if err := gads.Try(func() { view = views.View(id) }); err != nil {
		c.ReportError(err.Error())
	}
	if view == nil {
		view = views.DefaultView()
	}
	return view
}

// columnsForFilter is Layout->columns_for_filter
func (o *ttLayoutObj) columnsForFilter(showAll bool) *tt.List {
	opts := gads.AllOptions{IncludeInternal: true, UserCanRead: !showAll}
	out := tt.NewList()
	add := func(fid, fname string, col *gads.Column) {
		out.Items = append(out.Items, tt.HashOf(
			"filter_id", fid,
			"filter_name", fname,
			"return_type", emptyNil(col.ReturnTypeFn()),
			"type", col.Type,
			"column", o.c.ttColumn(col),
		))
	}
	for _, col := range o.l.All(opts) {
		add(fmtInt(col.ID), col.Name, col)
		if col.IsCurcommon() {
			if lp := col.LayoutParent(); lp != nil {
				for _, c := range lp.All(opts) {
					if c.Type == "autocur" {
						continue
					}
					add(fmtInt(col.ID)+"_"+fmtInt(c.ID), col.Name+" ("+c.Name+")", c)
				}
			}
		}
	}
	return out
}

// ttGraphObj is GADS::Graph
type ttGraphObj struct {
	c *Ctx
	g *gads.Graph
}

func (c *Ctx) ttGraph(g *gads.Graph) any { return &ttGraphObj{c: c, g: g} }

func (o *ttGraphObj) TTMethod(name string, args []any) ([]any, bool, error) {
	g := o.g
	one := func(x any) ([]any, bool, error) { return []any{x}, true, nil }
	switch name {
	case "id":
		if g.RawID() != "" {
			return one(g.RawID())
		}
		return one(g.ID)
	case "title":
		return one(emptyNil(g.Title))
	case "description":
		return one(emptyNil(g.Description))
	case "type":
		return one(emptyNil(g.Type))
	case "x_axis_name":
		return one(g.XAxisName())
	case "y_axis_label":
		return one(emptyNil(g.YAxisLabel))
	case "stackseries":
		return one(ttBool01(g.Stackseries))
	case "showlegend":
		return one(ttBool01(g.Showlegend()))
	case "selected":
		return one(ttBool01(g.Selected))
	case "is_shared":
		return one(ttBool01(g.IsShared))
	case "writable":
		return one(ttBoolEmpty(g.Writable()))
	case "x_axis":
		return one(nilIfZero(g.XAxis))
	case "x_axis_full":
		return one(emptyNil(g.XAxisFull()))
	case "x_axis_grouping":
		return one(emptyNil(g.XAxisGrouping))
	case "x_axis_range":
		return one(emptyNil(g.XAxisRange))
	case "y_axis":
		return one(nilIfZero(g.YAxis))
	case "y_axis_stack":
		return one(emptyNil(g.YAxisStack))
	case "group_by":
		return one(nilIfZero(g.GroupBy))
	case "group_id":
		return one(nilIfZero(g.GroupID))
	case "trend":
		return one(emptyNil(g.Trend))
	case "metric_group_id":
		return one(nilIfZero(g.MetricGroupID))
	case "as_percent":
		return one(ttBool01(g.AsPercent))
	case "from_formatted", "to_formatted":
		t := g.From
		if name == "to_formatted" {
			t = g.To
		}
		if t == nil {
			return one(nil)
		}
		return one(gads.FormatDate(*t, (&ttConfig{s: o.c.s}).dateformat()))
	}
	return nil, false, nil
}
