package gads

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/lua"
)

// multiValue is a value returned by fetch_multivalues.
type multiValue struct {
	layoutID int64
	recordID int64
	value    rawValue
	order    int
}

type fetchMultiOptions struct {
	isDraft     int64 // draft user ID
	rewind      *time.Time
	alreadySeen *seenNode
	limitRows   *int64 // overrides the column's limit_rows
}

// FetchMultivalues retrieves multivalue values for record IDs
// (GADS::Column::fetch_multivalues and overrides).
func (c *Column) FetchMultivalues(recordIDs []int64, o fetchMultiOptions) []multiValue {
	switch c.Type {
	case "autocur":
		return c.fetchAutocur(recordIDs, o)
	case "curval", "filval":
		return c.fetchCurvalMulti(recordIDs, o)
	}
	if len(recordIDs) == 0 {
		return nil
	}
	args := []any{}
	var idConds []string
	for _, id := range recordIDs {
		idConds = append(idConds, `"me"."record_id" = ?`)
		args = append(args, id)
	}
	args = append(args, c.ID)
	var sql string
	switch c.Type {
	case "enum", "tree":
		sql = `SELECT "me"."layout_id", "me"."record_id", "value"."id" AS "vid", "value"."value" AS "vvalue", "value"."deleted" AS "vdeleted", "me"."id", "me"."child_unique" FROM "enum" "me" LEFT JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" LEFT JOIN "enumval" "value" ON "value"."id" = "me"."value" WHERE ( ( ` + strings.Join(idConds, " OR ") + ` ) AND "layout"."multivalue" = 1 AND "me"."layout_id" = ? ) ORDER BY "value"."value"`
	case "person":
		var sel []string
		for _, p := range userColumns {
			sel = append(sel, `"value".`+q(p)+` AS `+q("u_"+p))
		}
		sql = `SELECT "me"."layout_id", "me"."record_id", ` + strings.Join(sel, ", ") + `, "me"."id", "me"."child_unique" FROM "person" "me" LEFT JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" LEFT JOIN "user" "value" ON "value"."id" = "me"."value" WHERE ( ( ` + strings.Join(idConds, " OR ") + ` ) AND "layout"."multivalue" = 1 AND "me"."layout_id" = ? ) ORDER BY "value"."value"`
	case "file":
		sql = `SELECT "me"."layout_id", "me"."record_id", "value"."name" AS "vname", "value"."mimetype" AS "vmimetype", "value"."id" AS "vid", "me"."id", "me"."child_unique" FROM "file" "me" LEFT JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" LEFT JOIN "fileval" "value" ON "value"."id" = "me"."value" WHERE ( ( ` + strings.Join(idConds, " OR ") + ` ) AND "layout"."multivalue" = 1 AND "me"."layout_id" = ? ) ORDER BY "value"."name"`
	case "createdby":
		// Fetch users for IDs
		return nil
	default:
		sql = `SELECT "me".*, "me"."layout_id", "me"."record_id" FROM ` + q(c.Table()) + ` "me" LEFT JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE ( ( ` + strings.Join(idConds, " OR ") + ` ) AND "layout"."multivalue" = 1 AND "me"."layout_id" = ? ) ORDER BY "me".` + q(c.ValueField())
	}
	var out []multiValue
	for _, row := range c.e.DB.Query(sql, args...) {
		mv := multiValue{}
		mv.layoutID, _ = db.AsInt64(row["layout_id"])
		mv.recordID, _ = db.AsInt64(row["record_id"])
		rv := rawValue{ChildUnique: rowBool(row["child_unique"])}
		rv.ID, _ = db.AsInt64(row["id"])
		switch c.Type {
		case "enum", "tree":
			if row["vid"] == nil {
				rv.Value = nil
			} else {
				rv.ID, _ = db.AsInt64(row["vid"])
				rv.HasText = true
				rv.Text, _ = db.AsString(row["vvalue"])
				rv.Deleted = rowBool(row["vdeleted"])
			}
		case "person":
			if row["u_id"] != nil {
				u := map[string]any{}
				for _, p := range userColumns {
					u[p] = row["u_"+p]
				}
				rv.User = u
			}
		case "file":
			if row["vid"] != nil {
				rv.ID, _ = db.AsInt64(row["vid"])
				rv.HasText = true
				rv.Text, _ = db.AsString(row["vname"])
				rv.Mimetype, _ = db.AsString(row["vmimetype"])
			}
		default:
			rv = rawFromTableRow(c, row)
		}
		mv.value = rv
		out = append(out, mv)
	}
	return out
}

