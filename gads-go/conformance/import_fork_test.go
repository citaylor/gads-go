package conformance

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// An import on a forked engine and the application write the same SQLite
// file at once: each waits for the other's lock (busy_timeout), and every
// row of both is written
func TestImportForkedConcurrent(t *testing.T) {
	if pgParams() != "" {
		t.Skip("SQLite only")
	}
	forkImports = true
	defer func() { forkImports = false }()
	r, err := newRunner("import_fork_concurrent", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if res := r.runStep("sheet", "sheet", map[string]any{"as": "s"}); res.Error != nil {
		t.Fatal(*res.Error)
	}
	const imported, written = 300, 40
	count := func() int64 {
		n, _ := db.AsInt64(r.e.DB.QueryRow(`SELECT COUNT(*) AS "n" FROM "current"`)["n"])
		return n
	}
	before := count()
	var b strings.Builder
	b.WriteString("string1,integer1\n")
	for i := 0; i < imported; i++ {
		fmt.Fprintf(&b, "Imported %d,%d\n", i, i)
	}
	var id int64
	if err := gads.Try(func() {
		id = r.e.NewImport(r.sheet("s").Layout(), r.user(nil), []byte(b.String()), gads.ImportOptions{}).Process()
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < written; i++ {
		if err := gads.Try(func() {
			r.stepCreateRecords(map[string]any{"sheet": "s", "data": []any{map[string]any{"string1": fmt.Sprintf("Written %d", i)}}})
		}); err != nil {
			t.Fatalf("write %d during the import: %v", i, err)
		}
	}
	if !r.e.WaitBackground(time.Minute) {
		t.Fatal("the import did not end")
	}
	st := r.e.DB.QueryRow(`SELECT * FROM "import" WHERE "id" = ?`, id)
	if n, _ := db.AsInt64(st["written_count"]); n != imported {
		t.Errorf("import wrote %d rows (%v), want %d", n, st["result"], imported)
		for _, row := range r.e.DB.Query(`SELECT "errors" FROM "import_row" WHERE "import_id" = ? AND "errors" IS NOT NULL`, id) {
			t.Errorf("row error: %v", row["errors"])
		}
	}
	if n := count(); n != before+imported+written {
		t.Errorf("%d records, want %d", n, before+imported+written)
	}
}
