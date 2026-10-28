package gads

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// The SQL of GADS::Helper::Concat (helper_concat, helper_least,
// helper_greatest) and DBIx::Class::Helper::ResultSet::DateMethods1
// (dt_SQL_pluck), which differ between SQLite and PostgreSQL

func (e *Engine) pg() bool { return e.DB != nil && e.DB.Dialect == db.Postgres }

// sqlConcat is helper_concat of SQL expressions
func (e *Engine) sqlConcat(exprs ...string) string {
	if e.pg() {
		return "CONCAT( " + strings.Join(exprs, ", ") + " )"
	}
	var fields []string
	for _, x := range exprs {
		fields = append(fields, "COALESCE("+x+", '')")
	}
	return strings.Join(fields, " || ")
}

// sqlLeast is helper_least of SQL expressions (SQLite's MIN() of several
// values is NULL if any one is, so those are given a default)
func (e *Engine) sqlLeast(exprs ...string) string {
	if e.pg() {
		return "LEAST( " + strings.Join(exprs, ", ") + " )"
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	return "MIN( " + coalesceAll(exprs, "9999-12-31") + " )"
}

// sqlGreatest is helper_greatest of SQL expressions
func (e *Engine) sqlGreatest(exprs ...string) string {
	if e.pg() {
		return "GREATEST( " + strings.Join(exprs, ", ") + " )"
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	return "MAX( " + coalesceAll(exprs, "0000-01-01") + " )"
}

func coalesceAll(exprs []string, def string) string {
	var out []string
	for _, x := range exprs {
		out = append(out, "COALESCE("+x+", '"+def+"')")
	}
	return strings.Join(out, ", ")
}

// sqlPluck is dt_SQL_pluck of a date expression: its year, month or day
// of the month
func (e *Engine) sqlPluck(expr, part string) string {
	if e.pg() {
		pgPart := map[string]string{"year": "year", "month": "month", "day": "day"}[part]
		return "date_part('" + pgPart + "', " + expr + ")"
	}
	f := map[string]string{"year": "%Y", "month": "%m", "day": "%d"}[part]
	return "STRFTIME('" + f + "', " + expr + ")"
}
