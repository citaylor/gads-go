package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
