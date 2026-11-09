package cldr

import (
	"testing"
	"time"
)

// FuzzParse parses any value with any pattern (the values typed in date
// fields, with the site's date format) without failing. Seeds: the
// golden cases.
func FuzzParse(f *testing.F) {
	g := loadGolden(f)
	for _, c := range g.Parse {
		f.Add(c.Value, c.Pattern)
	}
	f.Fuzz(func(t *testing.T, value, pattern string) {
		Parse(value, pattern)
	})
}

// FuzzFormat formats a time with any pattern without failing
func FuzzFormat(f *testing.F) {
	g := loadGolden(f)
	for _, c := range g.Format {
		for p := range c.Out {
			f.Add(int64(1412944496), p)
		}
	}
	f.Fuzz(func(t *testing.T, sec int64, pattern string) {
		Format(time.Unix(sec%(1<<40), 0).UTC(), pattern)
	})
}
