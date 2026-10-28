package gads

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"

	"github.com/ctrlo/gads-go/internal/db"
)

// User is a row of the user table (GADS::Schema::Result::User).
type User struct {
	e            *Engine
	ID           int64
	Username     string
	Email        string
	Firstname    string
	Surname      string
	Value        string
	Organisation *int64
	DepartmentID *int64
	TeamID       *int64
	Title        *int64
	Freetext1    *string
	Freetext2    *string

	// dirty are the values of an update that failed validation: as DBIC
	// sets them on the row object before validating, they remain to be
	// validated again by its next update
	dirty map[string]any

	// Row is the user's database row (as loaded)
	Row db.Row

	permission map[string]bool
	hasGroup   map[int64]bool
}

var userColumns = []string{"id", "email", "username", "firstname", "surname", "freetext1", "freetext2", "organisation", "department_id", "team_id", "title", "value", "deleted"}

// FindUser loads a user by ID (nil if not found).
func (e *Engine) FindUser(id int64) *User {
	row := e.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, id)
	if row == nil {
		return nil
	}
	return e.userFromRow(row)
}

func (e *Engine) userFromRow(row db.Row) *User {
	u := &User{e: e, Row: row}
	u.ID, _ = db.AsInt64(row["id"])
	u.Username, _ = db.AsString(row["username"])
	u.Email, _ = db.AsString(row["email"])
	u.Firstname, _ = db.AsString(row["firstname"])
	u.Surname, _ = db.AsString(row["surname"])
	u.Value, _ = db.AsString(row["value"])
	u.Organisation = optInt(row["organisation"])
	u.DepartmentID = optInt(row["department_id"])
	u.TeamID = optInt(row["team_id"])
	u.Title = optInt(row["title"])
	u.Freetext1 = optStr(row["freetext1"])
	u.Freetext2 = optStr(row["freetext2"])
	return u
}

func optInt(v any) *int64 {
	if v == nil {
		return nil
	}
	i, ok := db.AsInt64(v)
	if !ok {
		return nil
	}
	return &i
}

func optStr(v any) *string {
	if v == nil {
		return nil
	}
	s, _ := db.AsString(v)
	return &s
}

// Permission returns the global permissions of the user (superadmin etc).
func (u *User) Permission() map[string]bool {
	if u.permission != nil {
		return u.permission
	}
	u.permission = map[string]bool{}
	rows := u.e.DB.Query(`SELECT "permission"."name" FROM "user_permission" "me" JOIN "permission" "permission" ON "permission"."id" = "me"."permission_id" WHERE "me"."user_id" = ?`, u.ID)
	for _, r := range rows {
		n, _ := db.AsString(r["name"])
		u.permission[n] = true
	}
	return u.permission
}

// ClearPermission clears the cached permissions.
func (u *User) ClearPermission() { u.permission = nil }

// HasGroup returns the groups the user belongs to.
func (u *User) HasGroup() map[int64]bool {
	if u.hasGroup != nil {
		return u.hasGroup
	}
	u.hasGroup = map[int64]bool{}
	for _, g := range u.e.DB.QueryInt64s(`SELECT "group_id" FROM "user_group" WHERE "user_id" = ?`, u.ID) {
		u.hasGroup[g] = true
	}
	return u.hasGroup
}

// SetGroups sets the groups of the user, as set by the logged in user (who
// can only add or remove groups they belong to, unless superadmin). As Perl,
// the cached groups of the user object are not cleared.
func (u *User) SetGroups(loggedIn *User, groups []int64) {
	allowed := func(g int64) bool {
		return loggedIn == nil || loggedIn.Permission()["superadmin"] || loggedIn.HasGroup()[g]
	}
	for _, g := range groups {
		if !allowed(g) {
			continue
		}
		if _, ok := u.e.DB.QueryInt64(`SELECT "me"."user_id" FROM "user_group" "me" WHERE ( ( "me"."group_id" = ? AND "me"."user_id" = ? ) )`, g, u.ID); !ok {
			u.e.DB.InsertMap("user_group", map[string]any{"user_id": u.ID, "group_id": g})
		}
	}
	// Delete any groups that are no longer set
	var all []any
	for _, g := range u.e.DB.QueryInt64s(`SELECT "me"."id" FROM "group" "me"`) {
		if allowed(g) {
			all = append(all, g)
		}
	}
	q := `DELETE FROM "user_group" WHERE "user_id" = ? AND ` + inList(`"group_id"`, len(all))
	args := append([]any{u.ID}, all...)
	for _, g := range groups {
		q += ` AND "group_id" != ?`
		args = append(args, g)
	}
	u.e.DB.Exec(q, args...)
}

