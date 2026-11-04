package autoformat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The cases of testdata/golden.json come from Text::Autoformat itself
// (conformance/perl/gen_autoformat_golden.pl)
func TestGolden(t *testing.T) {
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		In, Out, Error string
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	failed := 0
	for i, c := range cases {
		if c.Error != "" {
			t.Errorf("case %d: Perl failed: %s", i, c.Error)
			continue
		}
		if got := Autoformat(c.In); got != c.Out {
			failed++
			if failed <= 5 {
				t.Errorf("case %d: Autoformat(%q)\n got %q\nwant %q", i, c.In, got, c.Out)
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d cases differ", failed, len(cases))
	}
}

// An email of 2000 changed fields as one paragraph: each word is taken
// from the rest of the paragraph without scanning it again (that took 36 s)
func BenchmarkAutoformatLongParagraph(b *testing.B) {
	text := strings.Repeat("Field changed: old value to new value for record 1234\n", 2000)
	for b.Loop() {
		Autoformat(text)
	}
}
