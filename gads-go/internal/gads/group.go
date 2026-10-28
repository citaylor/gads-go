package gads

// Group helpers (GADS::Group).

// CreateGroup creates a group as GADS::Group::write does (insert then
// update with the name and default permissions).
func (e *Engine) CreateGroup(name string) int64 {
	// The name is checked before the group's row is created
	if name == "" {
		Fail("Please enter a name")
	}
	id := e.InsertSite("group", map[string]any{"name": nil})
	e.WriteGroup(id, name)
	return id
}

// WriteGroup updates a group's name and default permissions.
func (e *Engine) WriteGroup(id int64, name string) {
	if name == "" {
		Fail("Please enter a name")
	}
	// DBIx::Class only writes changed columns: the default_* values are
	// unset (undef) on a new group so only the name is written
	e.DB.UpdateMap("group", map[string]any{"name": name}, map[string]any{"id": id})
}

// DeleteGroup deletes a group and its permissions.
func (e *Engine) DeleteGroup(id int64) {
	e.DB.Exec(`DELETE FROM "layout_group" WHERE "group_id" = ?`, id)
	e.DB.Exec(`DELETE FROM "instance_group" WHERE "group_id" = ?`, id)
	e.DB.Exec(`DELETE FROM "user_group" WHERE "group_id" = ?`, id)
	e.DB.Exec(`DELETE FROM "group" WHERE "id" = ?`, id)
}

// FindGroup is the group of an ID in the site (GADS::Group from_id), or
// nil
func (e *Engine) FindGroup(id any) map[string]any {
	return e.DB.QueryRow(`SELECT "me".* FROM "group" "me" WHERE "me"."id" = ?`+e.siteCond("me"), id)
}

// Groups is GADS::Groups all: the groups of the site in order of name
func (e *Engine) Groups() []map[string]any {
	var out []map[string]any
	for _, r := range e.DB.Query(`SELECT "me".* FROM "group" "me" WHERE 1 = 1` + e.siteCond("me") + ` ORDER BY "me"."name"`) {
		out = append(out, r)
	}
	return out
}

// DeleteMissingGroup is GADS::Group delete of a group not found: the
// permissions without a group are deleted, then the lazily created row of
// a new group
func (e *Engine) DeleteMissingGroup() {
	e.DB.Exec(`DELETE FROM "layout_group" WHERE "group_id" IS NULL`)
	e.DB.Exec(`DELETE FROM "instance_group" WHERE "group_id" IS NULL`)
	e.DB.Exec(`DELETE FROM "user_group" WHERE "group_id" IS NULL`)
	id := e.InsertSite("group", map[string]any{"name": nil})
	e.DB.Exec(`DELETE FROM "group" WHERE "id" = ?`, id)
}
