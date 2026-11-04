package gads

import "testing"

func TestExpandTabs(t *testing.T) {
	for _, c := range [][2]string{
		{"none", "none"},
		{"\tx", "        x"},
		{"abcdefgh\tx", "abcdefgh        x"},
		{"ab\tc\td", "ab      c       d"},
	} {
		if got := expandTabs(c[0]); got != c[1] {
			t.Errorf("expandTabs(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}
