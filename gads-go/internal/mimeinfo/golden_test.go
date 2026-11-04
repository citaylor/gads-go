package mimeinfo

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// TestGolden compares with File::MimeInfo, with the globs of testdata
// (conformance/perl/gen_mime_golden.pl)
func TestGolden(t *testing.T) {
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Name     string
			Contents *string
			Mime     string
		}
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	data, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(data, "home"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(data, "system"))
	once = sync.Once{}
	t.Cleanup(func() { once = sync.Once{} })
	dir := t.TempDir()
	for i, c := range golden.Cases {
		sub := filepath.Join(dir, strconv.Itoa(i))
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(sub, c.Name)
		if c.Contents != nil {
			content, err := hex.DecodeString(*c.Contents)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := Mimetype(file); got != c.Mime {
			contents := "none"
			if c.Contents != nil {
				contents = *c.Contents
			}
			t.Errorf("%q (contents %s): got %q, Perl %q", c.Name, contents, got, c.Mime)
		}
	}
}
