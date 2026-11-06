// Package textcsv reads CSV as Text::CSV->new({ binary => 1 })->getline
// does (Text::CSV_PP 2.06), which GADS's CSV imports use: the data import,
// the user upload and bin/import-user.pl. It differs from encoding/csv: a
// blank line is a row of one empty field, a lone CR ends a line (and is
// the line end from then on), and the first error ends the reading, where
// Perl's `while (my $row = $csv->getline($fh))` stops.
//
// This is a translation of Text::CSV_PP's ____parse (cx_Parse) keeping
// only what its default options reach (sep_char ",", quote_char and
// escape_char '"', binary), with the reading of its file handle a line at
// a time ($/, "\n", or "\r" once CR is the line end), its look-ahead
// (_AHEAD) and the state it keeps between calls (_CACHE). The tests compare
// it with Perl's (testdata/golden.json, conformance/perl/gen_csv_golden.pl).
package textcsv

import (
	"bytes"
	"io"
	"regexp"
	"strings"
)

// Error is the error_diag of the parse error that ended the reading
type Error struct {
	Code int
}

var messages = map[int]string{
	2010: "ECR - QUO char inside quotes followed by CR not part of EOL",
	2011: "ECR - Characters after end of quoted field",
	2023: "EIQ - QUO character not allowed",
	2027: "EIQ - Quoted field not terminated",
	2031: "EIF - CR char is first char of field, not part of EOL",
	2032: "EIF - CR char inside unquoted, not part of EOL",
	2034: "EIF - Loose unescaped quote",
}

func (e *Error) Error() string { return messages[e.Code] }

// Reader reads the rows of its input
type Reader struct {
	in    []byte
	inPos int
	rs    string // $/ of the current call

	// The state kept between calls (_CACHE)
	sep         string
	eolIsCR     bool
	recno       int
	hasAhead    bool
	ahead       string // _AHEAD
	aheadDef    bool
	hasLeftover bool
	tmp         string
	tmpDef      bool
	used, size  int // (0 when undefined, as Perl numifies undef)
	usedDef     bool
	pos         int // pos($ctx->{tmp})
	zeroLen     bool
	eofIO       bool // useIO_EOF
	done        error
}

// NewReader reads r
func NewReader(r io.Reader) *Reader {
	b, err := io.ReadAll(r)
	rd := &Reader{in: b, sep: ","}
	if err != nil {
		rd.done = err
	}
	return rd
}

// Read is getline: the next row, or io.EOF at the end of the input, or
// the *Error that ended it (every later call returns the same)
func (r *Reader) Read() ([]string, error) {
	if r.done != nil {
		return nil, r.done
	}
	fields, code := r.getline()
	if code != 0 {
		if code == 2012 {
			r.done = io.EOF
		} else {
			r.done = &Error{Code: code}
		}
		return nil, r.done
	}
	return fields, nil
}

// getline is ___parse for a file handle: the row, or the error code
func (r *Reader) getline() ([]string, int) {
	// local $/ = $ctx->{eol} if $ctx->{eol_is_cr}
	r.rs = "\n"
	if r.eolIsCR {
		r.rs = "\r"
	}
	r.eofIO = false
	// (_setup_ctx starts each call with size and used 0)
	r.size, r.used, r.usedDef = 0, 0, true
	r.tmp, r.tmpDef = "", false
	r.pos, r.zeroLen = 0, false
	if r.hasAhead && r.aheadDef {
		r.tmp, r.tmpDef = r.ahead, true
		r.size, r.used, r.usedDef = len(r.tmp), 0, true
	}
	fields, code := r.parse(nil)
	r.recno++
	if r.tmpDef && r.used < r.size && r.hasAhead {
		r.ahead, r.aheadDef = r.tmp[r.used:r.size], true
	} else {
		r.hasAhead = false
	}
	return fields, code
}

