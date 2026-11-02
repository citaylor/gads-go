package conformance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// missingRequirements are the programs that a scenario needs (its requires
// key) and that are not installed: chromium, as GADS runs it, and
// pdftotext, which observes PDFs
func missingRequirements(t *testing.T, file string) []string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var scenario struct {
		Requires []string `yaml:"requires"`
	}
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, r := range scenario.Requires {
		found := false
		switch r {
		case "chromium":
			st, err := os.Stat("/usr/bin/chromium")
			found = err == nil && st.Mode()&0o111 != 0
		case "pdftotext":
			_, err := exec.LookPath("pdftotext")
			found = err == nil
		}
		if !found {
			missing = append(missing, r)
		}
	}
	return missing
}

// goldenDir is the directory of the Perl reference results
func goldenDir() string {
	if pgParams() != "" {
		return "../../conformance/results/perl-pg"
	}
	return "../../conformance/results/perl"
}

// TestConformance runs every scenario against the Go engine and checks:
//  1. the expectations ported from the original GADS test suite, and
//  2. parity: every observation is identical to the Perl reference
//     results in conformance/results/perl (results/perl-pg on PostgreSQL,
//     with GADS_TEST_PG set).
func TestConformance(t *testing.T) {
	scenarios, _ := filepath.Glob("../../conformance/scenarios/*.yaml")
	if len(scenarios) == 0 {
		t.Skip("no scenarios found")
	}
	for _, f := range scenarios {
		f := f
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		t.Run(name, func(t *testing.T) {
			if missing := missingRequirements(t, f); len(missing) > 0 {
				t.Skipf("needs %s", strings.Join(missing, ", "))
			}
			res := RunFile(f)
			if res.Fatal != "" {
				t.Fatalf("fatal: %s", res.Fatal)
			}
			for _, s := range res.Steps {
				for _, m := range s.Failures {
					t.Errorf("%s: %s", s.ID, m)
				}
			}
			golden := filepath.Join(goldenDir(), res.Scenario+".json")
			if _, err := os.Stat(golden); err != nil {
				t.Logf("no Perl reference result for %s", res.Scenario)
				return
			}
			tmp := filepath.Join(t.TempDir(), "go.json")
			if err := WriteJSON(tmp, res); err != nil {
				t.Fatal(err)
			}
			diffs, err := CompareFiles(golden, tmp)
			if err != nil {
				t.Fatal(err)
			}
			for i, d := range diffs {
				if i >= 25 {
					t.Errorf("... %d more differences", len(diffs)-25)
					break
				}
				t.Errorf("parity: %s", d)
			}
		})
	}
}

// TestHardened runs the scenarios of conformance/hardened, which check the
// security fixes of hardened mode (deviations from Perl, so that they have
// no Perl results): only their expectations are checked
func TestHardened(t *testing.T) {
	scenarios, _ := filepath.Glob("../../conformance/hardened/*.yaml")
	if len(scenarios) == 0 {
		t.Skip("no scenarios found")
	}
	for _, f := range scenarios {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		t.Run(name, func(t *testing.T) {
			if missing := missingRequirements(t, f); len(missing) > 0 {
				t.Skipf("needs %s", strings.Join(missing, ", "))
			}
			res := RunFile(f)
			if res.Fatal != "" {
				t.Fatalf("fatal: %s", res.Fatal)
			}
			for _, s := range res.Steps {
				for _, m := range s.Failures {
					t.Errorf("%s: %s", s.ID, m)
				}
			}
		})
	}
}
