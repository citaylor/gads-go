package conformance

import (
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ctrlo/gads-go/internal/db"
)

// With GADS_TEST_PG (libpq parameters of the server, such as
// "host=/var/run/postgresql") each database of a scenario is a new
// PostgreSQL database instead of an in-memory SQLite one, dropped when the
// database is closed (as the Perl runner).
var pgCount atomic.Int64

func pgParams() string { return os.Getenv("GADS_TEST_PG") }

func pgAdmin(f func(*sql.DB) error) error {
	admin, err := sql.Open("pgx", pgParams()+" dbname=postgres")
	if err != nil {
		return err
	}
	defer admin.Close()
	return f(admin)
}

// openPG creates and opens a new database, returning it and its name
func openPG() (*db.DB, string, error) {
	name := fmt.Sprintf("gads_conf_go_%d_%d", os.Getpid(), pgCount.Add(1))
	if err := pgAdmin(func(a *sql.DB) error {
		_, err := a.Exec(`CREATE DATABASE "` + name + `"`)
		return err
	}); err != nil {
		return nil, "", err
	}
	d, err := db.Open("pgx", pgParams()+" dbname="+name)
	if err != nil {
		dropPG(name)
		return nil, "", err
	}
	return d, name, nil
}

func dropPG(name string) {
	// GADS_TEST_PG_KEEP=1 keeps the database (to serve it after a run)
	if os.Getenv("GADS_TEST_PG_KEEP") != "" {
		fmt.Fprintf(os.Stderr, "Kept database %s\n", name)
		return
	}
	if err := pgAdmin(func(a *sql.DB) error {
		_, err := a.Exec(`DROP DATABASE IF EXISTS "` + name + `"`)
		return err
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to drop database %s: %s\n", name, err)
	}
}

// syncSequencesSQL moves the sequence of each serial column past the
// largest value in its column, never back. The fixtures (DataSheet) insert
// users and other rows with explicit IDs, which leave PostgreSQL's
// sequences behind, so that a later insert would take an ID in use;
// SQLite gives the largest rowid + 1. The Perl runner runs the same SQL.
const syncSequencesSQL = `DO $$
DECLARE
  r record;
  m bigint;
  last bigint;
BEGIN
  FOR r IN SELECT s.oid::regclass::text AS seq, t.relname AS tbl, a.attname AS col
    FROM pg_class s
    JOIN pg_depend d ON d.objid = s.oid AND d.deptype = 'a'
    JOIN pg_class t ON t.oid = d.refobjid
    JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = d.refobjsubid
    WHERE s.relkind = 'S'
  LOOP
    EXECUTE format('SELECT max(%I) FROM %I', r.col, r.tbl) INTO m;
    EXECUTE format('SELECT last_value FROM %s', r.seq) INTO last;
    IF m IS NOT NULL AND m >= last THEN
      PERFORM setval(r.seq, m);
    END IF;
  END LOOP;
END
$$`

// syncSequences runs syncSequencesSQL after each step on PostgreSQL
func (r *Runner) syncSequences() {
	if r.pgName == "" || r.db == nil {
		return
	}
	if _, err := r.db.TryExec(syncSequencesSQL); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to move the sequences on: %s\n", err)
	}
}
