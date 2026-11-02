package db

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// A database deployed with schema 114 (the DDL before GADS 115, kept in
// testdata) is upgraded as GADS's migration 114-115 does it
func TestUpgrade114To115SQLite(t *testing.T) {
	d, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	testUpgrade114To115(t, d, "testdata/sql_sqlite_114.sql")
}

// The same on PostgreSQL (GADS_TEST_PG), in a database of its own
func TestUpgrade114To115Postgres(t *testing.T) {
	dsn := os.Getenv("GADS_TEST_PG")
	if dsn == "" {
		t.Skip("GADS_TEST_PG not set")
	}
	admin, err := Open("pgx", dsn+" dbname=postgres")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("gads_upgrade_test_%d", os.Getpid())
	admin.Exec(`DROP DATABASE IF EXISTS ` + name)
	admin.Exec(`CREATE DATABASE ` + name)
	defer admin.Exec(`DROP DATABASE IF EXISTS ` + name)
	d, err := Open("pgx", dsn+" dbname="+name)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	testUpgrade114To115(t, d, "testdata/sql_postgres_114.sql")
}

func testUpgrade114To115(t *testing.T, d *DB, ddl string) {
	old, err := os.ReadFile(ddl)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.deploySchema(string(old)); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO "site" ("id", "host") VALUES (1, 'localhost')`)
	d.Exec(`INSERT INTO "instance" ("id", "name", "site_id") VALUES (1, 'Table1', 1)`)
	d.Exec(`INSERT INTO "dashboard" ("id", "site_id", "instance_id") VALUES (1, 1, 1)`)
	d.Exec(`INSERT INTO "widget" ("id", "grid_id", "dashboard_id", "type", "tl_options") VALUES (1, 'a', 1, 'timeline', '{"label":"5"}')`)
	d.Exec(`INSERT INTO "widget" ("id", "grid_id", "dashboard_id", "type", "globe_options") VALUES (2, 'b', 1, 'globe', '{"group":"6"}')`)
	d.Exec(`INSERT INTO "widget" ("id", "grid_id", "dashboard_id", "type", "content") VALUES (3, 'c', 1, 'notice', 'Hello')`)

	applied, err := d.Upgrade()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(applied, []string{"114-115"}) {
		t.Fatalf("applied %v", applied)
	}
	var got []string
	for _, r := range d.Query(`SELECT "id", "options", "tl_options", "globe_options" FROM "widget" ORDER BY "id"`) {
		var parts []string
		for _, k := range []string{"id", "options", "tl_options", "globe_options"} {
			s, ok := AsString(r[k])
			if !ok {
				s = "NULL"
			}
			parts = append(parts, s)
		}
		got = append(got, strings.Join(parts, " "))
	}
	want := []string{`1 {"label":"5"} NULL NULL`, `2 {"group":"6"} NULL NULL`, `3 NULL NULL NULL`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("widgets %q, want %q", got, want)
	}
	if v, _ := AsString(d.QueryRow(`SELECT "type" FROM "dashboard" WHERE "id" = 1`)["type"]); v != "table" {
		t.Errorf("dashboard type %q", v)
	}
	if v, _ := AsString(d.QueryRow(`SELECT "record_display" FROM "instance" WHERE "id" = 1`)["record_display"]); v != "record" {
		t.Errorf("instance record_display %q", v)
	}
	// The new columns work as in a database deployed at 115
	d.Exec(`INSERT INTO "group" ("id", "name", "site_id") VALUES (1, 'G', 1)`)
	d.Exec(`INSERT INTO "dashboard" ("id", "site_id", "instance_id", "group_id", "type") VALUES (2, 1, 1, 1, 'record')`)

	// Nothing more to do
	applied, err = d.Upgrade()
	if err != nil || len(applied) != 0 {
		t.Errorf("second upgrade: %v %v", applied, err)
	}
}

// A database deployed with the current schema needs no upgrade
func TestUpgradeCurrent(t *testing.T) {
	d, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	applied, err := d.Upgrade()
	if err != nil || len(applied) != 0 {
		t.Errorf("upgrade: %v %v", applied, err)
	}
}
