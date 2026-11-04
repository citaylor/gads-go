package cldr

import (
	"sync"
	"testing"
	"time"
)

// Patterns are parsed and cached by goroutines at once (the engines of a
// pool), with the same results (run with -race)
func TestPatternCacheConcurrent(t *testing.T) {
	want := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Several goroutines parse each new pattern first
			for _, p := range []string{"yyyy-MM-dd HH:mm:ss", "dd/MM/yyyy HH:mm:ss", "MM.dd.yyyy HH:mm:ss"} {
				in := Format(want, p)
				got, ok := Parse(in, p)
				if !ok || !got.Equal(want) {
					t.Errorf("goroutine %d: %s %q: %v %v", i, p, in, got, ok)
				}
			}
		}()
	}
	wg.Wait()
}