func (c *Column) fetchCurvalMulti(recordIDs []int64, o fetchMultiOptions) []multiValue {
	e := c.e
	oldIP := e.IgnorePermissions
	oldIPS := e.IgnorePermissionsSearch
	if c.OverridePermissions() {
		e.IgnorePermissions = true
	}
	e.IgnorePermissionsSearch = true
	defer func() { e.IgnorePermissions = oldIP; e.IgnorePermissionsSearch = oldIPS }()
	if len(recordIDs) == 0 {
		return nil
	}
	var conds []string
	var args []any
	for _, id := range recordIDs {
		conds = append(conds, `"me"."record_id" = ?`)
		args = append(args, id)
	}
	args = append(args, c.ID)
	rows := e.DB.Query(`SELECT "me".* FROM "curval" "me" WHERE ( ( `+strings.Join(conds, " OR ")+` ) AND "me"."layout_id" = ? ) ORDER BY "me"."record_id"`, args...)
	tree := o.alreadySeen
	if tree == nil {
		tree = newSeenRoot()
	}
	child := tree.addChild(c.ID)
	lp := c.LayoutParent()
	if lp == nil {
		return nil
	}
	recs := e.NewRecords(lp, c.layout.User())
	recs.Rewind = o.rewind
	recs.alreadySeen = child
	recs.IncludeDeleted = true
	// As Perl, is_draft is the draft user ID of the record being found
	recs.IsDraft = o.isDraft
	recs.SetColumns(c.CurvalFieldIDsRetrieve(curvalRetrieveOptions{}))
	limitRows := c.LimitRows()
	if o.limitRows != nil {
		limitRows = *o.limitRows
	}
	recs.MaxResults = int(limitRows)
	recs.IgnoreViewLimitExtra = true
	var out []multiValue
	if limitRows > 0 {
		grouped := map[int64][]int64{}
		var order []int64
		for _, r := range rows {
			rid, _ := db.AsInt64(r["record_id"])
			if _, ok := grouped[rid]; !ok {
				order = append(order, rid)
				grouped[rid] = nil
			}
			if truthy(r["value"]) {
				v, _ := db.AsInt64(r["value"])
				grouped[rid] = append(grouped[rid], v)
			}
		}
		for _, rid := range order {
			if len(grouped[rid]) == 0 {
				continue
			}
			recs.SetLimitCurrentIDs(grouped[rid])
			for {
				rec := recs.Single()
				if rec == nil {
					break
				}
				out = append(out, multiValue{layoutID: c.ID, recordID: rid, value: rawValue{Record: rec, Value: rec.currentID}})
			}
			recs.ClearRecords()
		}
		return out
	}
	var ids []int64
	for _, r := range rows {
		if v, ok := db.AsInt64(r["value"]); ok && r["value"] != nil {
			ids = append(ids, v)
		}
	}
	recs.SetLimitCurrentIDs(ids)
	type ro struct {
		rec   *Record
		order int
	}
	retrieved := map[int64]ro{}
	n := 0
	for {
		rec := recs.Single()
		if rec == nil {
			break
		}
		n++
		retrieved[rec.currentID] = ro{rec, n}
	}
	var single []multiValue
	var last int64
	flush := func() {
		sort.SliceStable(single, func(i, j int) bool { return single[i].order < single[j].order })
		out = append(out, single...)
		single = nil
	}
	for _, r := range rows {
		rid, _ := db.AsInt64(r["record_id"])
		if last != 0 && last != rid {
			flush()
		}
		mv := multiValue{layoutID: c.ID, recordID: rid}
		if truthy(r["value"]) {
			v, _ := db.AsInt64(r["value"])
			if x, ok := retrieved[v]; ok {
				mv.value = rawValue{Record: x.rec, Value: v}
				mv.order = x.order
			} else {
				mv.value = rawValue{Value: nil}
			}
		} else {
			mv.value = rawValue{Value: nil}
		}
		single = append(single, mv)
		last = rid
	}
	flush()
	return out
}

func (c *Column) fetchAutocur(recordIDs []int64, o fetchMultiOptions) []multiValue {
	e := c.e
	if len(recordIDs) == 0 {
		return nil
	}
	oldIP := e.IgnorePermissions
	oldIPS := e.IgnorePermissionsSearch
	if c.OverridePermissions() {
		e.IgnorePermissions = true
	}
	e.IgnorePermissionsSearch = true
	defer func() { e.IgnorePermissions = oldIP; e.IgnorePermissionsSearch = oldIPS }()
	related := c.RelatedField()
	if related == nil {
		return nil
	}
	var conds []string
	var args []any
	args = append(args, related.ID)
	for _, id := range recordIDs {
		conds = append(conds, `"records"."id" = ?`)
		args = append(args, id)
	}
	possible := e.DB.QueryInt64s(`SELECT "record"."current_id" FROM "curval" "me" LEFT JOIN "record" "record" ON "record"."id" = "me"."record_id" LEFT JOIN "current" "value" ON "value"."id" = "me"."value" LEFT JOIN "record" "records" ON "records"."current_id" = "value"."id" WHERE ( ( "me"."layout_id" = ? AND ( `+strings.Join(conds, " OR ")+` ) ) ) GROUP BY "record"."current_id"`, args...)
	var pconds []string
	var sargs []any
	sargs = append(sargs, 0)
	for _, id := range possible {
		pconds = append(pconds, `"me"."id" = ?`)
		sargs = append(sargs, id)
	}
	pc := "0=1"
	if len(pconds) > 0 {
		pc = "( " + strings.Join(pconds, " OR ") + " )"
	}
	sub := `SELECT "record_single"."id" FROM "current" "me" LEFT JOIN "record" "record_single" ON "record_single"."current_id" = "me"."id" LEFT JOIN "record" "record_later" ON ( ( "record_later"."created" > record_single.created OR ( "record_later"."created" = record_single.created AND "record_later"."id" > record_single.id ) ) AND "record_later"."approval" = ? AND "record_later"."current_id" = "record_single"."current_id" ) WHERE ( ( ` + pc + ` AND "record_later"."id" IS NULL ) )`
	args2 := []any{related.ID}
	args2 = append(args2, sargs...)
	var rconds []string
	for _, id := range recordIDs {
		rconds = append(rconds, `"records"."id" = ?`)
		args2 = append(args2, id)
	}
	rows := e.DB.Query(`SELECT "record"."current_id" AS "my_current_id", "records"."id" AS "records_id" FROM "curval" "me" LEFT JOIN "record" "record" ON "record"."id" = "me"."record_id" LEFT JOIN "current" "value" ON "value"."id" = "me"."value" LEFT JOIN "record" "records" ON "records"."current_id" = "value"."id" WHERE ( ( "me"."layout_id" = ? AND "me"."record_id" IN ( `+sub+` ) AND ( `+strings.Join(rconds, " OR ")+` ) ) ) GROUP BY "record"."current_id", "records"."id"`, args2...)
	tree := o.alreadySeen
	if tree == nil {
		tree = newSeenRoot()
	}
	child := tree.addChild(c.ID)
	uniq := map[int64]bool{}
	var all []int64
	for _, r := range rows {
		cid, _ := db.AsInt64(r["my_current_id"])
		if !uniq[cid] {
			uniq[cid] = true
			all = append(all, cid)
		}
	}
	if len(all) == 0 {
		return nil
	}
	type ro struct {
		rec   *Record
		order int
	}
	retrieved := map[int64]ro{}
	if len(all) == 1 && c.layout.CachedRecordsAutocur()[all[0]] != nil {
		rec := c.layout.CachedRecordsAutocur()[all[0]]
		retrieved[rec.currentID] = ro{rec, 1}
	} else {
		recs := e.NewRecords(c.LayoutParent(), c.layout.User())
		recs.alreadySeen = child
		recs.SetColumns(c.CurvalFieldIDsRetrieve(curvalRetrieveOptions{}))
		recs.SetLimitCurrentIDs(all)
		recs.SetIncludeChildren(true)
		recs.IgnoreViewLimitExtra = true
		n := 0
		for {
			rec := recs.Single()
			if rec == nil {
				break
			}
			n++
			retrieved[rec.currentID] = ro{rec, n}
			c.layout.CachedRecordsAutocur()[rec.currentID] = rec
		}
	}
	var v []multiValue
	done := map[int64]map[int64]bool{}
	for _, r := range rows {
		cid, _ := db.AsInt64(r["my_current_id"])
		x, ok := retrieved[cid]
		if !ok {
			continue
		}
		rid, _ := db.AsInt64(r["records_id"])
		if done[cid] == nil {
			done[cid] = map[int64]bool{}
		}
		if !done[cid][rid] {
			v = append(v, multiValue{layoutID: c.ID, recordID: rid, value: rawValue{Record: x.rec, Value: cid}, order: x.order})
		}
		done[cid][rid] = true
	}
	sort.SliceStable(v, func(i, j int) bool { return v[i].order < v[j].order })
	return v
}

