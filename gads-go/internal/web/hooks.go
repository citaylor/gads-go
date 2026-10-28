package web

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

var (
	csrfExemptRe = regexp.MustCompile(`^(/api/token|/print|/saml)$`)
	// Requests not audited (record views are audited by their routes)
	noAuditRe = regexp.MustCompile(`^/(record|record_body)/`)
	// Pages open to a user who has not accepted the user status
	userStatusOpenRe = regexp.MustCompile(`^/(user_status|aup|mfa)`)
)

// beforeHook is the GADS before hook (lib/GADS.pm hook before)
func (s *Server) beforeHook(c *Ctx) {
	e := s.E
	e.SiteID = 0
	nsites, _ := e.DB.QueryInt64(`SELECT COUNT(*) FROM "site"`)
	if nsites > 1 && !strings.Contains(c.Path(), "/invalidsite") {
		row := e.DB.QueryRow(`SELECT "id" FROM "site" WHERE "host" = ? ORDER BY "id" LIMIT 1`, c.Host())
		if row == nil {
			c.Redirect("/invalidsite")
		}
		id, _ := db.AsInt64(row["id"])
		e.SiteID = id
		c.SetVar("site", e.Site())
	} else {
		row := e.DB.QueryRow(`SELECT "id", "host" FROM "site" ORDER BY "id" LIMIT 1`)
		var host string
		if row != nil {
			host, _ = db.AsString(row["host"])
		}
		if c.Host() != host {
			u, _ := url.Parse(c.Base())
			if p := u.Port(); p != "" {
				u.Host = host + ":" + p
			} else {
				u.Host = host
			}
			c.Redirect(u.String())
		}
		id, _ := db.AsInt64(row["id"])
		e.SiteID = id
		c.SetVar("site", e.Site())
	}

	var user *gads.User
	if strings.HasPrefix(c.URI(), "/api/") && c.Var("api_user") != nil {
		user, _ = c.Var("api_user").(*gads.User)
	} else {
		user = c.LoggedInUser()
	}

	// Hardened: PUT, PATCH and DELETE need the token too (H-1), when made
	// in a session (an API client authenticates with its bearer token)
	checkCSRF := c.IsPost() || (e.Config.Hardened && !safeMethod(c.Method()) &&
		c.LoggedInUser() != nil && c.Var("api_user") == nil)
	if checkCSRF && !csrfExemptRe.MatchString(c.Path()) {
		var token string
		if c.RequestContentType() == "application/json" {
			var body map[string]any
			if json.Unmarshal(c.Body(), &body) == nil {
				if t, ok := body["csrf_token"].(string); ok {
					token = t
				}
			}
		}
		if token == "" {
			token, _ = c.QueryParam("csrf-token")
		}
		if token == "" {
			token, _ = c.BodyParam("csrf_token")
		}
		if token == "" {
			gads.Fail("csrf-token missing for uri %s", c.URI())
		}
		// Only checked for a logged in user (so that the API returns its
		// own error on timeout) or a login
		if c.LoggedInUser() != nil || c.Path() == "/login" {
			if st, _ := c.SessionGet("csrf_token").(string); token != st {
				gads.Fail("The CSRF token is invalid or has expired. Please try reloading the page and making the request again.")
			}
			if c.Path() == "/login" {
				c.updateCSRFToken()
			}
		}
	}

	if user != nil {
		c.SetVar("instances", e.NewInstances(user))
		if name, ok := c.RouteParam("layout_name"); ok && name != "" {
			if l := c.Instances().LayoutByShortname(name); l != nil {
				c.SetVar("layout", l)
			}
		}
	}

	if !noAuditRe.MatchString(c.Path()) {
		c.auditLog()
	}

	if c.URI() != "/aup_text" {
		c.Header("X-Frame-Options", "DENY")
	}
	if e.Config.Hardened {
		c.securityHeaders()
	}

	if logged := c.LoggedInUser(); logged != nil {
		if truthy(s.Config.Gads["aup"]) {
			accepted := false
			if t, ok := db.AsTime(logged.Row["aup_accepted"]); ok && logged.Row["aup_accepted"] != nil {
				accepted = t.After(e.Now().AddDate(0, -12, 0))
			}
			if !accepted && !strings.HasPrefix(c.URI(), "/aup") {
				c.Redirect("/aup")
			}
		}
		if truthy(s.Config.Gads["user_status"]) && !truthy(c.SessionGet("status_accepted")) {
			if !userStatusOpenRe.MatchString(c.URI()) {
				c.Redirect("/user_status")
			}
		} else if c.passwordExpired() && !truthy(c.SessionGet("is_sso")) {
			if c.URI() != "/myaccount" && c.URI() != "/logout" && c.URI() != "/mfa" {
				c.ForwardHome("danger", "Your password has expired. Please use the Change password button\n                below to set a new password.", "myaccount")
			}
		}
		if !e.Config.Hardened {
			c.Header("Content-Security-Policy", "script-src 'self';")
		}
		persistent := c.persistent()
		if v, _ := c.Param("instance"); truthy(v) {
			c.SessionSet("search", nil)
		}
		if l := c.Layout(); l != nil {
			persistent["instance_id"] = l.InstanceID
		}
		if len(c.Instances().All()) == 0 {
			c.Notice("You do not have permission to access any part of this application. Please contact your system administrator.")
		}
	}
}

// auditLog is _audit_log
func (c *Ctx) auditLog() {
	user := c.LoggedInUser()
	if user == nil {
		return
	}
	desc := `User "` + user.Username + `" made "` + c.Method() + `" request to "` + c.Path() + `"`
	if q := c.QueryString(); q != "" {
		desc += ` with query "` + q + `"`
	}
	m := map[string]any{
		"user_id":     user.ID,
		"description": desc,
		"type":        "user_action",
		"method":      c.Method(),
		"url":         c.Path(),
		"instance_id": nil,
	}
	if l := c.Layout(); l != nil {
		m["instance_id"] = l.InstanceID
	}
	c.E.Audit(m)
}

// Instances is var 'instances' (created if needed, as for templates)
func (c *Ctx) Instances() *gads.Instances {
	if is, ok := c.Var("instances").(*gads.Instances); ok {
		return is
	}
	is := c.E.NewInstances(c.LoggedInUser())
	return is
}

// Layout is var 'layout'
func (c *Ctx) Layout() *gads.Layout {
	l, _ := c.Var("layout").(*gads.Layout)
	return l
}

// Site is var 'site'
func (c *Ctx) Site() *gads.Site {
	s, _ := c.Var("site").(*gads.Site)
	return s
}

// persistent is session('persistent') (an empty hash when not set: Perl
// autovivifies a hash that is not stored)
func (c *Ctx) persistent() map[string]any {
	p, ok := c.SessionGet("persistent").(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return p
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != "" && x != "0"
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	}
	return true
}
