package web

import (
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// The user administration pages of lib/GADS.pm

// ttGroups is GADS::Groups all for templates (hashes, as templates set
// keys of the group objects)
func (s *Server) ttGroups() *tt.List {
	l := tt.NewList()
	for _, g := range s.E.Groups() {
		l.Items = append(l.Items, tt.HashOf("id", ttValue(g["id"]), "name", ttValue(g["name"])))
	}
	return l
}

// ttUserValues is the values of the user fields with lists (GADS::Users
// titles, organisations, departments and teams)
func (s *Server) ttUserValues() *tt.Hash {
	list := func(table string) *tt.List {
		l := tt.NewList()
		for _, r := range s.E.OrderedList(table) {
			l.Items = append(l.Items, tt.HashOf("label_plain", ttValue(r["name"]), "value", ttValue(r["id"])))
		}
		return l
	}
	return tt.HashOf(
		"title", list("title"),
		"organisation", list("organisation"),
		"department_id", list("department"),
		"team_id", list("team"),
	)
}

// ttPermissions is GADS::Users permissions: the global permissions in
// their order
func (s *Server) ttPermissions() *tt.List {
	l := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT * FROM "permission" "me" ORDER BY "order"`) {
		l.Items = append(l.Items, newRow("permission", r))
	}
	return l
}

// any '/user_overview/' (GADS.pm:1507)
func (s *Server) hUserOverview(c *Ctx) string {
	if truthy(c.ParamS("sendemail")) {
		var emails []string
		if truthy(c.ParamS("group_ids")) {
			emails = s.E.GroupUserEmails(c.allParams().getAll("group_ids"))
		} else {
			emails = s.E.ActiveUserEmails()
		}
		if len(emails) > 0 {
			if c.Process(func() {
				s.E.SendMessage(c.ParamS("email_subject"), c.ParamS("email_text"), emails, c.LoggedInUser())
			}) {
				c.ForwardHome("success", "The message has been sent successfully", "user_overview/")
			}
		} else {
			c.ReportError("The groups selected contain no users")
		}
	}
	return c.Template("user/user_overview", tt.HashOf(
		"groups", s.ttGroups(),
		"values", s.ttUserValues(),
		"permissions", s.ttPermissions(),
		"page", "user",
	), nil)
}

// retireUser is the deletion of a user by an administrator (retire with
// the rejection email, and the audit of it); it dies as Perl calling a
// method of undef if the user does not exist
func (s *Server) retireUser(c *Ctx, id, rejectText string) bool {
	var u *gads.User
	if n, ok := toInt64(id); ok {
		u = s.E.FindSiteUser(n)
	}
	ok := c.Process(func() {
		if u == nil {
			gads.Panic(`Can't call method "retire" on an undefined value`)
		}
		c.LoggedInUser().CheckCanManage(u)
		u.Retire(true, rejectText)
	})
	if ok {
		s.E.AuditLoginChange(c.LoggedInUser(), "User ID "+id+" deleted")
	}
	return ok
}

// any '/user_requests/' (GADS.pm:1547)
func (s *Server) hUserRequests(c *Ctx) string {
	requests := tt.NewList()
	for _, u := range s.E.AccountRequests() {
		requests.Items = append(requests.Items, c.ttUser(u))
	}
	if id := c.ParamS("delete"); truthy(id) {
		if n, ok := toInt64(id); ok && n == c.LoggedInUser().ID {
			c.ForwardHome("danger", "Cannot delete current logged-in User", "")
		}
		if s.retireUser(c, id, c.ParamS("reject_reason")) {
			c.ForwardHome("success", "User has been deleted successfully", "user_requests/")
		}
	}
	return c.Template("user/user_request", tt.HashOf(
		"users", requests,
		"groups", s.ttGroups(),
		"values", s.ttUserValues(),
		"permissions", s.ttPermissions(),
		"page", "user",
	), nil)
}

