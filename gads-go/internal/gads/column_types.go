package gads

import (
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// =====================================================================
// Enum
// =====================================================================

// Enumvals returns the (non-deleted) values of an enum or all values of a
// tree, ordered as GADS orders them.
func (c *Column) Enumvals() []map[string]any {
	if c.enumvalsLoaded {
		return c.enumvals
	}
	c.enumvalsLoaded = true
	var rows []db.Row
	if c.Type == "tree" {
		rows = c.e.DB.Query(`SELECT * FROM "enumval" "me" WHERE "layout_id" = ? ORDER BY "me"."value"`, c.ID)
	} else {
		order := `"me"."position", "me"."id"`
		switch c.Ordering {
		case "asc":
			order = `"me"."value"`
		case "desc":
			order = `"me"."value" DESC`
		}
		rows = c.e.DB.Query(`SELECT * FROM "enumval" "me" WHERE "layout_id" = ? AND "deleted" = 0 ORDER BY `+order, c.ID)
	}
	c.enumvals = nil
	for _, r := range rows {
		id, _ := db.AsInt64(r["id"])
		v, _ := db.AsString(r["value"])
		m := map[string]any{
			"id":          id,
			"value":       v,
			"deleted":     rowBool(r["deleted"]),
			"selector_id": id,
			"value_id":    id,
			"position":    r["position"],
			"html":        htmlEscape(v),
		}
		if c.Type == "tree" {
			m["parent"] = r["parent"]
		}
		c.enumvals = append(c.enumvals, m)
	}
	return c.enumvals
}

// SetEnumvals sets the enum values to be written.
func (c *Column) SetEnumvals(vals []map[string]any) {
	c.enumvals = vals
	c.enumvalsLoaded = true
	c.enumvalsIndex = nil
}

// ClearEnumvals clears the cache.
func (c *Column) ClearEnumvals() {
	c.enumvalsLoaded = false
	c.enumvals = nil
	c.enumvalsIndex = nil
	c.tree = nil
}

// Enumval returns an enum value by id.
func (c *Column) Enumval(id int64) map[string]any {
	if id == 0 {
		return nil
	}
	if c.enumvalsIndex == nil {
		c.enumvalsIndex = map[int64]map[string]any{}
		for _, e := range c.Enumvals() {
			eid, _ := anyToInt64(e["id"])
			c.enumvalsIndex[eid] = e
		}
	}
	return c.enumvalsIndex[id]
}

// StringAsID converts an enum/tree text value to its id.
func (c *Column) StringAsID(value string) int64 {
	ids := c.e.DB.QueryInt64s(`SELECT "id" FROM "enumval" WHERE "layout_id" = ? AND "deleted" = 0 AND "value" = ?`, c.ID, value)
	if len(ids) > 1 {
		Fail("More than one value for %s in field %s", value, c.Name)
	}
	if len(ids) == 0 {
		Fail("Value %s not found in field %s", value, c.Name)
	}
	return ids[0]
}

func (c *Column) writeEnumvals() {
	// Enumvals are lazily built from the database if not set
	c.Enumvals()
	position := int64(0)
	for _, en := range c.enumvals {
		value := anyToString(en["value"])
		if !reEnumValid.MatchString(value) {
			if value == "" {
				Fail("%s is not a valid value for an item of a drop-down list", "A blank value")
			}
			Fail("%s is not a valid value for an item of a drop-down list", "'"+value+"'")
		}
		position++
		pos := position
		if p, ok := anyToInt64(en["position"]); ok && p != 0 {
			pos = p
		}
		if truthy(en["id"]) {
			// The ID may be a string of a submitted form
			id, ok := anyToInt64(en["id"])
			if !ok {
				// Looked up as given (no row on SQLite, an error on
				// PostgreSQL)
				if _, exists := c.e.DB.QueryInt64(`SELECT "id" FROM "enumval" WHERE "id" = ?`, anyToString(en["id"])); !exists {
					Fail("Bad ID %s for multiple select update", anyToString(en["id"]))
				}
			}
			if _, exists := c.e.DB.QueryInt64(`SELECT "id" FROM "enumval" WHERE "id" = ?`, id); !exists {
				Fail("Bad ID %d for multiple select update", id)
			}
			c.e.DB.Exec(`UPDATE "enumval" SET "position" = ?, "value" = ? WHERE "id" = ?`, pos, value, id)
		} else {
			nid := c.e.DB.Insert("enumval", []string{"layout_id", "position", "value"}, c.ID, pos, value)
			en["id"] = nid
		}
		if c.e.EnumMapping != nil && truthy(en["source_id"]) {
			src, _ := anyToInt64(en["source_id"])
			c.e.EnumMapping[src], _ = anyToInt64(en["id"])
		}
	}
	// Delete any that no longer exist
	keep := map[int64]bool{}
	for _, en := range c.enumvals {
		id, _ := anyToInt64(en["id"])
		keep[id] = true
	}
	for _, r := range c.e.DB.Query(`SELECT "id", "deleted" FROM "enumval" WHERE "layout_id" = ? ORDER BY "id"`, c.ID) {
		id, _ := db.AsInt64(r["id"])
		if rowBool(r["deleted"]) || keep[id] {
			continue
		}
		n, _ := c.e.DB.QueryInt64(`SELECT COUNT(*) FROM "enum" WHERE "layout_id" = ? AND "value" = ?`, c.ID, id)
		if n > 0 {
			c.e.DB.Exec(`UPDATE "enumval" SET "deleted" = 1 WHERE "id" = ?`, id)
		} else {
			c.e.DB.Exec(`DELETE FROM "enumval" WHERE "id" = ?`, id)
		}
	}
}

// =====================================================================
// Tree
// =====================================================================

type treeNode struct {
	id       int64
	value    string
	deleted  bool
	parent   *treeNode
	children []*treeNode
}

type treeData struct {
	root  *treeNode
	nodes map[int64]*treeNode
}

func (c *Column) buildTree() *treeData {
	if c.tree != nil {
		return c.tree
	}
	t := &treeData{root: &treeNode{value: "Root"}, nodes: map[int64]*treeNode{}}
	var order []int64
	parents := map[int64]int64{}
	for _, e := range c.Enumvals() {
		id, _ := anyToInt64(e["id"])
		n := &treeNode{id: id, value: anyToString(e["value"]), deleted: truthy(e["deleted"])}
		t.nodes[id] = n
		order = append(order, id)
		if !n.deleted {
			if p, ok := anyToInt64(e["parent"]); ok && p != 0 {
				parents[id] = p
			}
		}
	}
	for _, id := range order {
		n := t.nodes[id]
		if p, ok := parents[id]; ok && t.nodes[p] != nil {
			n.parent = t.nodes[p]
			t.nodes[p].children = append(t.nodes[p].children, n)
		} else {
			n.parent = t.root
			t.root.children = append(t.root.children, n)
		}
	}
	c.tree = t
	return t
}

// Node returns a tree node by id.
func (c *Column) Node(id int64) *treeNode {
	if id == 0 {
		return nil
	}
	return c.buildTree().nodes[id]
}

// TreeJSON returns the tree in jstree format (GADS::Column::Tree::json).
func (c *Column) TreeJSON(selected ...int64) []any {
	sel := map[int64]bool{}
	for _, s := range selected {
		sel[s] = true
	}
	t := c.buildTree()
	var walk func(n *treeNode) []any
	walk = func(n *treeNode) []any {
		var out []any
		for _, ch := range n.children {
			if ch.deleted {
				continue
			}
			leaf := map[string]any{"text": ch.value, "id": ch.id}
			if sel[ch.id] {
				leaf["state"] = map[string]any{"selected": true}
			}
			if kids := walk(ch); len(kids) > 0 {
				leaf["children"] = kids
			}
			out = append(out, leaf)
		}
		return out
	}
	out := walk(t.root)
	if out == nil {
		return []any{}
	}
	return out
}

// TreeUpdate updates the tree from the jstree representation.
func (c *Column) TreeUpdate(tree []map[string]any) {
	newTree := map[int64]bool{}
	existing := map[int64]map[string]any{}
	for _, e := range c.Enumvals() {
		id, _ := anyToInt64(e["id"])
		existing[id] = e
	}
	var update func(t map[string]any, parent *int64)
	update = func(t map[string]any, parent *int64) {
		text := anyToString(t["text"])
		source, hasSource := anyToInt64(t["source_id"])
		hasSource = hasSource && truthy(t["source_id"])
		delete(t, "source_id")
		var id int64
		if s := anyToString(t["id"]); reDigits.MatchString(s) {
			eid, _ := anyToInt64(s)
			if e, ok := existing[eid]; ok {
				if anyToString(e["value"]) != text {
					c.e.DB.Exec(`UPDATE "enumval" SET "parent" = ?, "value" = ? WHERE "id" = ?`, parent, text, eid)
					if c.e.EnumMapping != nil && hasSource {
						c.e.EnumMapping[source] = eid
					}
				}
				id = eid
			}
		}
		if id == 0 {
			id = c.e.DB.Insert("enumval", []string{"layout_id", "parent", "value"}, c.ID, parent, text)
			if c.e.EnumMapping != nil && hasSource {
				c.e.EnumMapping[source] = id
			}
		}
		newTree[id] = true
		if kids, ok := t["children"].([]any); ok {
			for _, k := range kids {
				if km, ok := k.(map[string]any); ok {
					pid := id
					update(km, &pid)
				}
			}
		} else if kids, ok := t["children"].([]map[string]any); ok {
			for _, km := range kids {
				pid := id
				update(km, &pid)
			}
		}
	}
	for _, t := range tree {
		var parent *int64
		if p := anyToString(t["parent"]); p != "" && p != "#" {
			pi, _ := anyToInt64(p)
			parent = &pi
		}
		update(t, parent)
	}
	c.deleteUnusedNodes(newTree)
	c.ClearEnumvals()
}

func (c *Column) deleteUnusedNodes(keep map[int64]bool) {
	rows := c.e.DB.Query(`SELECT "id", "deleted", "parent" FROM "enumval" WHERE "layout_id" = ?`, c.ID)
	type flat struct {
		id, parent int64
		level      int
		deleted    bool
	}
	var all []flat
	for _, r := range rows {
		id, _ := db.AsInt64(r["id"])
		p, _ := db.AsInt64(r["parent"])
		all = append(all, flat{id: id, parent: p, deleted: rowBool(r["deleted"])})
	}
	var out []flat
	var walk func(n flat, level int)
	walk = func(n flat, level int) {
		n.level = level
		out = append(out, n)
		for _, ch := range all {
			if ch.parent != 0 && ch.parent == n.id {
				walk(ch, level+1)
			}
		}
	}
	for _, n := range all {
		if n.parent == 0 {
			walk(n, 0)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].level > out[j].level })
	for _, n := range out {
		if n.deleted {
			continue
		}
		if keep[n.id] {
			if n.parent != 0 && !keep[n.parent] {
				c.e.DB.Exec(`UPDATE "enumval" SET "parent" = NULL WHERE "id" = ?`, n.id)
			}
			continue
		}
		count, _ := c.e.DB.QueryInt64(`SELECT COUNT(*) FROM "enum" WHERE "layout_id" = ? AND "value" = ?`, c.ID, n.id)
		haschild := false
		for _, o := range out {
			if o.parent != 0 && o.parent == n.id {
				haschild = true
			}
		}
		if count > 0 || haschild {
			c.e.DB.Exec(`UPDATE "enumval" SET "deleted" = 1 WHERE "id" = ?`, n.id)
		} else {
			c.e.DB.Exec(`DELETE FROM "enumval" WHERE "id" = ?`, n.id)
		}
	}
}

