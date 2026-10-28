package web

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/cldr"
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// This file holds the adapters that present Go objects to the templates
// as the Perl objects (DBIC rows, GADS objects) that the views expect.

// ttValue converts a Go value to a template value
func ttValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case bool:
		if x {
			return int64(1)
		}
		return ""
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case *int64:
		if x == nil {
			return nil
		}
		return *x
	case *string:
		if x == nil {
			return nil
		}
		return *x
	case time.Time:
		return &ttDateTime{t: x}
	case []byte:
		return string(x)
	case map[string]any:
		h := tt.NewHash()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.Set(k, ttValue(x[k]))
		}
		return h
	case []any:
		l := tt.NewList()
		for _, i := range x {
			l.Items = append(l.Items, ttValue(i))
		}
		return l
	case []string:
		l := tt.NewList()
		for _, i := range x {
			l.Items = append(l.Items, i)
		}
		return l
	case []int64:
		l := tt.NewList()
		for _, i := range x {
			l.Items = append(l.Items, i)
		}
		return l
	case []map[string]any:
		l := tt.NewList()
		for _, i := range x {
			l.Items = append(l.Items, ttValue(i))
		}
		return l
	}
	// Any other slice is a Perl list
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice {
		l := tt.NewList()
		for i := 0; i < rv.Len(); i++ {
			l.Items = append(l.Items, ttValue(rv.Index(i).Interface()))
		}
		return l
	}
	return v
}

// toJSONValue converts a template value to a value for encoding/json
func toJSONValue(v any) any {
	switch x := v.(type) {
	case *tt.Hash:
		m := map[string]any{}
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			m[k] = toJSONValue(val)
		}
		return m
	case *tt.List:
		l := make([]any, len(x.Items))
		for i, item := range x.Items {
			l[i] = toJSONValue(item)
		}
		return l
	case tt.Stringer:
		return x.TTString()
	}
	return v
}

func toInt64(v any) (int64, bool) { return db.AsInt64(v) }

func fmtInt(i int64) string { return strconv.FormatInt(i, 10) }

// encodeJSONPerl encodes as JSON encode_json (keys are sorted here, where
// Perl's order is that of its hash)
func encodeJSONPerl(v any) string {
	return gads.EncodeJSON(v)
}

// ---------------------------------------------------------------------
// DateTime
// ---------------------------------------------------------------------

// ttDateTime is a DateTime object
type ttDateTime struct{ t time.Time }

func (d *ttDateTime) TTString() string { return d.t.Format("2006-01-02T15:04:05") }

func (d *ttDateTime) TTMethod(name string, args []any) ([]any, bool, error) {
	t := d.t
	switch name {
	case "ymd":
		sep := "-"
		if len(args) > 0 {
			sep = tt.Str(args[0])
		}
		return []any{fmt.Sprintf("%04d%s%02d%s%02d", t.Year(), sep, t.Month(), sep, t.Day())}, true, nil
	case "hms":
		sep := ":"
		if len(args) > 0 {
			sep = tt.Str(args[0])
		}
		return []any{fmt.Sprintf("%02d%s%02d%s%02d", t.Hour(), sep, t.Minute(), sep, t.Second())}, true, nil
	case "epoch":
		return []any{t.Unix()}, true, nil
	case "year":
		return []any{int64(t.Year())}, true, nil
	case "month":
		return []any{int64(t.Month())}, true, nil
	case "day", "mday":
		return []any{int64(t.Day())}, true, nil
	case "hour":
		return []any{int64(t.Hour())}, true, nil
	case "minute":
		return []any{int64(t.Minute())}, true, nil
	case "second":
		return []any{int64(t.Second())}, true, nil
	case "datetime", "iso8601":
		return []any{d.TTString()}, true, nil
	case "format_cldr":
		return []any{cldr.Format(t, tt.Str(argAt(args, 0)))}, true, nil
	case "strftime":
		return []any{strftime(t, tt.Str(argAt(args, 0)))}, true, nil
	case "replace":
		// DateTime has no replace method and TT then looks up the hash key
		// of the object, which is undef (not the scalar vmethod)
		return []any{nil}, true, nil
	}
	return nil, false, nil
}

func strftime(t time.Time, f string) string {
	r := strings.NewReplacer("%Y", fmt.Sprintf("%04d", t.Year()), "%m", fmt.Sprintf("%02d", t.Month()),
		"%d", fmt.Sprintf("%02d", t.Day()), "%H", fmt.Sprintf("%02d", t.Hour()),
		"%M", fmt.Sprintf("%02d", t.Minute()), "%S", fmt.Sprintf("%02d", t.Second()),
		"%e", fmt.Sprintf("%2d", t.Day()), "%b", t.Format("Jan"), "%B", t.Format("January"),
		"%a", t.Format("Mon"), "%A", t.Format("Monday"), "%%", "%")
	return r.Replace(f)
}

