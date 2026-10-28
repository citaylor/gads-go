package gads

import (
	"time"

	"github.com/ctrlo/gads-go/internal/db"
)

// Port of GADS::RecordsJoin: tracks the joins and prefetches needed for a
// records query, and computes the table aliases that DBIx::Class assigns
// to them.

// joinSpec is a DBIx::Class join attribute: a relationship name with
// optional nested joins ({name => kids} in Perl, or a plain string).
type joinSpec struct {
	name string
	kids []*joinSpec
}

func (j *joinSpec) isHash() bool { return j != nil && len(j.kids) > 0 }

// hashValueIsString reports the case {key => 'string'}.
func (j *joinSpec) hashValueString() (string, bool) {
	if len(j.kids) == 1 && len(j.kids[0].kids) == 0 {
		return j.kids[0].name, true
	}
	return "", false
}

func joinLeaf(name string) *joinSpec { return &joinSpec{name: name} }

func joinHash(name string, kids ...*joinSpec) *joinSpec {
	return &joinSpec{name: name, kids: kids}
}

// tjoinOptions are options to Column.Tjoin.
type tjoinOptions struct {
	currentVersion bool // join_current_version
}

// joinOpts are the flags used when adding or retrieving joins.
type joinOpts struct {
	prefetch, search, linked, sort, group, drcol bool
	hasPrefetch, hasLinked                       bool // whether option explicitly provided
	parent                                       *Column
	includeMultivalue                            int64
	retainJoinOrder                              bool
	aggregate                                    bool
	extraColumn                                  *Column
	limit, page                                  int
	alt                                          bool
	findValue                                    bool
	rootTable                                    string
	alias                                        string
	currentVersionOnly                           bool
	multivalue                                   *bool // jpfetch: only (non-)multivalue columns
	column                                       *Column
	// aggregateOp is the aggregate operator recorded when adding a join
	// (add_aggregate); aggregate above is the query option
	aggregateOp string
}

// jp is an entry in the join store.
type jp struct {
	join                                         *joinSpec
	prefetch, search, linked, sort, group, drcol bool
	column                                       *Column
	parent                                       *Column
	children                                     []*jp
	curval                                       bool
	aggregate                                    string // aggregate operator, if added by add_aggregate
}

type joinStore struct {
	r     *Records
	store []*jp
}

func (s *joinStore) allJoinsRecurse(joins []*jp) []*jp {
	var out []*jp
	for _, j := range joins {
		out = append(out, j)
		if j.children != nil {
			out = append(out, s.allJoinsChildren(j.children)...)
		}
	}
	return out
}

func (s *joinStore) allJoinsChildren(children []*jp) []*jp {
	var out []*jp
	for _, c := range children {
		if c.children != nil {
			out = append(out, s.allJoinsChildren(c.children)...)
		}
		out = append(out, c)
	}
	return out
}

func (s *joinStore) allJoins(o joinOpts) []*jp {
	return s.allJoinsRecurse(s.jpfetchEntries(o))
}

func compareParents(a, b *Column) bool {
	if a == nil && b == nil {
		return true
	}
	if (a == nil) != (b == nil) {
		return false
	}
	return a.ID == b.ID
}

func (s *joinStore) addChildren(j *jp, column *Column, o joinOpts, alreadySeen *seenNode) {
	if j.children == nil {
		j.children = []*jp{}
	}
	existing := map[int64]bool{}
	for _, c := range j.children {
		existing[c.column.ID] = true
	}
	for _, c := range column.CurvalFieldsRetrieve(curvalRetrieveOptions{}) {
		if c.Internal {
			continue
		}
		child := &jp{
			prefetch: true,
			curval:   c.IsCurcommon(),
			column:   c,
			parent:   column,
		}
		if !existing[c.ID] {
			j.children = append(j.children, child)
		}
	}
}

