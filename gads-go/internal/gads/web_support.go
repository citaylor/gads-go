package gads

import (
	"regexp"

	"github.com/ctrlo/gads-go/internal/db"
)

// FindActiveUser finds a user that can log in by username (the
// Auth::Extensible DBIC provider's users resultset with its
// user_valid_conditions: not deleted and not an account request).
func (e *Engine) FindActiveUser(username string) *User {
	q := `SELECT * FROM "user" "me" WHERE "me"."username" = ? AND "me"."deleted" IS NULL AND "me"."account_request" = 0`
	if e.SiteID != 0 {
		q += ` AND "me"."site_id" = ` + itoa(e.SiteID)
	}
	rows := e.DB.Query(q, username)
	if len(rows) == 0 {
		return nil
	}
	return e.userFromRow(rows[0])
}

// Audit writes an audit row (GADS::Audit).
func (e *Engine) Audit(m map[string]any) {
	if _, ok := m["datetime"]; !ok {
		m["datetime"] = e.Now()
	}
	if e.SiteID != 0 {
		m["site_id"] = e.SiteID
	}
	e.DB.InsertMap("audit", m)
}

var emailValidRe = regexp.MustCompile(`(?i)^[=+'a-z0-9._-]+@[a-z0-9.-]+\.[a-z]{2,10}$`)

// EmailValid is GADS::Util->email_valid
func EmailValid(email string) bool { return emailValidRe.MatchString(email) }

// AnyCurrent reports whether any record exists (rset('Current')->next)
func (e *Engine) AnyCurrent() bool {
	_, ok := e.DB.QueryInt64(`SELECT "me"."id" FROM "current" "me" LIMIT 1`)
	return ok
}

// ApprovalCount is GADS::Approval->count for a layout and user
func (e *Engine) ApprovalCount(layout *Layout, user *User) int {
	return len(e.ApprovalRecords(layout, user))
}

// DefaultView is GADS::Views->default: the first of the user's views
// (shared, personal, admin), or nil
func (vs *Views) DefaultView() *View {
	all := vs.UserViewsAll()
	if len(all) == 0 || all[0].ID == 0 {
		return nil
	}
	return vs.View(all[0].ID)
}

// View is GADS::Views->view: the view if it exists in the layout, or nil
func (vs *Views) View(id int64) *View {
	if id == 0 {
		return nil
	}
	v := vs.e.LoadView(id, vs.layout)
	if !v.Exists() {
		return nil
	}
	return v
}

// Row returns the instance row of the layout
func (l *Layout) Row() db.Row { return l.rset() }

// HasTopics reports whether the layout has topics
func (l *Layout) HasTopics() bool {
	_, ok := l.e.DB.QueryInt64(`SELECT "id" FROM "topic" WHERE "instance_id" = ? LIMIT 1`, l.InstanceID)
	return ok
}

// HasGlobe reports whether the layout has a globe field
func (l *Layout) HasGlobe() bool {
	for _, c := range l.All(AllOptions{}) {
		if c.ReturnTypeFn() == "globe" {
			return true
		}
	}
	return false
}

// ShowAddRecord is Layout->show_add_record
func (l *Layout) ShowAddRecord() bool {
	if l.UserCan("layout") {
		return true
	}
	for _, c := range l.ReferredBy() {
		if col := l.Column(c.ID); col != nil && col.ShowAdd() {
			return false
		}
	}
	return true
}

// InsertSite inserts a row as GADS's site-filtered resultsets create rows:
// the site ID is set for tables that have one (unless given)
func (e *Engine) InsertSite(table string, m map[string]any) int64 {
	if _, ok := m["site_id"]; !ok && e.SiteID != 0 && db.ColumnType(table, "site_id") != "" {
		m["site_id"] = e.SiteID
	}
	return e.DB.InsertMap(table, m)
}