// ---------------------------------------------------------------------
// Curval formatting and value retrieval
// ---------------------------------------------------------------------

// curvalRow is the result of Curcommon::_format_row.
type curvalRow struct {
	ID         int64
	ValueID    string
	SelectorID string
	Record     *Record
	Value      string
	Values     []string
	HTML       string
	AsQuery    string
}

// FormatValue joins values (Curcommon::format_value).
func FormatValue(values []string) string {
	return strings.Join(values, ", ")
}

func (c *Column) formatRow(row *Record) curvalRow {
	lp := c.LayoutParent()
	var values, htmls []string
	for _, fid := range c.CurvalFieldIDs() {
		if !c.OverridePermissions() && lp.ColumnPerm(fid, "read") == nil {
			continue
		}
		col := lp.Column(fid)
		d := row.GetFieldValue(col)
		htmls = append(htmls, d.HTML())
		values = append(values, d.AsString())
	}
	var nonEmpty []string
	for _, h := range htmls {
		if h != "" && h != "0" {
			nonEmpty = append(nonEmpty, h)
		}
	}
	html := strings.Join(nonEmpty, ", ")
	if html == "" {
		html = "&lt;blank&gt;"
	}
	asQuery := ""
	if row.IsDraft() != 0 || row.currentID == 0 {
		asQuery = row.AsQuery(false)
	}
	valueID := asQuery
	if valueID == "" {
		valueID = itoa(row.currentID)
	}
	return curvalRow{
		ID:         row.currentID,
		ValueID:    valueID,
		SelectorID: row.SelectorID(),
		Record:     row,
		Value:      FormatValue(values),
		Values:     values,
		HTML:       html,
		AsQuery:    asQuery,
	}
}

type idsToValuesOptions struct {
	rewind *time.Time
}

func (c *Column) recordsFromDB(ids []int64, hasIDs bool, noFilter bool, includeDeleted bool, rewind *time.Time) *Records {
	if !hasIDs && c.e.PanicOnCurvalBuildValues {
		Panic("Entering curval _build_values and PANIC_ON_CURVAL_BUILD_VALUES is true")
	}
	lp := c.LayoutParent()
	if lp == nil {
		return nil
	}
	var view *View
	if !hasIDs && !noFilter {
		view = c.ViewFn()
		if view == nil {
			return nil
		}
	}
	recs := c.e.NewRecords(lp, c.layout.User())
	recs.View = view
	recs.Rewind = rewind
	recs.SetColumns(c.CurvalFieldIDsRetrieve(curvalRetrieveOptions{}))
	if hasIDs {
		recs.SetLimitCurrentIDs(ids)
	}
	recs.IgnoreViewLimitExtra = true
	recs.IncludeDeleted = includeDeleted
	recs.SetIncludeChildren(true)
	isDraft := c.layout.Record() != nil && c.layout.Record().IsDraft() != 0
	if isDraft && c.ShowAdd() && c.layout.User() != nil {
		recs.IsDraft = c.layout.User().ID
	}
	if lp.SortLayoutID() == nil || *lp.SortLayoutID() == 0 {
		var s []SortSpec
		for _, id := range c.CurvalFieldIDs() {
			s = append(s, SortSpec{ID: id})
		}
		recs.SetSort(s)
	}
	return recs
}

// IDsToValues retrieves formatted values for curval IDs.
func (c *Column) IDsToValues(ids []int64, o idsToValuesOptions) []curvalRow {
	rows := c.getRows(ids, o)
	var out []curvalRow
	for _, r := range rows {
		out = append(out, c.formatRow(r))
	}
	return out
}

func (c *Column) getRows(ids []int64, o idsToValuesOptions) []*Record {
	if len(ids) == 0 {
		return nil
	}
	// Sanity check for duplicates
	want := map[int64]bool{}
	for _, id := range ids {
		if want[id] {
			Fail("Duplicate value requested for field ID %d: %d", c.ID, id)
		}
		want[id] = true
	}
	var ret []*Record
	cache := c.layout.CachedRecords()
	all := true
	for _, id := range ids {
		if cache[id] == nil {
			all = false
			break
		}
	}
	if all {
		for _, id := range ids {
			ret = append(ret, cache[id])
		}
	} else {
		e := c.e
		oldIP, oldIPS := e.IgnorePermissions, e.IgnorePermissionsSearch
		if c.OverridePermissions() {
			e.IgnorePermissions = true
		}
		e.IgnorePermissionsSearch = true
		recs := c.recordsFromDB(ids, true, false, true, o.rewind)
		ret = recs.Results()
		e.IgnorePermissions, e.IgnorePermissionsSearch = oldIP, oldIPS
		for _, r := range ret {
			cache[r.currentID] = r
		}
	}
	deleted := map[int64]bool{}
	for _, r := range ret {
		if r.Deleted() != nil {
			deleted[r.currentID] = true
		}
	}
	var keepIDs []int64
	for _, id := range ids {
		if !deleted[id] {
			keepIDs = append(keepIDs, id)
		}
	}
	var keep []*Record
	for _, r := range ret {
		if !deleted[r.currentID] {
			keep = append(keep, r)
		}
	}
	if len(keep) != len(keepIDs) {
		found := map[int64]bool{}
		for _, r := range keep {
			found[r.currentID] = true
		}
		var notFound []string
		for _, id := range keepIDs {
			if !found[id] {
				notFound = append(notFound, itoa(id))
			}
		}
		if len(notFound) > 0 {
			// A curval field which has had its related table changed may
			// have IDs from the old table
			Fail("Could not find these requested Curval IDs for field ID %d: %s", c.ID, strings.Join(notFound, " "))
		}
		var s []string
		for _, id := range keepIDs {
			s = append(s, itoa(id))
		}
		Fail("Invalid Curval ID list %s", strings.Join(s, " "))
	}
	return keep
}

