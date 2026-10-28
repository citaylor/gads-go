package web

import (
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// loginPageTokens are the tokens of the pages using the login layout
func (s *Server) loginPageTokens(c *Ctx, page string) *tt.Hash {
	h := tt.HashOf(
		"titles", s.idNameList("title"),
		"organisations", s.idNameList("organisation"),
		"departments", s.idNameList("department"),
		"teams", s.idNameList("team"),
		"page", page,
		"body_class", "p-0",
		"container_class", "login container-fluid",
		"main_class", "login__main row",
	)
	if site := c.Site(); site != nil {
		h.Set("register_text", ttValue(site.Row()["register_text"]))
	} else {
		h.Set("register_text", nil)
	}
	return h
}

func (c *Ctx) siteName() string {
	if site := c.Site(); site != nil {
		if n, _ := db.AsString(site.Row()["name"]); n != "" {
			return n
		}
	}
	return "Linkspace"
}

// GET/POST /register (GADS.pm:674)
func (s *Server) hRegister(c *Ctx) string {
	var errText string
	if truthy(c.ParamS("register")) {
		if truthy(c.Site().Row()["hide_account_request"]) {
			gads.Fail("Self-service account requests are not enabled on this site")
		}
		email := c.ParamS("email")
		if s.E.UserExists(email) {
			code := s.E.NewToken(32)
			user := s.E.FindActiveUser(email)
			s.E.DB.UpdateMap("user", map[string]any{"resetpw": code}, map[string]any{"id": user.ID})
			m := s.welcomeText(c, code)
			m.To = []string{email}
			if c.Process(func() { s.E.SendEmail(m) }) {
				// Show same message as normal request
				c.ForwardHome("success", "Your account request has been received successfully", "")
			}
			s.E.AuditLoginChange(nil, "Account request for "+email+". Account already existed, resending welcome email.")
			c.ForwardHome("success", "Your account request has been received successfully", "")
		}
		params := map[string]any{}
		for _, k := range []string{"firstname", "surname", "email", "account_request_notes", "organisation", "department_id", "team_id", "title", "freetext1", "freetext2"} {
			if v, ok := c.Param(k); ok {
				params[k] = v
			}
		}
		err := gads.Try(func() { s.E.Register(params) })
		if ge, ok := err.(*gads.Error); ok && (ge.Level == gads.LevelError || ge.Level == "") {
			errText = ge.Msg
		} else if err != nil {
			panic(err)
		} else {
			s.E.AuditLoginChange(nil, "New user account request for "+email)
			c.ForwardHome("success", "Your account request has been received successfully", "")
		}
	}
	tokens := s.loginPageTokens(c, "register")
	tokens.Set("error", errText)
	return c.Template("register", tokens, nil)
}

// GET/POST /myaccount (GADS.pm:748)
func (s *Server) hMyAccount(c *Ctx) string {
	user := c.LoggedInUser()
	if truthy(c.ParamS("newpassword")) {
		pw := s.E.RandomPassword()
		username, _ := c.SessionGet("logged_in_user").(string)
		if c.authenticateUser(username, c.ParamS("oldpassword")) {
			s.setUserPassword(username, pw)
			s.E.AuditLoginChange(user, "New password set for user")
			c.ForwardHome("success", "Your password has been changed to: "+pw, "myaccount")
		} else {
			c.ForwardHome("danger", "The existing password entered is incorrect", "myaccount")
		}
	}
	if truthy(c.ParamS("submit")) {
		update := map[string]any{}
		for _, f := range c.Site().UserFields() {
			if !f.Editable {
				continue
			}
			if v := c.ParamS(f.Name); truthy(v) {
				update[f.Name] = v
			} else {
				update[f.Name] = nil
			}
		}
		update["current_user"] = c.LoggedInUser()
		update["edit_own_user"] = int64(1)
		if c.Process(func() { user.UpdateUser(update) }) {
			c.ForwardHome("success", "The account details have been updated", "myaccount")
		}
	}
	return c.Template("user/my_account", tt.HashOf(
		"user", c.ttUser(user),
		"page", "myaccount",
		"values", tt.HashOf(
			"title", s.idNameList("title"),
			"organisation", s.idNameList("organisation"),
			"department_id", s.idNameList("department"),
			"team_id", s.idNameList("team"),
		),
	), nil)
}

// setUserPassword is set_user_password of the DBIC provider: the password
// of the first active user with the username is set, with the time changed
func (s *Server) setUserPassword(username, password string) {
	u := s.E.FindActiveUser(username)
	if u == nil {
		return
	}
	s.E.DB.UpdateMap("user", map[string]any{"password": HashPassword(password), "pwchanged": s.E.Now()}, map[string]any{"id": u.ID})
}

// userByResetCode is get_user_by_code of the DBIC provider: the active
// user with the password reset code
func (s *Server) userByResetCode(code string) *gads.User {
	if code == "" {
		return nil
	}
	id, ok := s.E.DB.QueryInt64(`SELECT "me"."id" FROM "user" "me" WHERE "me"."deleted" IS NULL AND "me"."account_request" = 0 AND "me"."resetpw" = ?`+siteAndMe(s.E)+` LIMIT 1`, code)
	if !ok {
		return nil
	}
	return s.E.FindUser(id)
}

// passwordResetSend is password_reset_send of Auth::Extensible: a reset
// code is set for the user and the reset email (GADS::reset_text) sent.
// The result is always defined (0 when there is no such user).
func (s *Server) passwordResetSend(c *Ctx, username string) bool {
	code := s.E.NewToken(32)
	u := s.E.FindActiveUser(username)
	if u == nil {
		return true
	}
	s.E.DB.UpdateMap("user", map[string]any{"resetpw": code}, map[string]any{"id": u.ID})
	name := c.siteName()
	url := c.Base() + "resetpw/" + code
	body := "A request to reset your " + name + " password has been received. Please\nclick on the following link to set and retrieve a new password:\n\n" + url + "\n"
	html := "<p>A request to reset your " + name + " password has been received. Please\nclick on the following link to set and retrieve a new password:</p>\n\n<p><a href=\"" + url + "\">" + url + "<a></p>\n"
	email, _ := db.AsString(u.Row["email"])
	if s.E.Emailer != nil {
		s.E.Emailer(gads.Email{Subject: "Password reset request", To: []string{email}, Text: gads.TextWrap(body), HTML: html})
	}
	return true
}

// GET/POST /resetpw (GADS.pm:1967)
func (s *Server) hResetPW(c *Ctx) string {
	if c.LoggedInUser() != nil {
		c.ForwardHome("", "", "")
	}
	var errText string
	if _, ok := c.Param("resetpwd"); ok {
		if username := c.ParamS("emailreset"); username != "" {
			if gads.EmailValid(username) {
				s.E.AuditLoginChange(nil, "Password reset request for "+username)
				if s.passwordResetSend(c, username) {
					c.Success("An email has been sent to your email address with a link to reset your password")
				} else {
					c.ReportError("Failed to send a password reset link. Did you enter a valid email address?")
				}
			} else {
				errText = `"` + username + `" is not a valid email address`
			}
		} else {
			errText = "Please enter an email address for the password reset to be sent to"
		}
	}
	tokens := s.loginPageTokens(c, "reset")
	tokens.Set("error", errText)
	return c.Template("reset_password_request", tokens, nil)
}

// GET/POST /resetpw/:code (GADS.pm:2017)
func (s *Server) hResetPWCode(c *Ctx) string {
	if c.LoggedInUser() != nil {
		c.DestroySession()
		c.updateCSRFToken()
	}
	code, _ := c.RouteParam("code")
	user := s.userByResetCode(code)
	if user == nil {
		c.ForwardHome("danger", "The password reset code is not valid. Please request a new one\n                using the \"Reset Password\" link", "login")
	}
	tokens := tt.HashOf(
		"site_name", c.siteName(),
		"page", "reset",
		"body_class", "p-0",
		"container_class", "login container-fluid",
		"main_class", "login__main row",
	)
	if _, ok := c.Param("execute_reset"); ok {
		c.DestroySession()
		username, _ := db.AsString(user.Row["username"])
		active := s.E.FindActiveUser(username)
		s.E.DB.UpdateMap("user", map[string]any{"failcount": int64(0)}, map[string]any{"id": active.ID})
		s.E.AuditLoginChange(active, "Password reset performed for user ID "+fmtInt(active.ID))
		pw := s.E.RandomPassword()
		if u := s.userByResetCode(code); u != nil {
			name, _ := db.AsString(u.Row["username"])
			s.setUserPassword(name, pw)
			// Stop reset code being reused
			if a := s.E.FindActiveUser(name); a != nil {
				s.E.DB.UpdateMap("user", map[string]any{"resetpw": nil}, map[string]any{"id": a.ID})
			}
			c.DestroySession()
		}
		c.updateCSRFToken()
		tokens.Set("password", pw)
		return c.Template("reset_password_mail_generate", tokens, nil)
	}
	// Default pw reset landing page to prevent invalidating the one time
	// use pw reset link, if the page is scanned
	return c.Template("reset_password_mail_landing", tokens, nil)
}
