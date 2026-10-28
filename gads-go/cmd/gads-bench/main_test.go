package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctrlo/gads-go/internal/benchunits"
)

// capture runs the command, returning its status and output
func capture(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var b bytes.Buffer
	stdout = &b
	defer func() { stdout = os.Stdout }()
	status := run(args)
	return status, b.String()
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestUnits(t *testing.T) {
	defer benchunits.CloseFixture()
	status, out := capture(t, "units", "-time", "5ms", "date/format_cldr", "db/count")
	if status != 0 {
		t.Fatalf("status %d", status)
	}
	var f unitsFile
	if err := json.Unmarshal([]byte(out), &f); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if f.Runner != "go" || len(f.Units) != 2 {
		t.Fatalf("unexpected units %s", out)
	}
	want := map[string]string{
		"date/format_cldr": sha("Friday 10 October 2014 12:34:56"),
		// (the records of the fixture: 1000 on the main sheet)
		"db/count": sha("1000"),
	}
	for _, u := range f.Units {
		if u.OutputSHA256 != want[u.Name] {
			t.Errorf("%s: output SHA-256 %s, want %s", u.Name, u.OutputSHA256, want[u.Name])
		}
		if len(u.Samples) != benchunits.Samples || u.Iterations < 3*benchunits.Samples ||
			!(u.MinNs <= u.NsPerOp && u.NsPerOp <= u.MaxNs) || u.MinNs <= 0 {
			t.Errorf("%s: unexpected measurement %+v", u.Name, u)
		}
	}
	// To a file
	file := filepath.Join(t.TempDir(), "units.json")
	if status, _ := capture(t, "units", "-time", "1ms", "-out", file, "password/match"); status != 0 {
		t.Fatalf("status %d", status)
	}
	data, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(data), `"password/match"`) {
		t.Errorf("unexpected file (%v): %s", err, data)
	}
	if status, _ := capture(t, "units", "-out", filepath.Join(file, "x"), "password/match"); status != 1 {
		t.Errorf("unwritable output: status %d, want 1", status)
	}
}

// writeJSON writes v as JSON to the file
func writeJSON(t *testing.T, file string, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// result is a benchmark scenario's observation with the step timings
func result(name string, elapsed float64, steps ...map[string]any) map[string]any {
	return map[string]any{"scenario": name, "runner": "x", "elapsed_ms": elapsed, "steps": steps}
}

func step(id string, pass bool, median float64, observed any) map[string]any {
	return map[string]any{"id": id, "type": "search", "pass": pass, "observed": observed,
		"timing": map[string]any{"runs": 3, "total_ms": median * 3, "median_ms": median}}
}

func unit(name string, ns float64, sha string) map[string]any {
	return map[string]any{"name": name, "iterations": 10, "ns_per_op": ns, "min_ns": ns * 0.9,
		"max_ns": ns * 1.1, "output_sha256": sha}
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	perl, goDir := filepath.Join(dir, "perl"), filepath.Join(dir, "go")
	os.Mkdir(perl, 0o755)
	os.Mkdir(goDir, 0o755)
	writeJSON(t, filepath.Join(perl, "b1.json"), result("b1", 1000, step("count", true, 10, 5), step("page", true, 400, 1)))
	writeJSON(t, filepath.Join(goDir, "b1.json"), result("b1", 50, step("count", true, 1, 5), step("page", true, 20, 1)))
	unitsPerl, unitsGo := filepath.Join(dir, "up.json"), filepath.Join(dir, "ug.json")
	writeJSON(t, unitsPerl, map[string]any{"runner": "perl", "perl": "5.40.5",
		"backends": map[string]string{"json": "JSON::XS", "csv": "Text::CSV_XS"},
		"units":    []any{unit("a", 2000, "s1"), unit("b", 100, "s2")}})
	writeJSON(t, unitsGo, map[string]any{"runner": "go", "go": "go1.x",
		"units": []any{unit("a", 1000, "s1"), unit("b", 400, "s2")}})

	status, out := capture(t, "compare", "-perl", perl, "-go", goDir, "-units-perl", unitsPerl, "-units-go", unitsGo)
	if status != 0 {
		t.Errorf("status %d, want 0:\n%s", status, out)
	}
	for _, want := range []string{"count", "10x", "page", "20x", "geometric mean of steps",
		"b1", "same", "total", "a ", "2.0x", "0.2x", "±10%", "(perl 5.40.5, csv Text::CSV_XS, json JSON::XS, go1.x)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	status, out = capture(t, "compare", "-md", "-perl", perl, "-go", goDir, "-units-perl", unitsPerl, "-units-go", unitsGo)
	if status != 0 || !strings.Contains(out, "### b1") || !strings.Contains(out, "### Units") ||
		!strings.Contains(out, "| unit | perl | spread | go | spread | go faster | output |") {
		t.Errorf("Markdown: status %d:\n%s", status, out)
	}

	// A unit whose outputs differ, a failed step, a parity difference and
	// a scenario without a Go result each fail the comparison
	writeJSON(t, unitsGo, map[string]any{"runner": "go", "units": []any{unit("a", 1000, "other"), unit("b", 400, "")}})
	status, out = capture(t, "compare", "-perl", "/nonexistent", "-go", "/nonexistent", "-units-perl", unitsPerl, "-units-go", unitsGo)
	if status != 1 || !strings.Contains(out, "DIFFERENT") {
		t.Errorf("different outputs: status %d:\n%s", status, out)
	}
	writeJSON(t, filepath.Join(goDir, "b1.json"), result("b1", 50, step("count", false, 1, 6), step("page", true, 20, 1)))
	status, out = capture(t, "compare", "-perl", perl, "-go", goDir)
	if status != 1 || !strings.Contains(out, "count (FAILED)") || !strings.Contains(out, "1 diffs") {
		t.Errorf("failed step: status %d:\n%s", status, out)
	}
	writeJSON(t, filepath.Join(perl, "b2.json"), result("b2", 1))
	status, out = capture(t, "compare", "-perl", perl, "-go", goDir)
	if status != 1 || !strings.Contains(out, "b2: no Go result") {
		t.Errorf("missing result: status %d:\n%s", status, out)
	}
	if status, _ := capture(t, "compare", "-perl", "/nonexistent", "-go", "/nonexistent", "-units-perl", filepath.Join(dir, "none.json"), "-units-go", unitsGo); status != 1 {
		t.Errorf("missing units file: status %d, want 1", status)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"nosuchcommand"}, {"units", "-nosuchflag"}, {"compare", "-nosuchflag"}} {
		if status, _ := capture(t, args...); status != 2 {
			t.Errorf("%q: status %d, want 2", args, status)
		}
	}
}

func TestFormatting(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{ratio(10, 1), "10x"}, {ratio(1, 4), "0.2x"}, {ratio(0, 1), "-"},
		{msStr(5000), "5000"}, {msStr(12.34), "12.3"}, {msStr(1.234), "1.23"},
		{nsStr(512), "512 ns"}, {nsStr(1500), "1.5 µs"}, {nsStr(2.5e6), "2.50 ms"},
		{spreadStr(100, 90, 110), "±10%"}, {spreadStr(0, 0, 0), "-"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
