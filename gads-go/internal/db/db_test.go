package db

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// The errors as GADS reports DBI exceptions of DBD::Pg
func TestPgErrorText(t *testing.T) {
	q := `SELECT 1`
	one := &pgconn.PgError{Severity: "ERROR", Message: "current transaction is aborted"}
	dup := &pgconn.PgError{Severity: "ERROR", Message: `duplicate key value violates unique constraint "user_pkey"`, Detail: "Key (id)=(1) already exists.", Code: "23505"}
	cases := []struct {
		err  *Error
		want string
	}{
		{&Error{Query: q, Err: one}, `DBI Exception: DBD::Pg::st execute failed: ERROR:  current transaction is aborted [for Statement "SELECT 1"]`},
		// Over several lines: not decoded by Log::Report
		{&Error{Query: q, Err: dup}, ""},
		// In full once the web application's exception_action is set
		{&Error{Query: q, Err: dup, Full: true}, "DBI Exception: DBD::Pg::st execute failed: ERROR:  duplicate key value violates unique constraint \"user_pkey\"\nDETAIL:  Key (id)=(1) already exists. [for Statement \"SELECT 1\"]"},
		// A bulk insert of populate
		{&Error{Query: q, Err: one, Populate: true}, ""},
		{&Error{Query: q, Err: errors.New("UNIQUE constraint failed: x.y")}, "UNIQUE constraint failed: x.y"},
	}
	for i, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("%d: %q, want %q", i, got, c.want)
		}
	}
	if !IsUniqueViolation(&Error{Query: q, Err: dup}) || IsUniqueViolation(&Error{Query: q, Err: one}) {
		t.Errorf("IsUniqueViolation of PostgreSQL errors (by SQLSTATE)")
	}
	if !IsUniqueViolation(&Error{Query: q, Err: errors.New("UNIQUE constraint failed: x.y")}) {
		t.Errorf("IsUniqueViolation of a SQLite error")
	}
}

// Dates and times are given as text, as DBD::Pg does
func TestPgTimeText(t *testing.T) {
	tm := time.Date(2014, 10, 10, 1, 2, 3, 0, time.UTC)
	frac := time.Date(2014, 10, 10, 1, 2, 3, 250000000, time.UTC)
	cases := []struct {
		t    time.Time
		typ  string
		want any
	}{
		{tm, "DATE", "2014-10-10"},
		{tm, "TIMESTAMP", "2014-10-10 01:02:03"},
		{frac, "TIMESTAMP", "2014-10-10 01:02:03.25"},
		{tm, "TIMESTAMPTZ", tm},
	}
	for _, c := range cases {
		if got := pgTimeText(c.t, c.typ); got != c.want {
			t.Errorf("pgTimeText(%v, %s) = %v, want %v", c.t, c.typ, got, c.want)
		}
	}
}

func TestPopulate(t *testing.T) {
	d, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`CREATE TABLE "pt" ("a" integer, "b" text, UNIQUE ("a"))`)
	d.Populate("pt", []map[string]any{{"a": 1, "b": "x"}, {"a": 2, "b": "y"}})
	if n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "pt"`); n != 2 {
		t.Errorf("populated %d rows", n)
	}
	d.Populate("pt", nil)
	func() {
		defer func() {
			e, ok := recover().(*Error)
			if !ok || !e.Populate || e.Error() != "" {
				t.Errorf("populate failure: %v", e)
			}
		}()
		d.Populate("pt", []map[string]any{{"a": 3, "b": "z"}, {"a": 1, "b": "dup"}})
	}()
	if n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "pt"`); n != 3 {
		t.Errorf("rows before the failure: %d", n)
	}
}