// readLine is $fh->getline with $/ (false at the end)
func (r *Reader) readLine() (string, bool) {
	if r.inPos >= len(r.in) {
		return "", false
	}
	rest := r.in[r.inPos:]
	n := len(rest)
	if i := bytes.Index(rest, []byte(r.rs)); i >= 0 {
		n = i + len(r.rs)
	}
	r.inPos += n
	return string(rest[:n]), true
}

// getFromSrc is __get_from_src
func (r *Reader) getFromSrc() bool {
	if r.tmpDef && r.used <= 0 {
		return true
	}
	if r.used < r.size {
		return true
	}
	if res, ok := r.readLine(); ok {
		if r.hasAhead {
			eol := ""
			if r.eolIsCR {
				eol = "\r"
			}
			r.tmp = r.ahead + eol + res
			r.hasAhead = false
		} else {
			r.tmp = res
		}
		r.tmpDef = true
		r.size = len(r.tmp)
		r.used, r.usedDef = -1, true
		r.pos, r.zeroLen = 0, false
		return true
	} else if r.hasLeftover {
		r.hasLeftover = false
		r.tmp, r.tmpDef = r.ahead, r.aheadDef
		r.hasAhead = false
		r.eofIO = true
		if r.size = len(r.tmp); r.size > 0 {
			r.used, r.usedDef = -1, true
			r.pos, r.zeroLen = 0, false
			return true
		}
	}
	r.tmpDef = true
	r.eofIO = true
	return false
}

// printable is whether a character is not matched by [^\x09\x20-\x7E]
// (an undefined one is, as Perl's undef !~ /.../)
func printable(c string, def bool) bool {
	if !def {
		return true
	}
	return len(c) == 1 && (c[0] == '\t' || (c[0] >= 0x20 && c[0] <= 0x7e))
}

// special is the length of the character of $re at s[i] (sep, quote, CR,
// LF, TAB, space, or a byte that is not printable), 0 if none
func (r *Reader) special(s string, i int) int {
	if strings.HasPrefix(s[i:], r.sep) {
		return len(r.sep)
	}
	switch b := s[i]; {
	case b == '"' || b == '\r' || b == '\n' || b == '\t' || b == ' ':
		return 1
	case b < 0x20 || b > 0x7e:
		return 1
	}
	return 0
}

// match is the next match of /\G(.*?)($re)/gs on tmp, where $re ends with
// `$` (c "" at the end): a zero-length match can't follow a zero-length
// match at the same position
func (r *Reader) match() (hit, c string, ok bool) {
	s := r.tmp
	if r.pos > len(s) {
		return "", "", false
	}
	for i := r.pos; i <= len(s); i++ {
		n := 0
		if i < len(s) {
			if n = r.special(s, i); n == 0 {
				continue
			}
		}
		if i == r.pos && n == 0 && r.zeroLen {
			return "", "", false
		}
		hit, c = s[r.pos:i], s[i:i+n]
		r.zeroLen = i == r.pos && n == 0
		r.pos = i + n
		return hit, c, true
	}
	return "", "", false
}

// get is __get: the next character, reading on (false at the end)
func (r *Reader) get() (string, bool) {
	if !r.usedDef {
		return "", false
	}
	if r.used >= r.size {
		if r.getFromSrc() {
			return r.get()
		}
		return "", false
	}
	if r.pos < len(r.tmp) {
		n := r.special(r.tmp, r.pos)
		if n == 0 {
			n = 1
		}
		c := r.tmp[r.pos : r.pos+n]
		r.pos += n
		r.zeroLen = false
		r.used = r.pos
		return c, true
	}
	if r.getFromSrc() {
		return r.get()
	}
	return "", false
}

func (r *Reader) setEOLIsCR() { r.eolIsCR = true }

var reSepLine = regexp.MustCompile(`(?is)^sep=([^\n]{1,16})$`)