// ---------------------------------------------------------------------
// Generic database row (DBIC result object)
// ---------------------------------------------------------------------

// ttRow is a DBIC row: methods are its column accessors (datetime columns
// inflate to DateTime objects)
type ttRow struct {
	table string
	row   db.Row
	// methods override or extend the column accessors
	methods map[string]func(args []any) any
	str     func() string
}

func newRow(table string, row db.Row) *ttRow { return &ttRow{table: table, row: row} }

func (r *ttRow) col(name string) (any, bool) {
	v, ok := r.row[name]
	if !ok {
		if db.ColumnType(r.table, name) == "" {
			return nil, false
		}
		return nil, true
	}
	if v == nil {
		return nil, true
	}
	switch db.ColumnType(r.table, name) {
	case "datetime", "date", "timestamp":
		if t, ok := db.AsTime(v); ok {
			return &ttDateTime{t: t}, true
		}
	}
	return ttValue(v), true
}

func (r *ttRow) TTMethod(name string, args []any) ([]any, bool, error) {
	if f, ok := r.methods[name]; ok {
		return []any{f(args)}, true, nil
	}
	if name == "get_column" {
		v, _ := r.col(tt.Str(argAt(args, 0)))
		return []any{v}, true, nil
	}
	if v, ok := r.col(name); ok {
		return []any{v}, true, nil
	}
	return nil, false, nil
}

func (r *ttRow) TTString() string {
	if r.str != nil {
		return r.str()
	}
	return fmt.Sprintf("GADS::Schema::Result::%s=HASH(0x%x)", perlResultClass(r.table), reflect.ValueOf(r).Pointer())
}

