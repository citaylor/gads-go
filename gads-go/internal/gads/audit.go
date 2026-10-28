package gads

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
)

// AuditTypes is GADS::Audit audit_types
var AuditTypes = []string{"user_action", "login_change", "login_success", "logout", "login_failure"}

// AuditFiltering is the filtering of GADS::Audit (its coerced value):
// the dates default to the last day
type AuditFiltering struct {
	Method, Type, User any
	From, To           time.Time
}

// NewAuditFiltering coerces the filtering kept in the session (strings,
// or nil for unset values)
func (e *Engine) NewAuditFiltering(m map[string]any) *AuditFiltering {
	f := &AuditFiltering{Method: m["method"], Type: m["type"], User: m["user"]}
	parse := func(v any) (time.Time, bool) {
		if !truthy(v) {
			return time.Time{}, false
		}
		return e.ParseDateTime(anyToString(v))
	}
	var ok bool
	if f.From, ok = parse(m["from"]); !ok {
		f.From = e.Now().Add(-24 * time.Hour)
	}
	if f.To, ok = parse(m["to"]); !ok {
		f.To = e.Now()
	}
	return f
}

// AuditLog is a row of the audit log
type AuditLog struct {
	ID          int64
	Type        string
	Datetime    string // gads_time
	Description any
	User        db.Row // nil without a user
}

var reAuditUserID = regexp.MustCompile(`^[0-9]+$`)

// AuditLogs is GADS::Audit logs: the logs of the filtering, newest first
func (e *Engine) AuditLogs(f *AuditFiltering) []*AuditLog {
	const sqlTime = "2006-01-02 15:04:05"
	where := []string{`( "datetime" BETWEEN ? AND ? )`}
	args := []any{f.From.Format(sqlTime), f.To.Format(sqlTime)}
	if e.SiteID != 0 {
		where = append(where, `"me"."site_id" = ?`)
		args = append(args, e.SiteID)
	}
	if truthy(f.Method) {
		where = append(where, `"method" = ?`)
		args = append(args, strings.ToUpper(anyToString(f.Method)))
	}
	if truthy(f.Type) {
		where = append(where, `"type" = ?`)
		args = append(args, f.Type)
	}
	if truthy(f.User) {
		u := anyToString(f.User)
		if !reAuditUserID.MatchString(u) {
			Fail("Invalid user ID %s", u)
		}
		where = append(where, `"user_id" = ?`)
		args = append(args, u)
	}
	rows := e.DB.Query(`SELECT "me".* FROM "audit" "me" LEFT JOIN "user" "user" ON "user"."id" = "me"."user_id" WHERE ( ( `+strings.Join(where, " AND ")+` ) ) ORDER BY "datetime" DESC`, args...)
	var out []*AuditLog
	for _, r := range rows {
		l := &AuditLog{Description: r["description"]}
		l.ID, _ = db.AsInt64(r["id"])
		l.Type, _ = db.AsString(r["type"])
		if t, ok := db.AsTime(r["datetime"]); ok {
			l.Datetime = GadsTime(t)
		}
		if r["user_id"] != nil {
			l.User = e.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, r["user_id"])
		}
		out = append(out, l)
	}
	return out
}

// GadsTime is the DateTime gads_time method of GADS: the time in London
// as strftime %e %b %Y %H:%M:%S
func GadsTime(t time.Time) string {
	t = t.In(London())
	return fmt.Sprintf("%2d %s", t.Day(), t.Format("Jan 2006 15:04:05"))
}

// AuditCSV is GADS::Audit csv. The username of each row is empty: Perl
// reads the "text" key of the user's row object.
func (e *Engine) AuditCSV(f *AuditFiltering) string {
	out := csvLine([]string{"ID", "Username", "Type", "Time", "Description"})
	for _, l := range e.AuditLogs(f) {
		out += csvLine([]string{itoa(l.ID), "", l.Type, l.Datetime, anyToString(l.Description)})
	}
	return out
}