// parse is ____parse: the fields of a row, or the error code (2012 at the
// end of the input)
func (r *Reader) parse(fields []string) ([]string, int) {
	const quot = `"`
	sep := r.sep
	seen := false
	waiting := true
	var value string
	vref := false
	quoted := false
	fldIdx := 0
	push := func() { fields = append(fields, value) }

	for r.getFromSrc() {
	inner:
		for {
			hit, c, ok := r.match()
			if !ok {
				break
			}
			r.used, r.usedDef = r.pos, true
			cdef := true
			if !waiting && c == "" && hit != "" && !r.eofIO {
				r.ahead, r.aheadDef = hit, true
				r.hasAhead, r.hasLeftover = true, true
				break
			}
			if seen && hit == "" && c == "" {
				break
			}
			if !vref {
				value, vref = "", true
				quoted = false
				fldIdx++
			}
			seen = true
			if hit != "" {
				waiting = false
				value += hit
			}

			eolx := false
		restart:
			for {
				if eolx {
					// EOLX
					eolx = false
					if waiting {
						fields = append(fields, "")
						return fields, 0
					}
					if quoted {
						value += c
						continue inner
					}
					// A first line sep=X sets the separator
					if r.recno == 0 && fldIdx == 1 {
						if m := reSepLine.FindStringSubmatch(hit); m != nil {
							r.sep = m[1]
							return r.parse(fields)
						}
					}
					push()
					return fields, 0
				}
				switch {
				case cdef && c == sep:
					if waiting {
						fields = append(fields, "")
						vref = false
					} else if quoted {
						value += c
					} else {
						push()
						vref, waiting = false, true
					}
					continue inner
				case cdef && c == quot:
					if waiting {
						quoted, waiting = true, false
						continue inner
					}
					if !quoted {
						return nil, 2034
					}
					c2, c2def := r.get()
					if !c2def {
						push()
						return fields, 0
					}
					if c2 == sep {
						push()
						vref, waiting = false, true
						continue inner
					}
					if c2 == "\n" || (r.eolIsCR && c2 == "\r") {
						push()
						return fields, 0
					}
					// (the escape character is the quote)
					if c2 == "0" {
						value += "\x00"
						continue inner
					}
					if c2 == quot {
						value += c2
						continue inner
					}
					if c2 == "\r" {
						c3, c3def := r.get()
						if c3def && c3 == "\n" {
							push()
							return fields, 0
						}
						if !r.eolIsCR {
							if c3def && c3 == "\r" {
								r.setEOLIsCR()
								quoted = false
								c = "\r"
								eolx = true
								continue restart
							}
							if printable(c3, c3def) {
								r.setEOLIsCR()
								r.used--
								r.hasAhead = true
								push()
								return fields, 0
							}
						}
						return nil, 2023
					}
					r.used--
					return nil, 2023
				case cdef && (c == "\n" || c == ""):
					eolx = true
					continue restart
				case cdef && c == "\r":
					if waiting {
						if r.eolIsCR {
							c = "\n"
							eolx = true
							continue restart
						}
						c2, c2def := r.get()
						if !c2def {
							cdef = false
							if !seen {
								break inner
							}
							continue restart
						}
						if c2 == "\n" {
							c = c2
							eolx = true
							continue restart
						}
						waiting = false
						if printable(c2, true) {
							r.setEOLIsCR()
							r.used--
							r.hasAhead = true
							push()
							return fields, 0
						}
						r.used--
						return nil, 2031
					}
					if quoted {
						value += c
						continue inner
					}
					if r.eolIsCR {
						eolx = true
						continue restart
					}
					c2, c2def := r.get()
					if c2def && c2 == "\n" {
						eolx = true
						continue restart
					}
					if printable(c2, c2def) || c2 == "\r" {
						r.setEOLIsCR()
						r.used--
						r.hasAhead = true
						push()
						return fields, 0
					}
					return nil, 2032
				default:
					if waiting {
						waiting = false
						continue restart
					}
					if !quoted && !cdef {
						break inner
					}
					if cdef {
						value += c
					}
					continue inner
				}
			}
		}
	}

	if waiting {
		if seen {
			fields = append(fields, "")
			return fields, 0
		}
		return nil, 2012
	}
	if quoted {
		return nil, 2027
	}
	if vref {
		push()
	}
	return fields, 0
}