// perlResultClass is the DBIC result class name of a table (import_row is
// ImportRow).
func perlResultClass(table string) string {
	parts := strings.Split(table, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func (r *ttRow) set(name string, f func(args []any) any) *ttRow {
	if r.methods == nil {
		r.methods = map[string]func(args []any) any{}
	}
	r.methods[name] = f
	return r
}

// ---------------------------------------------------------------------
// Request, session, config
// ---------------------------------------------------------------------

type ttRequest struct{ c *Ctx }

func (r *ttRequest) TTMethod(name string, args []any) ([]any, bool, error) {
	c := r.c
	switch name {
	case "uri", "request_uri":
		return []any{c.URI()}, true, nil
	case "path", "path_info":
		return []any{c.Path()}, true, nil
	case "base":
		return []any{c.Base()}, true, nil
	case "method":
		return []any{c.Method()}, true, nil
	case "scheme":
		return []any{c.Scheme()}, true, nil
	case "host":
		return []any{c.hostPort()}, true, nil
	case "query_string":
		return []any{c.QueryString()}, true, nil
	case "uri_for":
		return []any{c.URIFor(tt.Str(argAt(args, 0)), nil)}, true, nil
	}
	return nil, false, nil
}

type ttSession struct{ c *Ctx }

func (s *ttSession) TTMethod(name string, args []any) ([]any, bool, error) {
	v := s.c.SessionGet(name)
	if t, ok := v.(time.Time); ok {
		return []any{&ttDateTime{t: t}}, true, nil
	}
	return []any{ttValue(v)}, true, nil
}

// ttConfig is GADS::Config
type ttConfig struct{ s *Server }

func (cf *ttConfig) dateformat() string {
	if f, ok := cf.s.Config.Gads["dateformat"].(string); ok && f != "" {
		return f
	}
	return "yyyy-MM-dd"
}

// DateformatDatepicker converts the CLDR date format for the datepicker
func DateformatDatepicker(f string) string {
	f = strings.Replace(f, "eeee", "DD", 1)
	f = strings.Replace(f, "eee", "D", 1)
	f = replaceNotFollowedByM(f, "MM", "mm")
	f = replaceNotFollowedByM(f, "M", "m")
	f = strings.Replace(f, "MMMM", "MM", 1)
	f = strings.Replace(f, "MMM", "M", 1)
	return f
}

// replaceNotFollowedByM is s/$from(?!M)/$to/ (first match)
func replaceNotFollowedByM(s, from, to string) string {
	for i := 0; i+len(from) <= len(s); i++ {
		if s[i:i+len(from)] == from && (i+len(from) == len(s) || s[i+len(from)] != 'M') {
			return s[:i] + to + s[i+len(from):]
		}
	}
	return s
}

func (cf *ttConfig) TTMethod(name string, args []any) ([]any, bool, error) {
	switch name {
	case "dateformat":
		return []any{cf.dateformat()}, true, nil
	case "dateformat_datepicker":
		return []any{DateformatDatepicker(cf.dateformat())}, true, nil
	case "gads":
		return []any{ttValue(cf.s.Config.Gads)}, true, nil
	case "login_instance":
		if v, ok := toInt64(cf.s.Config.Gads["login_instance"]); ok && v != 0 {
			return []any{v}, true, nil
		}
		return []any{int64(1)}, true, nil
	case "url":
		u, _ := cf.s.Config.Gads["url"].(string)
		return []any{strings.TrimSuffix(u, "/")}, true, nil
	}
	return nil, false, nil
}

// ---------------------------------------------------------------------
// Site
// ---------------------------------------------------------------------

func (c *Ctx) ttSite(site *gads.Site) any {
	r := newRow("site", site.Row())
	r.set("organisation_name", func([]any) any { return site.OrganisationName() })
	r.set("department_name", func([]any) any { return site.DepartmentName() })
	r.set("team_name", func([]any) any { return site.TeamName() })
	r.set("organisation_name_plural", func([]any) any { return gads.Plural(site.OrganisationName()) })
	r.set("department_name_plural", func([]any) any { return gads.Plural(site.DepartmentName()) })
	r.set("team_name_plural", func([]any) any { return gads.Plural(site.TeamName()) })
	r.set("user_fields", func([]any) any {
		l := tt.NewList()
		for _, f := range site.UserFields() {
			h := tt.HashOf("name", f.Name, "description", f.Description, "type", f.Type, "placeholder", f.Placeholder)
			if f.IsRequired {
				h.Set("is_required", int64(1))
			}
			if f.UserHidden {
				h.Set("user_hidden", int64(1))
			}
			if f.Table != "" {
				h.Set("table", f.Table)
				h.Set("field", f.Field)
			}
			h.Set("editable", ttValue(f.EditableRaw))
			l.Items = append(l.Items, h)
		}
		return l
	})
	r.set("user_fields_as_string", func([]any) any { return site.UserFieldsAsString() })
	r.set("user_field_is_editable", func(args []any) any { return ttBoolEmpty(site.UserFieldIsEditable(tt.Str(argAt(args, 0)))) })
	r.set("has_main_homepage", func([]any) any { return ttBoolEmpty(site.HasMainHomepage()) })
	r.set("has_table_homepage", func([]any) any { return ttBoolEmpty(site.HasTableHomepage(c.E)) })
	return r
}

// ---------------------------------------------------------------------
// User (GADS::Schema::Result::User)
// ---------------------------------------------------------------------

func (c *Ctx) ttUser(u *gads.User) any {
	if u == nil {
		return nil
	}
	r := newRow("user", u.Row)
	r.set("permission", func([]any) any {
		h := tt.NewHash()
		perms := u.Permission()
		keys := make([]string, 0, len(perms))
		for k := range perms {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.Set(k, int64(1))
		}
		return h
	})
	// Result::User groups: the groups of the user
	r.set("groups", func([]any) any {
		l := tt.NewList()
		for _, g := range c.E.DB.Query(`SELECT "group".* FROM "user_group" "me" JOIN "group" "group" ON "group"."id" = "me"."group_id" WHERE "me"."user_id" = ? ORDER BY "me"."id"`, u.ID) {
			l.Items = append(l.Items, newRow("group", g))
		}
		return l
	})
	r.set("has_group", func([]any) any {
		h := tt.NewHash()
		for g := range u.HasGroup() {
			h.Set(fmt.Sprint(g), int64(1))
		}
		return h
	})
	r.set("need_mobile_verification", func([]any) any { return ttBoolEmpty(u.NeedMobileVerification()) })
	r.set("has_draft", func(args []any) any {
		id, _ := toInt64(argAt(args, 0))
		return ttBoolEmpty(u.HasDraft(id))
	})
	r.set("groups_viewable", func([]any) any {
		l := tt.NewList()
		for _, g := range u.GroupsViewableRows() {
			l.Items = append(l.Items, newRow("group", g))
		}
		return l
	})
	r.set("value_html", func([]any) any { return tt.EncodeEntities(u.Value) })
	// The view limits of the user, or one undefined limit (for an empty
	// selector on the user edit page)
	r.set("view_limits_with_blank", func([]any) any {
		l := tt.NewList()
		for _, vl := range c.E.DB.Query(`SELECT * FROM "view_limit" "me" WHERE "me"."user_id" = ?`, u.ID) {
			l.Items = append(l.Items, newRow("view_limit", vl))
		}
		if len(l.Items) == 0 {
			l.Items = append(l.Items, nil)
		}
		return l
	})
	return r
}
