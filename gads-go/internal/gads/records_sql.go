package gads

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// =====================================================================
// Conditions (a small SQL::Abstract equivalent)
// =====================================================================

// cond is a WHERE clause fragment.
type cond interface{}

type condAnd []cond
type condOr []cond

// condCmp compares a field. Value may be a scalar, nil, []any (list with
// combiner) or *subquery.
type condCmp struct {
	field    string
	op       string // = != > < >= <= LIKE NOT LIKE IN NOT IN
	value    any
	combiner string // for lists: "OR" (default) or "AND"
	orNull   bool   // [ {op => v}, undef ]: ( field op v OR field IS NULL )
	ident    bool   // value is an identifier
}

type condRaw struct {
	sql  string
	args []any
}

type subquery struct {
	sql  string
	args []any
}

func condIsNull(field string) cond    { return condCmp{field: field, op: "=", value: nil} }
func condNotNull(field string) cond   { return condCmp{field: field, op: "!=", value: nil} }
func condEq(field string, v any) cond { return condCmp{field: field, op: "=", value: v} }

var condFalse = condRaw{sql: "0=1"}

func quoteField(f string) string {
	if strings.HasPrefix(f, "(") || strings.Contains(f, " ") {
		return f
	}
	return db.Q(f)
}

// renderCond renders a condition to SQL.
func renderCond(c cond, args *[]any) string {
	switch x := c.(type) {
	case nil:
		return ""
	case condAnd:
		return renderJunction([]cond(x), "AND", args)
	case condOr:
		return renderJunction([]cond(x), "OR", args)
	case []cond:
		return renderJunction(x, "AND", args)
	case condRaw:
		*args = append(*args, x.args...)
		return x.sql
	case condCmp:
		return renderCmp(x, args)
	}
	Panic("Unknown condition type %T", c)
	return ""
}

