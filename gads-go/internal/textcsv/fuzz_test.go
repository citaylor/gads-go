package textcsv

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// FuzzRead reads any input to its end or its error without failing (the
// data import and the user uploads read files from users). A row can hold
// more than the input: as Text::CSV_PP, a lone CR before a quote makes
// "\r\",0000\n0" the rows [""] and [",0000\n", "0000"]
// (testdata/fuzz/FuzzRead). Seeds: the golden cases.
func FuzzRead(f *testing.F) {
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		f.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			In string `json:"in"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		f.Fatal(err)
	}
	for _, c := range golden.Cases {
		f.Add(c.In)
	}
	f.Fuzz(func(t *testing.T, in string) {
		r := NewReader(strings.NewReader(in))
		// a row takes at least one byte, but for the blank line of
		// one empty field
		for n := 0; ; n++ {
			if n > len(in)+1 {
				t.Fatalf("more rows than bytes in %q", in)
			}
			_, err := r.Read()
			if err != nil {
				if _, err2 := r.Read(); err2 != err && err != io.EOF {
					t.Fatalf("error changed: %v then %v", err, err2)
				}
				return
			}
		}
	})
}
