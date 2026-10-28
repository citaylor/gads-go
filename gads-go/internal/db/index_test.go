package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The record_later join of a search, as the records queries build it
const recordLaterQuery = `SELECT COUNT(*) FROM "current" "me" LEFT JOIN "record" "record_single" ON "record_single"."current_id" = "me"."id" LEFT JOIN "record" "record_later" ON ( ( "record_later"."created" > record_single.created OR ( "record_later"."created" = record_single.created AND "record_later"."id" > record_single.id ) ) AND "record_later"."approval" = ? AND "record_later"."current_id" = "record_single"."current_id" ) WHERE "me"."instance_id" = ? AND "record_later"."current_id" IS NULL`

func recordLaterPlan(t *testing.T, d *DB) string {
	t.Helper()
	rows := d.Query("EXPLAIN QUERY PLAN "+recordLaterQuery, 0, 1)
	var plan []string
	for _, r := range rows {
		if s, _ := r["detail"].(string); strings.Contains(s, "record_later") {
			plan = append(plan, s)
		}
	}
	return strings.Join(plan, "; ")
}

// Later versions of a record are found by record (not through the approval
// index, which made searches quadratic in the number of records)
func TestRecordLaterIndex(t *testing.T) {
	d, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	if plan := recordLaterPlan(t, d); !strings.Contains(plan, "record_idx_current_id_approval_created") {
		t.Errorf("record_later plan: %s", plan)
	}
}

// A database deployed without the index gains it when opened
func TestRecordLaterIndexAdded(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gads.db")
	d, err := Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.conn.ExecContext(context.Background(), `DROP INDEX "record_idx_current_id_approval_created"`); err != nil {
		t.Fatal(err)
	}
	if plan := recordLaterPlan(t, d); !strings.Contains(plan, "record_idx_approval") {
		t.Fatalf("expected the slow plan without the index: %s", plan)
	}
	d.Close()
	d, err = Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if plan := recordLaterPlan(t, d); !strings.Contains(plan, "record_idx_current_id_approval_created") {
		t.Errorf("record_later plan after reopening: %s", plan)
	}
}
