package tt

import (
	"os"
	"testing"
)

// Templates render as Template Toolkit 3 (the full language is covered by
// the conformance scenario 100_tt_language, compared with Perl)
func TestProcess(t *testing.T) {
	files := map[string]string{
		"/t/main.tt": `[% INCLUDE inc.tt n=3 %]|[% FOREACH i IN [1..n] %][% i %][% loop.last ? '' : ',' %][% END %]|[% x | html %]|[% 7 div 2 %]`,
		"/t/inc.tt":  `[% n * 2 %]`,
	}
	e := NewEngine("/t")
	e.Load = func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
	out, err := e.Process("main.tt", HashOf("n", int64(4), "x", "<a&b>"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "6|1,2,3,4|&lt;a&amp;b&gt;|3"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if _, err := e.Process("missing.tt", nil); err == nil || err.(*Exception).Type != "file" {
		t.Errorf("missing template error: %v", err)
	}
}

func TestParseError(t *testing.T) {
	e := NewEngine("/t")
	e.Load = func(string) ([]byte, error) { return []byte("[% IF %]"), nil }
	_, err := e.Process("bad.tt", nil)
	if err == nil {
		t.Fatal("no parse error")
	}
}

func TestEncodeEntities(t *testing.T) {
	if got := EncodeEntities(`<é & "x">`); got != "&lt;&eacute; &amp; &quot;x&quot;&gt;" {
		t.Errorf("got %s", got)
	}
}
