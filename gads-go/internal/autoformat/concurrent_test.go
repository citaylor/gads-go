package autoformat

import (
	"sync"
	"testing"
)

// Autoformat runs in goroutines at once (the mailer and the engines of a
// pool) with the result of one at a time; postRx's cache is shared (run
// with -race)
func TestConcurrent(t *testing.T) {
	texts := []string{
		"(a) one\n(a) two\n[1] three\n<1> four",
		"1. First\n2. Second that is long enough to be wrapped at the right margin of seventy two columns",
		"> quoted\n> again\n\nPS: a note",
	}
	want := make([]string, len(texts))
	for i, s := range texts {
		want[i] = Autoformat(s)
	}
	rePostClose.Clear()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, s := range texts {
				if got := Autoformat(s); got != want[i] {
					t.Errorf("text %d: %q, want %q", i, got, want[i])
				}
			}
		}()
	}
	wg.Wait()
}
