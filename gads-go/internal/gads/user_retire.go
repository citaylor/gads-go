package gads

import (
	"regexp"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// Retire is Result::User retire: an account request is deleted (with a
// rejection email); a user is marked deleted after removing their graphs,
// alerts, dashboards and views (with the site's deletion email)
func (u *User) Retire(sendRejectEmail bool, emailRejectText string) {
	e := u.e
	site := e.Site()
	row := e.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, u.ID)
	email, _ := db.AsString(row["email"])
	if rowBool(row["account_request"]) {
		if sendRejectEmail {
			body := orDefault(emailRejectText, orDefault(site.str("email_reject_text"), "Your account request has been rejected"))
			e.SendEmail(Email{
				Subject: orDefault(site.str("email_reject_subject"), "Account request rejected"),
				To:      []string{email},
				Text:    u.mapFields(row, body),
			})
		}
		e.DB.Exec(`DELETE FROM "user" WHERE "id" = ?`, u.ID)
		return
	}
	// Hardened (M-5): the user's API tokens are revoked (Perl keeps them)
	if e.Config.Hardened {
		e.DB.Exec(`DELETE FROM "oauthtoken" WHERE "user_id" = ?`, u.ID)
	}
	e.DB.Exec(`DELETE FROM "user_graph" WHERE "user_id" = ?`, u.ID)
	alerts := e.DB.QueryInt64s(`SELECT "id" FROM "alert" WHERE "user_id" = ?`, u.ID)
	for _, a := range alerts {
		e.DB.Exec(`DELETE FROM "alert_send" WHERE "alert_id" = ?`, a)
	}
	e.DB.Exec(`DELETE FROM "alert" WHERE "user_id" = ?`, u.ID)
	// Delete dashboards
	for _, d := range e.DB.QueryInt64s(`SELECT "id" FROM "dashboard" WHERE "user_id" = ?`, u.ID) {
		e.DB.Exec(`DELETE FROM "widget" WHERE "dashboard_id" = ?`, d)
	}
	e.DB.Exec(`DELETE FROM "dashboard" WHERE "user_id" = ?`, u.ID)
	e.DB.UpdateMap("user", map[string]any{"lastview": nil}, map[string]any{"id": u.ID})
	views := e.DB.QueryInt64s(`SELECT "id" FROM "view" WHERE "user_id" = ?`, u.ID)
	for _, t := range []string{"filter", "view_layout", "sort", "alert_cache", "alert", "view_group"} {
		for _, v := range views {
			e.DB.Exec(`DELETE FROM "`+t+`" WHERE "view_id" = ?`, v)
		}
	}
	e.DB.Exec(`DELETE FROM "view" WHERE "user_id" = ?`, u.ID)
	e.DB.UpdateMap("user", map[string]any{"deleted": e.Now()}, map[string]any{"id": u.ID})
	if msg := site.str("email_delete_text"); msg != "" {
		e.SendEmail(Email{
			Subject: orDefault(site.str("email_delete_subject"), "Account deleted"),
			To:      []string{email},
			Text:    msg,
		})
	}
}

// AuditLoginChange records a change of a login (GADS::Audit login_change)
// by a user
func (e *Engine) AuditLoginChange(user *User, description string) {
	e.auditLoginChange(user, description)
}

// ActiveUserEmails is the emails of GADS::Users all (active users in
// order of surname)
func (e *Engine) ActiveUserEmails() []string {
	var out []string
	for _, r := range e.DB.Query(`SELECT "me"."email" FROM "user" "me" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL` + e.siteCond("me") + ` ORDER BY "surname"`) {
		s, _ := db.AsString(r["email"])
		out = append(out, s)
	}
	return out
}

// GroupUserEmails is the emails of GADS::Users all_in_groups: the active
// users in any of the groups
func (e *Engine) GroupUserEmails(groupIDs []string) []string {
	if len(groupIDs) == 0 {
		return nil
	}
	args := []any{}
	for _, g := range groupIDs {
		args = append(args, g)
	}
	var out []string
	for _, r := range e.DB.Query(`SELECT DISTINCT "me"."id", "me"."email" FROM "user" "me" JOIN "user_group" "user_groups" ON "user_groups"."user_id" = "me"."id" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL AND "user_groups"."group_id" IN (?`+strings.Repeat(", ?", len(args)-1)+`)`+e.siteCond("me")+` ORDER BY "me"."id"`, args...) {
		s, _ := db.AsString(r["email"])
		out = append(out, s)
	}
	return out
}

var reTrailingSpace = regexp.MustCompile(`\s+$`)

// SendMessage is GADS::Email message: a message from a user, signed with
// their name and email, with replies to them
func (e *Engine) SendMessage(subject, text string, emails []string, user *User) {
	if len(emails) == 0 {
		return
	}
	value, _ := db.AsString(user.Row["value"])
	text = reTrailingSpace.ReplaceAllString(text, "") + "\n\nMessage sent by: " + value + " (" + user.Email + ")\n"
	e.SendEmail(Email{Subject: subject, To: emails, Text: text, ReplyTo: user.Email})
}

// FindSiteUser is find on the site's users (rset('User')->find)
func (e *Engine) FindSiteUser(id int64) *User {
	row := e.DB.QueryRow(`SELECT * FROM "user" "me" WHERE "me"."id" = ?`+e.siteCond("me"), id)
	if row == nil {
		return nil
	}
	return e.userFromRow(row)
}

// FindActiveOrRequestUser is a user not deleted, or an account request
// (active_and_requests)
func (e *Engine) FindActiveOrRequestUser(id int64) *User {
	row := e.DB.QueryRow(`SELECT * FROM "user" "me" WHERE "me"."deleted" IS NULL AND "id" = ?`+e.siteCond("me"), id)
	if row == nil {
		return nil
	}
	return e.userFromRow(row)
}

// AccountRequests is GADS::Users register_requests
func (e *Engine) AccountRequests() []*User {
	var out []*User
	for _, r := range e.DB.Query(`SELECT * FROM "user" "me" WHERE "account_request" = 1` + e.siteCond("me")) {
		out = append(out, e.userFromRow(r))
	}
	return out
}

// mapFields is Result::User _map_fields: the {firstname}, {surname},
// {email}, {title}, {organisation}, {department}, {team} and {notes}
// placeholders of a text replaced with the user's values. The
// relationship fields interpolate in Perl as the row object reference
// (whose address is not reproducible), so those are given with a fixed
// address
func (u *User) mapFields(row map[string]any, text string) string {
	if text == "" || text == "0" {
		return text
	}
	rels := map[string]string{"title": "Title", "organisation": "Organisation", "department": "Department", "team": "Team"}
	for _, f := range []string{"firstname", "surname", "email", "title", "organisation", "department", "team"} {
		var value string
		if cls, ok := rels[f]; ok {
			if row[f] != nil {
				value = "GADS::Schema::Result::" + cls + "=HASH(0x0)"
			}
		} else {
			value, _ = db.AsString(row[f])
		}
		text = strings.ReplaceAll(text, "{"+f+"}", value)
	}
	notes, _ := db.AsString(row["account_request_notes"])
	return strings.ReplaceAll(text, "{notes}", notes)
}
