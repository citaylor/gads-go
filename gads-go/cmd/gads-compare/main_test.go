package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctrlo/gads-go/conformance"
)

// observe runs a scenario evaluating calc code with the Go runner and
// writes its observation as name.json into each of the directories
func observe(t *testing.T, scenarioDir, name string, dirs ...string) {
	t.Helper()
	file := filepath.Join(scenarioDir, name+".yaml")
	yaml := "name: " + name + `
steps:
- sheet:
    as: main
- lua:
    id: double
    sheet: main
    return_type: integer
    code: function evaluate (L1integer1) return L1integer1 * 2 end
    vars:
      L1integer1: 21
- lua:
    id: concat
    sheet: main
    code: function evaluate (L1string1) return L1string1 .. "!" end
    vars:
      L1string1: hello
`
	if err := os.WriteFile(file, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	res := conformance.RunFile(file)
	for _, d := range dirs {
		if err := conformance.WriteJSON(filepath.Join(d, name+".json"), res); err != nil {
			t.Fatal(err)
		}
	}
}

// edit replaces text in an observation file
func edit(t *testing.T, file, old, new string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s lacks %q", file, old)
	}
	os.WriteFile(file, []byte(strings.ReplaceAll(string(data), old, new)), 0o644)
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	perl, goDir := filepath.Join(dir, "perl"), filepath.Join(dir, "go")
	os.Mkdir(perl, 0o755)
	os.Mkdir(goDir, 0o755)
	observe(t, dir, "a_same", perl, goDir)
	observe(t, dir, "b_diff", perl, goDir)
	observe(t, dir, "c_missing", perl)
	observe(t, dir, "d_error", perl, goDir)
	edit(t, filepath.Join(goDir, "b_diff.json"), `"42"`, `"43"`)
	edit(t, filepath.Join(goDir, "b_diff.json"), `"hello!"`, `"hello?"`)
	os.WriteFile(filepath.Join(goDir, "d_error.json"), []byte("{not json"), 0o644)

	var b bytes.Buffer
	status := run([]string{"-perl", perl, "-go", goDir, "-max", "1"}, &b)
	if status != 1 {
		t.Errorf("status %d, want 1", status)
	}
	got := b.String()
	for _, want := range []string{"same  a_same", "DIFF  b_diff (2 differences)", "... 1 more",
		"MISSING c_missing (no Go result)", "ERROR d_error:", "4 scenarios compared, 1 identical, 3 different"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	// Only the scenarios named
	b.Reset()
	if status := run([]string{"-perl", perl, "-go", goDir, "a_same"}, &b); status != 0 {
		t.Errorf("a_same alone: status %d, want 0:\n%s", status, b.String())
	}
	if !strings.Contains(b.String(), "1 scenarios compared, 1 identical, 0 different") {
		t.Errorf("unexpected output:\n%s", b.String())
	}
	if status := run([]string{"-nosuchflag"}, &b); status != 2 {
		t.Errorf("bad flag: status %d, want 2", status)
	}
}
