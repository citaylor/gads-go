package autoformat

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dlclark/regexp2"

	"github.com/ctrlo/gads-go/internal/perl"
)

// The parts of Text::Reform 1.20 that Text::Autoformat uses: form, with the
// fields of its formats (block fields [[[ ||| ~, line fields <<< >>>),
// the options squeeze, trim, fill and break, and break_wrap and
// break_with('-'). Headers, footers, pages, columns and numeric fields are
// left out: no format of Text::Autoformat has them.

// config is the configuration of form
type config struct {
	squeeze, trim, fill bool
	brk                 breaker
}

// breaker is a break option: it breaks a word that does not fit in rem of
// a field of length fldlen
type breaker func(text string, rem, fldlen int) (string, string)

// configFn changes the configuration from one argument of form on (a hash
// of options among the arguments)
type configFn func(*config)

const minbreak = 2

var (
	// split /(\n|(?:\\.)+|$fieldpat)/: the decimal point is '.'
	rePart     = regexp.MustCompile(`(\n|(?:\\.)+|>+(?:\.<{1,})|\]+(?:\.\[{1,})|<{2,}>{2,}|\[{2,}\]{2,}|~+|\[{2,}|\|{2,}|\]{2,}|<{2,}|\^{2,}|>{2,})`)
	reEscapes  = regexp.MustCompile(`\A(?:\\.)+`)
	reUnescape = regexp.MustCompile(`\\(.)`)
	// $lfieldmark and $fieldmark
	reLFieldmark = regexp.MustCompile(`(>+(?:\.<{1,})|<{2,}>{2,}|<{2}|\^{2}|>{2})`)
	reFieldmark  = regexp.MustCompile(`(>+(?:\.<{1,})|\]+(?:\.\[{1,})|~+|<{2,}>{2,}|\[{2,}\]{2,}|>+(?:\.<{1,})|<{2,}>{2,}|<{2}|\^{2}|>{2}|\]+(?:\.\[{1,})|\[{2,}\]{2,}|~+|\[{2}|\|{2}|\]{2})`)
	reLJustified = regexp.MustCompile(`<{2,}>{2,}`)
	reBJustified = regexp.MustCompile(`\[{2,}\]{2,}`)
	reNUL        = regexp.MustCompile("\x00(\x00*)")
	reTrailing   = regexp.MustCompile(`(?m)[ ]+$`)
	reNonNL      = regexp.MustCompile(`[^\n]`)
	reBlankOnly  = regexp.MustCompile(`\A` + ws + `*\z`)
	reBreakWrap  = regexp.MustCompile(`(?s)\A(` + ws + `*` + nws + `*)(.*)`)
	reSpaces     = regexp.MustCompile(` +`)
	reNumField   = regexp.MustCompile(`([\]>]+)\.([\[<]+)`)
	reTrailZeros = regexp.MustCompile(`(\.[0-9]+?)(0+)$`)
	// strtod1
	reReal      = regexp2.MustCompile(`^\s*((?:[+-]?)(?:(?=[0123456789]|[.])(?:[0123456789]*)(?:(?:[.])(?:[0123456789]{0,}))?)(?:(?:[eE])(?:(?:[+-]?)(?:[0123456789]+))|))(.*)$`, regexp2.Singleline)
	reNotNumber = regexp.MustCompile(`\A` + ws + `*` + nws + `*`)
)

// Perl's \s and \S for a character string (Go's are ASCII)
const (
	ws  = `[\t\n\v\f\r\x{85}\p{Z}]`
	nws = `[^\t\n\v\f\r\x{85}\p{Z}]`
)

func runes(s string) int { return utf8.RuneCountInString(s) }

func notSpace(r rune) bool { return !unicode.IsSpace(r) }

// hasNonSpace is /\S/
func hasNonSpace(s string) bool { return strings.IndexFunc(s, notSpace) >= 0 }

