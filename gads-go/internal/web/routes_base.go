package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// GET /ping (GADS.pm:436)
func (s *Server) hPing(c *Ctx) string {
	c.ContentType("text/plain")
	return "alive"
}

// GET /saml (GADS.pm:474)
func (s *Server) hSamlGet(c *Ctx) string {
	c.Redirect("/")
	return ""
}

// GET /login/denied (GADS.pm:508)
func (s *Server) hLoginDenied(c *Ctx) string {
	c.ForwardHome("danger", "You do not have permission to access this page", "")
	return ""
}

// GET /invalidsite (GADS.pm:2078)
func (s *Server) hInvalidSite(c *Ctx) string {
	return c.Template("invalidsite", tt.HashOf("page", "invalidsite"), nil)
}

// GET /logout (GADS.pm:1962)
func (s *Server) hLogout(c *Ctx) string {
	c.DestroySession()
	c.ForwardHome("", "", "")
	return ""
}

// forwardLastTable is _forward_last_table
func (s *Server) forwardLastTable(c *Ctx) {
	site := c.Site()
	if site == nil || !truthy(site.Row()["remember_user_location"]) {
		c.ForwardHome("", "", "")
	}
	var forward string
	if l, ok := toInt64(c.persistent()["instance_id"]); ok && l != 0 {
		if layout := s.E.NewInstances(c.LoggedInUser()).Layout(l); layout != nil {
			forward = layout.Identifier()
		}
	}
	c.ForwardHome("", "", forward)
}

// successfulLogin is _successful_login
func (s *Server) successfulLogin(c *Ctx, username, realm string) {
	c.ChangeSessionID()
	c.SessionSet("logged_in_user", username)
	c.SessionSet("logged_in_user_realm", realm)
	c.clearUserCache()
	if v, _ := c.Param("remember_me"); truthy(v) {
		c.SetCookie(&http.Cookie{Name: "remember_me", Value: url.QueryEscape(username), Path: "/", // #nosec G124 -- Secure on HTTPS and HttpOnly, as Perl
			Expires: s.E.Now().Add(60 * 24 * time.Hour), Secure: c.Scheme() == "https", HttpOnly: true})
	} else if _, ok := c.Cookie("remember_me"); ok {
		c.SetCookie(&http.Cookie{Name: "remember_me", Value: "", Path: "/", Expires: s.E.Now().Add(-24 * time.Hour)}) // #nosec G124 -- an expired, empty cookie that clears remember_me
	}
	user := c.LoggedInUser()
	s.E.Audit(map[string]any{
		"user_id":     user.ID,
		"description": "Successful login by username " + user.Username,
		"type":        "login_success",
	})
	s.E.DB.UpdateMap("user", map[string]any{"failcount": 0, "lastfail": nil}, map[string]any{"id": user.ID})
	persistent := map[string]any{}
	if ss, ok := db.AsString(user.Row["session_settings"]); ok {
		var v map[string]any
		if json.Unmarshal([]byte(ss), &v) == nil && v != nil {
			persistent = v
		}
	}
	c.SessionSet("persistent", persistent)
	if ret, ok := c.QueryParam("return_url"); ok && ret != "" {
		ret, _ = url.PathUnescape(ret)
		if ret == "/" {
			s.forwardLastTable(c)
		}
		u, err := url.Parse(ret)
		if err != nil {
			u = &url.URL{Path: ret}
		}
		c.Redirect(c.URIFor(u.Path, u.Query()))
	}
	s.forwardLastTable(c)
}

// GET/POST /login (GADS.pm:562)
func (s *Server) hLogin(c *Ctx) string {
	e := s.E
	if c.LoggedInUser() != nil {
		c.ForwardHome("", "", "")
	}
	// Authentication providers
	enabled := e.DB.Query(`SELECT * FROM "authentication" WHERE "enabled" = 1` + siteAnd(e))
	if len(enabled) == 1 {
		if v, _ := c.QueryParam("password"); !truthy(v) {
			if t, _ := db.AsString(enabled[0]["type"]); t == "saml2" {
				s.samlInitiate(c, enabled[0])
			}
		}
	}

	var errorText, errorModal string
	if v, _ := c.Param("resetpwd"); truthy(v) {
		if username, _ := c.Param("emailreset"); username != "" {
			if gads.EmailValid(username) {
				e.Audit(map[string]any{"user_id": nil, "description": "Password reset request for " + username, "type": "login_change"})
				if s.passwordResetSend(c, username) {
					c.Success("An email has been sent to your email address with a link to reset your password")
				} else {
					c.ReportError("Failed to send a password reset link. Did you enter a valid email address?")
				}
			} else {
				errorText = `"` + username + `" is not a valid email address`
				errorModal = "resetpw"
			}
		} else {
			errorText = "Please enter an email address for the password reset to be sent to"
			errorModal = "resetpw"
		}
	}

	if _, ok := c.Param("signin"); ok {
		username := c.ParamS("username")
		lastfail := e.Now().Add(-15 * time.Minute)
		q := `SELECT COUNT(*) FROM "user" "me" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL AND "me"."username" = ? AND "me"."failcount" >= 5 AND "me"."lastfail" > ?` + siteAndMe(e)
		fail, _ := e.DB.QueryInt64(q, username, db.FormatDateTime(lastfail))
		success := fail == 0 && c.authenticateUser(username, c.ParamS("password"))
		if success {
			s.successfulLogin(c, username, "dbic")
		}
		e.Audit(map[string]any{"description": "Login failure using username " + username, "type": "login_failure"})
		if u := e.FindActiveUser(username); u != nil {
			fc, _ := toInt64(u.Row["failcount"])
			e.DB.UpdateMap("user", map[string]any{"failcount": fc + 1, "lastfail": e.Now()}, map[string]any{"id": u.ID})
		}
		c.ReportError("The username or password was not recognised")
	}

	tokens := tt.HashOf(
		"titles", s.idNameList("title"),
		"organisations", s.idNameList("organisation"),
		"departments", s.idNameList("department"),
		"teams", s.idNameList("team"),
		"page", "login",
		"body_class", "p-0",
		"container_class", "login container-fluid",
		"main_class", "login__main row",
	)
	if v, ok := c.Cookie("remember_me"); ok {
		tokens.Set("username", v)
	} else {
		tokens.Set("username", nil)
	}
	if site := c.Site(); site != nil {
		tokens.Set("register_text", ttValue(site.Row()["register_text"]))
	}
	if errorText != "" {
		tokens.Set("error", errorText)
		tokens.Set("error_modal", errorModal)
	}
	return c.Template("login", tokens, nil)
}

// idNameList is GADS::Users titles/organisations/departments/teams: the
// rows ordered by name as {label_plain, value}
func (s *Server) idNameList(table string) *tt.List {
	l := tt.NewList()
	q := `SELECT "id", "name" FROM ` + db.Q(table) + ` "me" WHERE "me"."deleted" = 0` + siteAndMe(s.E) + ` ORDER BY "me"."name"`
	for _, r := range s.E.DB.Query(q) {
		l.Items = append(l.Items, tt.HashOf("label_plain", ttValue(r["name"]), "value", ttValue(r["id"])))
	}
	return l
}

func siteAnd(e *gads.Engine) string {
	if e.SiteID == 0 {
		return ""
	}
	return ` AND "site_id" = ` + itoa(e.SiteID)
}

func siteAndMe(e *gads.Engine) string {
	if e.SiteID == 0 {
		return ""
	}
	return ` AND "me"."site_id" = ` + itoa(e.SiteID)
}

func itoa(i int64) string { return fmtInt(i) }
