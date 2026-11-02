package conformance

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var trailingSpaceRe = regexp.MustCompile(`(?m)[ \t]+$`)

// pdfText is the text of a PDF, as pdftotext (poppler) extracts it: the
// text of each page, without trailing spaces (the observation of a PDF
// response)
func pdfText(content []byte) map[string]any {
	dir, err := os.MkdirTemp("", "gads-pdf")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(file, content, 0o600); err != nil {
		panic(err)
	}
	out, err := exec.Command("pdftotext", "-enc", "UTF-8", file, "-").Output() // #nosec G204 -- a fixed command on a file of the runner
	if err != nil {
		return map[string]any{"error": "pdftotext: " + err.Error()}
	}
	parts := strings.Split(string(out), "\f")
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	pages := []any{}
	for _, p := range parts {
		p = trailingSpaceRe.ReplaceAllString(p, "")
		pages = append(pages, strings.TrimRight(p, "\n"))
	}
	return map[string]any{"pages": pages}
}

// pngSize is the size of a PNG, from its header (the observation of a PNG
// response: its pixels depend on the browser)
func pngSize(content []byte) map[string]any {
	if len(content) < 24 || string(content[:8]) != "\x89PNG\r\n\x1a\n" || string(content[12:16]) != "IHDR" {
		return map[string]any{"error": "not a PNG"}
	}
	return map[string]any{
		"width":  int64(binary.BigEndian.Uint32(content[16:])),
		"height": int64(binary.BigEndian.Uint32(content[20:])),
	}
}
