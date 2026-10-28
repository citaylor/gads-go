package web

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// decodeJSONBody is API.pm _decode_json_body: the decoded JSON object of
// the request body
func (c *Ctx) decodeJSONBody() map[string]any {
	if c.RequestContentType() != "application/json" {
		gads.Fail("Request must be of type application/json")
	}
	var body any
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil || !truthy(apiValue(body)) {
		gads.Fail("Failed to decode JSON")
	}
	m, ok := body.(map[string]any)
	if !ok {
		gads.Panic("Not a HASH reference")
	}
	return m
}

// post '/api/user_account/?:id?' (API.pm:536): _post_add_user_account,
// creating or updating a user from the user wizard
func (s *Server) hAPIUserAccount(c *Ctx) string {
	body := c.decodeJSONBody()
	logged := c.LoggedInUser()
	id, hasID := c.RouteParam("id")
	hasID = hasID && truthy(id)
	var update *gads.User
	if hasID {
		if n, ok := toInt64(id); ok {
			update = s.E.FindUser(n)
		}
		if update == nil {
			gads.Fail("User id %s not found", id)
		}
	}
	perms := logged.Permission()
	if !perms["superadmin"] && !perms["useradmin"] {
		gads.Fail("Unauthorised access")
	}
	values := map[string]any{"account_request": int64(0), "account_request_notes": apiValue(body["notes"])}
	if !truthy(values["account_request_notes"]) {
		values["account_request_notes"] = apiValue(body["account_request_notes"])
	}
	for _, k := range []string{"firstname", "surname", "email", "freetext1", "freetext2", "title", "organisation", "department_id", "team_id", "view_limits", "groups"} {
		values[k] = apiValue(body[k])
	}
	if perms["superadmin"] {
		values["permissions"] = apiValue(body["permissions"])
	}
	values["current_user"] = logged
	if hasID {
		update.UpdateUser(values)
		return c.apiSuccess("User updated successfully")
	}
	values["welcome_text"] = func(email, code string) gads.Email {
		m := s.welcomeText(c, code)
		m.To = []string{email}
		return m
	}
	s.E.CreateUser(values)
	return c.apiSuccess("User created successfully")
}

const (
	defaultWelcomeSubject = "Your new account details"
	defaultWelcomeText    = "An account for [NAME] has been created for you. Please\nclick on the following link to retrieve your password:\n\n[URL]\n"
)

// welcomeText is GADS::welcome_text: the welcome email of a new user, with
// the link to set their password
func (s *Server) welcomeText(c *Ctx, code string) gads.Email {
	row := c.Site().Row()
	name, _ := db.AsString(row["name"])
	if name == "" {
		name = "Linkspace"
	}
	subject, _ := db.AsString(row["email_welcome_subject"])
	if subject == "" {
		subject = defaultWelcomeSubject
	}
	body, _ := db.AsString(row["email_welcome_text"])
	if body == "" {
		body = defaultWelcomeText
	}
	body = strings.Replace(body, "[URL]", c.Base()+"resetpw/"+code, 1)
	body = strings.Replace(body, "[NAME]", name, 1)
	return gads.Email{Subject: subject, Text: body, HTML: text2html(body)}
}
