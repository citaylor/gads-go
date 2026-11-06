package pdf

import (
	"sync"
	"testing"
)

// Engines make PDFs at once: the fonts are loaded once, under a lock
// (run with -race)
func TestLoadFontConcurrent(t *testing.T) {
	fontMu.Lock()
	fontCache = map[string]*Font{}
	fontMu.Unlock()
	var wg sync.WaitGroup
	fonts := make([]*Font, 8)
	for i := range fonts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f, err := LoadFont([]string{"regular", "bold"}[i%2])
			if err != nil {
				t.Error(err)
			}
			fonts[i] = f
		}(i)
	}
	wg.Wait()
	for i := 2; i < len(fonts); i++ {
		if fonts[i] != fonts[i%2] {
			t.Errorf("font %d loaded twice", i)
		}
	}
}
