package conformance

import (
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
