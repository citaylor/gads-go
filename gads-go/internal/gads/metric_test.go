package gads

import "testing"

// A metric group ID is that of an existing group if true as in Perl
func TestIDTrue(t *testing.T) {
	for v, want := range map[any]bool{nil: false, "": false, "0": false, "1": true, int64(3): true, int64(0): false} {
		if got := idTrue(v); got != want {
			t.Errorf("idTrue(%#v) = %v", v, got)
		}
	}
}
