package gads

import (
	"sync"
	"testing"
)

// London is loaded once, by whichever goroutine asks first (run with
// -race)
func TestLondonConcurrent(t *testing.T) {
	locs := make([]string, 16)
	var wg sync.WaitGroup
	for i := range locs {
		wg.Add(1)
		go func() { defer wg.Done(); locs[i] = London().String() }()
	}
	wg.Wait()
	for i, l := range locs {
		if l != "Europe/London" {
			t.Errorf("goroutine %d: %s", i, l)
		}
	}
}
