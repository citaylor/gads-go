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
	if err := pgAdmin(func(a *sql.DB) error {
		_, err := a.Exec(`DROP DATABASE IF EXISTS "` + name + `"`)
		return err
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to drop database %s: %s\n", name, err)
	}
}
