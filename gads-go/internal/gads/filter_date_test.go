package gads

import (
	"testing"
	"time"
)

// The expected values are Perl's (parse_date_filter, DateTime)
func TestParseDateFilter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		in   string
		ok   bool
		want string
	}{
		{"CURDATE", true, "2026-01-01T00:00:00"},
		{"CURDATE\n", true, "2026-01-01T00:00:00"},
		{"CURDATE\n\n", false, ""},
		{"CURDATE\n+ 1", false, ""},
		{"\tCURDATE - 86400 ", false, ""},
		{"CURDATE\t- 86400 ", true, "2025-12-31T00:00:00"},
		{"60 + CURDATE + 60", true, "2026-01-01T00:02:00"},
		{"CURDATE + 10000000000", true, "2342-11-21T17:46:40"},
		{"CURDATE + 00", true, "2026-01-01T00:00:00"},
	} {
		got, ok := ParseDateFilter(now, c.in)
		if ok != c.ok || ok && got.Format("2006-01-02T15:04:05") != c.want {
			t.Errorf("%q: %v %v, want %v %s", c.in, got, ok, c.ok, c.want)
		}
	}
}
