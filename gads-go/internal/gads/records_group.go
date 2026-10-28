package gads

import (
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
)

// Port of GADS::Records::_build_group_results and aggregate_results.

type groupOptions struct {
	columns   []*groupCol
	isGroup   bool
	aggregate bool
}

type groupCol struct {
	id       int64
	column   *Column
	operator string
	group    bool
	parentID int64
	parent   *Column
	drcol    bool
	pluck    string
}

// AggregateResults returns the aggregate (totals) record, or nil if no
// columns have an aggregate.
func (r *Records) AggregateResults() *Record {
	var cols []*groupCol
	for _, c := range append(append([]*Column{}, r.ColumnsRender()...), r.columnsRecalcExtra()...) {
		if c.Aggregate != "" {
			cols = append(cols, &groupCol{id: c.ID, column: c, operator: c.Aggregate})
		}
	}
	if len(cols) == 0 {
		return nil
	}
	// Clear any list of record IDs from previous results
	r.currentIDsBuilt = false
	r.currentIDs = nil
	res := r.buildGroupResults(groupOptions{columns: cols, isGroup: true, aggregate: true})
	if len(res) > 1 {
		Panic("Unexpected number of aggregate results")
	}
	if len(res) == 0 {
		return nil
	}
	return res[0]
}

func (r *Records) columnsRecalcExtra() []*Column {
	retrieved := map[int64]bool{}
	var cols []*Column
	for _, c := range r.ColumnsRender() {
		if c.Aggregate != "" {
			cols = append(cols, c)
			retrieved[c.ID] = true
		}
	}
	var extra []*Column
	for _, c := range cols {
		if c.Aggregate == "recalc" {
			for _, d := range c.DependsOn() {
				if !retrieved[d] {
					if dc := r.Layout.Column(d); dc != nil {
						extra = append(extra, dc)
					}
				}
			}
		}
	}
	return extra
}

func compareGroupCol(a, b *groupCol) bool {
	if a.id != b.id || a.operator != b.operator {
		return false
	}
	if (a.parent == nil) != (b.parent == nil) {
		return false
	}
	if a.parent == nil {
		return true
	}
	return a.parent.ID == b.parent.ID
}

// groupColRef is an entry of group_cols: a grouped column with any parent
// curval.
type groupColRef struct {
	column *Column
	parent *Column
}

// aggField is an entry of aggregate_fields.
type aggField struct {
	column   *Column
	parent   *Column
	sel      string
	args     []any
	operator string
	as       string
}

type aggOpts struct {
	isGrouped bool
	isLinked  *Column
	groupCols []groupColRef
	parent    *Column
	rewind    *time.Time
}