func renderJunction(cs []cond, op string, args *[]any) string {
	var parts []string
	for _, c := range cs {
		s := renderCond(c, args)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "( " + strings.Join(parts, " "+op+" ") + " )"
}

func renderCmp(c condCmp, args *[]any) string {
	f := quoteField(c.field)
	base := func(op string, v any) string {
		if v == nil {
			if op == "=" {
				return f + " IS NULL"
			}
			if op == "!=" {
				return f + " IS NOT NULL"
			}
		}
		if sq, ok := v.(*subquery); ok {
			*args = append(*args, sq.args...)
			return f + " " + op + " ( " + sq.sql + " )"
		}
		if c.ident {
			return f + " " + op + " " + quoteField(anyToString(v))
		}
		*args = append(*args, v)
		return f + " " + op + " ?"
	}
	var s string
	switch v := c.value.(type) {
	case []any:
		if c.op == "IN" || c.op == "NOT IN" {
			if len(v) == 0 {
				if c.op == "IN" {
					s = "0=1"
				} else {
					s = "1=1"
				}
			} else {
				*args = append(*args, v...)
				s = f + " " + c.op + " ( " + db.Placeholders(len(v)) + " )"
			}
		} else {
			if len(v) == 0 {
				s = "0=1"
			} else if len(v) == 1 {
				s = base(c.op, v[0])
			} else {
				comb := c.combiner
				if comb == "" {
					comb = "OR"
				}
				var parts []string
				for _, e := range v {
					parts = append(parts, base(c.op, e))
				}
				s = "( " + strings.Join(parts, " "+comb+" ") + " )"
			}
		}
	default:
		s = base(c.op, c.value)
	}
	if c.orNull {
		return "( " + s + " OR " + f + " IS NULL )"
	}
	return s
}

// =====================================================================
// Join rendering (DBIx::Class join resolution)
// =====================================================================

type joinRenderer struct {
	e      *Engine
	layout *Layout
	seen   map[string]int
	sb     strings.Builder
	args   []any
	rewind string
}

func newJoinRenderer(e *Engine, rewind string) *joinRenderer {
	return &joinRenderer{e: e, seen: map[string]int{}, rewind: rewind}
}

// relationship describes how a relationship is joined.
type relationship struct {
	table string
	on    func(foreign, self string) (string, []any)
	// sourceType of the foreign table for nested relationships
	ftype string
}

func (jr *joinRenderer) alias(rel string) string {
	jr.seen[rel]++
	n := jr.seen[rel]
	if n > 1 {
		return rel + "_" + itoa(int64(n))
	}
	return rel
}

func eqIdent(a, b string) string { return q(a) + " = " + q(b) }

// recordLaterOn builds the record_later join condition.
func (jr *joinRenderer) recordLaterOn(foreign, self string, earlier bool) (string, []any) {
	gt := ">"
	if earlier {
		gt = "<"
	}
	s := "( ( " + q(foreign+".created") + " " + gt + " " + self + ".created OR ( " + q(foreign+".created") + " = " + self + ".created AND " + q(foreign+".id") + " " + gt + " " + self + ".id ) ) AND " + q(foreign+".approval") + " = ? AND " + q(foreign+".current_id") + " = " + q(self+".current_id")
	args := []any{0}
	if jr.rewind != "" && !earlier {
		s += " AND " + q(foreign+".created") + " <= ?"
		args = append(args, jr.rewind)
	}
	return s + " )", args
}

// resolve returns the relationship for a source type and name.
func (jr *joinRenderer) resolve(stype, rel string) relationship {
	base := strings.TrimSuffix(rel, "_alternative")
	switch stype {
	case "current":
		switch base {
		case "record_single":
			return relationship{table: "record", ftype: "record", on: func(f, s string) (string, []any) { return eqIdent(f+".current_id", s+".id"), nil }}
		case "current_version":
			return relationship{table: "record", ftype: "record", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".current_version_id"), nil }}
		case "currents":
			return relationship{table: "current", ftype: "current", on: func(f, s string) (string, []any) { return eqIdent(f+".parent_id", s+".id"), nil }}
		case "deletedby":
			return relationship{table: "user", ftype: "user", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".deletedby"), nil }}
		case "linked":
			return relationship{table: "current", ftype: "current", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".linked_id"), nil }}
		case "records":
			return relationship{table: "record", ftype: "record", on: func(f, s string) (string, []any) { return eqIdent(f+".current_id", s+".id"), nil }}
		case "curvals":
			return relationship{table: "curval", ftype: "curval", on: func(f, s string) (string, []any) { return eqIdent(f+".value", s+".id"), nil }}
		}
	case "record":
		switch base {
		case "record_later":
			return relationship{table: "record", ftype: "record", on: func(f, s string) (string, []any) { return jr.recordLaterOn(f, s, false) }}
		case "record_earlier":
			return relationship{table: "record", ftype: "record", on: func(f, s string) (string, []any) { return jr.recordLaterOn(f, s, true) }}
		case "createdby":
			return relationship{table: "user", ftype: "user", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".createdby"), nil }}
		case "current":
			return relationship{table: "current", ftype: "current", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".current_id"), nil }}
		case "curvals":
			return relationship{table: "curval", ftype: "curval", on: func(f, s string) (string, []any) { return eqIdent(f+".record_id", s+".id"), nil }}
		case "strings", "intgrs", "dates", "dateranges", "ragvals", "calcvals", "enums", "people", "files":
			table := map[string]string{"strings": "string", "intgrs": "intgr", "dates": "date", "dateranges": "daterange",
				"ragvals": "ragval", "calcvals": "calcval", "enums": "enum", "people": "person", "files": "file"}[base]
			return relationship{table: table, ftype: table, on: func(f, s string) (string, []any) { return eqIdent(f+".record_id", s+".id"), nil }}
		}
		if strings.HasPrefix(base, "field") {
			id, _ := anyToInt64(strings.TrimPrefix(base, "field"))
			col := jr.layout.Column(id)
			if col == nil {
				Panic("Unknown relationship %s", rel)
			}
			if col.Type == "autocur" {
				related := col.RelatedField()
				return relationship{table: "curval", ftype: "curval", on: func(f, s string) (string, []any) {
					sub := `SELECT "record_single"."id" FROM "current" "me" LEFT JOIN "record" "record_single" ON "record_single"."current_id" = "me"."id" LEFT JOIN "record" "record_later" ON ( ( "record_later"."created" > record_single.created OR ( "record_later"."created" = record_single.created AND "record_later"."id" > record_single.id ) ) AND "record_later"."approval" = ? AND "record_later"."current_id" = "record_single"."current_id" ) WHERE ( "record_later"."id" IS NULL )`
					return "( " + q(f+".layout_id") + " = ? AND " + q(f+".record_id") + " IN ( " + sub + " ) AND " + q(f+".value") + " = " + q(s+".current_id") + " )", []any{related.ID, 0}
				}}
			}
			return relationship{table: col.Table(), ftype: col.Table(), on: func(f, s string) (string, []any) {
				return "( " + q(f+".layout_id") + " = ? AND " + q(f+".record_id") + " = " + q(s+".id") + " )", []any{col.ID}
			}}
		}
	case "enum":
		if base == "value" {
			return relationship{table: "enumval", ftype: "enumval", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".value"), nil }}
		}
	case "person":
		if base == "value" {
			return relationship{table: "user", ftype: "user", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".value"), nil }}
		}
	case "file":
		if base == "value" {
			return relationship{table: "fileval", ftype: "fileval", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".value"), nil }}
		}
	case "curval":
		switch base {
		case "value":
			return relationship{table: "current", ftype: "current", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".value"), nil }}
		case "record":
			return relationship{table: "record", ftype: "record", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".record_id"), nil }}
		case "layout":
			return relationship{table: "layout", ftype: "layout", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".layout_id"), nil }}
		}
	}
	if base == "layout" {
		return relationship{table: "layout", ftype: "layout", on: func(f, s string) (string, []any) { return eqIdent(f+".id", s+".layout_id"), nil }}
	}
	Panic("Unknown relationship %s from %s", rel, stype)
	return relationship{}
}

// render renders a list of joins from a source alias of a given type.
func (jr *joinRenderer) render(stype, salias string, joins []*joinSpec) {
	for _, j := range joins {
		if j == nil {
			continue
		}
		rel := jr.resolve(stype, j.name)
		a := jr.alias(j.name)
		on, args := rel.on(a, salias)
		jr.sb.WriteString(" LEFT JOIN " + q(rel.table) + " " + q(a) + " ON " + on)
		jr.args = append(jr.args, args...)
		if len(j.kids) > 0 {
			jr.render(rel.ftype, a, j.kids)
		}
	}
}
