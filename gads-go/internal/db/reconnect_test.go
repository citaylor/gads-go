package db

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// A PostgreSQL connection that the server drops is replaced outside a
// transaction, and not inside one (GADS_TEST_PG)
func TestReconnect(t *testing.T) {
	dsn := os.Getenv("GADS_TEST_PG")
	if dsn == "" {
		t.Skip("GADS_TEST_PG not set")
	}
	dsn += " dbname=postgres"
	d, err := Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	pid := func() int64 {
		v, err := d.TryQueryCol(`SELECT pg_backend_pid()`)
		if err != nil {
			t.Fatal(err)
		}
		return v[0].(int64)
	}
	kill := func(p int64) {
		if _, err := admin.Exec(`SELECT pg_terminate_backend($1)`, p); err != nil {
			t.Fatal(err)
		}
	}
	// recovers is whether a query succeeds after at most one failure (the
	// statement that finds the connection broken)
	recovers := func() bool {
		for range 2 {
			if _, err := d.TryQueryCol(`SELECT 1`); err == nil {
				return true
			}
		}
		return false
	}

	first := pid()
	kill(first)
	if !recovers() {
		t.Fatal("no reconnect after the connection was dropped")
	}
	second := pid()
	if second == first {
		t.Fatal("same backend after the reconnect")
	}

	// The health check's ping reconnects too
	kill(second)
	ctx := context.Background()
	// (the first ping may find the connection broken)
	if err := d.Ping(ctx); err != nil {
		if err := d.Ping(ctx); err != nil {
			t.Errorf("ping did not reconnect: %v", err)
		}
	}

	// In a transaction the connection is not replaced: the transaction's
	// statements must not run on another connection
	d.Begin()
	kill(pid())
	for range 3 {
		if _, err := d.TryQueryCol(`SELECT 1`); err == nil {
			t.Fatal("query in a dropped transaction succeeded")
		}
	}
	d.RollbackAll()
	if !recovers() {
		t.Error("no reconnect after the transaction ended")
	}
}
