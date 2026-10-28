package gads

import (
	"fmt"
	"regexp"
	"strings"
)

// auditLoginChange records a login change (GADS::Audit->login_change).
func (e *Engine) auditLoginChange(user *User, description string) {
	m := map[string]any{
		"user_id":     nil,
		"description": description,
		"type":        "login_change",
		"datetime":    e.Now(),
	}
	if user != nil {
		m["user_id"] = user.ID
	}
	if e.SiteID != 0 {
		m["site_id"] = e.SiteID
	}
	e.DB.InsertMap("audit", m)
}

// perlFmt formats a value as Log::Report's __x does (undef as "undef").
func perlFmt(v any) string {
	if v == nil {
		return "undef"
	}
	return anyToString(v)
}

// activeUserExists reports whether an active user of the site has the value
// in a column (ResultSet::User->active, limited by site).
func (e *Engine) activeUserExists(column string, value any) bool {
	q := `SELECT COUNT(*) FROM "user" "me" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL AND "me".` + q(column) + ` = ?`
	if e.SiteID != 0 {
		q += ` AND "me"."site_id" = ` + itoa(e.SiteID)
	}
	n, _ := e.DB.QueryInt64(q, value)
	return n > 0
}

// CreateUser creates a user (ResultSet::User->create_user). params are as
// the Perl hash: current_user (*User), email, firstname, surname, notes,
// organisation, department_id, team_id, title, freetext1, freetext2,
// groups ([]int64), permissions ([]string), view_limits ([]int64). Welcome
// emails are not sent.
func (e *Engine) CreateUser(params map[string]any) *User {
	var user *User
	var code string
	e.DB.Txn(func() {
		if !truthy(params["email"]) {
			Fail("An email address must be specified for the user")
		}
		if params["username"] != nil {
			Panic("username is no longer accepted for create_user - use email instead")
		}
		if e.activeUserExists("email", params["email"]) {
			Fail("User %s already exists", anyToString(params["email"]))
		}
		code = e.NewToken(32)
		current, _ := params["current_user"].(*User)
		if current == nil {
			Panic(`Can't call method "id" on an undefined value`)
		}
		m := map[string]any{
			"created_by_id":         current.ID,
			"email":                 params["email"],
			"username":              params["email"],
			"resetpw":               code,
			"created":               e.Now(),
			"account_request_notes": params["notes"],
		}
		if e.SiteID != 0 {
			m["site_id"] = e.SiteID
		}
		id := e.InsertUser(m)
		user = e.FindUser(id)
		username := params["username"]
		if !truthy(username) {
			username = params["email"]
		}
		e.auditLoginChange(current, fmt.Sprintf("User created by %s, id: %d, username: %s", current.Email, id, perlFmt(username)))
		user.UpdateUser(params)
		// Delete account request user if this is a new account request
		if ar, ok := params["account_request"]; ok && truthy(ar) {
			e.DB.Exec(`DELETE FROM "user" WHERE "id" = ?`, ar)
		}
	})
	// The welcome email (send_welcome_email), with the text of the
	// welcome_text parameter (GADS::welcome_text of the web application)
	if !truthy(params["no_welcome_email"]) {
		if f, ok := params["welcome_text"].(func(email, code string) Email); ok {
			email := anyToString(params["email"])
			if email == "" {
				email = user.Email
			}
			e.SendEmail(f(email, code))
		}
	}
	return e.FindUser(user.ID)
}

// CheckCanManage fails if the user may not change or retire target.
// Hardened (H-3): only a superadmin manages a superadmin, so that a
// useradmin cannot take over a superadmin's account by changing its email.
func (u *User) CheckCanManage(target *User) {
	if u.e.Config.Hardened && target.Permission()["superadmin"] && !u.Permission()["superadmin"] {
		Fail("Only a superadmin can change or delete a superadmin")
	}
}

var reEmailValid = regexp.MustCompile(`(?i)^[=+'a-z0-9._-]+@[a-z0-9.-]+\.[a-z]{2,10}$`)

