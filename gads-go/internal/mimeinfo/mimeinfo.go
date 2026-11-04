// Package mimeinfo is File::MimeInfo->mimetype: the MIME type of a file
// from the globs of the freedesktop.org shared-mime-info database, else
// from its contents.
package mimeinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type glob struct {
	re   *regexp.Regexp
	mime string
}

var (
	once      sync.Once
	literal   map[string]string
	extension map[string]string
	globs     []glob
)

// dataDirs are the XDG data directories (File::BaseDir data_dirs), the
// most important first
func dataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	return append([]string{home}, strings.Split(dirs, ":")...)
}

func load() {
	literal = map[string]string{}
	extension = map[string]string{}
	globs = nil
	dirs := dataDirs()
	// The least important first, so that the others override them
	for i := len(dirs) - 1; i >= 0; i-- {
		hashGlobs(filepath.Join(dirs[i], "mime", "globs"))
	}
}

// extRe is Perl's /^\*\.(\w+(\.\w+)*)$/ on a glob, read as UTF-8: \w is
// a letter, mark, decimal digit or connector
var extRe = regexp.MustCompile(`^\*\.(` + word + `+(?:\.` + word + `+)*)$`)

const word = `[\p{L}\p{Nl}\p{M}\p{Nd}\p{Pc}]`

func hashGlobs(file string) {
	f, err := os.Open(file) // #nosec G304 -- the shared-mime-info database files
	if err != nil {
		return
	}
	defer f.Close()
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		mime, g, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if !strings.ContainsAny(g, "?*[") {
			literal[g] = mime
		} else if m := extRe.FindStringSubmatch(g); m != nil {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			extension[m[1]] = mime
		} else {
			globs = append([]glob{{globToRegexp(g), mime}}, globs...)
		}
	}
}

var (
	globSpecial = regexp.MustCompile(`([^\p{L}\p{Nl}\p{M}\p{Nd}\p{Pc}/\\.?*\[\]])`)
)

func globToRegexp(g string) *regexp.Regexp {
	g = strings.ReplaceAll(g, ".", `\.`)
	g = strings.ReplaceAll(g, "?", ".?")
	g = strings.ReplaceAll(g, "*", ".*")
	// The ? of .? and * of .* were introduced above: ".?" and ".*" stay
	g = globSpecial.ReplaceAllString(g, `\$1`)
	re, err := regexp.Compile("^" + g + "$")
	if err != nil {
		return regexp.MustCompile(`^\z.`)
	}
	return re
}

// fileExtRe is File::MimeInfo's extension: the name is bytes (from the
// command line), on which Perl's \w is ASCII
var fileExtRe = regexp.MustCompile(`\.(\w+(?:\.\w+)*)$`)

// latin1 gives each byte of a name as a character, as Perl compares a
// string of bytes with the globs (read as UTF-8): a name in UTF-8 never
// matches a glob beyond ASCII
func latin1(name string) string {
	r := make([]rune, len(name))
	for i := 0; i < len(name); i++ {
		r[i] = rune(name[i])
	}
	return string(r)
}

// lcASCII is Perl's lc of bytes, and hasUpper its /[A-Z]/
func lcASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func hasUpper(s string) bool {
	return strings.ContainsAny(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

func fromGlobs(bytes string) (string, bool) {
	name := latin1(bytes)
	if m, ok := literal[name]; ok {
		return m, true
	}
	if m := fileExtRe.FindStringSubmatch(name); m != nil {
		ext := strings.Split(m[1], ".")
		for len(ext) > 0 {
			if mime, ok := extension[strings.Join(ext, ".")]; ok {
				return mime, true
			}
			ext = ext[1:]
		}
	}
	for _, g := range globs {
		if g.re.MatchString(name) {
			return g.mime, true
		}
	}
	if hasUpper(bytes) {
		return fromGlobs(lcASCII(bytes))
	}
	return "", false
}

// fromContents is File::MimeInfo default: plain text unless the start of
// the file has control characters
func fromContents(file string) string {
	f, err := os.Open(file) // #nosec G304 -- the shared-mime-info database files
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 32*utf8.UTFMax)
	n, _ := f.Read(buf)
	buf = buf[:n]
	if n == 0 {
		return "text/plain"
	}
	// 32 characters of the file
	var chars []rune
	valid := true
	for b := buf; len(b) > 0 && len(chars) < 32; {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size <= 1 {
			valid = false
			break
		}
		chars = append(chars, r)
		b = b[size:]
	}
	if !valid {
		if len(buf) > 32 {
			buf = buf[:32]
		}
		for _, c := range buf {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
				continue
			}
			if c < 0x20 || c == 0x7f {
				return "application/octet-stream"
			}
		}
		return "text/plain"
	}
	for _, r := range chars {
		if !perlPrintOrSpace(r) {
			return "application/octet-stream"
		}
	}
	return "text/plain"
}

// perlPrintOrSpace is Perl's [[:print:]\s] for a character: anything but
// a control character (other than the spaces), a surrogate or an
// unassigned character (U+FEFF and the other format characters are
// printable, unlike Go's unicode.IsPrint)
func perlPrintOrSpace(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cs, r) {
		return false
	}
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Zs, unicode.Cf, unicode.Co)
}

// Mimetype is the MIME type of a file ("" if there is none, such as for a
// file that does not exist)
func Mimetype(file string) string {
	once.Do(load)
	st, err := os.Lstat(file)
	if err == nil && !st.Mode().IsRegular() {
		switch m := st.Mode(); {
		case m&os.ModeSymlink != 0:
			return "inode/symlink"
		case m.IsDir():
			return "inode/directory"
		case m&os.ModeNamedPipe != 0:
			return "inode/fifo"
		case m&os.ModeCharDevice != 0:
			return "inode/chardevice"
		case m&os.ModeDevice != 0:
			return "inode/blockdevice"
		case m&os.ModeSocket != 0:
			return "inode/socket"
		}
	}
	if mime, ok := fromGlobs(filepath.Base(file)); ok {
		return mime
	}
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return fromContents(file)
}
