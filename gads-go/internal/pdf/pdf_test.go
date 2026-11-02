package pdf

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The cases of testdata/cases.json are made by CtrlO::PDF with the logic
// of GADS::PDFGenerator (testdata/perl_golden.pl), and their words (page,
// box and text, from pdftotext -bbox) are in testdata/perl. The words of
// Go's PDF must be the same, at the same places.

type testField struct {
	Name     string      `json:"name"`
	Value    *string     `json:"value"`
	Grade    *string     `json:"grade"`
	Selected *[][]string `json:"selected"`
}

type testCase struct {
	Name     string  `json:"name"`
	Marking  *string `json:"marking"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle"`
	Logo     string  `json:"logo"`
	Topics   []struct {
		Name   string      `json:"name"`
		Fields []testField `json:"fields"`
	} `json:"topics"`
	LastEdited  string `json:"last_edited"`
	GeneratedBy string `json:"generated_by"`
}

func (c testCase) generator(t *testing.T) *Generator {
	g := &Generator{Title: c.Title, Subtitle: c.Subtitle, LastEdited: c.LastEdited, GeneratedBy: c.GeneratedBy}
	if c.Marking != nil {
		g.Marking = *c.Marking
	}
	if c.Logo != "" {
		logo, err := base64.StdEncoding.DecodeString(c.Logo)
		if err != nil {
			t.Fatal(err)
		}
		g.Logo = logo
	}
	for _, tp := range c.Topics {
		topic := GeneratorTopic{Name: tp.Name}
		for _, f := range tp.Fields {
			gf := GeneratorField{Name: f.Name, Value: f.Value, Grade: f.Grade}
			if f.Selected != nil {
				gf.IsSelected = true
				for _, values := range *f.Selected {
					var vs []*string
					for _, v := range values {
						vs = append(vs, &v)
					}
					gf.Selected = append(gf.Selected, vs)
				}
			}
			topic.Fields = append(topic.Fields, gf)
		}
		g.Topics = append(g.Topics, topic)
	}
	return g
}

type word struct {
	page                   int
	xMin, yMin, xMax, yMax float64
	text                   string
}

var reWord = regexp.MustCompile(`<word xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">(.*)</word>`)

// pdfWords are the words of a PDF, from pdftotext -bbox
func pdfWords(t *testing.T, pdf []byte) []word {
	t.Helper()
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdftotext", "-bbox", file, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	var words []word
	page := 0
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "<page ") {
			page++
		}
		if m := reWord.FindStringSubmatch(l); m != nil {
			w := word{page: page, text: m[5]}
			w.xMin, _ = strconv.ParseFloat(m[1], 64)
			w.yMin, _ = strconv.ParseFloat(m[2], 64)
			w.xMax, _ = strconv.ParseFloat(m[3], 64)
			w.yMax, _ = strconv.ParseFloat(m[4], 64)
			words = append(words, w)
		}
	}
	return words
}

func readWords(t *testing.T, file string) []word {
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var words []word
	s := bufio.NewScanner(f)
	for s.Scan() {
		var w word
		parts := strings.SplitN(s.Text(), " ", 6)
		if len(parts) != 6 {
			continue
		}
		w.page, _ = strconv.Atoi(parts[0])
		w.xMin, _ = strconv.ParseFloat(parts[1], 64)
		w.yMin, _ = strconv.ParseFloat(parts[2], 64)
		w.xMax, _ = strconv.ParseFloat(parts[3], 64)
		w.yMax, _ = strconv.ParseFloat(parts[4], 64)
		w.text = parts[5]
		words = append(words, w)
	}
	return words
}

func TestGeneratorMatchesPerl(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext (poppler-utils) is not installed")
	}
	data, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			pdf, err := c.generator(t).Build()
			golden, _ := os.ReadFile("testdata/perl/" + c.Name + ".txt")
			if msg, ok := strings.CutPrefix(string(golden), "error: "); ok {
				if err == nil || err.Error() != strings.TrimSpace(msg) {
					t.Fatalf("error %v, want %s", err, msg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := pdfWords(t, pdf)
			want := readWords(t, "testdata/perl/"+c.Name+".txt")
			var diffs []string
			for i := 0; i < max(len(got), len(want)); i++ {
				var g, w word
				if i < len(got) {
					g = got[i]
				}
				if i < len(want) {
					w = want[i]
				}
				if g.page != w.page || g.text != w.text || math.Abs(g.xMin-w.xMin) > 0.02 || math.Abs(g.yMin-w.yMin) > 0.02 || math.Abs(g.xMax-w.xMax) > 0.02 {
					diffs = append(diffs, fmt.Sprintf("word %d: got %d %.2f %.2f %.2f %q, want %d %.2f %.2f %.2f %q",
						i, g.page, g.xMin, g.yMin, g.xMax, g.text, w.page, w.xMin, w.yMin, w.xMax, w.text))
				}
			}
			if len(diffs) > 0 {
				if len(diffs) > 10 {
					diffs = append(diffs[:10], fmt.Sprintf("... %d differences", len(diffs)))
				}
				t.Error(strings.Join(diffs, "\n"))
			}
		})
	}
}

// A word wider than its cell is an error, as PDF::Table's die
func TestTableWordTooWide(t *testing.T) {
	g := &Generator{Title: "Record 1", Topics: []GeneratorTopic{{Name: "Other", Fields: []GeneratorField{
		{Name: "MMMMMMMMMMMMMMMMMMMMMM", Value: ptr("x")},
	}}}}
	_, err := g.Build()
	want := "!!! Error !!! first word in paragraph for row 1, col 0 'MMMMMMMMMMMMMMMMMMMM' doesn't fit into empty line!"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func ptr(s string) *string { return &s }

func TestFontMetrics(t *testing.T) {
	f, err := LoadFont("regular")
	if err != nil {
		t.Fatal(err)
	}
	// PDF::Builder: int(units * 1000 / upem)
	if f.Ascender != 905 || f.Descender != -211 {
		t.Errorf("ascender %d descender %d", f.Ascender, f.Descender)
	}
	if w := f.Width("Record 12", 16); math.Abs(w-73.792) > 1e-9 {
		t.Errorf("width %v", w)
	}
	if !bytes.HasPrefix(f.data, []byte{0, 1, 0, 0}) {
		t.Error("not a TrueType font")
	}
}

func TestSplitLongWords(t *testing.T) {
	for in, want := range map[string]string{
		"short words": "short words",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghij": "ABCDEFGHIJKLMNOPQRST UVWXYZ0123456789abcd efghij",
		"exactlytwentycharsxx":                           "exactlytwentycharsxx",
	} {
		if got := splitLongWords(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
