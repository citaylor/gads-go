package web

import (
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/tt"
)

// any '/aup' (GADS.pm:441): the acceptable use policy
func (s *Server) hAUP(c *Ctx) string {
	if truthy(c.ParamS("accepted")) {
		// update_current_user
		s.E.DB.UpdateMap("user", map[string]any{"aup_accepted": s.E.Now()}, map[string]any{"id": c.LoggedInUser().ID})
		c.Redirect("/")
	}
	return c.Template("aup", tt.HashOf("page", "aup"), nil)
}

// get '/aup_text' (GADS.pm:454): the policy without the page layout
func (s *Server) hAUPText(c *Ctx) string {
	return c.Template("aup_text", tt.NewHash(), &TemplateOptions{NoLayout: true})
}

// any '/user_status' (GADS.pm:459): the time of the last login
func (s *Server) hUserStatus(c *Ctx) string {
	if truthy(c.ParamS("accepted")) {
		c.SessionSet("status_accepted", int64(1))
		s.forwardLastTable(c)
	}
	// logged_in_user_lastlogin: the epoch kept in the session at login
	var lastlogin any
	if n, ok := toInt64(fmtAny(c.SessionGet("logged_in_user_lastlogin"))); ok {
		lastlogin = &ttDateTime{t: time.Unix(n, 0).UTC()}
	}
	return c.Template("user_status", tt.HashOf(
		"lastlogin", lastlogin,
		"message", c.s.Config.Gads["user_status_message"],
		"page", "user_status",
	), nil)
}

// any '/settings/report_defaults/' (GADS.pm:1328)
func (s *Server) hReportDefaults(c *Ctx) string {
	site := c.Site()
	if v, _ := c.BodyParam("submit"); truthy(v) {
		var marking any
		if m, ok := c.BodyParam("security_marking"); ok {
			marking = m
		}
		s.E.DB.UpdateMap("site", map[string]any{"security_marking": marking}, map[string]any{"id": site.ID()})
		site.Row()["security_marking"] = marking
	}
	logo := int64(0)
	if l, _ := db.AsString(site.Row()["site_logo"]); l != "" {
		logo = 1
	}
	// read_security_marking
	marking, _ := db.AsString(site.Row()["security_marking"])
	if marking == "" {
		marking = s.E.Config.Header
	}
	return c.Template("layouts/page_reporting_overview", tt.HashOf(
		"page", "report_defaults",
		"page_title", "Report Defaults",
		"page_description", "In this window you can set the default values for reports.",
		"back_url", "/settings/",
		"logo", logo,
		"security_marking", ttValue(marking),
		"data_attributes", tt.HashOf("fileupload_url", "/settings/logo"),
	), nil)
}