func (s *joinStore) addJP(column *Column, o joinOpts) {
	if column == nil {
		// As Perl, which dies calling a method on the undefined column
		// (e.g. a layout without columns for a table of another site)
		Fail(`Can't call method "link_parent" on an undefined value`)
	}
	if lp := column.LinkParent(); lp != nil && lp.InstanceID == column.InstanceID {
		Panic("Link parent of field ID %d is from same table as field itself", column.ID)
	}
	prefetch := (column.FetchWithRecord() || (o.includeMultivalue != 0 && o.includeMultivalue == column.ID)) && o.prefetch
	// Make sure that the parent join is added, otherwise the field on its
	// own is useless
	if o.parent != nil {
		o2 := o
		o2.parent = nil
		s.addJP(o.parent, o2)
	}
	for _, j := range s.allJoinsRecurse(s.store) {
		if column.ID == j.column.ID && compareParents(o.parent, j.parent) {
			j.prefetch = j.prefetch || prefetch
			j.search = j.search || o.search
			j.linked = j.linked || o.linked
			j.sort = j.sort || o.sort
			j.group = j.group || o.group
			j.drcol = j.drcol || o.drcol
			if j.aggregate == "" {
				j.aggregate = o.aggregateOp
			}
			if column.IsCurcommon() && prefetch {
				s.addChildren(j, column, o, nil)
			}
			return
		}
	}
	add := &jp{
		prefetch:  prefetch,
		search:    o.search,
		linked:    o.linked,
		sort:      o.sort,
		group:     o.group,
		drcol:     o.drcol,
		column:    column,
		parent:    o.parent,
		aggregate: o.aggregateOp,
	}
	if column.IsCurcommon() && prefetch {
		s.addChildren(add, column, o, nil)
	}
	if o.parent != nil {
		for _, c := range s.store {
			if c.column.ID == o.parent.ID {
				var exists *jp
				for _, ch := range c.children {
					if ch.column.ID == add.column.ID {
						exists = ch
					}
				}
				if exists != nil {
					exists.search = exists.search || o.search
					exists.sort = exists.sort || o.sort
					exists.group = exists.group || o.group
					exists.drcol = exists.drcol || o.drcol
				} else {
					c.children = append(c.children, add)
				}
			}
		}
	} else {
		s.store = append(s.store, add)
	}
}

func (s *joinStore) addPrefetch(c *Column, o joinOpts) { o.prefetch = true; s.addJP(c, o) }
func (s *joinStore) addGroup(c *Column, o joinOpts)    { o.group = true; s.addJP(c, o) }
func (s *joinStore) addDrcol(c *Column, o joinOpts)    { o.drcol = true; s.addJP(c, o) }
func (s *joinStore) addJoin(c *Column, o joinOpts)     { s.addJP(c, o) }

// addAggregate is add_aggregate: a join whose value is selected with an
// aggregate operator. For a curcommon all its child fields are added too, so
// that the full value can be displayed (not just its ID)
func (s *joinStore) addAggregate(c *Column, operator string, o joinOpts) {
	o.aggregateOp = operator
	s.addJP(c, o)
	if c.IsCurcommon() {
		for _, f := range c.CurvalFieldsRetrieve(curvalRetrieveOptions{}) {
			if f.Internal {
				continue
			}
			oc := o
			oc.parent = c
			s.addAggregate(f, "max", oc)
		}
	}
}
func (s *joinStore) addLinkedPrefetch(c *Column, o joinOpts) {
	o.prefetch = true
	o.linked = true
	s.addJP(c, o)
}

func (s *joinStore) hasLinked() bool {
	for _, j := range s.store {
		if j.linked {
			return true
		}
	}
	return false
}

func (s *joinStore) hasCreatedTime(o joinOpts) bool {
	for _, j := range s.jpfetchEntries(o) {
		if j.column.NameShort == "_created" {
			return true
		}
	}
	return false
}