// ViewFn returns the curval filter view with record values substituted.
func (c *Column) ViewFn() *View {
	if c.Type != "curval" {
		return nil
	}
	if c.viewBuilt {
		return c.view
	}
	c.viewBuilt = true
	lp := c.LayoutParent()
	f := c.FilterFn()
	nf := NewFilterHash(cloneHash(f.AsHash()), lp)
	v := c.e.NewTempView(lp, nf)
	if !nf.SubValues(c.layout) {
		c.view = nil
		return nil
	}
	c.view = v
	return v
}

func cloneHash(h map[string]any) map[string]any {
	js := EncodeJSON(h)
	v, _ := DecodeJSON(js)
	m, _ := v.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// ClearCurval clears caches of a curval column.
func (c *Column) ClearCurval() {
	c.viewBuilt = false
	c.view = nil
	c.layoutParent = nil
	c.curvalFieldsLoaded = false
	c.filter = nil
	c.subvalsBuilt = false
	c.allValuesCache = nil
	c.hasValuesIndex = false
}

// FilteredValues returns the selectable values of a dropdown curval.
func (c *Column) FilteredValues(submissionToken string) []curvalRow {
	if c.ValueSelector() != "dropdown" {
		return nil
	}
	e := c.e
	recs := c.recordsFromDB(nil, false, false, false, nil)
	oldIP, oldIPS := e.IgnorePermissions, e.IgnorePermissionsSearch
	if c.OverridePermissions() {
		e.IgnorePermissions = true
	}
	e.IgnorePermissionsSearch = true
	defer func() { e.IgnorePermissions, e.IgnorePermissionsSearch = oldIP, oldIPS }()
	var subID int64
	if submissionToken != "" {
		subID, _ = e.DB.QueryInt64(`SELECT "id" FROM "submission" WHERE "token" = ? ORDER BY "id" LIMIT 1`, submissionToken)
	}
	if subID != 0 {
		for _, fv := range c.FilvalFields() {
			e.DB.Exec(`DELETE FROM "filtered_value" WHERE "submission_id" = ? AND "layout_id" = ?`, subID, fv.ID)
		}
	}
	if recs == nil || recs.Count() == 0 {
		if subID != 0 {
			for _, fv := range c.FilvalFields() {
				e.DB.InsertMap("filtered_value", map[string]any{"submission_id": subID, "layout_id": fv.ID, "current_id": nil})
			}
		}
		return nil
	}
	var out []curvalRow
	for {
		r := recs.Single()
		if r == nil {
			break
		}
		out = append(out, c.formatRow(r))
		if subID != 0 {
			for _, fv := range c.FilvalFields() {
				e.DB.TryInsertMap("filtered_value", map[string]any{"submission_id": subID, "layout_id": fv.ID, "current_id": r.currentID})
			}
		}
	}
	if c.ShowAdd() && c.layout.Record() != nil {
		if d, ok := c.layout.Record().Fields()[c.ID].(*CurcommonDatum); ok {
			for _, qr := range d.ValuesAsQueryRecords() {
				out = append(out, c.formatRow(qr))
			}
		}
	}
	return out
}

// AllValues returns all values of a dropdown curval (unfiltered).
func (c *Column) AllValues() []curvalRow {
	if c.ValueSelector() != "dropdown" {
		return nil
	}
	recs := c.recordsFromDB(nil, false, true, false, nil)
	if recs == nil {
		return nil
	}
	var out []curvalRow
	for {
		r := recs.Single()
		if r == nil {
			break
		}
		out = append(out, c.formatRow(r))
	}
	return out
}

// ValuesBeginningWith implements typeahead searches.
func (c *Column) ValuesBeginningWith(match string, noempty bool, useID bool) []map[string]any {
	if c.IsCurcommon() {
		return c.curvalValuesBeginningWith(match, noempty, useID)
	}
	var table, vf, joins string
	var where []string
	var args []any
	switch c.Type {
	case "string":
		table, vf = "string", "value"
		where = append(where, `"me"."layout_id" = ?`)
		args = append(args, c.ID)
	case "enum", "tree":
		table, vf = "enumval", "value"
		where = append(where, `"me"."layout_id" = ?`, `"me"."deleted" = 0`)
		args = append(args, c.ID)
	case "calc":
		if c.ValueField() != "value_text" {
			return nil
		}
		table, vf = "calc_unique", "value_text"
		where = append(where, `"me"."layout_id" = ?`)
		args = append(args, c.ID)
	case "person":
		// ResultSet::User->summary->with_filter
		table, vf = "user", "value"
		joins = ` LEFT JOIN "organisation" "organisation" ON "organisation"."id" = "me"."organisation" LEFT JOIN "department" "department" ON "department"."id" = "me"."department_id" LEFT JOIN "team" "team" ON "team"."id" = "me"."team_id" LEFT JOIN "title" "title" ON "title"."id" = "me"."title"`
		where = append(where, `"me"."account_request" = 0`, `"me"."deleted" IS NULL`)
		if fw, fa := c.e.userFilterSQL(c.FilterFn()); fw != "" {
			where = append(where, strings.TrimPrefix(fw, " AND "))
			args = append(args, fa...)
		}
	default:
		return nil
	}
	ms := strings.ReplaceAll(match, "_", `\_`)
	ms = strings.ReplaceAll(ms, "%", `\%`)
	if match != "" {
		where = append(where, `"me".`+q(vf)+` LIKE ?`)
		args = append(args, ms+"%")
	} else if noempty {
		// GADS gives a reference to the literal SQL "0 = 1" as a column
		// name, so its stringification is quoted as an identifier: SQLite
		// takes it as a string (so no rows), PostgreSQL fails
		where = append([]string{`"SCALAR(0x0)" IS NULL`}, where...)
	}
	w := strings.Join(where, " AND ")
	// The user resultset is ordered by surname, which is not grouped (an
	// error on PostgreSQL, as with GADS)
	order := ""
	if table == "user" {
		order = ` ORDER BY "surname"`
	}
	var out []map[string]any
	if c.Fixedvals() {
		rows := c.e.DB.Query(`SELECT MAX( "me"."id" ) AS "id", "me".`+q(vf)+` AS "label" FROM `+q(table)+` "me"`+joins+` WHERE `+w+` GROUP BY "me".`+q(vf)+order+` LIMIT 10`, args...)
		for _, r := range rows {
			out = append(out, map[string]any{"id": r["id"], "label": r["label"]})
		}
		return out
	}
	rows := c.e.DB.Query(`SELECT MAX( "me".`+q(vf)+` ) AS "v" FROM `+q(table)+` "me"`+joins+` WHERE `+w+` GROUP BY "me".`+q(vf)+order+` LIMIT 10`, args...)
	for _, r := range rows {
		out = append(out, map[string]any{"label": r["v"]})
	}
	return out
}

func (c *Column) curvalValuesBeginningWith(match string, noempty, useID bool) []map[string]any {
	if c.Type == "curval" && c.ViewFn() == nil {
		return nil
	}
	var rules []any
	matches := strings.FieldsFunc(match, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	if useID {
		cid := c.layout.ColumnID()
		for _, m := range matches {
			rules = append(rules, map[string]any{"field": cid.ID, "id": cid.ID, "type": cid.Type, "value": m, "operator": "equal"})
		}
	} else {
		for _, m := range matches {
			var conds []any
			for _, f := range c.CurvalFields() {
				if f == nil {
					continue
				}
				op := "equal"
				if f.ReturnTypeFn() == "string" {
					op = "contains"
				}
				conds = append(conds, map[string]any{"field": f.ID, "id": f.ID, "type": f.Type, "value": m, "operator": op})
			}
			rules = append(rules, map[string]any{"condition": "AND", "rules": []any{map[string]any{"condition": "OR", "rules": conds}}})
		}
	}
	if v := c.ViewFn(); v != nil {
		rules = append(rules, v.Filter().AsHash())
	}
	lp := c.LayoutParent()
	view := c.e.NewTempView(lp, nil)
	if match != "" {
		view.SetFilter(NewFilterHash(map[string]any{"condition": "AND", "rules": rules}, lp))
	}
	e := c.e
	oldIP := e.IgnorePermissions
	if c.OverridePermissions() {
		e.IgnorePermissions = true
	}
	defer func() { e.IgnorePermissions = oldIP }()
	recs := e.NewRecords(lp, c.layout.User())
	recs.Rows = 10
	recs.View = view
	recs.SetColumns(c.CurvalFieldIDs())
	var out []map[string]any
	if match != "" || !noempty {
		for _, r := range recs.Results() {
			fr := c.formatRow(r)
			out = append(out, map[string]any{"id": fr.ID, "label": fr.Value, "html": fr.HTML})
		}
	}
	return out
}

// ---------------------------------------------------------------------
// Curcommon datum
// ---------------------------------------------------------------------

// CurcommonDatum is GADS::Datum::Curval / Autocur / Filval.
type CurcommonDatum struct {
	datumBase
	ids               []int64
	idsSet            bool
	values            []curvalRow
	valuesBuilt       bool
	initHash          *curInitHash
	valuesAsQuery     []string
	queryRecords      []*Record
	queryRecordsBuilt bool
	valuesAsRecords   []*Record
	allIDsCache       []int64
	allIDsBuilt       bool
	idsDeleted        []int64
	text              *string
}

type curInitHash struct {
	records []*Record
	ids     []int64
}

func (d *CurcommonDatum) initValueHash() *curInitHash {
	if d.initHash != nil {
		return d.initHash
	}
	h := &curInitHash{}
	col := d.column
	if d.hasInitValue {
		for _, v := range d.initValue {
			rec, id := d.transformValue(v)
			if rec != nil {
				h.records = append(h.records, rec)
			}
			if id != 0 && (rec == nil || rec.IsDraft() == 0) {
				h.ids = append(h.ids, id)
			}
		}
	} else if col.Type == "autocur" && d.valuesAsRecords == nil && d.record != nil {
		for _, mv := range col.FetchMultivalues([]int64{d.record.recordID}, fetchMultiOptions{alreadySeen: newSeenRoot()}) {
			if mv.value.Record != nil {
				h.records = append(h.records, mv.value.Record)
				h.ids = append(h.ids, mv.value.Record.currentID)
			}
		}
	}
	d.initHash = h
	return h
}

func (d *CurcommonDatum) transformValue(v rawValue) (*Record, int64) {
	if v.Record != nil {
		return v.Record, v.Record.currentID
	}
	if v.Value == nil {
		return nil, 0
	}
	id, _ := anyToInt64(v.Value)
	return nil, id
}

// IDs returns the selected record IDs.
func (d *CurcommonDatum) IDs() []int64 {
	if !d.idsSet {
		d.ids = append([]int64(nil), d.initValueHash().ids...)
		d.idsSet = true
	}
	return d.ids
}

// AllIDs includes IDs not shown due to limit_rows.
func (d *CurcommonDatum) AllIDs() []int64 {
	if d.allIDsBuilt {
		return d.allIDsCache
	}
	d.allIDsBuilt = true
	col := d.column
	if col.LimitRows() == 0 {
		d.allIDsCache = d.IDs()
		return d.allIDsCache
	}
	if d.record == nil || d.record.NewEntry() {
		d.allIDsCache = nil
		return nil
	}
	if d.changed {
		d.allIDsCache = d.IDs()
		return d.allIDsCache
	}
	rid := d.record.recordIDOld
	if rid == 0 {
		rid = d.record.recordID
	}
	d.allIDsCache = col.e.DB.QueryInt64s(`SELECT "value" FROM "curval" WHERE "value" IS NOT NULL AND "record_id" = ? AND "layout_id" = ? ORDER BY "id"`, rid, col.ID)
	return d.allIDsCache
}

// SetValue implements Curcommon set_value.
func (d *CurcommonDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	col := d.column
	if col.Type == "autocur" && !opts.AllowSetAutocur {
		Panic("Records passed to autocur set_value")
	}
	clone := d.Clone(nil, false)
	var vals []any
	for _, v := range valueList(value) {
		if truthy(v) {
			vals = append(vals, v)
		}
	}
	sort.SliceStable(vals, func(i, j int) bool { return anyToString(vals[i]) < anyToString(vals[j]) })
	var records []*Record
	var ids []int64
	var queries []string
	for _, v := range vals {
		if r, ok := v.(*Record); ok {
			records = append(records, r)
			continue
		}
		s := anyToString(v)
		if reDigits.MatchString(s) {
			id, _ := anyToInt64(s)
			ids = append(ids, id)
		} else {
			queries = append(queries, s)
		}
	}
	var oldIDs []int64
	oldIDs = append(oldIDs, d.AllIDs()...)
	sortInt64sString(oldIDs)
	if col.LimitRows() > 0 && col.MultivalueFn() {
		sub := map[int64]bool{}
		for _, id := range ids {
			sub[id] = true
		}
		for _, o := range oldIDs {
			if !sub[o] {
				ids = append(ids, o)
			}
		}
		sortInt64sString(ids)
	}
	if len(records) > 0 && (len(ids) > 0 || len(queries) > 0) {
		Panic("Records cannot be mixed with other set values")
	}
	changed := false
	d.valuesAsRecords = nil
	if len(records) > 0 {
		d.valuesAsRecords = records
		ids = nil
		for _, r := range records {
			if !r.NewEntry() {
				ids = append(ids, r.currentID)
			}
		}
		var qs []string
		for _, r := range records {
			if r.NewEntry() {
				qs = append(qs, r.AsQuery(opts.AllowSetAutocur))
			}
		}
		d.valuesAsQuery = qs
		d.queryRecordsBuilt = false
	}
	if len(queries) > 0 {
		d.valuesAsQuery = queries
		d.queryRecordsBuilt = false
		for _, r := range d.ValuesAsQueryRecords() {
			if r.IsEdited() {
				changed = true
			}
		}
		updated := map[int64]bool{}
		for _, r := range d.ValuesAsQueryRecords() {
			if !r.NewEntry() {
				updated[r.currentID] = true
			}
		}
		var keep []int64
		for _, o := range oldIDs {
			if !updated[o] {
				keep = append(keep, o)
			}
		}
		oldIDs = keep
		d.allIDsBuilt = false
	}
	if !changed {
		changed = joinInt64s(ids) != joinInt64s(oldIDs)
	}
	if changed {
		d.changed = true
		for _, id := range ids {
			col.Validate(itoa(id), true)
		}
		d.valuesBuilt = false
		d.values = nil
		d.text = nil
		d.hasInitValue = false
		d.initValue = nil
		d.initHash = nil
		d.allIDsBuilt = false
	}
	d.ids = ids
	d.idsSet = true
	d.oldvalue = clone
}

func sortInt64sString(s []int64) {
	sort.SliceStable(s, func(i, j int) bool { return itoa(s[i]) < itoa(s[j]) })
}

func joinInt64s(s []int64) string {
	var parts []string
	for _, i := range s {
		parts = append(parts, itoa(i))
	}
	return strings.Join(parts, " ")
}

// ValuesAsQueryRecords converts submitted query strings into records.
func (d *CurcommonDatum) ValuesAsQueryRecords() []*Record {
	if d.queryRecordsBuilt {
		return d.queryRecords
	}
	d.queryRecordsBuilt = true
	d.queryRecords = nil
	col := d.column
	lp := col.LayoutParent()
	for _, query := range d.valuesAsQuery {
		params, err := url.ParseQuery(query)
		if err != nil {
			Panic("Invalid query string: %s", query)
		}
		for k := range params {
			if strings.HasSuffix(k, "_typeahead") {
				delete(params, k)
			}
		}
		for k := range params {
			if !reQueryKey.MatchString(k) {
				Panic("Invalid query string: %s", query)
			}
		}
		cols := lp.All(AllOptions{UserCanWrite: true, Userinput: BoolP(true)})
		var colIDs []int64
		for _, c := range cols {
			colIDs = append(colIDs, c.ID)
		}
		rec := col.e.newRecordObject(lp, col.layout.User())
		rec.SetColumns(colIDs)
		if cid := params.Get("current_id"); cid != "" {
			id, _ := anyToInt64(cid)
			rec.FindCurrentID(id, FindOptions{IncludeDraft: true})
		} else {
			rec.Initialise(0)
		}
		for _, c := range cols {
			vals, ok := params[c.Field()]
			if !ok || !c.Userinput() {
				continue
			}
			var nv []any
			for _, v := range vals {
				nv = append(nv, v)
			}
			rec.GetFieldValue(c).SetValue(nv, SetValueOptions{})
		}
		d.queryRecords = append(d.queryRecords, rec)
	}
	return d.queryRecords
}

var reQueryKey = regexpMust(`^(?:csrf_token|submit|current_id|field[0-9]+)$`)

// Values returns the formatted values.
func (d *CurcommonDatum) Values() []curvalRow {
	if d.valuesBuilt {
		return d.values
	}
	d.valuesBuilt = true
	col := d.column
	var out []curvalRow
	if h := d.initValueHash(); h.records != nil {
		for _, r := range h.records {
			out = append(out, col.formatRow(r))
		}
	} else if d.valuesAsRecords != nil {
		for _, rec := range d.valuesAsRecords {
			fr := col.formatRow(rec)
			if rec.NewEntry() {
				fr.ID = 0
				fr.AsQuery = rec.AsQuery(false)
			}
			fr.Record = rec
			out = append(out, fr)
		}
	} else if len(d.IDs()) > 0 || len(d.valuesAsQuery) > 0 {
		out = d.AllRecords()
	}
	d.values = out
	return out
}

// AllRecords returns all values including query records.
func (d *CurcommonDatum) AllRecords() []curvalRow {
	col := d.column
	var rewind *time.Time
	if d.record != nil {
		rewind = d.record.Rewind
		if rewind == nil {
			rewind = d.record.valuesRewind
		}
	}
	out := col.IDsToValues(d.AllIDs(), idsToValuesOptions{rewind: rewind})
	recs := d.ValuesAsQueryRecords()
	for i, q := range d.valuesAsQuery {
		if i >= len(recs) {
			break
		}
		rec := recs[i]
		fr := col.formatRow(rec)
		fr.ID = rec.currentID
		fr.AsQuery = q
		out = append(out, fr)
	}
	return out
}

// TextAll returns the value text of each record.
func (d *CurcommonDatum) TextAll() []string {
	var out []string
	for _, v := range d.Values() {
		out = append(out, v.Value)
	}
	return out
}

// AsString joins the values with "; ".
func (d *CurcommonDatum) AsString() string {
	if d.text != nil {
		return *d.text
	}
	s := strings.Join(d.TextAll(), "; ")
	d.text = &s
	return s
}

// HTML escapes.
func (d *CurcommonDatum) HTML() string { return htmlEscape(d.AsString()) }

// Blank reports no values.
func (d *CurcommonDatum) Blank() bool {
	if d.valuesBuilt {
		return len(d.values) == 0
	}
	if len(d.IDs()) > 0 || len(d.valuesAsQuery) > 0 {
		return false
	}
	if h := d.initValueHash(); len(h.records) > 0 {
		return false
	}
	return true
}

// HasValue reports a value.
func (d *CurcommonDatum) HasValue() bool { return !d.Blank() }

// HTMLForm returns ids (or values for show_add).
func (d *CurcommonDatum) HTMLForm() []any {
	if !d.column.ShowAdd() {
		var out []any
		for _, id := range d.IDs() {
			out = append(out, id)
		}
		return out
	}
	var out []any
	for _, v := range d.Values() {
		m := map[string]any{"id": v.ID, "value": v.Value, "value_id": v.ValueID,
			"selector_id": v.SelectorID, "html": v.HTML}
		// As Perl, which passes the whole value (including any query)
		if v.AsQuery != "" {
			m["as_query"] = v.AsQuery
		}
		if v.Record != nil && v.Record.IsDraft() != 0 {
			m["as_query"] = v.Record.AsQuery(false)
		}
		if v.Record != nil && (v.Record.NewEntry() || v.Record.IsDraft() != 0) {
			delete(m, "id")
		}
		out = append(out, m)
	}
	return out
}

// SetValuesForChild returns the ids.
func (d *CurcommonDatum) SetValuesForChild() []any {
	var out []any
	for _, id := range d.IDs() {
		out = append(out, id)
	}
	return out
}

// SearchValuesUnique returns the ids as strings.
func (d *CurcommonDatum) SearchValuesUnique() []string {
	var out []string
	for _, id := range d.IDs() {
		out = append(out, itoa(id))
	}
	return out
}

// RawValues returns ids.
func (d *CurcommonDatum) RawValues() []any { return d.SetValuesForChild() }

// ValueRegexTest returns the text values (re-evaluating filvals).
func (d *CurcommonDatum) ValueRegexTest(token string) []string {
	if d.column.Type == "filval" && token != "" {
		d.reEvaluateFilval(token, false)
	}
	return d.TextAll()
}

// FilterValue returns the first id.
func (d *CurcommonDatum) FilterValue() string {
	ids := d.IDs()
	if len(ids) == 0 {
		return ""
	}
	return itoa(ids[0])
}

// IDsRemoved returns ids removed by the last change.
func (d *CurcommonDatum) IDsRemoved() []int64 {
	if !d.changed || d.oldvalue == nil {
		return nil
	}
	old := map[int64]bool{}
	for _, id := range d.oldvalue.(*CurcommonDatum).IDs() {
		old[id] = true
	}
	for _, id := range d.IDs() {
		delete(old, id)
	}
	for _, r := range d.ValuesAsQueryRecords() {
		if !r.NewEntry() {
			delete(old, r.currentID)
		}
	}
	var out []int64
	for id := range old {
		out = append(out, id)
	}
	sortInt64s(out)
	return out
}

// IDsAffected returns old and new ids.
func (d *CurcommonDatum) IDsAffected() []int64 {
	ids := map[int64]bool{}
	if d.oldvalue != nil {
		for _, id := range d.oldvalue.(*CurcommonDatum).AllIDs() {
			ids[id] = true
		}
	}
	for _, id := range d.AllIDs() {
		ids[id] = true
	}
	var out []int64
	for id := range ids {
		out = append(out, id)
	}
	sortInt64s(out)
	return out
}

// IDsChanged returns ids added or removed.
func (d *CurcommonDatum) IDsChanged() []int64 {
	oldIDs := map[int64]bool{}
	if d.oldvalue != nil {
		for _, id := range d.oldvalue.(*CurcommonDatum).AllIDs() {
			oldIDs[id] = true
		}
	}
	newIDs := map[int64]bool{}
	for _, id := range d.IDs() {
		newIDs[id] = true
	}
	var out []int64
	for _, id := range d.IDsAffected() {
		if oldIDs[id] && newIDs[id] {
			continue
		}
		out = append(out, id)
	}
	return out
}

// PurgeDrafts removes draft sub-records.
func (d *CurcommonDatum) PurgeDrafts() {
	var records []*Record
	if h := d.initValueHash(); h.records != nil {
		records = h.records
	} else {
		for _, v := range d.Values() {
			records = append(records, v.Record)
		}
	}
	for _, r := range records {
		if r != nil && r.IsDraft() != 0 {
			r.DeleteCurrent(true, 0)
			r.PurgeCurrent()
		}
	}
}

// ForCode returns the Lua value.
func (d *CurcommonDatum) ForCode(ctx ForCodeContext) any {
	col := d.column
	e := col.e
	old := e.IgnorePermissions
	e.IgnorePermissions = true
	defer func() { e.IgnorePermissions = old }()
	tree := ctx.AlreadySeen
	var child *seenNode
	if tree != nil {
		child = tree.addChild(col.ID)
	} else {
		child = &seenNode{id: col.ID}
	}
	var need []*Column
	for _, c := range col.CurvalFieldsRetrieve(curvalRetrieveOptions{allFields: true, alreadySeen: child}) {
		if c.NameShort != "" && ctx.Fields[c.NameShort] {
			need = append(need, c)
		}
	}
	var values []any
	for _, v := range d.AllRecords() {
		rec := v.Record
		fv := rec.ForCode(need, ctx.Fields, child)
		item := map[string]any{"id": lua.Int(rec.currentID)}
		if len(need) > 0 {
			item["field_values"] = fv
		}
		values = append(values, item)
	}
	if col.MultivalueFn() || len(values) > 1 {
		if values == nil {
			values = []any{}
		}
		return values
	}
	if len(values) > 0 {
		return values[0]
	}
	return nil
}

// Clone copies the datum.
func (d *CurcommonDatum) Clone(record *Record, fresh bool) Datum {
	n := &CurcommonDatum{datumBase: d.cloneBase(record)}
	if fresh && d.column.ValueSelector() == "noshow" {
		for _, v := range d.Values() {
			if v.Record != nil {
				// As Perl, the query is that of a clone of the record, so
				// that it is written as a new independent record
				n.valuesAsQuery = append(n.valuesAsQuery, v.Record.Clone().AsQuery(false))
			}
		}
		return n
	}
	n.ids = append([]int64(nil), d.IDs()...)
	n.idsSet = true
	if d.hasInitValue {
		n.initValue = d.initValue
		n.hasInitValue = true
	}
	if d.valuesBuilt {
		n.values = d.values
		n.valuesBuilt = true
	}
	return n
}

// ---------------------------------------------------------------------
// Filval
// ---------------------------------------------------------------------

func (d *CurcommonDatum) reEvaluateFilval(token string, newEntry bool) {
	col := d.column
	related := col.RelatedField()
	rf := d.record.Fields()[related.ID]
	changed := rf != nil && rf.base().changed
	for _, c := range related.SubvalsInputRequired() {
		if f := d.record.Fields()[c.ID]; f != nil && f.base().changed {
			changed = true
		}
	}
	if !changed && !newEntry {
		return
	}
	if token == "" {
		Panic("Missing submission token")
	}
	subID, _ := col.e.DB.QueryInt64(`SELECT "id" FROM "submission" WHERE "token" = ? ORDER BY "id" LIMIT 1`, token)
	ids := col.e.DB.QueryInt64s(`SELECT "current_id" FROM "filtered_value" WHERE "submission_id" = ? AND "layout_id" = ?`, subID, col.ID)
	if len(ids) == 0 {
		for _, r := range related.FilteredValues(token) {
			ids = append(ids, r.ID)
		}
	}
	var vals []any
	for _, id := range ids {
		vals = append(vals, id)
	}
	d.SetValue(vals, SetValueOptions{IsParentValue: true})
}

func (d *CurcommonDatum) writeFilval(token string) {
	col := d.column
	e := col.e
	ids := d.IDs()
	if len(ids) == 0 {
		e.DB.InsertMap("curval", map[string]any{"layout_id": col.ID, "record_id": d.recordID, "value": nil})
	}
	for _, id := range ids {
		e.DB.InsertMap("curval", map[string]any{"layout_id": col.ID, "record_id": d.recordID, "value": id})
	}
	if token != "" {
		subID, _ := e.DB.QueryInt64(`SELECT "id" FROM "submission" WHERE "token" = ? ORDER BY "id" LIMIT 1`, token)
		e.DB.Exec(`DELETE FROM "filtered_value" WHERE "layout_id" = ? AND "submission_id" = ?`, col.ID, subID)
	}
}

// ReEvaluate (filval only).
func (d *CurcommonDatum) ReEvaluate(o ReEvalOptions) {
	if d.column.Type == "filval" {
		d.reEvaluateFilval(o.SubmissionToken, o.NewEntry)
	}
}

// WriteValue is not used for curcommon (filval handled in fieldWrite).
func (d *CurcommonDatum) WriteValue() {}

// SubvalsInputRequired returns the fields needed for a filtered curval.
func (c *Column) SubvalsInputRequired() []*Column {
	if c.subvalsBuilt {
		return c.subvalsRequired
	}
	c.subvalsBuilt = true
	deps := map[int64]map[int64]bool{}
	f := NewFilterHash(cloneHash(c.FilterFn().AsHash()), c.layout)
	cols := append([]*Column(nil), f.ColumnsInSubs()...)
	for i := 0; i < len(cols); i++ {
		col := cols[i]
		if deps[c.ID] == nil {
			deps[c.ID] = map[int64]bool{}
		}
		deps[c.ID][col.ID] = true
		for _, cc := range col.DependsOn() {
			if deps[col.ID] == nil {
				deps[col.ID] = map[int64]bool{}
			}
			deps[col.ID][cc] = true
			cols = append(cols, c.layout.Column(cc))
		}
		for _, disp := range c.e.DB.QueryInt64s(`SELECT "display_field_id" FROM "display_field" WHERE "layout_id" = ?`, col.ID) {
			if deps[col.ID] == nil {
				deps[col.ID] = map[int64]bool{}
			}
			deps[col.ID][disp] = true
			cols = append(cols, c.layout.Column(disp))
		}
	}
	keys := make([]int64, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sortInt64s(keys)
	for _, k := range keys {
		if ret := c.layout.CheckRecursive(deps, k, nil); ret != "" {
			Fail(`Unable to produce list of fields required for filtered field "%s": calculated value or display condition recursive dependencies: %s`, c.Name, ret)
		}
	}
	needed := map[int64]*Column{}
	for _, col := range cols {
		if col != nil && col.Userinput() {
			needed[col.ID] = col
		}
	}
	var out []*Column
	for _, col := range needed {
		out = append(out, col)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.subvalsRequired = out
	return out
}

// FormatRow is Curcommon->_format_row
func (c *Column) FormatRow(row *Record) curvalRow { return c.formatRow(row) }

// CurcommonValue is a value of a curval (id, value, values and record)
type CurcommonValue = curvalRow
