package autoformat

import (
	"encoding/json"
	"os"
	"testing"
)

// FuzzAutoformat formats any text (every email's plain text, which can
// hold what users typed) without failing. Seeds: the golden cases.
func FuzzAutoformat(f *testing.F) {
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		f.Fatal(err)
	}
	var cases []struct{ In string }
	if err := json.Unmarshal(b, &cases); err != nil {
		f.Fatal(err)
	}
	for _, c := range cases {
		f.Add(c.In)
	}
	f.Fuzz(func(t *testing.T, in string) {
		Autoformat(in)
	})
}