func (s *joinStore) hasCreatedUser(o joinOpts) bool {
	for _, j := range s.jpfetchEntries(o) {
		if j.column.NameShort == "_created_user" {
			return true
		}
	}
	return false
}

func rootIsRecord(o joinOpts) bool { return o.rootTable == "record" }

// countVersionJoins is _count_version_joins: the number of record version
// joins of the query.
func (s *joinStore) countVersionJoins(o joinOpts) int {
	count := 1
	if rootIsRecord(o) {
		count = 0
	}
	includeLinked := o.linked && s.hasLinked()
	loops := 0
	if includeLinked {
		loops = 1
	}
	for li := 0; li <= loops; li++ {
		if li == 0 && includeLinked {
			count++
		}
		curvalsIncluded := map[int64]bool{}
		oo := o
		oo.hasLinked = false
		for _, j := range s.allJoins(oo) {
			if (li == 1) != j.linked {
				continue
			}
			isCur := j.column.IsCurcommon()
			if isCur || j.parent != nil {
				ccid := j.column.ID
				if !isCur {
					ccid = j.parent.ID
				}
				if (o.search && j.search && j.parent != nil && !isCur) ||
					(o.sort && j.sort) ||
					(o.group && j.group && j.parent != nil && !isCur) ||
					(o.aggregate && j.aggregate != "" && j.parent != nil && !isCur) ||
					(o.drcol && j.drcol) ||
					(o.prefetch && j.prefetch && j.parent == nil && len(j.children) > 0) {
					if !curvalsIncluded[ccid] {
						count++
						curvalsIncluded[ccid] = true
					}
				}
			}
		}
	}
	return count
}

// recordLaterSearch returns the conditions requiring each joined record to
// be the latest version.
func (s *joinStore) recordLaterSearch(o joinOpts) []cond {
	r := s.r
	// Not needed if using previous_values (in which case all versions
	// selected) or current_version_only (direct version join)
	if r.PreviousValues || o.currentVersionOnly {
		return nil
	}
	if r.RecordEarlier {
		return []cond{condIsNull("record_earlier.id")}
	}
	count := s.countVersionJoins(o)
	var out []cond
	for i := 1; i <= count; i++ {
		id := ""
		if i != 1 {
			id = "_" + itoa(int64(i))
		}
		alt := ""
		if o.alt {
			alt = "_alternative"
		}
		out = append(out, condIsNull("record_later"+alt+id+".current_id"))
	}
	return out
}

// recordSingleRewind restricts each record version join to versions
// created at or before the rewind time.
func (s *joinStore) recordSingleRewind(o joinOpts, rewind *time.Time) []cond {
	if rewind == nil {
		return nil
	}
	count := s.countVersionJoins(o)
	// The root table being record still needs the rewind condition on
	// itself
	if rootIsRecord(o) && count == 0 {
		count = 1
	}
	ro := o
	ro.linked = false
	ro.hasLinked = true
	rs := s.recordName(ro)
	rf := db.FormatDateTime(*rewind)
	var out []cond
	for i := 1; i <= count; i++ {
		id := ""
		if i != 1 {
			id = "_" + itoa(int64(i))
		}
		alt := ""
		if o.alt {
			alt = "_alternative"
		}
		f := rs + alt + id + ".created"
		out = append(out, condOr{condIsNull(f), condCmp{field: f, op: "<=", value: rf}})
	}
	return out
}