// substr is Perl's substr($s, off, n) for 0 <= off and n >= 0
func substr(s string, off, n int) string {
	r := []rune(s)
	if off > len(r) {
		return ""
	}
	r = r[off:]
	if n < len(r) {
		r = r[:n]
	}
	return string(r)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// breakWrap is Text::Reform break_wrap: a word that does not fit is put
// whole on a line of its own
func breakWrap(text string, reqlen, fldlen int) (string, string) {
	if reqlen == fldlen {
		m := reBreakWrap.FindStringSubmatch(text)
		return m[1], m[2]
	}
	return "", text
}

// breakWith is Text::Reform break_with($hyphen)
func breakWith(hyphen string) breaker {
	hylen := runes(hyphen)
	return func(text string, rem, fldlen int) (string, string) {
		var r0, r1 string
		if fldlen <= hylen {
			r0, r1 = substr(text, 0, 1), substr(text, 1, runes(text))
		} else {
			n := rem - hylen
			if n < 0 {
				// substr with a negative length leaves that many off the end
				n = runes(text) + n
				if n < 0 {
					n = 0
				}
			}
			r0 = substr(text, 0, n)
			r1 = substr(text, runes(r0), runes(text))
		}
		if matchZ(reBlankOnly, r0) {
			return "", text
		}
		return r0 + hyphen, r1
	}
}

// matchZ is a match of /\A\s*\Z/: \Z also matches before a final newline
func matchZ(re *regexp.Regexp, s string) bool {
	return re.MatchString(s) || re.MatchString(strings.TrimSuffix(s, "\n"))
}

// replace is Text::Reform replace: the text of one field of a line, taken
// from the start of *ref
func replace(typ string, length int, ref *string, cfg *config) string {
	text := ""
	rem := length
	filled := 0
	if cfg.fill {
		*ref = strings.TrimLeftFunc(*ref, unicode.IsSpace)
	} else {
		*ref = strings.TrimLeft(*ref, " \t")
	}
	if hasNonSpace(*ref) && runes(typ) > 2 {
		return replaceNumber(typ, ref)
	}
	// The words are found without regular expressions, which would scan
	// the rest of the text for each word (unicode.IsSpace is Perl's \s)
	for hasNonSpace(*ref) {
		if !cfg.fill {
			if t := strings.TrimLeft(*ref, " \t"); strings.HasPrefix(t, "\n") {
				*ref = t[1:]
				filled = 2
				break
			}
		}
		// \A(\s*)(\S+)(.*)\z
		i := strings.IndexFunc(*ref, notSpace)
		j := strings.IndexFunc((*ref)[i:], unicode.IsSpace)
		if j < 0 {
			j = len(*ref) - i
		}
		ws, word, extra := (*ref)[:i], (*ref)[i:i+j], (*ref)[i+j:]
		if cfg.fill {
			nl := " "
			if reNonNL.MatchString(ws) {
				nl = ""
			}
			ws = strings.ReplaceAll(ws, "\n", nl)
		}
		lead := ws
		if cfg.squeeze {
			lead = ""
			if ws != "" {
				lead = " "
			}
		}
		match := lead + word
		if perl.True(text) && strings.Contains(match, "\n") {
			break
		}
		if len1 := runes(match); len1 <= rem {
			text += match
			rem -= len1
			*ref = extra
			filled = 1 // the continue block
			continue
		}
		if rem-runes(lead) > minbreak {
			broken, left := cfg.brk(match, rem, length)
			text += broken
			*ref = left + extra
			rem -= runes(broken)
		}
		break
	}
	if filled == 0 && rem > 0 && hasNonSpace(*ref) && text == "" {
		// s/^\s*(.{1,$rem})//
		re := regexp.MustCompile(`\A` + ws + `*(.{1,` + itoa(rem) + `})`)
		if m := re.FindStringSubmatchIndex(*ref); m != nil {
			text = (*ref)[m[2]:m[3]]
			*ref = (*ref)[m[1]:]
			rem -= runes(text)
		}
	}
	switch {
	case strings.Contains(text, " ") && typ == "J" && hasNonSpace(*ref) && filled != 2:
		// fully justified: spaces are added to the gaps from the right
		r := []rune(text)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		t := string(r)
		for rem > 0 {
			t = reSpaces.ReplaceAllStringFunc(t, func(gap string) string {
				rem--
				if rem >= 0 {
					return " " + gap
				}
				return gap
			})
		}
		r = []rune(t)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		text = string(r)
	case strings.ContainsAny(typ, ">]"): // right justified
		text = spaces(rem) + text
	case strings.ContainsAny(typ, "^|"): // centre justified
		if rem > 0 {
			half := rem / 2
			text = spaces(half) + text + spaces(rem-half)
		}
	default: // left justified
		text += spaces(rem)
	}
	return text
}

// replaceNumber is the numeric field of replace (">>>.<<"): only a
// format made of the text of a paragraph has one
func replaceNumber(typ string, ref *string) string {
	fmtnum := runes(typ)
	m := reNumField.FindStringSubmatch(typ)
	ilen, dlen := runes(m[1]), runes(m[2])
	num, unconsumed, ok := "", runes(*ref), false
	if mm, _ := reReal.FindStringMatch(*ref); mm != nil {
		num, ok = mm.GroupByNumber(1).String(), true
		// length($2 || ''): a remainder of "0" counts as none
		if rest := mm.GroupByNumber(2).String(); rest == "0" {
			unconsumed = 0
		} else {
			unconsumed = runes(rest)
		}
	}
	if !ok || unconsumed == runes(*ref) {
		*ref = reNotNumber.ReplaceAllString(*ref, "")
		return strings.Repeat("?", ilen) + "." + strings.Repeat("?", dlen)
	}
	f := perl.Numify(num)
	var formatted string
	switch {
	case math.IsInf(f, 1):
		formatted = fmt.Sprintf("%*s", fmtnum, "Inf")
	case math.IsInf(f, -1):
		formatted = fmt.Sprintf("%*s", fmtnum, "-Inf")
	default:
		formatted = fmt.Sprintf("%*.*f", fmtnum, dlen, f)
	}
	text := formatted
	if runes(formatted) > fmtnum {
		text = strings.Repeat("#", ilen) + "." + strings.Repeat("#", dlen)
	}
	if !regexp.MustCompile(`\.[0-9][0-9]{` + itoa(dlen) + `,}$`).MatchString(num) {
		text = reTrailZeros.ReplaceAllStringFunc(text, func(s string) string {
			m := reTrailZeros.FindStringSubmatch(s)
			return m[1] + spaces(runes(m[2]))
		})
	}
	if unconsumed > 0 {
		r := []rune(*ref)
		*ref = string(r[len(r)-unconsumed:])
	} else {
		*ref = ""
	}
	return text
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// splitParts is split /(\n|(?:\\.)+|$fieldpat)/, $format: the literals
// and the separators, without the empty fields at the end
func splitParts(format string) []string {
	var parts []string
	last := 0
	for _, m := range rePart.FindAllStringIndex(format, -1) {
		parts = append(parts, format[last:m[0]], format[m[0]:m[1]])
		last = m[1]
	}
	parts = append(parts, format[last:])
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// form is Text::Reform form. Each argument is a format, the data of its
// fields (pointers, which form consumes: two arguments that are the same
// variable in Perl share one pointer), or a configFn, applied from there
// on. The first configFn, before a format, is a temporary configuration.
func form(args ...any) string {
	cfg := &config{brk: breakWith("-")}
	start := 0
	if len(args) > 1 {
		if f, ok := args[0].(configFn); ok {
			f(cfg)
			start = 1
		}
	}
	// Each data argument starts from its value (Perl restores them at the
	// end, so that the caller's are unchanged), and arguments that are the
	// same variable stay the same
	copies := map[*string]*string{}
	for i, a := range args {
		if p, ok := a.(*string); ok {
			c, seen := copies[p]
			if !seen {
				v := *p
				c = &v
				copies[p] = c
			}
			args[i] = c
		}
	}
	empty := ""
	arg := func(i int) *string {
		for i >= len(args) {
			args = append(args, &empty)
		}
		p, ok := args[i].(*string)
		if !ok {
			panic("Not a SCALAR reference")
		}
		return p
	}

	var text strings.Builder
	var stack []string
	for start < len(args) || len(stack) > 0 {
		if start < len(args) {
			if f, ok := args[start].(configFn); ok {
				f(cfg)
				start++
				continue
			}
		}
		if len(stack) == 0 {
			// ${$_[$startidx++]}||""
			f := *arg(start)
			if !perl.True(f) {
				f = ""
			}
			stack = []string{f}
			start++
		}
		format := stack[0]
		stack = stack[1:]

		parts := splitParts(format)
		if len(parts) == 0 || parts[len(parts)-1] != "\n" {
			parts = append(parts, "\n")
		}
		fieldcount := 0
		filled := false
		firstline := true
		for !filled {
			next := start
			var data []*string
			for _, part := range parts {
				switch {
				case reEscapes.MatchString(part):
					text.WriteString(reUnescape.ReplaceAllString(part, "$1"))
				case reLFieldmark.MatchString(part):
					if firstline {
						fieldcount++
						typ := reLFieldmark.FindString(part)
						if reLJustified.MatchString(part) {
							typ = "J"
						}
						text.WriteString(replace(typ, runes(part), arg(next), cfg))
					} else {
						text.WriteString(spaces(runes(part)))
					}
					next++
				case reFieldmark.MatchString(part) && !strings.HasPrefix(part, "~~"):
					if firstline {
						fieldcount++
					}
					typ := reFieldmark.FindString(part)
					if reBJustified.MatchString(part) {
						typ = "J"
					}
					ref := arg(next)
					text.WriteString(replace(typ, runes(part), ref, cfg))
					data = append(data, ref)
					next++
				default:
					text.WriteString(reNUL.ReplaceAllString(part, "$1"))
				}
			}
			firstline = false
			filled = true
			for _, d := range data {
				if hasNonSpace(*d) {
					filled = false
				}
			}
		}
		start += fieldcount
	}
	out := text.String()
	if cfg.trim {
		out = reTrailing.ReplaceAllString(out, "")
	}
	return out
}
