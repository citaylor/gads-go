package gads

import (
	"strings"
	"testing"
	"time"
)

// working_days_add counts forward working days; a negative or huge count
// is an error (Perl never returns)
func TestWorkingDaysAdd(t *testing.T) {
	e := &Engine{}
	// Friday 2020-01-03 plus one working day is Monday 2020-01-06
	res, err := e.workingDaysAddLua([]any{float64(1578009600), float64(1), "GB", "EAW"})
	if err != nil || len(res) != 1 || anyToString(res[0]) != "1578268800" {
		t.Errorf("add 1: %v %v", res, err)
	}
	for days, want := range map[float64]string{-1: "cannot be negative", 1e9: "cannot be more than"} {
		_, err := e.workingDaysAddLua([]any{float64(1578009600), days, "GB", "EAW"})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v days: %v", days, err)
		}
	}
}

// A regex that backtracks without end times out as no match
func TestPerlRegexTimeout(t *testing.T) {
	m := perlRegex(`^(a+)+$`)
	if m == nil || !m.MatchString("aaaa") {
		t.Fatal("simple match")
	}
	start := time.Now()
	if m.MatchString(strings.Repeat("a", 40) + "b") {
		t.Error("matched")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %s", d)
	}
}
