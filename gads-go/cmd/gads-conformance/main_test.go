package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScenario writes a scenario evaluating calc code, whose expectation
// is the given return value
func writeScenario(t *testing.T, dir, name, want string) string {
	t.Helper()
	file := filepath.Join(dir, name+".yaml")
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
    expect:
      return: '` + want + `'
`
	if err := os.WriteFile(file, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestRunPassAndFail(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	pass := writeScenario(t, dir, "s_pass", "42")
	fail := writeScenario(t, dir, "s_fail", "43")
	prof := filepath.Join(dir, "cpu.prof")
	var b bytes.Buffer
	status := run([]string{"-out", out, "-verbose", "-cpuprofile", prof, pass, fail}, &b)
	if status != 1 {
		t.Errorf("status %d, want 1 (a step failed)", status)
	}
	got := b.String()
	for _, want := range []string{"ok   s_pass (2 steps)", "FAIL s_fail (1/2 steps failed)",
		"    double: ", "    [double] pass=true", "2 scenarios, 1 failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	// The observation of the passing scenario
	data, err := os.ReadFile(filepath.Join(out, "s_pass.json"))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Runner string `json:"runner"`
		Steps  []struct {
			ID       string         `json:"id"`
			Pass     bool           `json:"pass"`
			Observed map[string]any `json:"observed"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if res.Runner != "go" || len(res.Steps) != 2 || !res.Steps[1].Pass || res.Steps[1].Observed["return"] != "42" {
		t.Errorf("unexpected observation %s", data)
	}
	if st, err := os.Stat(prof); err != nil || st.Size() == 0 {
		t.Errorf("no CPU profile written (%v)", err)
	}
}

func TestRunAllPass(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if status := run([]string{"-out", filepath.Join(dir, "out"), writeScenario(t, dir, "s_pass", "42")}, &b); status != 0 {
		t.Errorf("status %d, want 0:\n%s", status, b.String())
	}
}

func TestRunFatal(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	status := run([]string{"-out", filepath.Join(dir, "out"), filepath.Join(dir, "missing.yaml")}, &b)
	if status != 1 || !strings.Contains(b.String(), "FATAL:") {
		t.Errorf("status %d, output:\n%s", status, b.String())
	}
}

func TestRunUsage(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if status := run(nil, &b); status != 2 {
		t.Errorf("no files: status %d, want 2", status)
	}
	if status := run([]string{"-nosuchflag"}, &b); status != 2 {
		t.Errorf("bad flag: status %d, want 2", status)
	}
	// The output directory cannot be created (a file is in the way)
	file := filepath.Join(dir, "file")
	os.WriteFile(file, nil, 0o644)
	if status := run([]string{"-out", filepath.Join(file, "out"), "x.yaml"}, &b); status != 2 {
		t.Errorf("bad output directory: status %d, want 2", status)
	}
	if status := run([]string{"-cpuprofile", filepath.Join(file, "p"), "x.yaml"}, &b); status != 2 {
		t.Errorf("bad profile file: status %d, want 2", status)
	}
}