// jpfetchEntries is _jpfetch.
func (s *joinStore) jpfetchEntries(o joinOpts) []*jp {
	var joins []*jp
	var store []*jp
	if o.retainJoinOrder {
		store = append(store, s.store...)
	} else {
		for _, j := range s.store {
			if !j.prefetch {
				store = append(store, j)
			}
		}
		for _, j := range s.store {
			if j.prefetch {
				store = append(store, j)
			}
		}
	}
	var store2 []*jp
	for _, j := range store {
		if j.linked {
			store2 = append(store2, j)
		}
	}
	for _, j := range store {
		if !j.linked {
			store2 = append(store2, j)
		}
	}
	for _, j := range store2 {
		if o.hasPrefetch && !o.prefetch && j.prefetch && !o.group && !o.drcol {
			continue
		}
		// Remove aggregate joins where the aggregate value is retrieved
		// using a subquery instead (the same condition as in getAggregate):
		// as a join it can result in double-counting in totals
		if o.aggregate && !j.group && (j.column.MultivalueFn() || (j.parent != nil && j.parent.MultivalueFn())) {
			continue
		}
		if e := s.jpfetchAdd(o, j, nil); e != nil {
			joins = append(joins, e)
		}
	}
	var ret []*jp
	if o.limit > 0 && len(joins) > 0 {
		offset := (o.page - 1) * o.limit
		if len(joins) < offset {
			return nil
		}
		end := o.limit - 1 + offset
		if end > len(joins)-1 {
			end = len(joins) - 1
		}
		if o.search {
			for _, j := range joins[:offset] {
				if j.search {
					ret = append(ret, j)
				}
			}
		}
		if o.sort {
			for _, j := range joins[:offset] {
				if j.sort {
					ret = append(ret, j)
				}
			}
		}
		if offset <= end {
			ret = append(ret, joins[offset:end+1]...)
		}
		if o.search {
			for _, j := range joins[end+1:] {
				if j.search {
					ret = append(ret, j)
				}
			}
		}
		if o.sort {
			for _, j := range joins[end+1:] {
				if j.sort {
					ret = append(ret, j)
				}
			}
		}
	} else {
		ret = joins
	}
	if !o.hasLinked {
		return ret
	}
	var ret2 []*jp
	for _, j := range ret {
		if !o.linked && j.linked {
			continue
		}
		if o.linked && !j.linked {
			continue
		}
		ret2 = append(ret2, j)
	}
	return ret2
}

func (s *joinStore) jpfetchAdd(o joinOpts, j *jp, parent *Column) *jp {
	if !((o.search && j.search) || (o.sort && j.sort) || (o.group && j.group) ||
		(o.drcol && j.drcol) ||
		(o.aggregate && j.aggregate != "") ||
		// Include only aggregate columns if requested
		(o.prefetch && j.prefetch && (!o.aggregate || j.column.Aggregate != "")) ||
		(o.extraColumn != nil && j.column.ID == o.extraColumn.ID)) {
		return nil
	}
	column := j.column
	e := *j
	if column.IsCurcommon() {
		var children []*jp
		for _, ch := range j.children {
			if c := s.jpfetchAdd(o, ch, column); c != nil {
				children = append(children, c)
			}
		}
		if o.prefetch {
			var keep []*jp
			for _, ch := range children {
				if ch.search || ch.sort || ch.column.FetchWithRecord() || o.includeMultivalue != 0 || ch.group || ch.drcol || ch.aggregate != "" {
					keep = append(keep, ch)
				}
			}
			children = keep
		}
		var kids []*joinSpec
		for _, ch := range children {
			kids = append(kids, ch.join)
		}
		e.parent = parent
		e.join = column.makeJoinFrom(kids, o.currentVersionOnly)
		e.children = children
		if e.children == nil {
			e.children = []*jp{}
		}
		return &e
	}
	e.join = column.Tjoin(tjoinOptions{currentVersion: o.currentVersionOnly})
	return &e
}

// makeJoinFrom builds the join for a curcommon column given the child
// joins (GADS::Column::Curval::make_join etc).
func (c *Column) makeJoinFrom(kids []*joinSpec, currentVersion bool) *joinSpec {
	version := func() *joinSpec {
		if currentVersion {
			return &joinSpec{name: "current_version", kids: kids}
		}
		return joinHash("record_single", append([]*joinSpec{joinLeaf("record_later")}, kids...)...)
	}
	switch c.Type {
	case "autocur":
		return joinHash(c.Field(), joinHash("record", joinHash("current", version())))
	default:
		if len(kids) == 0 {
			return joinLeaf(c.Field())
		}
		return joinHash(c.Field(), joinHash("value", version()))
	}
}