func (r *Records) buildGroupResults(o groupOptions) []*Record {
	st := r.store()
	var cols []*groupCol
	view := r.View
	isTableGroup := !r.rawGroupRows()
	// Initial pass of all the columns to retrieve in the grouped query
	if o.columns != nil {
		// Supplied by the calling function
		cols = o.columns
	} else if view != nil && view.IsGroup() && isTableGroup {
		// Columns configured by a view: each grouped column is added once
		// for each of its parents (if any), flagged as a group column
		vg := map[int64][]int64{}
		for _, g := range view.Groups() {
			vg[g.LayoutID] = append(vg[g.LayoutID], g.ParentID)
		}
		for _, c := range append(append([]*Column{}, r.ColumnsSelected()...), r.columnsRecalcExtra()...) {
			if parents, ok := vg[c.ID]; ok {
				op := "max"
				if c.Numeric() {
					op = "sum"
				}
				for _, pid := range parents {
					cols = append(cols, &groupCol{id: c.ID, column: c, operator: op, group: true, parentID: pid})
				}
			} else {
				op := "distinct"
				if c.Numeric() {
					op = "sum"
				}
				cols = append(cols, &groupCol{id: c.ID, column: c, operator: op})
			}
		}
	} else {
		// Configured in the creation of the object
		cols = r.graphColumns
	}
	var parents []*groupCol
	var groupCols []groupColRef
	if isTableGroup && view != nil && !o.aggregate {
		for _, g := range view.Groups() {
			gc := groupColRef{column: r.Layout.Column(g.LayoutID)}
			if g.ParentID != 0 {
				gc.parent = r.Layout.Column(g.ParentID)
			}
			groupCols = append(groupCols, gc)
		}
	}
	hasGroupCol := func(id int64) bool {
		for _, g := range groupCols {
			if g.column.ID == id {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(cols); i++ {
		col := cols[i]
		column := r.Layout.Column(col.id)
		col.column = column
		if col.operator == "" {
			col.operator = "max"
		}
		col.drcol = r.drColumn != 0 && r.drColumn == column.ID
		// Only include full fields as a group column
		if col.group && col.parent == nil && !hasGroupCol(column.ID) {
			groupCols = append(groupCols, groupColRef{column: column})
		}
		if col.parentID != 0 {
			parent := r.Layout.Column(col.parentID)
			parents = append(parents, &groupCol{id: parent.ID, column: parent, operator: col.operator, group: col.group,
				drcol: r.drColumn != 0 && r.drColumn == column.ID})
			col.parent = parent
		}
		if column.IsCurcommon() && !isTableGroup {
			for _, f := range column.CurvalFields() {
				if f == nil {
					continue
				}
				cols = append(cols, &groupCol{id: f.ID, column: f, operator: col.operator, parent: column, group: col.group})
			}
		}
	}
	// Combine and flatten columns
	var newCols []*groupCol
	for _, c := range cols {
		var existing *groupCol
		for _, n := range newCols {
			if compareGroupCol(c, n) {
				existing = n
				break
			}
		}
		if existing != nil {
			existing.group = existing.group || c.group
		} else {
			newCols = append(newCols, c)
		}
	}
	cols = newCols
	// Add all columns first so that join numbers are correct
	for _, col := range append(append([]*groupCol{}, parents...), cols...) {
		column := col.column
		if column.Internal {
			continue
		}
		if r.drColumn != 0 && r.drColumn == column.ID {
			if r.drColumnParent != nil {
				st.addDrcol(r.drColumnParent, joinOpts{})
				st.addDrcol(column, joinOpts{parent: r.drColumnParent})
			} else {
				st.addDrcol(column, joinOpts{})
			}
		} else {
			st.addPrefetch(column, joinOpts{group: col.group, parent: col.parent})
			if lp := column.LinkParent(); lp != nil {
				st.addPrefetch(lp, joinOpts{linked: true, group: col.group, parent: col.parent})
			}
		}
	}
	drcol := r.drColumn != 0
	for _, col := range cols {
		if o.aggregate && col.column.Aggregate == "recalc" {
			continue
		}
		common := joinOpts{prefetch: true, hasPrefetch: true, search: false, parent: col.parent, group: col.group || col.drcol}
		st.addAggregate(col.column, col.operator, common)
		if lp := col.column.LinkParent(); lp != nil {
			lc := common
			lc.linked = true
			st.addAggregate(lp, col.operator, lc)
		}
	}
	// Build the full query first, to ensure that all join numbers etc are
	// calculated correctly
	r.searchQuery(searchOpts{joinOpts: joinOpts{search: true, sort: true}})
	selects := []string{`COUNT( 1 ) AS "id_count"`}
	var selArgs []any
	if r.drColumn != 0 {
		ds, da := r.drColumnSelects(cols)
		selects = append(selects, ds...)
		selArgs = append(selArgs, da...)
	}
	var groupBy []string
	for _, gc := range cols {
		if !gc.group {
			continue
		}
		col := r.Layout.Column(gc.id)
		if gc.pluck != "" {
			fq := q(st.fqvalue(col, joinOpts{search: true, prefetch: true, hasPrefetch: true}, false))
			groupBy = append(groupBy, r.e.sqlPluck(fq, "year"))
			if gc.pluck == "month" || gc.pluck == "day" {
				groupBy = append(groupBy, r.e.sqlPluck(fq, "month"))
			}
			if gc.pluck == "day" {
				groupBy = append(groupBy, r.e.sqlPluck(fq, "day"))
			}
			continue
		}
		if lp := col.LinkParent(); lp != nil {
			st.addGroup(col, joinOpts{})
			main := q(st.fqvalue(col, joinOpts{group: true, search: false, prefetch: true, hasPrefetch: true, retainJoinOrder: true}, false))
			st.addGroup(lp, joinOpts{linked: true})
			link := q(st.fqvalue(lp, joinOpts{group: true, search: false, prefetch: true, hasPrefetch: true, linked: true, hasLinked: true, retainJoinOrder: true}, false))
			groupBy = append(groupBy, r.e.sqlConcat(main, link))
			continue
		}
		if gc.parent != nil {
			st.addGroup(gc.parent, joinOpts{})
			st.addGroup(col, joinOpts{parent: gc.parent})
		} else {
			st.addGroup(col, joinOpts{})
		}
		groupBy = append(groupBy, q(st.fqvalue(col, joinOpts{group: true, search: false, prefetch: false, hasPrefetch: true,
			retainJoinOrder: true, parent: gc.parent, aggregate: true}, false)))
	}
	// Called first to generate joins
	r.searchQuery(searchOpts{joinOpts: joinOpts{prefetch: true, hasPrefetch: true, search: true, retainJoinOrder: true, group: true, sort: true, drcol: drcol}})
	cvo := r.cvoValues()
	// No joins of multi-value fields, which would cause multiple rows for a
	// record and therefore double-counting: these are subqueries of the
	// aggregates
	common := joinOpts{group: true, search: false, sort: true, drcol: drcol, currentVersionOnly: cvo, linked: false, hasLinked: true}
	jo := common
	jo.retainJoinOrder = true
	jo.aggregate = true
	jpFetch := st.jpfetch(jo)
	oo := common
	oo.linked = true
	oo.retainJoinOrder = true
	orderTerms := r.orderBy(orderByOpts{joinOpts: oo})
	for _, f := range r.aggregateFields(r.RewindValues(), groupCols) {
		selects = append(selects, f.sel)
		selArgs = append(selArgs, f.args...)
	}
	jr := newJoinRenderer(r.e, formatRewind(r.RewindValues()))
	jr.layout = r.Layout
	lo := common
	lo.prefetch, lo.hasPrefetch = true, true
	lo.retainJoinOrder = true
	lo.aggregate = o.aggregate
	if l := r.linkedJoins(lo); l != nil {
		jr.render("current", "me", []*joinSpec{l})
	}
	jr.render("current", "me", []*joinSpec{r.versionJoin(cvo, jpFetch)})
	// Outer search query so needs to match values being retrieved
	so := common
	so.aggregate = true
	search := r.resultsetSearch(so, true)
	var wargs []any
	where := renderCond(condAnd(search), &wargs)
	sql := "SELECT " + strings.Join(selects, ", ") + ` FROM "current" "me"` + jr.sb.String()
	if where != "" {
		sql += " WHERE " + where
	}
	if len(groupBy) > 0 {
		sql += " GROUP BY " + strings.Join(groupBy, ", ")
	}
	if ob := r.renderOrderBy(orderTerms, false); ob != "" {
		sql += " ORDER BY " + ob
	}
	args := append(append(append([]any{}, selArgs...), jr.args...), wargs...)
	rows := r.e.DB.Query(sql, args...)
	if r.rawGroupRows() {
		r.graphRows = rows
		return nil
	}
	groupColIDs := map[int64]bool{}
	for _, g := range groupCols {
		groupColIDs[g.column.ID] = true
	}
	var out []*Record
	for _, row := range rows {
		rec := r.e.newRecordObject(r.Layout, r.User)
		rec.records = r
		rec.isGroup = o.isGroup || r.isGroup()
		rec.groupCols = groupColIDs
		rec.columnsRetrievedNo = r.ColumnsRetrievedNo()
		rec.columnsRetrievedDo = r.ColumnsRetrievedDo()
		rec.columnsSelected = r.ColumnsSelected()
		rec.columnsRender = r.ColumnsRender()
		rec.columnsRecalcExtra = r.columnsRecalcExtra()
		rec.groupRow = row
		rec.hasRaw = true
		rec.idCount = row["id_count"]
		out = append(out, rec)
	}
	return out
}

// aggregateFields is aggregate_fields: the selects of all the aggregate
// joins of the store. The whole store is used, regardless of conditions, as
// aggregates rendered as subqueries are independent from the overall query.
func (r *Records) aggregateFields(rewind *time.Time, groupCols []groupColRef) []*aggField {
	var out []*aggField
	for _, j := range r.store().store {
		if j.aggregate == "" {
			continue
		}
		a := aggOpts{isGrouped: j.group, parent: j.parent, groupCols: groupCols, rewind: rewind}
		out = append(out, r.getAggregate(j.column, j.aggregate, a))
		// Also the linked column if required
		if lp := j.column.LinkParent(); lp != nil {
			al := a
			al.isLinked = j.column
			out = append(out, r.getAggregate(lp, j.aggregate, al))
		}
		for _, ch := range j.children {
			if ch.aggregate != "" {
				ac := a
				ac.parent = j.column
				out = append(out, r.getAggregate(ch.column, ch.aggregate, ac))
			}
		}
	}
	return out
}

// aggregateName is aggregate_name: the name of an aggregate field in the
// query, constructed of the parent field (if any), the field itself and the
// aggregate operator (none for max). Without an operator the one in the join
// store is used: for a curval child field (a Perl quirk) the aggregate
// configured for the column itself.
func (r *Records) aggregateName(column, parent *Column, operator string) string {
	name := column.Field()
	if parent != nil {
		name = parent.Field() + "_" + name
	}
	if operator == "" {
		found := false
		var op string
		for _, j := range r.store().store {
			if column.ID == j.column.ID {
				found, op = true, j.aggregate
			} else if parent != nil && parent.ID == j.column.ID {
				for _, ch := range j.children {
					if ch.column.ID == column.ID {
						found, op = true, ch.column.Aggregate
					}
				}
			}
		}
		if found {
			operator = op
		}
	}
	if operator != "" && operator != "max" {
		name += "_" + operator
	}
	return name
}

// getAggregate is get_aggregate: the select of a column's aggregate value
// for a grouped query.
func (r *Records) getAggregate(column *Column, operator string, a aggOpts) *aggField {
	if operator == "" {
		Panic("Missing operator")
	}
	// The operator used in the query may differ from the one required (e.g.
	// a subquery's value is retrieved using max so as not to double-count)
	originalOperator := operator
	st := r.store()
	asIndex := !r.groupValuesNotIndex
	drcol := r.drColumn != 0
	parent := a.parent
	cvo := r.cvoValues()
	var sel string
	var args []any
	if (column.MultivalueFn() || (parent != nil && parent.MultivalueFn())) && !a.isGrouped {
		if parent != nil {
			// A curval sub-field: search the curval table for the curvals of
			// the records retrieved
			jr := newJoinRenderer(r.e, formatRewind(r.RewindValues()))
			jr.layout = r.Layout
			tj := column.Tjoin(tjoinOptions{currentVersion: cvo})
			var rs *joinSpec
			if cvo {
				rs = &joinSpec{name: "current_version_alternative", kids: []*joinSpec{column.Tjoin(tjoinOptions{currentVersion: true})}}
			} else {
				rs = joinHash("record_single_alternative", joinLeaf("record_later_alternative"), column.Tjoin(tjoinOptions{}))
			}
			jr.render("curval", "mecurval", []*joinSpec{joinHash("value", rs)})
			field := column.Field()
			if tj != nil && tj.isHash() {
				field = "value_2"
			}
			vf := q(field + "." + column.ValueField())
			var agg string
			switch {
			case column.Numeric() && operator == "sum":
				agg = "SUM"
			case operator == "sum" || operator == "max":
				agg = "MAX"
			case operator == "distinct":
				Panic("Unexpected count distinct for curval sub-field %s (%d)", column.Name, column.ID)
			default:
				Panic("Unknown operator %s", operator)
			}
			match := `"record_single"."id"`
			if cvo {
				match = `"current_version"."id"`
			}
			where := `"mecurval"."record_id" = ` + match + ` AND "mecurval"."layout_id" = ?`
			wargs := []any{parent.ID}
			if !cvo {
				where += ` AND "record_later_alternative"."id" IS NULL`
			}
			if rw := r.RewindValues(); rw != nil {
				where += ` AND "record_single_alternative"."created" <= ?`
				wargs = append(wargs, formatRewind(rw))
			}
			sel = `(SELECT ` + agg + `( ` + vf + ` ) FROM "curval" "mecurval"` + jr.sb.String() + ` WHERE ( ` + where + ` ))`
			args = append(append([]any{}, jr.args...), wargs...)
		} else {
			// A standard subquery for the field, limited to the records of
			// the overall query and the group the row is part of
			hasGrouped := len(a.groupCols) > 0
			searchq := []cond{condCmp{field: "mefield.id", op: "IN", value: r.currentIDsSubquery(currentIDsOptions{})}}
			searchq = append(searchq, st.recordLaterSearch(joinOpts{alt: true, currentVersionOnly: cvo})...)
			if a.rewind != nil {
				searchq = append(searchq, st.recordSingleRewind(joinOpts{alt: true}, a.rewind)...)
			}
			common := joinOpts{search: false, sort: false, prefetch: false, hasPrefetch: true, aggregate: false,
				currentVersionOnly: cvo, group: hasGrouped, linked: false, hasLinked: true, drcol: drcol}
			for _, g := range a.groupCols {
				fo := common
				fo.extraColumn = g.column
				fo.parent = g.parent
				fa := fo
				fa.alt = true
				searchq = append(searchq, condCmp{field: st.fqvalue(g.column, fa, asIndex), op: "=", value: st.fqvalue(g.column, fo, asIndex), ident: true})
			}
			jr := newJoinRenderer(r.e, formatRewind(r.RewindValues()))
			jr.layout = r.Layout
			lo := common
			lo.alt = true
			lo.extraColumn = column
			if l := r.linkedJoins(lo); l != nil {
				jr.render("current", "mefield", []*joinSpec{l})
			}
			jo := common
			jo.extraColumn = column
			jo.alt = true
			kids := st.jpfetch(jo)
			if cvo {
				jr.render("current", "mefield", []*joinSpec{{name: "current_version_alternative", kids: kids}})
			} else {
				jr.render("current", "mefield", []*joinSpec{joinHash("record_single_alternative", append([]*joinSpec{joinLeaf("record_later_alternative")}, kids...)...)})
			}
			co := common
			co.alt = true
			co.extraColumn = column
			colFQ := q(st.fqvalue(column, co, asIndex))
			var wargs []any
			where := renderCond(condAnd(searchq), &wargs)
			var inner string
			switch {
			case column.Numeric() && operator == "sum":
				inner = "SUM( " + colFQ + " )"
				operator = "max"
			case operator == "sum" || operator == "max" || operator == "count":
				// count selects max here and then count as the operator
				inner = "MAX( " + colFQ + " )"
			case operator == "distinct":
				inner = "COUNT( DISTINCT " + colFQ + " )"
				operator = "max"
			default:
				Panic("Unknown operator %s", operator)
			}
			sel = `(SELECT ` + inner + ` FROM "current" "mefield"` + jr.sb.String()
			if where != "" {
				sel += " WHERE " + where
			}
			sel += ")"
			args = append(append([]any{}, jr.args...), wargs...)
		}
	} else {
		// Standard single-value field - select directly
		fo := joinOpts{aggregate: true, group: true, linked: a.isLinked != nil, hasLinked: true, parent: parent,
			retainJoinOrder: true, drcol: drcol, currentVersionOnly: cvo}
		sel = q(st.fqvalue(column, fo, asIndex))
	}
	var as string
	if a.isLinked != nil {
		as = r.aggregateName(a.isLinked, nil, originalOperator) + "_link"
	} else {
		as = r.aggregateName(column, parent, originalOperator)
	}
	var overall string
	if operator == "distinct" {
		overall = "COUNT( DISTINCT " + sel + " ) AS " + qa(as)
	} else {
		overall = strings.ToUpper(operator) + "( " + sel + " ) AS " + qa(as)
	}
	return &aggField{column: column, parent: parent, sel: overall, args: args, operator: operator, as: as}
}

// groupRawValues returns the raw values of a column from a group row.
func (rec *Record) groupRawValues(c *Column) ([]rawValue, bool) {
	row := rec.groupRow
	key := rec.records.aggregateName(c, nil, "")
	v, ok := row[key]
	if !ok {
		return nil, false
	}
	if v == nil {
		return []rawValue{}, true
	}
	rv := rawValue{Value: v}
	if c.Type == "calc" {
		switch c.ValueField() {
		case "value_text":
			rv.ValueText = v
		case "value_int":
			rv.ValueInt = v
		case "value_date":
			rv.ValueDate = v
		case "value_numeric":
			rv.ValueNumeric = v
		}
	}
	if c.Type == "daterange" {
		if s, ok := db.AsString(v); ok {
			if m := reDaterangeDB.FindStringSubmatch(s); m != nil {
				rv.From, rv.To = m[1], m[2]
			}
		}
	}
	return []rawValue{rv}, true
}

var reDaterangeDB = regexpMust(`^([-0-9]+) to ([-0-9]+)$`)

// drColumnSelects builds the per-interval sums for daterange graph x-axes
// (implemented in graph.go).
func (r *Records) drColumnSelects(cols []*groupCol) ([]string, []any) {
	return r.drSelects(cols)
}
