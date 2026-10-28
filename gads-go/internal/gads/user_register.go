package gads

import "github.com/ctrlo/gads-go/internal/db"

// UserExists is GADS::Users user_exists: whether an active user has the
// email address
func (e *Engine) UserExists(email string) bool {
	n, _ := e.DB.QueryInt64(`SELECT COUNT(*) FROM "user" "me" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL AND "me"."email" = ?`+e.siteCond("me"), email)
	return n > 0
}

// AdminEmails is the emails of GADS::Users all_admins: the active users
// with the useradmin permission, in order of surname
func (e *Engine) AdminEmails() []string {
	var out []string
	q := `SELECT "me"."id", "me"."email" FROM "user" "me"
		LEFT JOIN "user_permission" "user_permissions" ON "user_permissions"."user_id" = "me"."id"
		LEFT JOIN "permission" "permission" ON "permission"."id" = "user_permissions"."permission_id"
		WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL AND "permission"."name" = 'useradmin'` + e.siteCond("me") + `
		ORDER BY "surname"`
	seen := map[any]bool{}
	for _, r := range e.DB.Query(q) {
		if seen[r["id"]] {
			continue
		}
		seen[r["id"]] = true
		s, _ := db.AsString(r["email"])
		out = append(out, s)
	}
	return out
}

// Register is GADS::Users register: an account request is created from
// the fields of the registration form that the site uses, and the user
// administrators are emailed about it
func (e *Engine) Register(params map[string]any) {
	site := e.Site()
	email := anyToString(params["email"])
	if !EmailValid(email) {
		Fail("Please enter a valid email address")
	}
	fields := []string{"firstname", "surname", "email", "account_request_notes"}
	if site.flag("register_show_organisation") {
		fields = append(fields, "organisation")
	}
	if site.flag("register_show_department") {
		fields = append(fields, "department_id")
	}
	if site.flag("register_show_team") {
		fields = append(fields, "team_id")
	}
	if site.flag("register_show_title") {
		fields = append(fields, "title")
	}
	if site.str("register_freetext1_name") != "" {
		fields = append(fields, "freetext1")
	}
	if site.str("register_freetext2_name") != "" {
		fields = append(fields, "freetext2")
	}
	n := map[string]any{}
	for _, f := range fields {
		n[f] = params[f]
	}
	n["firstname"] = ucfirst(anyToString(n["firstname"]))
	n["surname"] = ucfirst(anyToString(n["surname"]))
	n["username"] = params["email"]
	n["account_request"] = int64(1)
	for _, f := range []string{"freetext1", "freetext2", "title", "organisation", "department_id", "team_id"} {
		if !truthy(n[f]) {
			delete(n, f)
		}
	}
	var id int64
	// Exceptions of the creation are thrown as panics
	if err := Try(func() { id = e.InsertUser(n) }); err != nil {
		msg := err.Error()
		if ge, ok := err.(*Error); ok {
			msg = ge.Msg
		}
		Panic("%s", msg)
	}
	user := e.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, id)
	name := func(table string, idv any) string {
		if idv == nil {
			return ""
		}
		r := e.DB.QueryRow(`SELECT "name" FROM `+q(table)+` WHERE "id" = ?`, idv)
		if r == nil {
			return ""
		}
		return anyToString(r["name"])
	}
	text := "A new account request has been received from the following person:\n\n"
	text += "First name: " + anyToString(n["firstname"]) + ", "
	text += "surname: " + anyToString(n["surname"]) + ", "
	text += "email: " + anyToString(n["email"]) + ", "
	if user["title"] != nil {
		text += "title: " + name("title", user["title"]) + ", "
	}
	if truthy(n["freetext1"]) {
		text += site.str("register_freetext1_name") + ": " + anyToString(n["freetext1"]) + ", "
	}
	if truthy(n["freetext2"]) {
		text += site.str("register_freetext2_name") + ": " + anyToString(n["freetext2"]) + ", "
	}
	if user["organisation"] != nil {
		text += site.str("register_organisation_name") + ": " + name("organisation", user["organisation"]) + ", "
	}
	if user["department_id"] != nil {
		text += site.str("register_department_name") + ": " + name("department", user["department_id"]) + ", "
	}
	if user["team_id"] != nil {
		text += site.str("register_team_name") + ": " + name("team", user["team_id"]) + ", "
	}
	text += "\n\n"
	text += "User notes: " + anyToString(n["account_request_notes"]) + "\n"
	m := Email{To: e.AdminEmails(), Subject: "New account request", Text: text}
	if len(m.To) == 0 {
		// The recipients are an empty list (not undefined), which
		// GADS::Email accepts
		m.To = []string{}
		if e.Emailer != nil {
			e.Emailer(m)
		}
		return
	}
	e.SendEmail(m)
}

// MatchUsers is ResultSet::User match: the active users whose first name,
// surname, email or username contain the query, as {id, name}
func (e *Engine) MatchUsers(query string) []any {
	like := "%" + query + "%"
	out := []any{}
	for _, r := range e.DB.Query(`SELECT "me"."id", "me"."firstname", "me"."surname", "me"."username" FROM "user" "me" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL AND ( "firstname" LIKE ? OR "surname" LIKE ? OR "email" LIKE ? OR "username" LIKE ? )`+e.siteCond("me"), like, like, like, like) {
		out = append(out, map[string]any{
			"id":   r["id"],
			"name": anyToString(r["surname"]) + ", " + anyToString(r["firstname"]) + " (" + anyToString(r["username"]) + ")",
		})
	}
	return out
}