// =====================================================================
// Person
// =====================================================================

// People returns the users selectable for a person column.
func (c *Column) People() []*User {
	if c.peopleLoaded {
		return c.people
	}
	c.peopleLoaded = true
	where, args := c.e.userFilterSQL(c.FilterFn())
	rows := c.e.DB.Query(`SELECT "me".* FROM "user" "me" LEFT JOIN "organisation" "organisation" ON "organisation"."id" = "me"."organisation" LEFT JOIN "department" "department" ON "department"."id" = "me"."department_id" LEFT JOIN "team" "team" ON "team"."id" = "me"."team_id" LEFT JOIN "title" "title" ON "title"."id" = "me"."title" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL`+where+` ORDER BY "surname", "me"."id"`, args...)
	for _, r := range rows {
		c.people = append(c.people, c.e.userFromRow(r))
	}
	return c.people
}

// ClearPeople clears the people cache.
func (c *Column) ClearPeople() { c.peopleLoaded = false; c.people = nil }

func (c *Column) validPerson(id int64) bool {
	for _, p := range c.People() {
		if p.ID == id {
			return true
		}
	}
	return false
}

// userFilterSQL converts a people filter into SQL conditions on the user
// table (GADS::Schema::ResultSet::User::with_filter).
func (e *Engine) userFilterSQL(f *Filter) (string, []any) {
	if f == nil {
		return "", nil
	}
	h := f.AsHash()
	if len(h) == 0 {
		return "", nil
	}
	sql, args := e.userRuleSQL(h)
	if sql == "" {
		return "", nil
	}
	return " AND " + sql, args
}

