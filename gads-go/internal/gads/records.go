package gads

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/perl"
)

// qa quotes a column alias (which may contain dots).
func qa(s string) string { return `"` + s + `"` }

// SortSpec is a sort requested on a records search.
type SortSpec struct {
	ID       int64
	ParentID int64
	Type     string
}

// AdditionalFilter is an extra filter (column => value).
type AdditionalFilter struct {
	ID     int64
	Value  any // A value or list of values
	IsText bool
}

// Records is a port of GADS::Records: a search of the records of a table.
type Records struct {
	e      *Engine
	Layout *Layout
	User   *User
	View   *View

	Rows       int
	Page       int
	Search     string
	hasSearch  bool
	Rewind     *time.Time
	From, To   *time.Time
	Exclusive  string
	MaxResults int

	ColumnIDs                  []int64 // "columns" option
	hasColumnIDs               bool
	ColumnsExtra               []SortSpec
	IsDeleted                  bool
	IncludeDeleted             bool
	PreviousValues             bool
	RecordEarlier              bool
	includeChildren            *bool
	IsDraft                    int64
	NoViewLimits               bool
	IgnoreViewLimitExtra       bool
	ViewLimitExtraID           *int64
	ViewLimitOverrideID        *int64
	IncludeApproval            bool
	LimitCurrentIDs            []int64
	hasLimitCurrentIDs         bool
	ForCurval                  *struct{ RecordID, LayoutID int64 }
	sortSpec                   []SortSpec
	hasSort                    bool
	AdditionalFilters          []AdditionalFilter
	SeparateRecordsForMulticol int64
	// IsGroupOverride sets is_group (instead of it coming from the view)
	IsGroupOverride    *bool
	viewLimitsOverride *[]*View
	alreadySeen        *seenNode

	rewindValues       *time.Time
	rewindValuesSet    bool
	rewindRecordset    *time.Time
	rewindRecordsetSet bool
	cvoValuesOverride  *bool
	chronology         bool // fetch_multivalues for a chronology

	viewLimitOverrideView  *View
	viewLimitOverrideBuilt bool

	// state
	js                 *joinStore
	columnsSelected    []*Column
	columnsRetrievedDo []*Column
	columnsRetrievedNo []*Column
	columnsRender      []*Column
	columnsBuilt       bool
	results            []*Record
	resultsBuilt       bool
	currentIDs         []int64
	currentIDsBuilt    bool
	allCIDsStore       []int64
	allCIDsBuilt       bool
	singlePage         int
	nextSingleID       int
	count              *int
	sorts              []SortSpec
	sortsBuilt         bool
	viewLimits         []*View
	viewLimitsBuilt    bool
	searchAll          *searchAllResult
	findRecordID       int64
	findDraftuserID    int64

	// Graph / grouping options (GADS::RecordsGraph)
	isGraph             bool
	isGlobe             bool
	globeGroup          bool
	graphColumns        []*groupCol
	graphRows           []db.Row
	drColumn            int64
	drColumnParent      *Column
	drInterval          string
	drYAxisID           int64
	groupValuesNotIndex bool
	drFrom, drTo        *time.Time

	// DefaultSortOverride replaces the table default sort
	DefaultSortOverride *SortSpec
}

// NewRecords creates a records search.
func (e *Engine) NewRecords(layout *Layout, user *User) *Records {
	return &Records{e: e, Layout: layout, User: user}
}

// SetColumns sets the column IDs to retrieve.
func (r *Records) SetColumns(ids []int64) {
	r.ColumnIDs = ids
	r.hasColumnIDs = true
	r.clearColumns()
}

// SetSort sets the sort.
func (r *Records) SetSort(s []SortSpec) {
	r.sortSpec = s
	r.hasSort = s != nil
	r.sorts = nil
	r.sortsBuilt = false
}

// SetSearch sets the quick search text (used if true, as in Perl)
func (r *Records) SetSearch(s string) { r.Search = s; r.hasSearch = s != "" && s != "0" }

// SetIncludeChildren forces inclusion of child records.
func (r *Records) SetIncludeChildren(v bool) { r.includeChildren = &v }

// SetLimitCurrentIDs limits the search to some current IDs.
func (r *Records) SetLimitCurrentIDs(ids []int64) {
	r.LimitCurrentIDs = ids
	r.hasLimitCurrentIDs = true
}

// AlreadySeen returns the recursion tree.
func (r *Records) AlreadySeen() *seenNode {
	if r.alreadySeen == nil {
		r.alreadySeen = newSeenRoot()
	}
	return r.alreadySeen
}

func (r *Records) store() *joinStore {
	if r.js == nil {
		r.js = &joinStore{r: r}
	}
	return r.js
}

func (r *Records) clearColumns() {
	r.columnsBuilt = false
	r.columnsSelected = nil
	r.columnsRetrievedDo = nil
	r.columnsRetrievedNo = nil
	r.columnsRender = nil
}

func (r *Records) includeChildrenFn() bool {
	if r.includeChildren != nil {
		return *r.includeChildren
	}
	for _, c := range r.ColumnsRetrievedNo() {
		if c.CanChild() {
			return true
		}
	}
	return false
}

func (r *Records) isGroup() bool {
	if r.IsGroupOverride != nil {
		return *r.IsGroupOverride
	}
	if r.isGlobe {
		return r.globeGroup
	}
	return r.isGraph || (r.View != nil && r.View.IsGroup())
}

// rawGroupRows is true for RecordsGraph and RecordsGlobe, whose grouped
// results are raw rows rather than records.
func (r *Records) rawGroupRows() bool { return r.isGraph || r.isGlobe }

// ---------------------------------------------------------------------
// Columns
// ---------------------------------------------------------------------

func (r *Records) buildColumns() {
	if r.columnsBuilt {
		return
	}
	r.columnsBuilt = true
	// columns_selected
	var cols []*Column
	if r.hasColumnIDs {
		ids := map[int64]bool{}
		for _, id := range r.ColumnIDs {
			ids[id] = true
		}
		for _, c := range r.Layout.All(AllOptions{}) {
			if ids[c.ID] {
				cols = append(cols, c)
			}
		}
	} else if r.View != nil {
		vl := map[int64]bool{}
		for _, id := range r.View.Columns() {
			vl[id] = true
		}
		cols = r.Layout.All(AllOptions{IncludeColumnIDs: vl})
		for _, g := range r.View.Groups() {
			if !vl[g.LayoutID] {
				if c := r.Layout.Column(g.LayoutID); c != nil {
					cols = append(cols, c)
				}
			}
		}
		if len(r.AdditionalFilters) == 0 {
			var keep []*Column
			for _, c := range cols {
				if !r.isGroup() || c.GroupDisplay() != "" || r.hasGroupColID(c.ID) {
					keep = append(keep, c)
				}
			}
			cols = keep
		}
	} else {
		cols = r.Layout.All(AllOptions{})
	}
	r.columnsSelected = cols
	// columns_retrieved_do
	do := append([]*Column(nil), cols...)
	for _, ce := range r.ColumnsExtra {
		if ce.ParentID != 0 {
			do = append(do, r.Layout.Column(ce.ParentID))
		}
		do = append(do, r.Layout.Column(ce.ID))
	}
	st := r.store()
	viewGroupParent := map[int64]int64{}
	if r.View != nil {
		for _, g := range r.View.Groups() {
			viewGroupParent[g.LayoutID] = g.ParentID
		}
	}
	for _, c := range do {
		var parent *Column
		if c != nil && viewGroupParent[c.ID] != 0 {
			parent = r.Layout.Column(viewGroupParent[c.ID])
		}
		st.addPrefetch(c, joinOpts{parent: parent, includeMultivalue: r.SeparateRecordsForMulticol})
		if lp := c.LinkParent(); lp != nil {
			st.addLinkedPrefetch(lp, joinOpts{})
		}
	}
	st.addPrefetch(r.Layout.ColumnByNameShort("_version_user"), joinOpts{})
	r.columnsRetrievedDo = do
	// columns_retrieved_no
	inDo := map[int64]bool{}
	for _, c := range do {
		if c != nil {
			inDo[c.ID] = true
		}
	}
	var no []*Column
	for _, c := range r.Layout.All(AllOptions{}) {
		if inDo[c.ID] {
			no = append(no, c)
		}
	}
	r.columnsRetrievedNo = no
	// columns_render
	var render []*Column
	for _, c := range r.columnsSelected {
		// With an override view, only its columns can be viewed
		if c.UserCan("read") && !r.overrideHidesColumn(c.ID) {
			render = append(render, c)
		}
	}
	if r.View != nil && len(r.AdditionalFilters) == 0 && r.isGroup() {
		var keep []*Column
		for _, c := range render {
			if !r.hasGroupColID(c.ID) {
				keep = append(keep, c)
			}
		}
		var gcols []*Column
		for _, c := range r.GroupCols() {
			if c.UserCan("read") {
				gcols = append(gcols, c)
			}
		}
		render = append(gcols, keep...)
	}
	if !r.isGroup() {
		render = append([]*Column{r.Layout.ColumnID()}, render...)
	}
	r.columnsRender = render
}

// ColumnsSelected returns the selected columns.
func (r *Records) ColumnsSelected() []*Column { r.buildColumns(); return r.columnsSelected }

// ColumnsRetrievedDo returns retrieved columns in dependency order.
func (r *Records) ColumnsRetrievedDo() []*Column { r.buildColumns(); return r.columnsRetrievedDo }

// ColumnsRetrievedNo returns retrieved columns in normal order.
func (r *Records) ColumnsRetrievedNo() []*Column { r.buildColumns(); return r.columnsRetrievedNo }

// ColumnsRender returns the columns to render.
func (r *Records) ColumnsRender() []*Column { r.buildColumns(); return r.columnsRender }

func (r *Records) groupColIDs() []int64 {
	var out []int64
	for _, c := range r.GroupCols() {
		out = append(out, c.ID)
	}
	return out
}

// GroupCols returns the columns the view is grouped by, as clones with the
// parent of each group (group_cols).
func (r *Records) GroupCols() []*Column {
	if r.View == nil {
		return nil
	}
	var out []*Column
	for _, g := range r.View.Groups() {
		col := r.Layout.Column(g.LayoutID)
		if col == nil {
			Fail(`Can't call method "clone" on an undefined value`)
		}
		out = append(out, col.Clone(g.ParentID))
	}
	return out
}

