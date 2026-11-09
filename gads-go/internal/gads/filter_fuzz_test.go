package gads

import (
	"testing"
	"time"
)

// FuzzParseDateFilter parses any filter value (from views and the API)
// without failing
func FuzzParseDateFilter(f *testing.F) {
	for _, s := range []string{"CURDATE", "CURDATE\n", "CURDATE - 86400", "60 + CURDATE + 60",
		"CURDATE + 10000000000", "CURDATE + 00", "CURDATE-1e400", "CURDATE + 9223372036854775807", "CURDATE+-1"} {
		f.Add(s)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, in string) {
		ParseDateFilter(now, in)
	})
}

// FuzzFilterJSON reads any filter JSON (a view's, or the API's) into its
// rules and back without failing
func FuzzFilterJSON(f *testing.F) {
	for _, s := range []string{
		`{}`, `{"rules":[{"id":"3","operator":"equal","value":"a"}],"condition":"AND"}`,
		`{"rules":[{"rules":[{"id":"2_5","value":["x",1]}]},{"id":4}],"condition":"OR"}`,
		`{"rules":"x"}`, `[]`, `{"id":null}`, `{"rules":[{"id":1.5e300}]}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, js string) {
		fl := NewFilterJSON(js, nil)
		fl.Filters()
		fl.ColumnIDs()
		fl.AsJSON()
	})
}