func (e *Engine) userRuleSQL(rule map[string]any) (string, []any) {
	if rules, ok := rule["rules"].([]any); ok {
		var parts []string
		var args []any
		for _, r := range rules {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			s, a := e.userRuleSQL(rm)
			if s != "" {
				parts = append(parts, s)
				args = append(args, a...)
			}
		}
		if len(parts) == 0 {
			return "", nil
		}
		join := " AND "
		if anyToString(rule["condition"]) == "OR" {
			join = " OR "
		}
		return "( " + strings.Join(parts, join) + " )", args
	}
	if len(rule) == 0 {
		return "", nil
	}
	// The valid fields are the user fields of the site
	field := anyToString(rule["field"])
	var uf *UserField
	for _, f := range e.Site().UserFields() {
		if f.Name == field {
			f := f
			uf = &f
		}
	}
	if uf == nil {
		Fail("Invalid user field %s", field)
	}
	op := anyToString(rule["operator"])
	sqlop, ok := conditionFieldMap[op]
	if !ok {
		Fail("Invalid operator %s", op)
	}
	val := getFilterValue(op, anyToString(rule["value"]), false)
	col := `"me".` + q(field)
	if uf.Table != "" {
		col = q(uf.Table) + "." + q(uf.Field)
	}
	switch sqlop {
	case "-like":
		return col + " LIKE ?", []any{val}
	case "-not_like":
		return col + " NOT LIKE ?", []any{val}
	}
	return col + " " + sqlop + " ?", []any{val}
}