// HasDraft reports whether the user has a draft in the instance.
func (u *User) HasDraft(instanceID int64) bool {
	_, ok := u.e.DB.QueryInt64(`SELECT "me"."id" FROM "current" "me" LEFT JOIN "curval" "curvals" ON "curvals"."value" = "me"."id" WHERE "me"."instance_id" = ? AND "me"."draftuser_id" = ? AND "curvals"."id" IS NULL LIMIT 1`, instanceID, u.ID)
	return ok
}

// HasViewLimit reports whether the user has a view limit on a table: one on
// their user account, or an overall one on the table.
func (u *User) HasViewLimit(instanceID int64) bool {
	if _, ok := u.e.DB.QueryInt64(`SELECT "me"."id" FROM "view_limit" "me" LEFT JOIN "view" "view" ON "view"."id" = "me"."view_id" WHERE "me"."user_id" = ? AND "view"."instance_id" = ? LIMIT 1`, u.ID, instanceID); ok {
		return true
	}
	_, ok := u.e.DB.QueryInt64(`SELECT "me"."id" FROM "instance" "me" WHERE "me"."id" = ? AND "me"."view_limit_id" IS NOT NULL LIMIT 1`, instanceID)
	return ok
}

// SetViewLimits sets the view limits for the user.
func (u *User) SetViewLimits(viewIDs []int64) {
	var ids []int64
	for _, v := range viewIDs {
		if v != 0 {
			ids = append(ids, v)
		}
	}
	for _, vid := range ids {
		if _, ok := u.e.DB.QueryInt64(`SELECT "id" FROM "view_limit" WHERE "user_id" = ? AND "view_id" = ?`, u.ID, vid); !ok {
			u.e.DB.Insert("view_limit", []string{"user_id", "view_id"}, u.ID, vid)
		}
	}
	if len(ids) > 0 {
		args := []any{u.ID}
		for _, id := range ids {
			args = append(args, id)
		}
		u.e.DB.Exec(`DELETE FROM "view_limit" WHERE "user_id" = ? AND "view_id" NOT IN (`+db.Placeholders(len(ids))+`)`, args...)
	} else {
		u.e.DB.Exec(`DELETE FROM "view_limit" WHERE "user_id" = ?`, u.ID)
	}
}

// OrganisationName returns the name of the user's organisation.
func (u *User) OrganisationName() string {
	return u.e.lookupName("organisation", u.Organisation)
}

// DepartmentName returns the name of the user's department.
func (u *User) DepartmentName() string { return u.e.lookupName("department", u.DepartmentID) }

// TeamName returns the name of the user's team.
func (u *User) TeamName() string { return u.e.lookupName("team", u.TeamID) }

// TitleName returns the name of the user's title.
func (u *User) TitleName() string { return u.e.lookupName("title", u.Title) }

func (e *Engine) lookupName(table string, id *int64) string {
	if id == nil {
		return ""
	}
	row := e.DB.QueryRow(`SELECT "name" FROM `+q(table)+` WHERE "id" = ?`, *id)
	if row == nil {
		return ""
	}
	s, _ := db.AsString(row["name"])
	return s
}

// UserHash returns the user as a hash of person properties (as
// HashRefInflator would produce from the user table).
func (u *User) Hash() map[string]any {
	return map[string]any{
		"id":            u.ID,
		"email":         u.Email,
		"username":      u.Username,
		"firstname":     u.Firstname,
		"surname":       u.Surname,
		"freetext1":     derefStr(u.Freetext1),
		"freetext2":     derefStr(u.Freetext2),
		"organisation":  derefInt(u.Organisation),
		"department_id": derefInt(u.DepartmentID),
		"team_id":       derefInt(u.TeamID),
		"title":         derefInt(u.Title),
		"value":         u.Value,
		"deleted":       u.Row["deleted"],
	}
}

func derefStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// userValue computes the user display value ("surname, firstname").
func userValue(firstname, surname string) string {
	return surname + ", " + firstname
}

