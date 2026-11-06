package cldr

import "testing"

// A fraction of more digits than an int holds is read, as
// DateTime::Format::CLDR does (2026-01-02 03:04:05.123456789 in Perl)
func TestParseLongFraction(t *testing.T) {
	d, ok := Parse("2026-01-02 03:04:05.1234567891234567891234", "yyyy-MM-dd HH:mm:ss.SSSSSSSSSSSSSSSSSSSSSS")
	if !ok || d.Format("2006-01-02 15:04:05.000000000") != "2026-01-02 03:04:05.123456789" {
		t.Errorf("%v %v", d, ok)
	}
}