// =====================================================================
// Curcommon helpers
// =====================================================================

// RefersToInstanceID returns the instance referred to by a curval etc.
func (c *Column) RefersToInstanceID() *int64 {
	if c.refersToInstanceID != nil {
		return c.refersToInstanceID
	}
	switch c.Type {
	case "curval":
		ids := c.CurvalFieldIDs()
		if len(ids) > 0 {
			if r := c.layout.Column(ids[0]); r != nil {
				v := r.InstanceID
				c.refersToInstanceID = &v
			}
		}
	case "autocur":
		if rf := c.RelatedField(); rf != nil {
			v := rf.InstanceID
			c.refersToInstanceID = &v
		}
	case "filval":
		if rf := c.RelatedField(); rf != nil {
			c.refersToInstanceID = rf.RefersToInstanceID()
		}
	}
	return c.refersToInstanceID
}

// SetRefersToInstanceID sets the instance referred to.
func (c *Column) SetRefersToInstanceID(id int64) {
	if id == 0 {
		c.refersToInstanceID = nil
		return
	}
	c.refersToInstanceID = &id
}

// CurvalFieldIDs returns the selected field ids of a curval.
func (c *Column) CurvalFieldIDs() []int64 {
	if !c.curvalFieldsLoaded {
		c.curvalFieldsLoaded = true
		if c.ID != 0 {
			c.curvalFieldIDs = c.e.DB.QueryInt64s(`SELECT "me"."child_id" FROM "curval_fields" "me" JOIN "layout" "child" ON "child"."id" = "me"."child_id" WHERE "me"."parent_id" = ? ORDER BY "child"."position", "me"."id"`, c.ID)
		}
	}
	return c.curvalFieldIDs
}

// SetCurvalFieldIDs sets the curval fields.
func (c *Column) SetCurvalFieldIDs(ids []int64) {
	c.curvalFieldIDs = ids
	c.curvalFieldsLoaded = true
}

// LayoutParent returns the layout of the table referred to.
func (c *Column) LayoutParent() *Layout {
	if c.layoutParent != nil {
		return c.layoutParent
	}
	rid := c.RefersToInstanceID()
	if rid == nil {
		return nil
	}
	c.layoutParent = c.layout.Clone(*rid)
	return c.layoutParent
}