// InsertUser inserts a user row applying GADS::Schema::Result::User
// validation (the value field is always recalculated).
func (e *Engine) InsertUser(fields map[string]any) int64 {
	m := map[string]any{}
	for k, v := range fields {
		m[k] = v
	}
	fn, _ := m["firstname"].(string)
	sn, _ := m["surname"].(string)
	m["value"] = userValue(fn, sn)
	// Created through the site's resultset
	if _, ok := m["site_id"]; !ok && e.SiteID != 0 {
		m["site_id"] = e.SiteID
	}
	if !truthy(m["username"]) {
		Fail("Username required")
	}
	if !truthy(m["email"]) {
		Fail("Email required")
	}
	validateMFAFields(m)
	for _, f := range []string{"username", "email"} {
		if _, ok := e.DB.QueryInt64(`SELECT "id" FROM "user" WHERE `+q(f)+` = ? AND "account_request" = 0 AND "deleted" IS NULL LIMIT 1`, m[f]); ok {
			Fail("%s already exists as an active user", anyToString(m[f]))
		}
	}
	return e.DB.InsertMap("user", m)
}

// UpdateUserFields updates user fields, recalculating the value.
func (e *Engine) UpdateUserFields(id int64, fields map[string]any) {
	row := e.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, id)
	if row == nil {
		Fail("User %d not found", id)
	}
	m := map[string]any{}
	for k, v := range fields {
		m[k] = v
	}
	fn := anyToString(row["firstname"])
	sn := anyToString(row["surname"])
	if v, ok := m["firstname"]; ok {
		fn = anyToString(v)
	}
	if v, ok := m["surname"]; ok {
		sn = anyToString(v)
	}
	m["value"] = userValue(fn, sn)
	merged := map[string]any{}
	for k, v := range row {
		merged[k] = v
	}
	for k, v := range m {
		merged[k] = v
	}
	validateMFAFields(merged)
	for _, f := range []string{"username", "email"} {
		v, ok := m[f]
		if !ok || anyToString(v) == anyToString(row[f]) {
			continue
		}
		if _, exists := e.DB.QueryInt64(`SELECT "id" FROM "user" WHERE `+q(f)+` = ? AND "id" != ? AND "account_request" = 0 AND "deleted" IS NULL LIMIT 1`, v, id); exists {
			Fail("%s already exists as an active user", anyToString(v))
		}
	}
	e.DB.UpdateMap("user", m, map[string]any{"id": id})
}

// GroupsViewable returns the names of the groups that the user can see for
// sharing (groups_viewable): all groups for a superadmin, otherwise the
// groups used in the tables that the user administers plus their own.
func (u *User) GroupsViewable() []string {
	if u.Permission()["superadmin"] {
		var out []string
		for _, r := range u.e.DB.Query(`SELECT "name" FROM "group" ORDER BY "id"`) {
			n, _ := db.AsString(r["name"])
			out = append(out, n)
		}
		return out
	}
	seen := map[int64]bool{}
	var ids []int64
	add := func(id int64) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range u.e.DB.QueryInt64s(`SELECT "me"."group_id" FROM "layout_group" "me" JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE "layout"."instance_id" IN (SELECT "me"."instance_id" FROM "instance_group" "me" JOIN "group" "group" ON "group"."id" = "me"."group_id" LEFT JOIN "user_group" "user_groups" ON "user_groups"."group_id" = "group"."id" WHERE "me"."permission" = ? AND "user_groups"."user_id" = ?)`, "layout", u.ID) {
		add(id)
	}
	for _, id := range u.e.DB.QueryInt64s(`SELECT "group_id" FROM "user_group" WHERE "user_id" = ?`, u.ID) {
		add(id)
	}
	var out []string
	for _, id := range ids {
		if row := u.e.DB.QueryRow(`SELECT "name" FROM "group" WHERE "id" = ?`, id); row != nil {
			n, _ := db.AsString(row["name"])
			out = append(out, n)
		}
	}
	return out
}

// EncryptionKey is the user's encryption key (encryption_key): the hash of a
// JWT of the user signed with the hash of their password.
func (u *User) EncryptionKey() string {
	b64 := base64.RawURLEncoding.EncodeToString
	input := b64([]byte(`{"typ":"JWT","alg":"HS256"}`)) + "." +
		b64([]byte(`{"sub":"`+strconv.FormatInt(u.ID, 10)+`","user":"`+u.Username+`"}`))
	password, _ := db.AsString(u.Row["password"])
	secret := sha256.Sum256([]byte(password))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(input))
	sum := sha256.Sum256([]byte(input + "." + b64(mac.Sum(nil))))
	return b64(sum[:])
}
