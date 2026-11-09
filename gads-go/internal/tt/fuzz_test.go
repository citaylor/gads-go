package tt

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// FuzzProcess parses and renders any template without a Go panic or
// stack overflow (both end the server): every failure must be a TT
// exception. Templates are the application's own views, but they run on
// users' data. Seeds: testdata/cases.txt.
func FuzzProcess(f *testing.F) {
	fh, err := os.Open("testdata/cases.txt")
	if err != nil {
		f.Fatal(err)
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		f.Add(sc.Text())
	}
	f.Fuzz(func(t *testing.T, tmpl string) {
		// (a range or a repeat can ask for any amount of memory or time,
		// in Perl too)
		if len(tmpl) > 300 || strings.Contains(tmpl, "..") || strings.Contains(tmpl, "repeat") {
			return
		}
		e := NewEngine("/t")
		e.Load = func(p string) ([]byte, error) {
			if p == "/t/case.tt" {
				return []byte(tmpl), nil
			}
			return nil, os.ErrNotExist
		}
		e.Process("case.tt", HashOf("l", NewList(int64(1), "a", nil), "h", HashOf("k", "v")))
	})
}