// CurvalFields returns the readable curval fields. As Perl (a bare return
// within map), fields that are not readable are left out.
func (c *Column) CurvalFields() []*Column {
	lp := c.LayoutParent()
	var out []*Column
	for _, id := range c.CurvalFieldIDs() {
		if lp == nil {
			out = append(out, nil)
			continue
		}
		if f := lp.ColumnPerm(id, "read"); f != nil {
			out = append(out, f)
		}
	}
	return out
}

// CurvalFieldIDsAll returns all non-internal fields of the parent table.
func (c *Column) CurvalFieldIDsAll() []int64 {
	if len(c.CurvalFieldIDs()) == 0 {
		return nil
	}
	lp := c.LayoutParent()
	return c.e.DB.QueryInt64s(`SELECT "id" FROM "layout" "me" WHERE "internal" = 0 AND "instance_id" = ? ORDER BY "me"."position", "me"."id"`, lp.InstanceID)
}

// CurvalFieldsAll returns all readable fields of the parent table.
func (c *Column) CurvalFieldsAll() []*Column {
	lp := c.LayoutParent()
	var out []*Column
	for _, id := range c.CurvalFieldIDsAll() {
		out = append(out, lp.ColumnPerm(id, "read"))
	}
	return out
}

type curvalRetrieveOptions struct {
	allFields   bool
	alreadySeen *seenNode
}

// CurvalFieldsRetrieve returns the fields to retrieve for values,
// preventing recursion through fields already seen.
func (c *Column) CurvalFieldsRetrieve(o curvalRetrieveOptions) []*Column {
	var all []*Column
	if o.allFields {
		all = c.CurvalFieldsAll()
	} else {
		all = c.CurvalFields()
	}
	var out []*Column
	exists := map[int64]bool{}
	if o.alreadySeen != nil {
		for _, a := range o.alreadySeen.ancestors() {
			exists[a.id] = true
		}
	}
	for _, f := range all {
		if f == nil {
			continue
		}
		if exists[f.ID] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// CurvalFieldIDsRetrieve returns the ids of CurvalFieldsRetrieve.
func (c *Column) CurvalFieldIDsRetrieve(o curvalRetrieveOptions) []int64 {
	var out []int64
	for _, f := range c.CurvalFieldsRetrieve(o) {
		out = append(out, f.ID)
	}
	return out
}

// CurvalFieldsMultivalue returns multivalue non-curval curval fields.
func (c *Column) CurvalFieldsMultivalue(o curvalRetrieveOptions) []*Column {
	var out []*Column
	for _, f := range c.CurvalFieldsRetrieve(o) {
		if !f.IsCurcommon() && f.MultivalueFn() {
			out = append(out, f)
		}
	}
	return out
}

// Autocurs returns autocur columns related to a curval.
func (c *Column) Autocurs() []*Column {
	if !c.autocursLoaded {
		c.autocursCache = c.e.DB.QueryInt64s(`SELECT "id" FROM "layout" WHERE "type" = 'autocur' AND "related_field" = ? ORDER BY "id"`, c.ID)
		c.autocursLoaded = true
	}
	var out []*Column
	for _, id := range c.autocursCache {
		if col := c.layout.Column(id); col != nil {
			out = append(out, col)
		}
	}
	return out
}

// FilvalFields returns filval columns for a curval.
func (c *Column) FilvalFields() []*Column {
	var out []*Column
	for _, id := range c.e.DB.QueryInt64s(`SELECT "id" FROM "layout" WHERE "type" = 'filval' AND "related_field" = ? ORDER BY "id"`, c.ID) {
		if col := c.layout.Column(id); col != nil {
			out = append(out, col)
		}
	}
	return out
}

// seenNode is a node of the Tree::DAG_Node used to prevent recursion.
type seenNode struct {
	id       int64
	root     bool
	parent   *seenNode
	children []*seenNode
}

func newSeenRoot() *seenNode { return &seenNode{root: true} }

func (n *seenNode) addChild(id int64) *seenNode {
	ch := &seenNode{id: id, parent: n}
	n.children = append(n.children, ch)
	return ch
}

// ancestors returns all ancestors including the root (Tree::DAG_Node
// ancestors excludes the node itself).
func (n *seenNode) ancestors() []*seenNode {
	var out []*seenNode
	for p := n.parent; p != nil; p = p.parent {
		out = append(out, p)
	}
	return out
}