// any '/user/:id' (GADS.pm:1582)
func (s *Server) hUserEdit(c *Ctx) string {
	id, _ := c.RouteParam("id")
	if !truthy(id) {
		gads.Fail("User id not available")
	}
	var edit *gads.User
	if n, ok := toInt64(id); ok {
		edit = s.E.FindActiveOrRequestUser(n)
	}
	if edit == nil {
		gads.Fail("User id %s not found", id)
	}
	if truthy(c.ParamS("submit")) {
		param := func(k string) any {
			if v, ok := c.Param(k); ok {
				return v
			}
			return nil
		}
		orUndef := func(k string) any {
			if v := c.ParamS(k); truthy(v) {
				return v
			}
			return nil
		}
		values := map[string]any{
			"current_user":          c.LoggedInUser(),
			"firstname":             param("firstname"),
			"surname":               param("surname"),
			"email":                 param("email"),
			"username":              param("email"),
			"freetext1":             param("freetext1"),
			"freetext2":             param("freetext2"),
			"title":                 orUndef("title"),
			"organisation":          orUndef("organisation"),
			"department_id":         orUndef("department_id"),
			"team_id":               orUndef("team_id"),
			"account_request":       param("account_request"),
			"account_request_notes": param("account_request_notes"),
			"view_limits":           toAnyList(c.body.getAll("view_limits")),
			"groups":                toAnyList(c.body.getAll("groups")),
		}
		if c.LoggedInUser().Permission()["superadmin"] {
			values["permissions"] = toAnyList(c.body.getAll("permissions"))
		}
		if c.Process(func() { edit.UpdateUser(values) }) {
			c.ForwardHome("success", "User has been updated successfully", "user_overview/")
		}
	} else if del := c.ParamS("delete"); truthy(del) {
		if n, ok := toInt64(del); ok && n == c.LoggedInUser().ID {
			c.ForwardHome("danger", "Cannot delete current logged-in User", "")
		}
		if s.retireUser(c, del, "") {
			c.ForwardHome("success", "User has been deleted successfully", "user_overview/")
		}
	}
	return c.Template("user/user_edit", tt.HashOf(
		"edituser", c.ttUser(edit),
		"groups", s.ttGroups(),
		"values", s.ttUserValues(),
		"permissions", s.ttPermissions(),
		"page", "user",
	), nil)
}

func toAnyList(l []string) []any {
	out := []any{}
	for _, s := range l {
		out = append(out, s)
	}
	return out
}

// any '/user_upload/' (GADS.pm:1426)
func (s *Server) hUserUpload(c *Ctx) string {
	if truthy(c.ParamS("submit")) {
		var count int
		file := c.Upload("file")
		if c.Process(func() {
			var content []byte
			if file != nil {
				content = file.Content
			}
			count = s.E.UploadUsers(content, file != nil, gads.UploadUsersOptions{
				ViewLimits:  toAnyList(c.body.getAll("view_limits")),
				Groups:      toAnyList(c.body.getAll("groups")),
				Permissions: toAnyList(c.body.getAll("permission")),
				CurrentUser: c.LoggedInUser(),
				WelcomeText: func(email, code string) gads.Email {
					m := s.welcomeText(c, code)
					m.To = []string{email}
					return m
				},
			})
		}) {
			c.ForwardHome("success", itoa(int64(count))+" users were successfully uploaded", "user_overview/")
		}
	}
	return c.Template("user/user_upload", tt.HashOf(
		"page", "user",
		"groups", s.ttGroups(),
		"permissions", s.ttPermissions(),
	), nil)
}

// any '/user_export/?' (GADS.pm:1456)
func (s *Server) hUserExport(c *Ctx) string {
	user := c.LoggedInUser()
	if v, _ := c.BodyParam("submit"); truthy(v) {
		if c.Process(func() { s.E.ExportUsers(user) }) {
			c.ForwardHome("success", "The export has been started successfully", "user_export/")
		}
	}
	if id, _ := c.QueryParam("download"); truthy(id) {
		row := s.E.DB.QueryRow(`SELECT * FROM "export" "me" WHERE "id" = ? AND "user_id" = ? LIMIT 1`, id, user.ID)
		if row == nil {
			gads.Fail("Download not found")
		}
		content, _ := db.AsString(row["content"])
		now := ""
		if t, ok := db.AsTime(row["completed"]); ok {
			now = t.Format("2006-01-02T15:04:05")
		}
		header := ""
		if h, _ := c.s.Config.Gads["header"].(string); h != "" {
			content = h + "\n" + content
			header = "-" + h
		}
		return c.SendFile([]byte(content), `text/csv; charset="utf-8"`, now+header+".csv")
	}
	if v, _ := c.BodyParam("clear"); truthy(v) {
		if c.Process(func() { s.E.DB.Exec(`DELETE FROM "export" WHERE "user_id" = ?`, user.ID) }) {
			c.ForwardHome("success", "The export has been deleted successfully", "user_export/")
		}
	}
	exports := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT * FROM "export" "me" WHERE "me"."user_id" = ? ORDER BY "me"."completed" DESC`, user.ID) {
		row := newRow("export", r)
		r := r
		row.set("success", func([]any) any {
			if r["completed"] == nil {
				return nil
			}
			return ttValue(r["content"])
		})
		exports.Items = append(exports.Items, row)
	}
	return c.Template("user/user_export", tt.HashOf(
		"exports", tt.HashOf("count", int64(len(exports.Items)), "all", exports),
		"page", "user",
	), nil)
}
