package mimeinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMimetype(t *testing.T) {
	if _, err := os.Stat("/usr/share/mime/globs"); err != nil {
		t.Skip("no shared-mime-info database")
	}
	dir := t.TempDir()
	write := func(name string, content []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	text := []byte("hello\n")
	for name, want := range map[string]string{
		"a.txt":        "text/plain",
		"A.TXT":        "text/plain",
		"a.csv":        "text/csv",
		"a.pdf":        "application/pdf",
		"a.docx":       "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"a.xls":        "application/vnd.ms-excel",
		"a.png":        "image/png",
		"a.unknownext": "text/plain",
		"noext":        "text/plain",
	} {
		if got := Mimetype(write(name, text)); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	// shared-mime-info 2.2 renamed application/x-yaml to application/yaml
	if got := Mimetype(write("a.yaml", text)); got != "application/x-yaml" && got != "application/yaml" {
		t.Errorf("a.yaml: got %q", got)
	}
	if got := Mimetype(write("bin", []byte{0, 1, 2})); got != "application/octet-stream" {
		t.Errorf("binary: got %q", got)
	}
	if got := Mimetype(write("empty", nil)); got != "text/plain" {
		t.Errorf("empty: got %q", got)
	}
	if got := Mimetype(dir); got != "inode/directory" {
		t.Errorf("directory: got %q", got)
	}
}
