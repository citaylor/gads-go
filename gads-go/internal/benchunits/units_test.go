package benchunits

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/ctrlo/gads-go/internal/gads"
)

// BenchmarkUnits runs each unit as a sub-benchmark:
//
//	go test -run '^$' -bench . -benchmem ./internal/benchunits
func BenchmarkUnits(b *testing.B) {
	for _, u := range Units {
		b.Run(u.Name, func(b *testing.B) {
			op, done := u.Setup()
			if done != nil {
				defer done()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				op()
			}
		})
	}
}

// TestUnits checks that every unit runs (as part of go test ./...)
func TestUnits(t *testing.T) {
	for _, u := range Units {
		op, done := u.Setup()
		op()
		if done != nil {
			done()
		}
	}
}

// TestJSONOutput checks that the JSON units produce the agreed bytes (the
// Perl units check the same digest)
func TestJSONOutput(t *testing.T) {
	sum := sha256.Sum256([]byte(gads.EncodeJSON(Rows(1000))))
	if got := hex.EncodeToString(sum[:]); got != JSONSHA256 {
		t.Errorf("JSON of Rows(1000) has SHA-256 %s, want %s", got, JSONSHA256)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	CloseFixture()
	os.Exit(code)
}