// UpdateUser updates a user from the parameters of a user form
// (Result::User->update_user).
func (u *User) UpdateUser(params map[string]any) {
	e := u.e
	p := map[string]any{}
	for k, v := range params {
		p[k] = v
	}
	e.DB.Txn(func() {
		current, _ := p["current_user"].(*User)
		if current != nil {
			current.CheckCanManage(u)
		}
		site := e.Site()
		// Set null values where required for database insertions
		for _, f := range []string{"organisation", "department_id", "team_id", "title"} {
			if !truthy(p[f]) && !site.UserFieldIsEditable(f) {
				delete(p, f)
			}
		}
		values := map[string]any{"account_request_notes": p["account_request_notes"]}
		request := false
		if ar, ok := p["account_request"]; ok && ar != nil {
			values["account_request"] = ar
			row := e.DB.QueryRow(`SELECT "account_request" FROM "user" WHERE "id" = ?`, u.ID)
			request = row != nil && rowBool(row["account_request"]) && !truthy(ar)
		}
		if request {
			e.CreateUser(p)
			e.DB.Exec(`DELETE FROM "user" WHERE "id" = ?`, u.ID)
			return
		}
		row := e.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, u.ID)
		originalUsername := anyToString(row["username"])
		for _, f := range site.UserFields() {
			v, ok := p[f.Name]
			if !ok {
				continue
			}
			values[f.Name] = v
			if f.Name == "email" {
				values["username"] = v
			}
		}
		if originalUsername != "" {
			if nu, ok := values["username"]; ok && anyToString(nu) != originalUsername {
				e.auditLoginChange(current, fmt.Sprintf("Username %s (id %d) being changed to %s", originalUsername, u.ID, anyToString(nu)))
			}
		}
		// Coerce empty view_limits, groups and permissions to remove all
		for _, f := range []string{"view_limits", "groups", "permissions"} {
			if v, ok := p[f]; ok && !truthyList(v) {
				p[f] = []any{}
			}
		}
		// The values are set in the row object (kept if the update then
		// fails, as DBIx::Class row objects are)
		for k, v := range values {
			u.Row[k] = v
		}
		e.UpdateUserFields(u.ID, values)
		if g, ok := p["groups"]; ok {
			u.SetGroups(current, int64sOf(g))
			u.hasGroup = nil
			u.HasGroup()
		}
		if perms, ok := p["permissions"]; ok {
			if current == nil || !current.Permission()["superadmin"] {
				Fail("You do not have permission to set global user permissions")
			}
			u.SetPermissions(stringsOf(perms))
			u.permission = nil
			u.Permission()
		}
		if vl, ok := p["view_limits"]; ok {
			u.SetViewLimits(int64sOf(vl))
		}
		required := site.flag("register_organisation_mandatory")
		if truthy(p["edit_own_user"]) {
			required = false
		}
		if !truthy(p["organisation"]) && required {
			Fail("Please select a %s for the user", site.OrganisationName())
		}
		required = site.flag("register_team_mandatory") && !truthy(p["edit_own_user"])
		if !truthy(p["team_id"]) && required {
			Fail("Please select a %s for the user", site.TeamName())
		}
		required = site.flag("register_department_mandatory") && !truthy(p["edit_own_user"])
		if !truthy(p["department_id"]) && required {
			Fail("Please select a %s for the user", site.DepartmentName())
		}
		if len(anyToString(p["firstname"])) > 128 {
			Fail("Forename must be less than 128 characters")
		}
		if len(anyToString(p["surname"])) > 128 {
			Fail("Surname must be less than 128 characters")
		}
		for _, f := range []struct{ key, name string }{{"organisation", "organisation"}, {"department_id", "department"}, {"team_id", "team"}} {
			if v, ok := p[f.key]; ok && v != nil && !reDigits.MatchString(anyToString(v)) {
				Fail("Invalid %s %s", f.name, anyToString(v))
			}
		}
		if !reEmailValid.MatchString(anyToString(p["email"])) {
			Fail(`The email address "%s" is invalid`, perlFmt(p["email"]))
		}
		username := p["username"]
		if !truthy(username) {
			username = p["email"]
		}
		msg := fmt.Sprintf("User updated: ID %d, username: %s", u.ID, perlFmt(username))
		if g, ok := p["groups"]; ok {
			var s []string
			for _, x := range anyList(g) {
				s = append(s, anyToString(x))
			}
			msg += ", groups: " + strings.Join(s, ", ")
		}
		if perms, ok := p["permissions"]; ok {
			msg += ", permissions: " + strings.Join(stringsOf(perms), ", ")
		}
		e.auditLoginChange(current, msg)
	})
}

// SetPermissions sets the global permissions of the user
// (Result::User->permissions).
func (u *User) SetPermissions(perms []string) {
	want := map[string]bool{}
	for _, p := range perms {
		want[p] = true
	}
	for _, perm := range []string{"useradmin", "audit", "superadmin"} {
		pid, ok := u.e.DB.QueryInt64(`SELECT "id" FROM "permission" WHERE "name" = ?`, perm)
		if !ok {
			continue
		}
		if want[perm] {
			if _, ok := u.e.DB.QueryInt64(`SELECT "id" FROM "user_permission" WHERE "user_id" = ? AND "permission_id" = ?`, u.ID, pid); !ok {
				u.e.DB.InsertMap("user_permission", map[string]any{"user_id": u.ID, "permission_id": pid})
			}
		} else {
			u.e.DB.Exec(`DELETE FROM "user_permission" WHERE "user_id" = ? AND "permission_id" = ?`, u.ID, pid)
		}
	}
}

func anyList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []int64:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out
	}
	return nil
}

// truthyList reports whether a value is true as Perl sees an array ref (any
// list, even empty, is true).
func truthyList(v any) bool {
	switch v.(type) {
	case []any, []int64, []string:
		return true
	}
	return truthy(v)
}

func int64sOf(v any) []int64 {
	var out []int64
	for _, x := range anyList(v) {
		if n, ok := anyToInt64(x); ok {
			out = append(out, n)
		}
	}
	return out
}

func stringsOf(v any) []string {
	var out []string
	for _, x := range anyList(v) {
		out = append(out, anyToString(x))
	}
	return out
}