func toAlt(j *joinSpec) *joinSpec {
	if j == nil {
		Panic("Missing join")
	}
	n := &joinSpec{name: j.name + "_alternative"}
	for _, k := range j.kids {
		n.kids = append(n.kids, toAlt(k))
	}
	return n
}

// jpfetch returns the joins (DBIC join attributes).
func (s *joinStore) jpfetch(o joinOpts) []*joinSpec {
	var out []*joinSpec
	for _, j := range s.jpfetchEntries(o) {
		if o.multivalue != nil && j.column.MultivalueFn() != *o.multivalue && !j.group {
			continue
		}
		if j.join != nil {
			if o.alt {
				out = append(out, toAlt(j.join))
			} else {
				out = append(out, j.join)
			}
		}
	}
	return out
}

func recordNameByCount(count int, o joinOpts) string {
	name := "record_single"
	if o.currentVersionOnly {
		name = "current_version"
	}
	if count == 1 {
		return name
	}
	return name + "_" + itoa(int64(count))
}

// recordName returns the alias of the record table for the query.
func (s *joinStore) recordName(o joinOpts) string {
	store := s.jpfetchEntries(o)
	count := 0
	if o.rootTable == "record" {
		if o.column == nil {
			return "me"
		}
		count = 0
	} else if s.hasLinked() {
		count = 1
	}
	if o.linked {
		return recordNameByCount(count, o)
	}
	for _, c := range store {
		if !c.column.IsCurcommon() || !c.linked {
			continue
		}
		if len(c.children) == 0 {
			continue
		}
		count++
		if o.column != nil && o.column.ID == c.column.ID {
			return recordNameByCount(count, o)
		}
	}
	if !rootIsRecord(o) {
		count++
	}
	if o.column != nil {
		for _, c := range store {
			if !c.column.IsCurcommon() || c.linked {
				continue
			}
			if len(c.children) == 0 {
				continue
			}
			count++
			if c.column.ID == o.column.ID {
				break
			}
		}
	}
	return recordNameByCount(count, o)
}

// tableName returns the alias of the value table for a column.
func (s *joinStore) tableName(column *Column, o joinOpts) string {
	if column.Internal {
		if column.Table() == "current" {
			if rootIsRecord(o) {
				return "current"
			}
			if o.alias != "" {
				return o.alias
			}
			return "me"
		}
		if column.Sprefix() == "record" {
			return s.recordName(o)
		}
		if o.alt {
			return column.Sprefix() + "_alternative"
		}
		return column.Sprefix()
	}
	jn := s.joinNumber(column, o)
	index := ""
	if jn > 1 {
		index = "_" + itoa(int64(jn))
	}
	tn := column.Sprefix()
	if o.alt {
		tn += "_alternative"
	}
	return tn + index
}

type findStash struct {
	counts          map[string]int
	parentsIncluded map[int64]bool
}

func (s *joinStore) joinNumber(column *Column, o joinOpts) int {
	oo := o
	oo.hasLinked = false
	store := s.jpfetchEntries(oo)
	stash := &findStash{counts: map[string]int{}, parentsIncluded: map[int64]bool{}}
	for _, j := range store {
		if n := findJoin(column, j, stash, o); n != 0 {
			return n
		}
		for _, j2 := range j.children {
			for _, j3 := range j2.children {
				if n := findJoin(column, j3, stash, o); n != 0 {
					return n
				}
			}
			if n := findJoin(column, j2, stash, o); n != 0 {
				return n
			}
		}
	}
	if o.findValue {
		return stash.counts["value"]
	}
	cid := int64(0)
	if column != nil {
		cid = column.ID
	}
	Panic("Unable to get join number: column %d hasn't been added", cid)
	return 0
}

