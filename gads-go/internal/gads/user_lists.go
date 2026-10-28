package gads

import (
	"fmt"

	"github.com/ctrlo/gads-go/internal/db"
)

// The user value lists of a site (titles, organisations, departments and
// teams: Result::Title etc.), administered on the settings pages

// siteCond is the condition of the site of a site-filtered resultset
func (e *Engine) siteCond(alias string) string {
	if e.SiteID == 0 {
		return ""
	}
	return fmt.Sprintf(` AND "%s"."site_id" = %d`, alias, e.SiteID)
}

// OrderedList is ResultSet::Title (etc.) ordered: the undeleted items of
// the site in order of name
func (e *Engine) OrderedList(table string) []db.Row {
	return e.DB.Query(`SELECT "me".* FROM "` + table + `" "me" WHERE "me"."deleted" = 0` + e.siteCond("me") + ` ORDER BY "me"."name"`)
}

// FindListItem is find on the site-filtered resultset of a list
func (e *Engine) FindListItem(table string, id any) db.Row {
	return e.DB.QueryRow(`SELECT "me".* FROM "`+table+`" "me" WHERE "me"."id" = ?`+e.siteCond("me"), id)
}

// SaveListItem is insert_or_update of a list item with its name: a new
// item (nil row) is created in the site
func (e *Engine) SaveListItem(table string, row db.Row, name any) {
	if row == nil || row["id"] == nil {
		e.InsertSite(table, map[string]any{"name": name})
		return
	}
	e.DB.UpdateMap(table, map[string]any{"name": name}, map[string]any{"id": row["id"]})
}

// DeleteListItem is delete_title (etc.): an item in use by active users
// cannot be deleted; one in use by deleted users is marked deleted. what
// is the name of the item in the error.
func (e *Engine) DeleteListItem(table, userColumn string, id any, what string) {
	count, _ := e.DB.QueryInt64(`SELECT COUNT(*) FROM "user" "me" WHERE "me"."`+userColumn+`" = ? AND "me"."deleted" IS NULL AND "me"."account_request" = 0`+e.siteCond("me"), id)
	if count == 1 {
		Fail("This %s cannot be deleted as it is in use by 1 user on the system", what)
	} else if count > 1 {
		Fail("This %s cannot be deleted as it is in use by %d users on the system", what, count)
	}
	deleted, _ := e.DB.QueryInt64(`SELECT COUNT(*) FROM "user" "me" WHERE "me"."deleted" IS NOT NULL AND "me"."`+userColumn+`" = ?`+e.siteCond("me"), id)
	if deleted > 0 {
		e.DB.UpdateMap(table, map[string]any{"deleted": 1}, map[string]any{"id": id})
	} else {
		e.DB.Exec(`DELETE FROM "`+table+`" WHERE "id" = ?`, id)
	}
}

// UpdateUserEditableFields is Site update_user_editable_fields: the user
// fields that users can edit themselves
func (e *Engine) UpdateUserEditableFields(site *Site, names []string) {
	editable := map[string]bool{}
	for _, n := range names {
		editable[n] = true
	}
	m := map[string]any{}
	for _, f := range site.UserFields() {
		if editable[f.Name] {
			m[f.Name] = int64(1)
		} else {
			m[f.Name] = int64(0)
		}
	}
	e.DB.UpdateMap("site", map[string]any{"user_editable_fields": EncodeJSON(m)}, map[string]any{"id": site.ID()})
}