func (r *Records) hasGroupColID(id int64) bool {
	for _, g := range r.groupColIDs() {
		if g == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------
// View limits
// ---------------------------------------------------------------------

func (r *Records) getViewLimits() []*View {
	if r.viewLimitsOverride != nil {
		return *r.viewLimitsOverride
	}
	if r.viewLimitsBuilt {
		return r.viewLimits
	}
	r.viewLimitsBuilt = true
	if r.User == nil || r.e.IgnorePermissions || r.IsDraft != 0 {
		return nil
	}
	ids := r.e.DB.QueryInt64s(`SELECT "me"."view_id" FROM "view_limit" "me" JOIN "view" "view" ON "view"."id" = "me"."view_id" WHERE "me"."user_id" = ? AND "view"."instance_id" = ? ORDER BY "me"."id"`, r.User.ID, r.Layout.InstanceID)
	if len(ids) == 0 {
		if vl := r.Layout.ViewLimitID(); vl != nil && *vl != 0 {
			ids = []int64{*vl}
		}
	}
	for _, id := range ids {
		r.viewLimits = append(r.viewLimits, r.e.LoadView(id, r.Layout))
	}
	return r.viewLimits
}

// viewLimitOverride is _view_limit_override: a view (in this table, set
// as a limit override) whose records are shown instead of those of the
// user's view limits.
func (r *Records) viewLimitOverride() *View {
	if r.viewLimitOverrideBuilt {
		return r.viewLimitOverrideView
	}
	r.viewLimitOverrideBuilt = true
	if r.ViewLimitOverrideID == nil || *r.ViewLimitOverrideID == 0 {
		return nil
	}
	row := r.e.DB.QueryRow(`SELECT "id", "instance_id", "is_limit_override" FROM "view" WHERE "id" = ?`, *r.ViewLimitOverrideID)
	if row == nil {
		Fail(`Can't call method "instance_id" on an undefined value`)
	}
	iid, _ := db.AsInt64(row["instance_id"])
	if iid == r.Layout.InstanceID && rowBool(row["is_limit_override"]) {
		r.viewLimitOverrideView = r.e.LoadView(*r.ViewLimitOverrideID, r.Layout)
	}
	return r.viewLimitOverrideView
}

// overrideHidesColumn reports whether a view limit override is in use that
// does not include the column.
func (r *Records) overrideHidesColumn(id int64) bool {
	o := r.viewLimitOverride()
	return o != nil && !o.HasColumnID(id)
}

func (r *Records) viewLimitExtra() *View {
	if r.IgnoreViewLimitExtra {
		return nil
	}
	extra := r.ViewLimitExtraID
	if extra == nil {
		extra = r.Layout.DefaultViewLimitExtraID()
	}
	def := r.Layout.DefaultViewLimitExtraID()
	if def != nil && *def != 0 && !r.Layout.UserCan("view_limit_extra") {
		extra = def
	}
	if extra != nil && *extra != 0 {
		iid, ok := r.e.DB.QueryInt64(`SELECT "instance_id" FROM "view" WHERE "id" = ?`, *extra)
		if ok && iid == r.Layout.InstanceID {
			return r.e.LoadView(*extra, r.Layout)
		}
	}
	return nil
}

func (r *Records) viewLimitsSearch(o searchOpts) []cond {
	if r.NoViewLimits {
		return nil
	}
	var search []cond
	var limits []*View
	if r.viewLimitOverride() == nil {
		limits = r.getViewLimits()
	}
	for _, v := range limits {
		if f := v.Filter(); f != nil {
			h := f.AsHash()
			if len(h) > 0 {
				oo := o
				oo.ignorePerms = true
				if c := r.searchConstruct(h, oo); c != nil {
					search = append(search, c)
				}
			}
		}
	}
	var limit []cond
	if len(search) > 0 {
		limit = []cond{condOr(search)}
	}
	if ve := r.viewLimitExtra(); ve != nil {
		h := ve.Filter().AsHash()
		if len(h) > 0 {
			oo := o
			oo.ignorePerms = true
			s := r.searchConstruct(h, oo)
			if len(limit) > 0 {
				limit = []cond{condAnd{condAnd(limit), s}}
			} else if s != nil {
				limit = []cond{s}
			}
		}
	}
	return limit
}

// ---------------------------------------------------------------------
// Search query
// ---------------------------------------------------------------------

type searchOpts struct {
	joinOpts
	ignorePerms   bool
	user          *User
	chronology    bool
	noRecordLater bool
	rewind        *time.Time
}

func (r *Records) commonSearch(current string) []cond {
	if current == "" {
		current = "me"
	}
	var out []cond
	if r.IsDeleted {
		out = append(out, condNotNull(current+".deleted"))
	} else if !r.IncludeDeleted {
		out = append(out, condIsNull(current+".deleted"))
	}
	if !r.includeChildrenFn() {
		out = append(out, condIsNull(current+".parent_id"))
	}
	if r.IsDraft != 0 {
		out = append(out, condOr{condIsNull(current + ".draftuser_id"), condEq(current+".draftuser_id", r.IsDraft)})
	} else {
		out = append(out, condIsNull(current+".draftuser_id"))
	}
	return out
}

func (r *Records) approvalQuery(o searchOpts) cond {
	if r.IncludeApproval {
		return nil
	}
	root := o.rootTable
	if root == "" {
		root = "current"
	}
	exists := false
	if root == "current" {
		_, exists = r.e.DB.QueryInt64(`SELECT "me"."id" FROM "current" "me" LEFT JOIN "record" "records" ON "records"."current_id" = "me"."id" WHERE "instance_id" = ? AND "records"."approval" = ? LIMIT 1`, r.Layout.InstanceID, 1)
	}
	if !exists {
		return nil
	}
	return condEq(r.store().recordName(o.joinOpts)+".approval", 0)
}

// RewindValues is rewind_values: the time at which to take values
// (defaults to Rewind).
func (r *Records) RewindValues() *time.Time {
	if r.rewindValuesSet {
		return r.rewindValues
	}
	return r.Rewind
}

// SetRewindValues sets rewind_values (only the values are rewound, not the
// set of records).
func (r *Records) SetRewindValues(t *time.Time) {
	r.rewindValues = t
	r.rewindValuesSet = true
}

// RewindRecordset is rewind_recordset: the time at which to take the set
// of records (defaults to Rewind).
func (r *Records) RewindRecordset() *time.Time {
	if r.rewindRecordsetSet {
		return r.rewindRecordset
	}
	return r.Rewind
}

// SetRewind sets rewind, resetting rewind_values and rewind_recordset.
func (r *Records) SetRewind(t *time.Time) {
	r.Rewind = t
	r.rewindValuesSet = false
	r.rewindRecordsetSet = false
	r.cvoValuesOverride = nil
}

// cvoValues is cvo_values: whether values are taken directly from the
// current version of each record.
func (r *Records) cvoValues() bool {
	if r.cvoValuesOverride != nil {
		return *r.cvoValuesOverride
	}
	// A find of a specific version (Record::_find record_id) selects that
	// version rather than the current one
	if r.findRecordID != 0 {
		return false
	}
	return !(r.RewindValues() != nil || r.RecordEarlier || r.PreviousValues)
}

// cvoRecordset is cvo_recordset: whether the set of records is taken from
// the current version of each record.
func (r *Records) cvoRecordset() bool {
	if r.findRecordID != 0 {
		return false
	}
	return !(r.RewindRecordset() != nil || r.RecordEarlier || r.PreviousValues)
}

func formatRewind(t *time.Time) string {
	if t == nil {
		return ""
	}
	return db.FormatDateTime(*t)
}

func (r *Records) searchQuery(o searchOpts) []cond {
	linked := o.linked
	o2 := o
	o2.linked = false
	o2.hasLinked = false
	search := r.queryParams(o2)
	root := o.rootTable
	if root == "" {
		root = "current"
	}
	current := "current"
	if o.alias != "" {
		current = o.alias
	} else if root == "current" {
		current = "me"
	}
	if a := r.approvalQuery(o2); a != nil {
		search = append(search, a)
	}
	if r.hasSearch {
		ids := r.searchAllFields().cids
		vals := make([]any, len(ids))
		for i, id := range ids {
			vals[i] = id
		}
		search = append(search, condCmp{field: current + ".id", op: "=", value: vals})
	}
	if r.hasLimitCurrentIDs {
		vals := make([]any, len(r.LimitCurrentIDs))
		for i, id := range r.LimitCurrentIDs {
			vals[i] = id
		}
		search = append(search, condCmp{field: current + ".id", op: "=", value: vals})
	}
	search = append(search, condEq(current+".instance_id", r.Layout.InstanceID))
	search = append(search, r.commonSearch(current)...)
	if !o.chronology && !o.noRecordLater {
		lo := o2.joinOpts
		lo.linked = linked
		lo.search = true
		search = append(search, r.findVersionConds(r.store().recordLaterSearch(lo), o2.joinOpts)...)
	}
	if r.findDraftuserID != 0 {
		search = append(search, condEq(current+".draftuser_id", r.findDraftuserID), condIsNull("curvals.id"))
	}
	if o.rewind != nil {
		if rw := r.store().recordSingleRewind(o2.joinOpts, o.rewind); len(rw) > 0 {
			search = append(search, condAnd(rw))
		}
	}
	return search
}

// ---------------------------------------------------------------------
// Query params (filters)
// ---------------------------------------------------------------------

func (r *Records) searchDate(c *Column, sd *[]any, parentID int64) {
	if c.IsCurcommon() {
		for _, cc := range c.CurvalFields() {
			if cc != nil {
				r.searchDate(cc, sd, c.ID)
			}
		}
		return
	}
	if !strings.Contains(c.ReturnTypeFn(), "date") {
		return
	}
	format := c.DateFormat()
	sid := any(c.ID)
	if parentID != 0 {
		sid = itoa(parentID) + "_" + itoa(c.ID)
	}
	var f []any
	if r.To != nil {
		op := "less_or_equal"
		if r.Exclusive == "to" {
			op = "less"
		}
		f = append(f, map[string]any{"id": sid, "operator": op, "value": formatUTC(*r.To, format)})
	}
	if r.From != nil {
		op := "greater_or_equal"
		if r.Exclusive == "from" {
			op = "greater"
		}
		f = append(f, map[string]any{"id": sid, "operator": op, "value": formatUTC(*r.From, format)})
	}
	if len(f) > 0 {
		*sd = append(*sd, map[string]any{"condition": "AND", "rules": f})
	}
}

func (r *Records) queryParams(o searchOpts) []cond {
	var sd []any
	for _, c := range r.ColumnsRetrievedNo() {
		r.searchDate(c, &sd, 0)
	}
	var search []cond
	for loop := 0; loop < 2; loop++ {
		search = nil
		if len(sd) > 0 {
			if c := r.searchConstruct(map[string]any{"condition": "OR", "rules": sd}, o); c != nil {
				search = append(search, c)
			}
		}
		for _, af := range r.AdditionalFilters {
			col := r.Layout.Column(af.ID)
			op := "equal"
			if col.StringStorage() || af.IsText {
				op = "contains"
			}
			f := map[string]any{"id": col.ID, "operator": op, "value": af.Value}
			if !af.IsText {
				f["value_field"] = col.ValueFieldAsIndex(af.Value)
			}
			if c := r.searchConstruct(f, o); c != nil {
				search = append(search, c)
			}
		}
		if r.View != nil && r.View.Filter() != nil && !r.hasSearch {
			h := r.View.Filter().AsHash()
			if len(h) > 0 {
				if c := r.searchConstruct(h, o); c != nil {
					search = append(search, c)
				}
			}
		}
		search = append(search, r.viewLimitsSearch(o)...)
		r.orderBy(orderByOpts{})
	}
	if fc := r.ForCurval; fc != nil {
		ids := r.e.DB.QueryInt64s(`SELECT "value" FROM "curval" WHERE "record_id" = ? AND "layout_id" = ? AND "value" IS NOT NULL`, fc.RecordID, fc.LayoutID)
		vals := make([]any, len(ids))
		for i, id := range ids {
			vals[i] = id
		}
		search = append(search, condCmp{field: "me.id", op: "IN", value: vals})
	}
	return search
}

// searchConstruct converts a filter hash to a condition (map_rules).
func (r *Records) searchConstruct(filter map[string]any, o searchOpts) cond {
	return r.mapRules(filter, o)
}

func (r *Records) mapRules(filter map[string]any, o searchOpts) cond {
	if rules, ok := filter["rules"]; ok && truthy(rules) {
		if hook := r.filterHook(filter); hook != nil {
			return hook
		}
		var final []cond
		if rl, ok := rules.([]any); ok {
			for _, rule := range rl {
				rm, ok := rule.(map[string]any)
				if !ok {
					continue
				}
				if c := r.mapRules(rm, o); c != nil {
					final = append(final, c)
				}
			}
		}
		if len(final) == 0 {
			return nil
		}
		if anyToString(filter["condition"]) == "OR" {
			return condOr(final)
		}
		return condAnd(final)
	}
	if len(filter) == 0 {
		return nil
	}
	return r.ruleToCondition(filter, o)
}

func (r *Records) filterHook(filter map[string]any) cond {
	pv, ok := filter["previous_values"]
	if !ok || !truthy(pv) {
		return nil
	}
	nf := map[string]any{}
	for k, v := range filter {
		nf[k] = v
	}
	nf["previous_values"] = int64(0)
	sub := r.e.NewRecords(r.Layout, r.User)
	empty := []*View{}
	sub.viewLimitsOverride = &empty
	sub.PreviousValues = true
	sub.View = r.e.NewTempView(r.Layout, NewFilterHash(nf, nil))
	sq := sub.currentIDsSubquery(currentIDsOptions{})
	op := "IN"
	if anyToString(pv) == "negative" {
		op = "NOT IN"
	}
	return condCmp{field: "me.id", op: op, value: sq}
}

var conditionFieldMap = map[string]string{
	"equal":            "=",
	"greater":          ">",
	"greater_or_equal": ">=",
	"less":             "<",
	"less_or_equal":    "<=",
	"not_equal":        "!=",
	"begins_with":      "-like",
	"not_begins_with":  "-not_like",
	"ends_with":        "-like",
	"contains":         "-like",
	"not_contains":     "-not_like",
	"is_empty":         "=",
	"is_not_empty":     "!=",
}

// escapeLike escapes the first occurrence only, as GADS does.
func escapeLike(s string) string {
	s = strings.Replace(s, `\`, `\\`, 1)
	s = strings.Replace(s, "%", `\%`, 1)
	s = strings.Replace(s, "_", `\_`, 1)
	return s
}

func getFilterValue(op, value string, isNumber bool) string {
	if isNumber {
		return value
	}
	switch strings.ToLower(op) {
	case "begins_with", "not_begins_with":
		return escapeLike(value) + "%"
	case "ends_with":
		return "%" + escapeLike(value)
	case "contains", "not_contains":
		return "%" + escapeLike(value) + "%"
	}
	return value
}

type ruleCondition struct {
	typ      string
	operator string
	sField   string
	values   []any
}

var reCuruser = regexp.MustCompile(`\[CURUSER(\.(ORG|DEPT|TEAM|ID|TITLE|EMAIL))?\]`)

func (r *Records) ruleToCondition(filter map[string]any, o searchOpts) cond {
	ignorePerms := o.ignorePerms || r.e.IgnorePermissionsSearch || r.e.IgnorePermissions
	perm := "read"
	if ignorePerms {
		perm = ""
	}
	if !truthy(filter["id"]) {
		return nil
	}
	ids := anyToString(filter["id"])
	var column, parent *Column
	if m := reParentChild.FindStringSubmatch(ids); m != nil {
		cid, _ := anyToInt64(m[2])
		pid, _ := anyToInt64(m[1])
		column = r.Layout.ColumnPerm(cid, perm)
		parent = r.Layout.ColumnPerm(pid, perm)
	} else {
		id, _ := anyToInt64(ids)
		column = r.Layout.ColumnPerm(id, perm)
	}
	if column == nil {
		return nil
	}
	if r.overrideHidesColumn(column.ID) {
		return nil
	}
	operatorIn := anyToString(filter["operator"])
	if operatorIn == "changed_after" {
		var value string
		fv := anyToString(filter["value"])
		if strings.Contains(fv, "CURDATE") {
			t, _ := ParseDateFilter(r.e.Now(), fv)
			value = db.FormatDate(t)
		} else {
			value = r.dateForDB(column, fv)
		}
		return r.resolve(column, ruleCondition{typ: "changed_after", values: []any{value}}, nil, false, resolveOpts{filter: filter, searchOpts: o})
	}
	if arr, ok := filter["value"].([]any); ok && len(arr) == 0 {
		filter["value"] = ""
	}
	previous := filter["previous_values"]
	reverse := truthy(previous) && strings.HasPrefix(operatorIn, "not")
	fop := operatorIn
	if fop != "is_empty" && fop != "is_not_empty" && !strings.Contains(fop, "is_empty") && !strings.Contains(fop, "is_not_empty") {
		v := filter["value"]
		blank := false
		switch x := v.(type) {
		case nil:
			blank = true
		case string:
			blank = x == ""
		case []any:
			var parts []string
			for _, p := range x {
				parts = append(parts, anyToString(p))
			}
			blank = strings.Join(parts, " ") == ""
		}
		if blank {
			if fop == "not_equal" {
				fop = "is_not_empty"
			} else {
				fop = "is_empty"
			}
		}
	}
	if reverse && fop == "not_equal" {
		fop = "equal"
	}
	if reverse && fop == "not_begins_with" {
		fop = "begins_with"
	}
	if reverse && fop == "not_contains" {
		fop = "contains"
	}
	operator, ok := conditionFieldMap[fop]
	if !ok {
		Fail("Invalid operator %s", fop)
	}
	var conditions []ruleCondition
	gate := "and"
	transformDate := false
	if column.ReturnTypeFn() == "daterange" {
		switch operator {
		case "!=", "=":
			conditions = append(conditions, ruleCondition{typ: fop, operator: operator, sField: column.ValueField()})
		case ">", "<=":
			transformDate = true
			conditions = append(conditions, ruleCondition{typ: fop, operator: operator, sField: column.FromField()})
		case ">=", "<":
			transformDate = true
			conditions = append(conditions, ruleCondition{typ: fop, operator: operator, sField: column.ToField()})
		case "-like", "-not_like":
			transformDate = true
			op1, op2 := "<=", ">="
			if operator == "-not_like" {
				op1, op2 = ">=", "<="
			}
			conditions = append(conditions, ruleCondition{typ: fop, operator: op1, sField: column.FromField()})
			conditions = append(conditions, ruleCondition{typ: fop, operator: op2, sField: column.ToField()})
			if operator == "-like" {
				operator = "equal"
			} else {
				operator = "not_equal"
				gate = "or"
			}
		default:
			Fail("Invalid operator %s for date range", operator)
		}
	} else {
		sf := column.ValueField()
		if vf := anyToString(filter["value_field"]); vf != "" {
			sf = vf
		}
		conditions = append(conditions, ruleCondition{typ: fop, operator: operator, sField: sf})
	}
	var values []any
	if fop == "is_empty" || fop == "is_not_empty" {
		if column.StringStorage() {
			values = []any{nil, ""}
		} else {
			values = []any{nil}
		}
	} else {
		var orig []any
		if arr, ok := filter["value"].([]any); ok {
			orig = arr
		} else {
			orig = []any{filter["value"]}
		}
		for _, ov := range orig {
			v := getFilterValue(fop, anyToString(ov), column.ReturnTypeFn() != "string")
			if !column.ValidateSearch(v, false, nil) {
				return condFalse
			}
			if strings.Contains(v, "CURDATE") {
				t, _ := ParseDateFilter(r.e.Now(), v)
				v = db.FormatDate(t)
			} else if transformDate || (column.ReturnTypeFn() == "date" && perl.True(v)) {
				v = r.dateForDB(column, v)
			}
			if operator == "-like" {
				v = strings.ReplaceAll(v, "_", `\_`)
			}
			if perl.True(v) && reCuruser.MatchString(v) {
				cu := o.user
				if cu == nil {
					cu = r.User
				}
				if cu == nil {
					r.e.Warn("FIXME: user not set for filter")
				}
				cuid := ""
				if cu != nil {
					cuid = itoa(cu.ID)
				}
				if column.Type == "person" {
					v = strings.ReplaceAll(v, "[CURUSER]", cuid)
					conditions[0].sField = "id"
				} else if column.ReturnTypeFn() == "string" {
					val, email, org, dept, team, title := "", "", "", "", "", ""
					if cu != nil {
						val, email = cu.Value, cu.Email
						org, dept, team, title = cu.OrganisationName(), cu.DepartmentName(), cu.TeamName(), cu.TitleName()
					}
					v = strings.ReplaceAll(v, "[CURUSER]", val)
					v = strings.ReplaceAll(v, "[CURUSER.EMAIL]", email)
					v = strings.ReplaceAll(v, "[CURUSER.ORG]", org)
					v = strings.ReplaceAll(v, "[CURUSER.DEPT]", dept)
					v = strings.ReplaceAll(v, "[CURUSER.TEAM]", team)
					v = strings.ReplaceAll(v, "[CURUSER.TITLE]", title)
					v = strings.ReplaceAll(v, "[CURUSER.ID]", cuid)
				} else if column.ReturnTypeFn() == "integer" {
					v = strings.ReplaceAll(v, "[CURUSER.ID]", cuid)
				}
			}
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return condFalse
	}
	if column.Type == "string" {
		var iv []any
		for _, v := range values {
			if v == nil || anyToString(v) == "" {
				iv = append(iv, v)
			} else {
				iv = append(iv, perl.Lc(perl.Substr(anyToString(v), 128)))
			}
		}
		conditions = append(conditions, ruleCondition{typ: fop, operator: operator, sField: "value_index", values: iv})
	}
	ro := resolveOpts{parent: parent, filter: filter, previous: truthy(previous), reverse: reverse, searchOpts: o}
	var final []cond
	for _, c := range conditions {
		if rc := r.resolve(column, c, values, false, ro); rc != nil {
			final = append(final, rc)
		}
	}
	var result cond
	if len(final) == 1 {
		result = final[0]
	} else if gate == "or" {
		result = condOr(final)
	} else {
		result = condAnd(final)
	}
	var parentLink *Column
	if parent != nil {
		parentLink = parent.LinkParent()
	}
	if parentLink != nil || column.LinkParent() != nil {
		var lp *Column
		if parent != nil {
			lp = column
		} else {
			lp = column.LinkParent()
		}
		var final2 []cond
		ro2 := ro
		ro2.parent = parentLink
		for _, c := range conditions {
			if rc := r.resolve(lp, c, values, true, ro2); rc != nil {
				final2 = append(final2, rc)
			}
		}
		var r2 cond
		if gate == "or" {
			r2 = condOr(final2)
		} else {
			r2 = condAnd(final2)
		}
		result = condOr{result, r2}
	}
	return result
}

type resolveOpts struct {
	searchOpts
	parent   *Column
	filter   map[string]any
	previous bool
	reverse  bool
}

func (r *Records) resolve(column *Column, c ruleCondition, defaultValue []any, isLinked bool, o resolveOpts) cond {
	value := c.values
	if value == nil {
		value = defaultValue
	}
	multivalue := column.MultivalueFn()
	if o.parent != nil {
		multivalue = o.parent.MultivalueFn()
	}
	meTable := "me"
	if o.searchOpts.alias != "" {
		meTable = o.searchOpts.alias
	}
	if c.typ == "changed_after" {
		return r.changedAfter(column, anyToString(value[0]), meTable)
	}
	// If this set of records is a search across all versions
	// (previous_values) then perform a normal join of all the conditions,
	// so that a match is only made when they are all true for a version
	if multivalue && c.typ == "not_equal" && !o.previous && !r.PreviousValues {
		nf := map[string]any{}
		for k, v := range o.filter {
			nf[k] = v
		}
		nf["operator"] = "equal"
		sub := r.e.NewRecords(r.Layout, r.User)
		empty := []*View{}
		sub.viewLimitsOverride = &empty
		sub.View = r.e.NewTempView(r.Layout, NewFilterHash(nf, nil))
		return condCmp{field: meTable + ".id", op: "NOT IN", value: sub.currentIDsSubquery(currentIDsOptions{})}
	}
	if o.previous {
		nf := map[string]any{}
		for k, v := range o.filter {
			if k == "previous_values" {
				continue
			}
			nf[k] = v
		}
		if o.reverse {
			nf["operator"] = strings.TrimPrefix(anyToString(nf["operator"]), "not_")
		}
		sub := r.e.NewRecords(r.Layout, r.User)
		empty := []*View{}
		sub.viewLimitsOverride = &empty
		sub.PreviousValues = true
		sub.View = r.e.NewTempView(r.Layout, NewFilterHash(nf, nil))
		op := "IN"
		if o.reverse {
			op = "NOT IN"
		}
		return condCmp{field: meTable + ".id", op: op, value: sub.currentIDsSubquery(currentIDsOptions{})}
	}
	if column.NameShort == "_created_user" || column.NameShort == "_created" {
		nf := map[string]any{}
		for k, v := range o.filter {
			nf[k] = v
		}
		var vid int64
		if column.NameShort == "_created" {
			vid = r.Layout.ColumnByNameShort("_version_datetime").ID
		} else {
			vid = r.Layout.ColumnByNameShort("_version_user").ID
		}
		nf["column_id"] = vid
		nf["id"] = vid
		sub := r.e.NewRecords(r.Layout, r.User)
		empty := []*View{}
		sub.viewLimitsOverride = &empty
		sub.RecordEarlier = true
		sub.View = r.e.NewTempView(r.Layout, NewFilterHash(nf, nil))
		return condCmp{field: meTable + ".id", op: "IN", value: sub.currentIDsSubquery(currentIDsOptions{})}
	}
	combiner := "OR"
	if c.typ == "is_not_empty" || c.typ == "not_equal" || c.typ == "not_begins_with" {
		combiner = "AND"
	}
	op := c.operator
	switch op {
	case "-like":
		op = "LIKE"
	case "-not_like":
		op = "NOT LIKE"
	}
	var v any
	if len(value) > 1 {
		v = value
	} else if len(value) == 1 {
		v = value[0]
	}
	cc := condCmp{op: op, value: v, combiner: combiner}
	if c.typ == "not_equal" || c.typ == "not_begins_with" || c.typ == "not_contains" {
		cc.orNull = true
	}
	st := r.store()
	if o.parent != nil {
		st.addJoin(o.parent, joinOpts{search: true, linked: isLinked})
	}
	st.addJoin(column, joinOpts{search: true, linked: isLinked, parent: o.parent})
	to := o.searchOpts.joinOpts
	to.parent = o.parent
	to.search = true
	tn := st.tableName(column, to)
	cc.field = tn + "." + c.sField
	return cc
}

func (r *Records) changedAfter(column *Column, date, meTable string) cond {
	// GADS::Helper::Changed: each value of the record versions created
	// after the date, with the value of the latest version before the date
	// (first_value). A record has changed if a value differs from that before
	// the date, or if the values after the date differ from each other (for
	// records only created after the date)
	vf := q(column.ValueField())
	table := q(column.Table())
	if column.Internal {
		return condFalse
	}
	changed := `SELECT "me_other"."id" AS "id", "field".` + vf + ` AS "val", FIRST_VALUE("field_2".` + vf + `) OVER (PARTITION BY "record_earlier"."current_id" ORDER BY "record_earlier"."created" DESC) AS "first_value" FROM "current" "me_other" LEFT JOIN "record" "records" ON "records"."current_id" = "me_other"."id" LEFT JOIN ` + table + ` "field" ON "field"."record_id" = "records"."id" AND "field"."layout_id" = ? LEFT JOIN "record" "record_earlier" ON "record_earlier"."current_id" = "records"."current_id" AND ("record_earlier"."created" < "records"."created" OR ("record_earlier"."created" = "records"."created" AND "record_earlier"."id" < "records"."id")) AND "record_earlier"."approval" = 0 AND "record_earlier"."created" < ? LEFT JOIN ` + table + ` "field_2" ON "field_2"."record_id" = "record_earlier"."id" AND "field_2"."layout_id" = ? WHERE "records"."approval" = 0 AND "me_other"."instance_id" = ? AND "records"."created" > ?`
	args := []any{column.ID, date, column.ID, r.Layout.InstanceID, date}
	sub1 := `SELECT "me_other2"."id" FROM (` + changed + `) "me_other2" WHERE "me_other2"."val" != "me_other2"."first_value"`
	sub2 := `SELECT "me_other2"."id" FROM (` + changed + `) "me_other2" GROUP BY "me_other2"."id" HAVING COUNT(DISTINCT "me_other2"."val") > 1`
	return condOr{
		condCmp{field: meTable + ".id", op: "IN", value: &subquery{sql: sub1, args: args}},
		condCmp{field: meTable + ".id", op: "IN", value: &subquery{sql: sub2, args: args}},
	}
}

func (r *Records) dateForDB(column *Column, value string) string {
	t, _ := column.ParseDate(value)
	if column.HasTime() {
		return db.FormatDateTime(t)
	}
	return db.FormatDate(t)
}

// ---------------------------------------------------------------------
// Sorting
// ---------------------------------------------------------------------

func (r *Records) limitQty() string {
	if (r.From != nil) == (r.To != nil) {
		return ""
	}
	if r.From != nil {
		return "from"
	}
	return "to"
}

func (r *Records) defaultSort() SortSpec {
	if r.DefaultSortOverride != nil {
		return *r.DefaultSortOverride
	}
	s := SortSpec{Type: r.Layout.SortType()}
	if id := r.Layout.SortLayoutID(); id != nil {
		s.ID = *id
	}
	return s
}

func (r *Records) allSorts(col *Column, sorts *[]SortSpec, parentID int64) {
	if col.IsCurcommon() {
		for _, c := range col.CurvalFields() {
			if c != nil {
				r.allSorts(c, sorts, col.ID)
			}
		}
		return
	}
	if strings.Contains(col.ReturnTypeFn(), "date") {
		t := "desc"
		if r.limitQty() == "from" {
			t = "asc"
		}
		*sorts = append(*sorts, SortSpec{ID: col.ID, ParentID: parentID, Type: t})
	}
}

func (r *Records) sortBuilder(limitQty bool) []SortSpec {
	var sorts []SortSpec
	colID := r.Layout.ColumnID()
	if limitQty && r.limitQty() != "" {
		for _, c := range r.ColumnsRetrievedNo() {
			r.allSorts(c, &sorts, 0)
		}
		if len(sorts) == 0 {
			return []SortSpec{}
		}
	}
	if len(sorts) == 0 && r.hasSort {
		for _, s := range r.sortSpec {
			col := r.Layout.Column(s.ID)
			if col == nil {
				continue
			}
			colParent := r.Layout.Column(s.ParentID)
			if s.ParentID != 0 && colParent == nil {
				continue
			}
			if col.InstanceID != r.Layout.InstanceID && colParent == nil {
				Fail("Column %d requested for sort from different table without parent", col.ID)
			}
			t := s.Type
			if t == "" {
				t = "asc"
			}
			var pid int64
			if colParent != nil {
				pid = colParent.ID
			}
			sorts = append(sorts, SortSpec{ID: col.ID, ParentID: pid, Type: t})
		}
	}
	if len(sorts) == 0 && r.View != nil && len(r.View.Sorts()) > 0 {
		for _, s := range r.View.Sorts() {
			id := s.LayoutID
			if id == 0 {
				id = colID.ID
			}
			t := s.Type
			if t == "" {
				t = "asc"
			}
			sorts = append(sorts, SortSpec{ID: id, ParentID: s.ParentID, Type: t})
		}
	}
	if len(sorts) == 0 {
		ds := r.defaultSort()
		if r.Layout.Column(ds.ID) != nil {
			id := ds.ID
			if id == 0 {
				id = colID.ID
			}
			t := ds.Type
			if t == "" {
				t = "asc"
			}
			sorts = append(sorts, SortSpec{ID: id, Type: t})
		}
	}
	if len(sorts) == 0 {
		sorts = append(sorts, SortSpec{ID: colID.ID, Type: "asc"})
	}
	return sorts
}

func (r *Records) getSorts() []SortSpec {
	if !r.sortsBuilt {
		r.sorts = r.sortBuilder(false)
		r.sortsBuilt = true
	}
	return r.sorts
}

type orderByOpts struct {
	joinOpts
	withMin bool
}

type orderTerm struct {
	expr    string // quoted expression
	dir     string // ASC/DESC
	random  bool
	hasTime bool
	field   string
	literal bool // literal SQL (helper_concat), which DBIC does not aggregate
}

func (r *Records) orderBy(o orderByOpts) []orderTerm {
	var sorts []SortSpec
	if o.withMin && r.limitQty() != "" {
		sorts = r.sortBuilder(true)
	} else {
		sorts = r.getSorts()
	}
	var terms []orderTerm
	st := r.store()
	for _, s := range sorts {
		if s.Type == "random" {
			terms = append(terms, orderTerm{random: true})
			continue
		}
		// The direction goes into the SQL, so only these two (the URL, view
		// and table sorts are all checked before they get here)
		var dir string
		switch strings.ToUpper(s.Type) {
		case "ASC":
			dir = "ASC"
		case "DESC":
			dir = "DESC"
		default:
			Panic("Invalid sort type %s", s.Type)
		}
		column := r.Layout.ColumnPerm(s.ID, "read")
		if column == nil {
			continue
		}
		columnParent := r.Layout.Column(s.ParentID)
		var colsMain []*Column
		if column.IsCurcommon() {
			colsMain = column.CurvalFields()
			columnParent = column
		} else {
			colsMain = []*Column{column}
		}
		var colsLink []*Column
		if lp := column.LinkParent(); lp != nil {
			if lp.IsCurcommon() {
				colsLink = lp.CurvalFields()
			} else {
				colsLink = []*Column{lp}
			}
		}
		for _, cs := range colsMain {
			if columnParent != nil {
				st.addJoin(columnParent, joinOpts{sort: true})
				st.addJoin(cs, joinOpts{sort: true, parent: columnParent})
			} else {
				st.addJoin(cs, joinOpts{sort: true})
			}
			to := o.joinOpts
			to.sort = true
			to.parent = columnParent
			sTable := st.tableName(cs, to)
			var expr string
			literal := false
			if column.NameShort == "_created_user" {
				expr = q("record_created_value")
			} else if column.NameShort == "_created" {
				expr = q("record_created_date")
			} else if column.LinkParent() != nil {
				cl := colsLink[0]
				colsLink = colsLink[1:]
				st.addJoin(cl, joinOpts{sort: true})
				lo := o.joinOpts
				lo.sort = true
				lo.linked = true
				lo.hasLinked = true
				main := q(sTable + "." + column.SortField())
				link := q(st.tableName(cl, lo) + "." + cl.SortField())
				expr = r.e.sqlConcat(main, link)
				literal = true
			} else {
				expr = q(sTable + "." + cs.SortField())
			}
			if o.group {
				st.addAggregate(cs, "max", joinOpts{sort: true, parent: columnParent})
				if column.LinkParent() != nil {
					st.addAggregate(cs.LinkParent(), "max", joinOpts{sort: true, linked: true, parent: columnParent})
				}
				terms = append(terms, orderTerm{expr: qa(r.aggregateName(cs, columnParent, "")), dir: dir, hasTime: cs.HasTime(), field: sTable + "." + cs.SortField(), literal: true})
				continue
			}
			terms = append(terms, orderTerm{expr: expr, dir: dir, hasTime: cs.HasTime(), field: sTable + "." + cs.SortField(), literal: literal})
		}
	}
	return terms
}

// ---------------------------------------------------------------------
// Current IDs
// ---------------------------------------------------------------------

type currentIDsOptions struct {
	// rows applies rows/page/max_results (current_ids builder)
	rows bool
}

// versionJoin returns the join of the record version from the current
// table: either directly to the current version, or to the version from
// record_single with record_later (or record_earlier).
func (r *Records) versionJoin(cvo bool, joins []*joinSpec) *joinSpec {
	if cvo {
		return &joinSpec{name: "current_version", kids: joins}
	}
	later := "record_later"
	if r.RecordEarlier {
		later = "record_earlier"
	}
	return joinHash("record_single", append([]*joinSpec{joinLeaf(later)}, joins...)...)
}

// currentIDsSQL is _current_rs: the query of the current IDs of the
// matching records.
func (r *Records) currentIDsSQL(o currentIDsOptions) (string, []any) {
	st := r.store()
	common := joinOpts{search: true, sort: true, currentVersionOnly: r.cvoRecordset()}
	so := searchOpts{joinOpts: common, rewind: r.RewindRecordset()}
	so.linked, so.hasLinked = true, true
	search := r.searchQuery(so)
	jl := common
	jl.linked, jl.hasLinked = false, true
	orderTerms := r.orderBy(orderByOpts{joinOpts: common, withMin: true})
	// joins
	jr := newJoinRenderer(r.e, formatRewind(r.RewindRecordset()))
	jr.layout = r.Layout
	from := `FROM "current" "me"`
	if linked := r.linkedJoins(common); linked != nil {
		jr.render("current", "me", []*joinSpec{linked})
	}
	jr.render("current", "me", []*joinSpec{r.versionJoin(common.currentVersionOnly, st.jpfetch(jl))})
	// If fetching a draft, make sure it's not a draft curval that's part of
	// another draft record
	if r.findDraftuserID != 0 {
		jr.render("current", "me", []*joinSpec{joinLeaf("curvals")})
	}
	var args []any
	args = append(args, jr.args...)
	var wargs []any
	where := renderCond(condAnd(search), &wargs)
	args = append(args, wargs...)
	// Selects for created date/user (used for sorting)
	sel := `"me"."id"`
	extra := ""
	if st.hasCreatedTime(common) {
		extra += ", " + meCreatedDate + ` AS "record_created_date"`
	}
	if st.hasCreatedUser(common) {
		extra += ", " + meCreatedUser + ` AS "record_created_user", ` + meCreatedValue + ` AS "record_created_value"`
	}
	sql := `SELECT ` + sel + extra + ` ` + from + jr.sb.String()
	if where != "" {
		sql += " WHERE " + where
	}
	sql += ` GROUP BY "me"."id"`
	if ob := r.renderOrderBy(orderTerms, true); ob != "" {
		sql += " ORDER BY " + ob
	}
	if o.rows {
		rows := r.Rows
		if rows == 0 && r.MaxResults > 0 {
			rows = r.MaxResults
		}
		if rows > 0 {
			page := r.Page
			if page < 1 {
				page = 1
			}
			sql += " LIMIT ? OFFSET ?"
			args = append(args, rows, (page-1)*rows)
		}
	}
	return sql, args
}

const meCreatedDate = `(SELECT "me_created_date"."created" FROM "record" "me_created_date" WHERE ( "me_created_date"."id" IN ( SELECT MIN( "records_alias"."id" ) FROM "record" "records_alias" WHERE ( "records_alias"."current_id" = "me"."id" ) ) ))`
const meCreatedUser = `(SELECT "me_created"."createdby" FROM "record" "me_created" WHERE ( "me_created"."id" IN ( SELECT MIN( "records_alias"."id" ) FROM "record" "records_alias" WHERE ( "records_alias"."current_id" = "me"."id" ) ) ))`
const meCreatedValue = `(SELECT "createdby"."value" FROM "record" "me_created_value" LEFT JOIN "user" "createdby" ON "createdby"."id" = "me_created_value"."createdby" WHERE ( "me_created_value"."id" IN ( SELECT MIN( "records_alias"."id" ) FROM "record" "records_alias" WHERE ( "records_alias"."current_id" = "me"."id" ) ) ))`

func (r *Records) renderOrderBy(terms []orderTerm, aggregate bool) string {
	if r.limitQty() != "" && aggregate && len(terms) > 0 {
		return r.renderLimitQtyOrder(terms)
	}
	var parts []string
	for _, t := range terms {
		if t.random {
			parts = append(parts, "RANDOM()")
			continue
		}
		expr := t.expr
		// As DBIC's grouping over the selection, which leaves the selected
		// aliases (the created date and user) without an aggregate
		selected := expr == `"record_created_date"` || expr == `"record_created_value"`
		if aggregate && expr != `"me"."id"` && !selected && !t.literal {
			if t.dir == "DESC" {
				expr = "MAX( " + expr + " )"
			} else {
				expr = "MIN( " + expr + " )"
			}
			parts = append(parts, expr+" "+t.dir)
			continue
		}
		parts = append(parts, expr+" "+t.dir)
	}
	return strings.Join(parts, ", ")
}

func (r *Records) renderLimitQtyOrder(terms []orderTerm) string {
	gt, lt := ">=", "<="
	if r.Exclusive == "from" {
		gt = ">"
	}
	if r.Exclusive == "to" {
		lt = "<"
	}
	var exprs []string
	base := r.From
	if base == nil {
		base = r.To
	}
	for _, t := range terms {
		date := db.FormatDate(*base)
		if t.hasTime {
			date = db.FormatDateTime(*base)
		}
		quoted := t.expr
		var e string
		if t.expr == `"record_created_date"` {
			e = meCreatedDate
		} else if strings.Contains(t.field, "from") {
			to := strings.Replace(t.field, "from", "to", 1)
			qto := q(to)
			if r.limitQty() == "from" {
				e = "CASE WHEN (" + quoted + " " + gt + " '" + date + "') THEN " + quoted + " WHEN (" + qto + " " + gt + " '" + date + "') THEN " + qto + " ELSE NULL END"
			} else {
				e = "CASE WHEN (" + qto + " " + lt + " '" + date + "') THEN " + qto + " WHEN (" + quoted + " " + lt + " '" + date + "') THEN " + quoted + " ELSE NULL END"
			}
		} else {
			if r.limitQty() == "from" {
				e = "CASE WHEN (" + quoted + " " + gt + " '" + date + "') THEN " + quoted + " ELSE NULL END"
			} else {
				e = "CASE WHEN (" + quoted + " " + lt + " '" + date + "') THEN " + quoted + " ELSE NULL END"
			}
		}
		fn := "MAX"
		if r.limitQty() == "from" {
			fn = "MIN"
		}
		exprs = append(exprs, fn+"( "+e+" )")
	}
	if r.limitQty() == "from" {
		return r.e.sqlLeast(exprs...) + " ASC"
	}
	return r.e.sqlGreatest(exprs...) + " DESC"
}

func (r *Records) currentIDsSubquery(o currentIDsOptions) *subquery {
	sql, args := r.currentIDsSQL(o)
	// The subquery must only return the id column
	return &subquery{sql: `SELECT "me"."id" FROM (` + sql + `) "me"`, args: args}
}

// CurrentIDs returns all current IDs matching the search, in order.
func (r *Records) CurrentIDs() []int64 {
	if r.currentIDsBuilt {
		return r.currentIDs
	}
	sql, args := r.currentIDsSQL(currentIDsOptions{rows: true})
	r.currentIDs = nil
	for _, row := range r.e.DB.Query(sql, args...) {
		id, _ := db.AsInt64(row["id"])
		r.currentIDs = append(r.currentIDs, id)
	}
	r.currentIDsBuilt = true
	return r.currentIDs
}

// linkedJoins is linked_hash.
func (r *Records) linkedJoins(o joinOpts) *joinSpec {
	st := r.store()
	if !st.hasLinked() {
		return nil
	}
	oo := o
	oo.linked = true
	oo.hasLinked = true
	tables := st.jpfetch(oo)
	// A find of a specific version (emulating Record::_find record_id)
	// keeps the record_single numbering of the main record
	if r.RewindRecordset() != nil || r.findRecordID != 0 {
		alt := ""
		if o.alt {
			alt = "_alternative"
		}
		return joinHash("linked", joinHash("record_single"+alt, append([]*joinSpec{joinLeaf("record_later" + alt)}, tables...)...))
	}
	return joinHash("linked", &joinSpec{name: "current_version", kids: tables})
}

// ---------------------------------------------------------------------
// Count
// ---------------------------------------------------------------------

// Count returns the number of matching records.
func (r *Records) Count() int {
	if r.count != nil {
		return *r.count
	}
	var n int
	if r.hasSearch {
		n = r.searchAllFields().count
	} else {
		st := r.store()
		cvo := r.cvoValues()
		so := searchOpts{joinOpts: joinOpts{search: true, linked: true, hasLinked: true, currentVersionOnly: cvo}, rewind: r.RewindValues()}
		jr := newJoinRenderer(r.e, formatRewind(r.RewindRecordset()))
		jr.layout = r.Layout
		var search []cond
		// Only count the current table itself if there is no filtering and
		// no other need for the records join (approval, rewind etc)
		if r.RewindRecordset() != nil || len(r.queryParams(searchOpts{joinOpts: joinOpts{search: true}})) > 0 || r.approvalQuery(searchOpts{}) != nil {
			search = r.searchQuery(so)
			if l := r.linkedJoins(joinOpts{search: true, linked: true, hasLinked: true, currentVersionOnly: cvo}); l != nil {
				jr.render("current", "me", []*joinSpec{l})
			}
			jr.render("current", "me", []*joinSpec{r.versionJoin(cvo, st.jpfetch(joinOpts{search: true, linked: false, hasLinked: true, currentVersionOnly: cvo}))})
		} else {
			so.noRecordLater = true
			search = r.searchQuery(so)
		}
		var wargs []any
		where := renderCond(condAnd(search), &wargs)
		sql := `SELECT COUNT( * ) FROM (SELECT "me"."id" FROM "current" "me"` + jr.sb.String()
		if where != "" {
			sql += " WHERE " + where
		}
		sql += ` GROUP BY "me"."id") "me"`
		args := append(append([]any{}, jr.args...), wargs...)
		c, _ := r.e.DB.QueryInt64(sql, args...)
		n = int(c)
	}
	r.count = &n
	return n
}

// Pages returns the number of pages.
func (r *Records) Pages() int {
	count := r.Count()
	if lr := r.searchLimitReached(); lr > 0 {
		count = lr
	}
	if r.isGroup() {
		return 1
	}
	if r.Rows > 0 {
		return int(math.Ceil(float64(count) / float64(r.Rows)))
	}
	return 1
}

func (r *Records) searchLimitReached() int {
	if r.hasSearch && r.searchAllFields().limitReached > 0 {
		return r.searchAllFields().limitReached
	}
	if r.MaxResults > 0 && r.MaxResults < r.Count() {
		return r.MaxResults
	}
	return 0
}

// ---------------------------------------------------------------------
// Results
// ---------------------------------------------------------------------

// findVersionConds replaces the latest-version condition with a specific
// record version when finding by record ID (Record::_find record_id).
func (r *Records) findVersionConds(conds []cond, o joinOpts) []cond {
	if r.findRecordID == 0 {
		return conds
	}
	// The main record's version joins follow any linked record joins, so
	// may have a suffix (record_single_2 and record_later_2)
	ro := o
	ro.linked, ro.hasLinked = false, false
	rs := r.store().recordName(ro)
	later := "record_later" + strings.TrimPrefix(rs, "record_single") + ".current_id"
	var out []cond
	for _, c := range conds {
		if cc, ok := c.(condCmp); ok && cc.field == later && cc.value == nil {
			continue
		}
		out = append(out, c)
	}
	return append(out, condEq(rs+".id", r.findRecordID))
}

// findResults returns the results for Record::_find.
func (r *Records) findResults() []*Record { return r.Results() }

// Results returns the records.
func (r *Records) Results() []*Record {
	if r.resultsBuilt {
		return r.results
	}
	if r.isGroup() {
		r.results = r.buildGroupResults(groupOptions{})
	} else {
		r.results = r.buildStandardResults()
	}
	r.resultsBuilt = true
	return r.results
}

// resultsetSearch is _resultset_search: the search of the outer query
// that retrieves the values of the records of the current IDs. The rewind
// of the values (rewind_values) is applied here.
func (r *Records) resultsetSearch(o joinOpts, isGroup bool) []cond {
	st := r.store()
	search := r.findVersionConds(st.recordLaterSearch(o), o)
	if r.findDraftuserID != 0 {
		search = append(search, condEq("me.draftuser_id", r.findDraftuserID), condIsNull("curvals.id"))
	}
	if isGroup {
		search = append(search, condCmp{field: "me.id", op: "IN", value: r.currentIDsSubquery(currentIDsOptions{})})
	} else {
		ids := r.CurrentIDs()
		vals := make([]any, len(ids))
		for i, id := range ids {
			vals[i] = id
		}
		search = append(search, condCmp{field: "me.id", op: "=", value: vals})
	}
	search = append(search, st.recordSingleRewind(o, r.RewindValues())...)
	if a := r.approvalQuery(searchOpts{joinOpts: o}); a != nil {
		search = append(search, a)
	}
	return search
}

type recordRow struct {
	id                int64
	currentID         int64
	deleted           any
	linkedID          *int64
	linkedRecordID    int64
	draftuserID       int64
	created           any
	serial            *int64
	parentID          int64
	deletedby         map[string]any
	recordCreatedDate any
	recordCreatedUser any
	children          []int64
	fields            map[int64][]rawValue
}

func (r *Records) buildStandardResults() []*Record {
	st := r.store()
	// Called first to build the columns and joins (as GADS does)
	r.searchQuery(searchOpts{joinOpts: joinOpts{search: true, sort: true, linked: true, hasLinked: true}, rewind: r.RewindValues()})
	limit := 10
	page := 1
	firstRun := true
	records := map[string]*recordRow{}
	var order []string
	cvo := r.cvoValues()
	for {
		common := joinOpts{prefetch: true, hasPrefetch: true, sort: true, limit: limit, page: page, currentVersionOnly: cvo}
		pre := st.jpfetch(common)
		if len(pre) == 0 && !firstRun {
			break
		}
		jo := common
		jo.linked, jo.hasLinked, jo.retainJoinOrder = false, true, true
		pre = st.jpfetch(jo)
		jr := newJoinRenderer(r.e, formatRewind(r.RewindValues()))
		jr.layout = r.Layout
		lo := common
		lo.retainJoinOrder = true
		if l := r.linkedJoins(lo); l != nil {
			jr.render("current", "me", []*joinSpec{l})
		}
		jr.render("current", "me", []*joinSpec{joinLeaf("deletedby"), joinLeaf("currents")})
		jr.render("current", "me", []*joinSpec{r.versionJoin(cvo, pre)})
		if r.findDraftuserID != 0 {
			jr.render("current", "me", []*joinSpec{joinLeaf("curvals")})
		}
		co := common
		co.retainJoinOrder = true
		cols := st.columnsFetch(co)
		hasLinked := st.hasLinked()
		base := st.recordName(common)
		var sel []string
		for _, c := range cols {
			sel = append(sel, q(c.alias)+" AS "+qa(c.key))
		}
		sel = append(sel, q(base+".id")+` AS "id"`, `"me"."deleted" AS "deleted"`, `"me"."linked_id" AS "linked_id"`)
		if hasLinked {
			if cvo {
				sel = append(sel, `"current_version"."id" AS "linked_record_id"`)
			} else {
				sel = append(sel, `"record_single"."id" AS "linked_record_id"`)
			}
		}
		sel = append(sel, `"me"."draftuser_id" AS "draftuser_id"`, q(base+".current_id")+` AS "current_id"`, q(base+".created")+` AS "created"`, q(base+".createdby")+` AS "createdby"`, `"me"."serial" AS "serial"`, `"me"."parent_id" AS "parent_id"`, `"currents"."id" AS "currents.id"`)
		for _, p := range userColumns {
			sel = append(sel, q("deletedby."+p)+" AS "+qa("deletedby."+p))
		}
		if st.hasCreatedTime(common) {
			sel = append(sel, meCreatedDate+` AS "record_created_date"`)
		}
		if st.hasCreatedUser(common) {
			sel = append(sel, meCreatedUser+` AS "record_created_user"`, meCreatedValue+` AS "record_created_value"`)
		}
		search := r.resultsetSearch(common, false)
		var wargs []any
		where := renderCond(condAnd(search), &wargs)
		oo := common
		oo.retainJoinOrder = true
		orderTerms := r.orderBy(orderByOpts{joinOpts: oo})
		sql := "SELECT " + strings.Join(sel, ", ") + ` FROM "current" "me"` + jr.sb.String()
		if where != "" {
			sql += " WHERE " + where
		}
		if ob := r.renderOrderBy(orderTerms, false); ob != "" {
			sql += " ORDER BY " + ob
		}
		args := append(append([]any{}, jr.args...), wargs...)
		rows := r.e.DB.Query(sql, args...)
		var lastCID int64
		recordCount := map[int64]int{}
		for _, row := range rows {
			cid, _ := db.AsInt64(row["current_id"])
			if row["current_id"] == nil {
				// A current without a version of the join (as one
				// imported, which has no current_version_id)
				r.e.perlWarn("Use of uninitialized value $current_id in hash element", "Records.pm")
				r.e.perlWarn("Use of uninitialized value $current_id in hash element", "Records.pm")
				r.e.perlWarn("Use of uninitialized value $current_id in concatenation (.) or string", "Records.pm")
			}
			if !(lastCID != 0 && lastCID == cid) {
				recordCount[cid]++
			}
			idx := itoa(cid) + "-" + itoa(int64(recordCount[cid]))
			rec := records[idx]
			if rec == nil {
				rec = &recordRow{fields: map[int64][]rawValue{}}
				records[idx] = rec
			}
			r.mergeRow(rec, row, cols)
			if firstRun && (lastCID == 0 || lastCID != cid) {
				order = append(order, idx)
			}
			lastCID = cid
		}
		page++
		firstRun = false
	}
	var all []*Record
	var recordIDs []int64
	for _, idx := range order {
		rec := records[idx]
		record := r.newRecordFromRow(rec)
		all = append(all, record)
		recordIDs = append(recordIDs, rec.id)
	}
	r.fetchMultivalues(recordIDs, all, r.findDraftuserID)
	return all
}

// mergeRow merges a result row into the record row structure.
func (r *Records) mergeRow(rec *recordRow, row db.Row, cols []fetchCol) {
	rec.id, _ = db.AsInt64(row["id"])
	rec.currentID, _ = db.AsInt64(row["current_id"])
	rec.deleted = row["deleted"]
	rec.linkedID = optInt(row["linked_id"])
	if v, ok := row["linked_record_id"]; ok {
		rec.linkedRecordID, _ = db.AsInt64(v)
	}
	rec.draftuserID, _ = db.AsInt64(row["draftuser_id"])
	rec.created = row["created"]
	rec.serial = optInt(row["serial"])
	rec.parentID, _ = db.AsInt64(row["parent_id"])
	if v, ok := row["record_created_date"]; ok {
		rec.recordCreatedDate = v
	}
	if v, ok := row["record_created_user"]; ok {
		rec.recordCreatedUser = v
	}
	if cid, ok := db.AsInt64(row["currents.id"]); ok && row["currents.id"] != nil {
		found := false
		for _, c := range rec.children {
			if c == cid {
				found = true
			}
		}
		if !found {
			rec.children = append(rec.children, cid)
		}
	}
	if row["deletedby.id"] != nil {
		u := map[string]any{}
		for _, p := range userColumns {
			u[p] = row["deletedby."+p]
		}
		rec.deletedby = u
	}
	// Group the fetched columns by field
	byField := map[string]map[string]any{}
	var fieldOrder []string
	for _, c := range cols {
		parts := strings.Split(c.key, ".")
		f := parts[0]
		if _, ok := byField[f]; !ok {
			byField[f] = map[string]any{}
			fieldOrder = append(fieldOrder, f)
		}
		byField[f][strings.Join(parts[1:], ".")] = row[c.key]
	}
	for _, f := range fieldOrder {
		vals := byField[f]
		id, _ := anyToInt64(strings.TrimPrefix(f, "field"))
		col := r.Layout.Column(id)
		if col == nil {
			continue
		}
		existing := rec.fields[id]
		if idv, ok := vals["id"]; ok && idv == nil {
			// An enum value row whose value is NULL: Perl's prefetch gives
			// it with an empty enumval (child_unique is NOT NULL, so is
			// only undefined if there is no value row)
			if col.Type == "enum" && vals["child_unique"] != nil && existing == nil {
				rec.fields[id] = []rawValue{{NullJoin: true, ChildUnique: rowBool(vals["child_unique"])}}
				continue
			}
			// As DBIC prefetch: a relationship with no rows is an empty list
			if existing == nil {
				rec.fields[id] = []rawValue{}
			}
			continue
		}
		rv := r.rawFromFetch(col, vals, "")
		if len(existing) > 0 && rv.ID != 0 {
			dup := false
			for _, e := range existing {
				if e.ID == rv.ID {
					dup = true
				}
			}
			if !dup {
				rec.fields[id] = append(existing, rv)
			}
			continue
		}
		if len(existing) == 0 {
			rec.fields[id] = []rawValue{rv}
		}
	}
}

// rawFromFetch converts fetched columns into a raw value.
func (r *Records) rawFromFetch(col *Column, vals map[string]any, prefix string) rawValue {
	get := func(k string) any { return vals[prefix+k] }
	rv := rawValue{}
	rv.ChildUnique = rowBool(get("child_unique"))
	switch col.Type {
	case "string", "intgr", "date":
		rv.ID, _ = db.AsInt64(get("id"))
		rv.Value = get("value")
	case "daterange":
		rv.ID, _ = db.AsInt64(get("id"))
		rv.From, rv.To = get("from"), get("to")
		rv.Value = get("value")
	case "enum", "tree":
		if get("id") == nil {
			rv.Value = nil
			rv.NullJoin = true
			return rv
		}
		rv.ID, _ = db.AsInt64(get("id"))
		rv.HasText = true
		rv.Text, _ = db.AsString(get("value"))
		rv.Deleted = rowBool(get("deleted"))
	case "file":
		if get("id") == nil {
			return rv
		}
		rv.ID, _ = db.AsInt64(get("id"))
		rv.HasText = true
		rv.Text, _ = db.AsString(get("name"))
		rv.Mimetype, _ = db.AsString(get("mimetype"))
	case "person", "createdby", "deletedby":
		if get("id") == nil {
			return rv
		}
		u := map[string]any{}
		for _, p := range userColumns {
			u[p] = get(p)
		}
		rv.User = u
		rv.ID, _ = db.AsInt64(get("id"))
	case "calc":
		rv.ID, _ = db.AsInt64(get("id"))
		vf := col.ValueField()
		rv.Value = get(vf)
		switch vf {
		case "value_text":
			rv.ValueText = get(vf)
		case "value_int":
			rv.ValueInt = get(vf)
		case "value_date":
			rv.ValueDate = get(vf)
		case "value_numeric":
			rv.ValueNumeric = get(vf)
		}
	case "rag":
		rv.ID, _ = db.AsInt64(get("id"))
		rv.Value = get("value")
	case "curval", "filval":
		rv.ID, _ = db.AsInt64(get("id"))
		rv.Value = get("value")
		if rid, ok := vals[prefix+"record_id"]; ok && rid != nil {
			rec := &recordRow{fields: map[int64][]rawValue{}}
			rec.id, _ = db.AsInt64(rid)
			rec.currentID, _ = db.AsInt64(get("value"))
			// child fields: keys like field8.value
			sub := map[string]map[string]any{}
			for k, v := range vals {
				if !strings.HasPrefix(k, prefix+"field") {
					continue
				}
				rest := strings.TrimPrefix(k, prefix)
				i := strings.IndexByte(rest, '.')
				if i < 0 {
					continue
				}
				f := rest[:i]
				if sub[f] == nil {
					sub[f] = map[string]any{}
				}
				sub[f][rest[i+1:]] = v
			}
			lp := col.LayoutParent()
			for f, sv := range sub {
				cid, _ := anyToInt64(strings.TrimPrefix(f, "field"))
				cc := lp.Column(cid)
				if cc == nil {
					continue
				}
				rec.fields[cid] = []rawValue{r.rawFromFetch(cc, sv, "")}
			}
			rv.Record = r.subRecord(col, rec)
		}
	default:
		rv.ID, _ = db.AsInt64(get("id"))
		rv.Value = get("value")
	}
	return rv
}

func (r *Records) subRecord(col *Column, rec *recordRow) *Record {
	cols := col.CurvalFieldsRetrieve(curvalRetrieveOptions{})
	sr := r.e.newRecordObject(col.LayoutParent(), nil)
	sr.currentID = rec.currentID
	sr.recordID = rec.id
	sr.raw = rec.fields
	sr.hasRaw = true
	sr.columnsRetrievedDo = cols
	sr.columnsRetrievedNo = cols
	return sr
}

func (r *Records) newRecordFromRow(rec *recordRow) *Record {
	record := r.e.newRecordObject(r.Layout, r.User)
	record.currentID = rec.currentID
	record.recordID = rec.id
	record.raw = rec.fields
	record.hasRaw = true
	record.valuesRewind = r.RewindValues()
	record.serial = rec.serial
	record.serialLoaded = true
	record.childRecords = rec.children
	record.parentID = rec.parentID
	record.parentLoaded = true
	if rec.linkedID != nil {
		record.linkedID = *rec.linkedID
	}
	record.linkedLoaded = true
	record.linkedRecordID = rec.linkedRecordID
	record.isDraft = rec.draftuserID
	record.isDraftLoaded = true
	record.columnsRetrievedNo = r.ColumnsRetrievedNo()
	record.columnsRetrievedDo = r.ColumnsRetrievedDo()
	record.columnsSelected = r.ColumnsSelected()
	record.columnsRender = r.ColumnsRender()
	record.setDeleted = rec.deleted
	record.setDeletedby = rec.deletedby
	record.setRecordCreated = rec.recordCreatedDate
	record.createdRaw = rec.created
	record.hasCreatedRaw = true
	record.createdbyFetched = true
	return record
}

// fetchMultivalues retrieves multivalue fields for the records.
func (r *Records) fetchMultivalues(recordIDs []int64, records []*Record, isDraft int64) {
	var linkedIDs []int64
	for _, rec := range records {
		if rec.linkedRecordID != 0 {
			linkedIDs = append(linkedIDs, rec.linkedRecordID)
		}
	}
	multi := map[string]map[int64]map[int64][]rawValue{} // parent field -> record id -> col id -> values
	colsDone := map[string]map[int64]bool{}
	curvalFields := map[string]map[string][]string{}
	for _, column := range r.ColumnsRetrievedNo() {
		if r.IsDraft != 0 && column.Type == "autocur" {
			continue
		}
		parentField := "main"
		cols := []*Column{column}
		if column.Type == "curval" {
			parentField = column.Field()
			mv := column.CurvalFieldsMultivalue(curvalRetrieveOptions{})
			cols = append(cols, mv...)
			for _, c := range mv {
				if curvalFields[column.Field()] == nil {
					curvalFields[column.Field()] = map[string][]string{}
				}
				curvalFields[column.Field()][c.Field()] = append(curvalFields[column.Field()][c.Field()], column.Field())
			}
		}
		for _, col := range cols {
			pft := "main"
			if parentField != "" && col.Field() != parentField {
				pft = parentField
			}
			isLinked := false
			for loop := 0; loop <= 1; loop++ {
				if loop == 1 && !isLinked {
					continue
				}
				if colsDone[pft] == nil {
					colsDone[pft] = map[int64]bool{}
				}
				if !col.FetchWithRecord() && !colsDone[pft][col.ID] {
					if r.SeparateRecordsForMulticol != 0 && r.SeparateRecordsForMulticol == col.ID {
						goto next
					}
					{
						retrieveIDs := recordIDs
						if isLinked {
							retrieveIDs = linkedIDs
						}
						if parents := curvalFields[column.Field()][col.Field()]; len(parents) > 0 {
							retrieveIDs = nil
							for _, pf := range parents {
								pcid, _ := anyToInt64(strings.TrimPrefix(pf, "field"))
								for _, rec := range records {
									for i, v := range rec.raw[pcid] {
										if v.Value == nil {
											continue
										}
										vid, _ := anyToInt64(v.Value)
										rid := r.latestRecordID(vid)
										if rid != 0 {
											retrieveIDs = append(retrieveIDs, rid)
											if v.Record != nil {
												v.Record.recordID = rid
											}
											rec.raw[pcid][i].subRecordID = rid
										}
									}
								}
							}
						}
						colsd := map[int64]bool{}
						fo := fetchMultiOptions{isDraft: isDraft, rewind: r.RewindValues(), alreadySeen: r.AlreadySeen()}
						if !col.IsCurcommon() || r.chronology {
							zero := int64(0)
							fo.limitRows = &zero
						}
						for _, val := range col.FetchMultivalues(retrieveIDs, fo) {
							if colsDone[pft][val.layoutID] {
								continue
							}
							if multi[pft] == nil {
								multi[pft] = map[int64]map[int64][]rawValue{}
							}
							if multi[pft][val.recordID] == nil {
								multi[pft][val.recordID] = map[int64][]rawValue{}
							}
							multi[pft][val.recordID][val.layoutID] = append(multi[pft][val.recordID][val.layoutID], val.value)
							colsd[val.layoutID] = true
						}
						for id := range colsd {
							colsDone[pft][id] = true
						}
					}
				}
			next:
				if lp := col.LinkParent(); lp != nil {
					col = lp
					isLinked = true
				} else {
					isLinked = false
				}
			}
		}
	}
	for _, rec := range records {
		rid := rec.recordID
		if rid == 0 {
			r.e.perlWarn("Use of uninitialized value $record_id in hash element", "Records.pm")
		}
		for cid, vals := range multi["main"][rid] {
			rec.raw[cid] = vals
		}
		for cv, subs := range curvalFields {
			for sub := range subs {
				for _, pf := range subs[sub] {
					pcid, _ := anyToInt64(strings.TrimPrefix(pf, "field"))
					scid, _ := anyToInt64(strings.TrimPrefix(sub, "field"))
					for _, v := range rec.raw[pcid] {
						if v.subRecordID == 0 || v.Record == nil {
							continue
						}
						if v.Record.raw == nil {
							v.Record.raw = map[int64][]rawValue{}
						}
						v.Record.raw[scid] = multi[cv][v.subRecordID][scid]
					}
				}
			}
		}
		if rec.linkedRecordID != 0 {
			for cid, vals := range multi["main"][rec.linkedRecordID] {
				rec.raw[cid] = vals
			}
		}
	}
}

// latestRecordID returns the record version of a current ID from which
// values are taken.
func (r *Records) latestRecordID(currentID int64) int64 {
	if r.cvoValues() {
		id, _ := r.e.DB.QueryInt64(`SELECT "me"."current_version_id" FROM "current" "me" WHERE "me"."id" = ?`, currentID)
		return id
	}
	rw := r.RewindValues()
	args := []any{0}
	later := ""
	if rw != nil {
		later = ` AND "record_later"."created" <= ?`
		args = append(args, formatRewind(rw))
	}
	args = append(args, currentID)
	extra := ""
	if rw != nil {
		extra = ` AND "record_single"."created" <= ?`
		args = append(args, formatRewind(rw))
	}
	sql := `SELECT "record_single"."id" FROM "current" "me" LEFT JOIN "record" "record_single" ON "record_single"."current_id" = "me"."id" LEFT JOIN "record" "record_later" ON ( ( "record_later"."created" > record_single.created OR ( "record_later"."created" = record_single.created AND "record_later"."id" > record_single.id ) ) AND "record_later"."approval" = ? AND "record_later"."current_id" = "record_single"."current_id"` + later + ` ) WHERE "me"."id" = ? AND "record_later"."id" IS NULL` + extra
	id, _ := r.e.DB.QueryInt64(sql, args...)
	return id
}

// Single iterates over the records in chunks of 100.
func (r *Records) Single() *Record {
	const chunk = 100
	next := r.nextSingleID
	if !r.isGroup() {
		if (next == 0 && r.singlePage == 0) || (next >= chunk && next >= len(r.Results())) {
			if next != 0 {
				r.singlePage++
			}
			if !r.allCIDsBuilt {
				r.allCIDsStore = r.CurrentIDs()
				r.allCIDsBuilt = true
			}
			start := chunk * r.singlePage
			end := start + chunk - 1
			if len(r.allCIDsStore)-1 < end {
				end = len(r.allCIDsStore) - 1
			}
			var fetch []int64
			if start <= end {
				fetch = append(fetch, r.allCIDsStore[start:end+1]...)
			}
			// Set those IDs for the next chunk retrieved from the DB
			r.currentIDs = fetch
			r.currentIDsBuilt = true
			r.resultsBuilt = false
			r.results = nil
			r.alreadySeen = nil
			next = 0
		}
	}
	res := r.Results()
	r.nextSingleID = next + 1
	if next < len(res) {
		return res[next]
	}
	return nil
}

// RecordsRetrievedCount returns the number of records iterated so far.
func (r *Records) RecordsRetrievedCount() int {
	return r.singlePage*100 + r.nextSingleID
}

// ClearRecords clears retrieved results.
func (r *Records) ClearRecords() {
	r.count = nil
	r.currentIDsBuilt = false
	r.currentIDs = nil
	r.resultsBuilt = false
	r.results = nil
	r.alreadySeen = nil
	r.nextSingleID = 0
	r.singlePage = 0
	r.allCIDsBuilt = false
	r.allCIDsStore = nil
}

// Clear clears everything.
func (r *Records) Clear() {
	r.viewLimitsBuilt = false
	r.viewLimits = nil
	r.clearColumns()
	r.searchAll = nil
	r.sortsBuilt = false
	r.sorts = nil
	r.js = nil
	r.ClearRecords()
}

// HasChildren reports whether any matching records are children.
func (r *Records) HasChildren() bool {
	st := r.store()
	cvo := r.cvoValues()
	// is_group so that the WHERE clause is a query instead of the full
	// list of current IDs
	o := joinOpts{search: true, hasLinked: true, linked: st.hasLinked(), currentVersionOnly: cvo}
	search := r.resultsetSearch(o, true)
	search = append(search, condNotNull("me.parent_id"))
	jr := newJoinRenderer(r.e, formatRewind(r.RewindValues()))
	jr.layout = r.Layout
	if l := r.linkedJoins(joinOpts{search: true}); l != nil {
		jr.render("current", "me", []*joinSpec{l})
	}
	jr.render("current", "me", []*joinSpec{r.versionJoin(cvo, st.jpfetch(joinOpts{search: true, currentVersionOnly: cvo}))})
	var wargs []any
	where := renderCond(condAnd(search), &wargs)
	sql := `SELECT "me"."id" FROM "current" "me"` + jr.sb.String() + " WHERE " + where + " LIMIT 1"
	args := append(append([]any{}, jr.args...), wargs...)
	_, ok := r.e.DB.QueryInt64(sql, args...)
	return ok
}

// ---------------------------------------------------------------------
// Quick search across all fields
// ---------------------------------------------------------------------

type searchAllResult struct {
	cids         []int64
	count        int
	limitReached int
}

func (r *Records) searchAllFields() *searchAllResult {
	if r.searchAll != nil {
		return r.searchAll
	}
	res := &searchAllResult{}
	r.searchAll = res
	search := r.Search
	if search == "" {
		return res
	}
	type sfield struct {
		typ, plural, indexField, valueField string
		sub                                 bool
	}
	fields := []sfield{
		{typ: "string", plural: "strings", indexField: "strings.value_index"},
		{typ: "int", plural: "intgrs"},
		{typ: "date", plural: "dates"},
		{typ: "string", plural: "dateranges"},
		{typ: "string", plural: "ragvals"},
		{typ: "string", plural: "calcvals", valueField: "value_text"},
		{typ: "number", plural: "calcvals", valueField: "value_numeric"},
		{typ: "int", plural: "calcvals", valueField: "value_int"},
		{typ: "date", plural: "calcvals", valueField: "value_date"},
		{typ: "string", plural: "enums", sub: true},
		{typ: "string", plural: "people", sub: true},
		{typ: "file", plural: "files", sub: true, valueField: "name"},
		{typ: "current_id"},
	}
	var canView []any
	for _, c := range r.Layout.All(AllOptions{UserCanRead: true}) {
		// In override mode (showing all records) do not allow a search on
		// fields not contained in the override view
		if r.overrideHidesColumn(c.ID) {
			continue
		}
		canView = append(canView, c.ID)
		if c.Type == "curval" {
			for _, id := range c.CurvalFieldIDs() {
				canView = append(canView, id)
			}
		}
	}
	basic := r.commonSearch("me")
	// Called twice as per GADS: first to add joins, then to name them correctly
	r.viewLimitsSearch(searchOpts{joinOpts: joinOpts{search: true}})
	basic = append(basic, r.viewLimitsSearch(searchOpts{joinOpts: joinOpts{search: true}})...)
	dateCol := &Column{e: r.e, Type: "date", layout: r.Layout, Options: map[string]any{}}
	found := map[int64]bool{}
	var foundOrder []int64
	st := r.store()
	for _, f := range fields {
		local := any(search)
		index := any(perl.Lc(perl.Substr(search, 128)))
		likeOp := "="
		if f.typ != "number" && f.typ != "int" && f.typ != "current_id" && f.typ != "date" {
			local = "%" + escapeLike(search) + "%"
			index = "%" + escapeLike(perl.Lc(perl.Substr(search, 128))) + "%"
			likeOp = "LIKE"
		}
		if f.typ == "number" && !perl.LooksLikeNumber(search) {
			continue
		}
		if (f.typ == "int" || f.typ == "current_id") && !reSignedInt.MatchString(search) {
			continue
		}
		if f.typ == "date" {
			if !dateCol.Validate(search, false) {
				continue
			}
			local = r.dateForDB(dateCol, search)
		}
		vf := f.valueField
		if vf == "" {
			vf = "value"
		}
		jr := newJoinRenderer(r.e, formatRewind(r.Rewind))
		jr.layout = r.Layout
		var s []cond
		s = append(s, basic...)
		jrs := st.jpfetch(joinOpts{search: true})
		cvo := r.cvoRecordset()
		var field string
		if f.typ == "current_id" {
			jr.render("current", "me", []*joinSpec{r.versionJoin(cvo, jrs)})
			s = append(s, condEq("me.id", local))
			s = append(s, condEq("me.instance_id", r.Layout.InstanceID))
		} else {
			kids := append([]*joinSpec{}, jrs...)
			var plural *joinSpec
			if f.sub {
				plural = joinHash(f.plural, joinLeaf("value"), joinLeaf("layout"))
			} else {
				plural = joinHash(f.plural, joinLeaf("layout"))
			}
			kids = append(kids, plural)
			jr.render("current", "me", []*joinSpec{r.versionJoin(cvo, kids)})
			if f.sub {
				field = st.valueNextJoin(joinOpts{search: true}) + "." + vf
			} else {
				field = f.plural + "." + vf
			}
			if f.indexField != "" {
				s = append(s, condCmp{field: f.indexField, op: likeOp, value: index})
			}
			s = append(s, condCmp{field: field, op: likeOp, value: local})
			s = append(s, condCmp{field: "layout.id", op: "=", value: canView})
			s = append(s, st.recordLaterSearch(joinOpts{search: true, currentVersionOnly: cvo})...)
		}
		var wargs []any
		where := renderCond(condAnd(s), &wargs)
		sql := `SELECT "me"."id", "me"."instance_id" FROM "current" "me"` + jr.sb.String() + " WHERE " + where + " LIMIT 1001"
		args := append(append([]any{}, jr.args...), wargs...)
		for _, row := range r.e.DB.Query(sql, args...) {
			cid, _ := db.AsInt64(row["id"])
			iid, _ := db.AsInt64(row["instance_id"])
			if iid != r.Layout.InstanceID {
				// Value in another table referred to by a curval
				jr2 := newJoinRenderer(r.e, formatRewind(r.Rewind))
				jr2.layout = r.Layout
				jr2.render("current", "me", []*joinSpec{joinHash("record_single", append([]*joinSpec{joinLeaf("record_later"), joinLeaf("curvals")}, st.jpfetch(joinOpts{search: true})...)...)})
				s2 := append([]cond{}, basic...)
				s2 = append(s2, condEq("curvals.value", cid))
				var w2 []any
				where2 := renderCond(condAnd(s2), &w2)
				sql2 := `SELECT "me"."id" FROM "current" "me"` + jr2.sb.String() + " WHERE " + where2
				for _, id := range r.e.DB.QueryInt64s(sql2, append(append([]any{}, jr2.args...), w2...)...) {
					if !found[id] {
						found[id] = true
						foundOrder = append(foundOrder, id)
					}
				}
			} else if !found[cid] {
				found[cid] = true
				foundOrder = append(foundOrder, cid)
			}
		}
		if len(found) > 1000 {
			break
		}
	}
	sort.Slice(foundOrder, func(i, j int) bool { return foundOrder[i] < foundOrder[j] })
	res.count = len(foundOrder)
	res.cids = foundOrder
	if res.count > 1000 {
		res.cids = foundOrder[:1000]
		res.limitReached = 1000
	}
	return res
}

var reSignedInt = regexp.MustCompile(`^-?\d+$`)

// IsGroup is Records->is_group
func (r *Records) IsGroup() bool { return r.isGroup() }

// GroupColIDs is Records->group_col_ids
func (r *Records) GroupColIDs() []int64 { return r.groupColIDs() }

// SearchLimitReached is Records->search_limit_reached
func (r *Records) SearchLimitReached() int { return r.searchLimitReached() }

// SortFirst is Records->sort_first: the first of the sorts used (nil if
// there are none)
func (r *Records) SortFirst() *SortSpec {
	s := r.getSorts()
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}
