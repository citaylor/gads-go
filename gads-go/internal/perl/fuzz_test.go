package perl

import "testing"

// FuzzNumify numifies any string (field values, parameters) without
// failing
func FuzzNumify(f *testing.F) {
	for _, s := range []string{"1", " -3.5e2x", "- 3", "--1", "0x10", "Inf", "nan", "1_000", "9223372036854775808", "\t+.5"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		LooksLikeNumber(s)
		Numify(s)
		NumifyTyped(s)
	})
}