func findJoin(needle *Column, j *jp, stash *findStash, o joinOpts) int {
	var join *joinSpec
	if j.column.IsCurcommon() {
		join = joinLeaf(j.column.Sprefix())
	} else {
		join = j.column.Tjoin(tjoinOptions{currentVersion: o.currentVersionOnly})
	}
	if join != nil && join.isHash() {
		key := join.name
		value, isStr := join.hashValueString()
		if (o.findValue && isStr && value == "value") || (needle != nil && isStr && needle.Sprefix() == value) {
			stash.counts[key]++
			stash.counts[value]++
			if j.parent != nil && !stash.parentsIncluded[j.parent.ID] {
				if j.parent.Type == "curval" || j.parent.Type == "filval" {
					stash.counts["value"]++
				}
				stash.parentsIncluded[j.parent.ID] = true
			}
			if !o.findValue && needle.Field() == key && compareParents(o.parent, j.parent) {
				return stash.counts[value]
			}
		}
		return 0
	}
	if join != nil {
		stash.counts[join.name]++
	}
	if j.parent != nil && !stash.parentsIncluded[j.parent.ID] {
		if j.parent.ValueField() == "value" {
			stash.counts["value"]++
		}
		stash.parentsIncluded[j.parent.ID] = true
	}
	if !o.findValue && join != nil && needle != nil && needle.Sprefix() == join.name {
		if compareParents(o.parent, j.parent) && (needle.Sprefix() != "current" || needle.ID == j.column.ID) {
			return stash.counts[needle.Sprefix()]
		}
	}
	return 0
}

// valueNextJoin returns the alias of the next "value" join.
func (s *joinStore) valueNextJoin(o joinOpts) string {
	o.findValue = true
	count := s.joinNumber(nil, o)
	count++
	if count == 1 {
		return "value"
	}
	return "value_" + itoa(int64(count))
}

// fqvalue returns the fully-qualified value field of a column.
func (s *joinStore) fqvalue(column *Column, o joinOpts, asIndex bool) string {
	tn := s.tableName(column, o)
	vf := column.ValueField()
	if asIndex {
		vf = column.ValueFieldAsIndex(nil)
	}
	return tn + "." + vf
}

// columnsFetch returns the columns to select for prefetches, as pairs of
// (output key, table.column).
type fetchCol struct {
	key   string // e.g. field8.value or field33.field8.value
	alias string // e.g. field8.value
}

func (s *joinStore) columnsFetch(o joinOpts) []fetchCol {
	var out []fetchCol
	oo := o
	oo.prefetch = true
	oo.hasPrefetch = true
	for _, j := range s.jpfetchEntries(oo) {
		if !j.prefetch {
			continue
		}
		column := j.column
		table := s.tableName(column, oo)
		fields := column.RetrieveFields()
		if fields == nil {
			continue
		}
		for _, f := range append(append([]string(nil), fields...), "id") {
			out = append(out, fetchCol{key: column.Field() + "." + f, alias: table + "." + f})
		}
		if column.Userinput() {
			out = append(out, fetchCol{key: column.Field() + ".child_unique", alias: column.Field() + ".child_unique"})
		}
		if len(j.children) > 0 {
			oc := oo
			oc.column = column
			rs := s.recordName(oc)
			out = append(out, fetchCol{key: column.Field() + ".record_id", alias: rs + ".id"})
			for _, ch := range j.children {
				c2 := ch.column
				op := oo
				op.parent = column
				t2 := s.tableName(c2, op)
				f2 := c2.RetrieveFields()
				if f2 == nil {
					continue
				}
				for _, f := range append(append([]string(nil), f2...), "id") {
					out = append(out, fetchCol{key: column.Field() + "." + c2.Field() + "." + f, alias: t2 + "." + f})
				}
			}
		}
	}
	return out
}
