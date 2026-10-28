package gads

import (
	"strings"
	"testing"
)

// Expected results from Perl's Text::Wrap wrap(”, ”, $text)
func TestTextWrap(t *testing.T) {
	cases := []struct{ in, want string }{
		{"A request to reset your Linkspace password has been received. Please\nclick on the following link to set and retrieve a new password:\n\nhttp://localhost/resetpw/T0000000000000000000000000000007\n", "A request to reset your Linkspace password has been received. Please\nclick on the following link to set and retrieve a new password:\n\nhttp://localhost/resetpw/T0000000000000000000000000000007\n"},
		{"A request to reset your Very Long Site Name Of A Company Database password has been received. Please\nclick here\n", "A request to reset your Very Long Site Name Of A Company Database password\nhas been received. Please\nclick here\n"},
		{"short no newline", "short no newline"},
		{"word ", "word "},
		{"trailing space \nnext", "trailing space \nnext"},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bbb\n", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\naaaaaaaaaaaaaaaa bbb\n"},
		{"tab\there\n\n\nthree blank", "tab\there\n\n\nthree blank"},
		{"x  y   z    w  ", "x  y   z    w  "},
		{"a       b        c\tdd\n", "a\tb\t c\tdd\n"},
		{"        x", "\tx"},
		{"ab  cd  ef      gh   ", "ab  cd\tef\tgh   "},
		{"\t\tdeep tab line that is quite long and will need wrapping at some point soon ok", "\t\tdeep tab line that is quite long and will need wrapping at\nsome point soon ok"},
	}
	for _, c := range cases {
		if got := TextWrap(c.in); got != c.want {
			t.Errorf("TextWrap(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRandomPassword(t *testing.T) {
	e := &Engine{}
	pw := e.RandomPassword()
	if len(pw) < 5 || pw[0] < 'A' || pw[0] > 'Z' || !strings.ContainsAny(pw[len(pw)-1:], "0123456789") {
		t.Errorf("RandomPassword = %q", pw)
	}
	if len(effWords) != 7776 {
		t.Errorf("word list has %d words", len(effWords))
	}
	e.PasswordGenerator = func() string { return "Fixed" }
	if e.RandomPassword() != "Fixed" {
		t.Error("PasswordGenerator not used")
	}
}
