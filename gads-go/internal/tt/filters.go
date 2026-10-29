package tt

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

type filterFunc func(string) (string, error)

func staticFilter(f func(string) string) filterFunc {
	return func(s string) (string, error) { return f(s), nil }
}

var staticFilters = map[string]filterFunc{
	"html":            staticFilter(htmlFilter),
	"html_para":       staticFilter(htmlParagraph),
	"html_break":      staticFilter(htmlParaBreak),
	"html_para_break": staticFilter(htmlParaBreak),
	"html_line_break": staticFilter(htmlLineBreak),
	"xml":             staticFilter(xmlFilter),
	"uri":             staticFilter(func(s string) string { return uriEscape(s, uriUnsafe) }),
	"url":             staticFilter(func(s string) string { return uriEscape(s, urlUnsafe) }),
	"upper":           staticFilter(perlUc),
	"lower":           staticFilter(perlLc),
	"ucfirst":         staticFilter(ucfirst),
	"lcfirst":         staticFilter(lcfirst),
	"stderr":          staticFilter(func(s string) string { return "" }),
	"trim":            staticFilter(func(s string) string { return strings.TrimFunc(s, isSpace) }),
	"null":            staticFilter(func(s string) string { return "" }),
	"collapse":        staticFilter(collapse),
}

type filterFactory func(c *Context, args []any) (filterFunc, error)

var dynamicFilters map[string]filterFactory

func init() {
	dynamicFilters = map[string]filterFactory{
		"html_entity": func(c *Context, args []any) (filterFunc, error) {
			return staticFilter(encodeEntities), nil
		},
		"indent": func(c *Context, args []any) (filterFunc, error) {
			pad := "4"
			if v := argN(args, 0); v != nil {
				pad = Str(v)
			}
			if isAllDigits(pad) {
				n, _ := strconv.Atoi(pad)
				pad = strings.Repeat(" ", n)
			}
			return staticFilter(func(s string) string { return indentLines(s, pad) }), nil
		},
		"format": func(c *Context, args []any) (filterFunc, error) {
			format := "%s"
			if v := argN(args, 0); v != nil {
				format = Str(v)
			}
			return func(s string) (string, error) {
				lines := perlSplitNewlines(s)
				out := make([]string, len(lines))
				for i, l := range lines {
					out[i] = perlSprintf(format, []any{l})
				}
				return strings.Join(out, "\n"), nil
			}, nil
		},
		"truncate": func(c *Context, args []any) (filterFunc, error) {
			length := 32
			if v := argN(args, 0); v != nil {
				n, _ := perlInt(v).(int64)
				length = int(n)
			}
			char := "..."
			if v := argN(args, 1); v != nil {
				char = Str(v)
			}
			lchar := visualLength(char)
			if length < lchar {
				char = runeSlice(char, 0, length)
				lchar = length
			}
			return staticFilter(func(s string) string {
				if visualLength(s) <= length {
					return s
				}
				return truncateVisual(s, length-lchar) + char
			}), nil
		},
		"repeat": func(c *Context, args []any) (filterFunc, error) {
			iter := any(int64(1))
			if v := argN(args, 0); v != nil && Str(v) != "" {
				iter = v
			}
			n, _ := perlInt(iter).(int64)
			return staticFilter(func(s string) string {
				if n <= 0 {
					return ""
				}
				return strings.Repeat(s, int(n))
			}), nil
		},
		"replace": func(c *Context, args []any) (filterFunc, error) {
			search := Str(argN(args, 0))
			replace := Str(argN(args, 1))
			return func(s string) (string, error) {
				re, err := PerlRegexp(search)
				if err != nil {
					return "", err
				}
				return replaceLiteral(re, s, replace)
			}, nil
		},
		"remove": func(c *Context, args []any) (filterFunc, error) {
			search := Str(argN(args, 0))
			return func(s string) (string, error) {
				re, err := PerlRegexp(search)
				if err != nil {
					return "", err
				}
				return replaceLiteral(re, s, "")
			}, nil
		},
		"eval":   evalFilterFactory,
		"evaltt": evalFilterFactory,
		"perl": func(c *Context, args []any) (filterFunc, error) {
			return nil, throwErr("perl", "EVAL_PERL is not set")
		},
		"evalperl": func(c *Context, args []any) (filterFunc, error) {
			return nil, throwErr("perl", "EVAL_PERL is not set")
		},
		"redirect": func(c *Context, args []any) (filterFunc, error) {
			return nil, throwErr("redirect", "OUTPUT_PATH is not set")
		},
		"file": func(c *Context, args []any) (filterFunc, error) {
			return nil, throwErr("redirect", "OUTPUT_PATH is not set")
		},
		"stdout": func(c *Context, args []any) (filterFunc, error) {
			return staticFilter(func(s string) string { return "" }), nil
		},
	}
}

func evalFilterFactory(c *Context, args []any) (filterFunc, error) {
	return func(s string) (string, error) {
		doc, err := parseTemplate(s, !c.eng.NoAnyCase)
		if err != nil {
			return "", throwErr("file", "parse error - input text "+err.Error())
		}
		doc.name = "input text"
		return c.process([]any{doc}, nil)
	}, nil
}

// filter is Template::Context::filter
func (c *Context) filter(name any, args []any, hasArgs bool, alias string) (filterFunc, error) {
	if code, ok := name.(Code); ok {
		return func(s string) (string, error) {
			r, err := code(c, []any{s})
			if err != nil {
				return "", err
			}
			if len(r) == 0 {
				return "", nil
			}
			return Str(r[0]), nil
		}, nil
	}
	n := Str(name)
	if !hasArgs {
		if f, ok := c.filterCache[n]; ok {
			return f, nil
		}
	}
	var f filterFunc
	if c.eng.Filters != nil {
		if sf, ok := c.eng.Filters[n]; ok {
			f = staticFilter(sf)
		}
	}
	if f == nil {
		if sf, ok := staticFilters[n]; ok {
			f = sf
		} else if df, ok := dynamicFilters[n]; ok {
			var err error
			f, err = df(c, args)
			if err != nil {
				return nil, err
			}
		}
	}
	if f == nil {
		return nil, throwErr("undef", n+": filter not found")
	}
	if alias != "" {
		c.filterCache[alias] = f
	}
	return f, nil
}

func htmlFilter(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func xmlFilter(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

var paraSplitRe = regexp2.MustCompile(`(?:\r?\n){2,}`, regexp2.None)

func htmlParagraph(s string) string {
	parts, _ := perlSplit(paraSplitRe, false, s, 0)
	strs := make([]string, len(parts))
	for i, p := range parts {
		strs[i] = Str(p)
	}
	return "<p>\n" + strings.Join(strs, "\n</p>\n\n<p>\n") + "</p>\n"
}

var paraBreakRe = regexp2.MustCompile(`(\r?\n){2,}`, regexp2.None)

func htmlParaBreak(s string) string {
	r, _ := paraBreakRe.ReplaceFunc(s, func(m regexp2.Match) string {
		g := m.GroupByNumber(1)
		nl := g.Captures[len(g.Captures)-1].String()
		return nl + "<br />" + nl + "<br />" + nl
	}, -1, -1)
	return r
}

var lineBreakRe = regexp2.MustCompile(`(\r?\n)`, regexp2.None)

func htmlLineBreak(s string) string {
	r, _ := lineBreakRe.ReplaceFunc(s, func(m regexp2.Match) string {
		return "<br />" + m.String()
	}, -1, -1)
	return r
}

const uriUnsafe = "A-Za-z0-9\\-_.~"
const urlUnsafe = ";/?:@&=+$,A-Za-z0-9\\-_.~"

func uriSafe(b byte, set string) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_' || b == '.' || b == '~':
		return true
	}
	if set == urlUnsafe && strings.IndexByte(";/?:@&=+$,", b) >= 0 {
		return true
	}
	return false
}

func uriEscape(s, set string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if uriSafe(s[i], set) {
			b.WriteByte(s[i])
		} else {
			fmt.Fprintf(&b, "%%%02X", s[i])
		}
	}
	return b.String()
}

// encodeEntities is HTML::Entities::encode_entities with the default set
func encodeEntities(s string) string {
	var b strings.Builder
	for _, r := range s {
		safe := r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == '!' || r == '#' || r == '$' || r == '%' ||
			(r >= '(' && r <= ';') || r == '=' || (r >= '?' && r <= '~')
		if safe {
			b.WriteRune(r)
			continue
		}
		if e, ok := char2entity[r]; ok {
			b.WriteString(e)
		} else {
			fmt.Fprintf(&b, "&#x%X;", r)
		}
	}
	return b.String()
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

// indentLines is s/^/$pad/mg
func indentLines(s, pad string) string {
	var b strings.Builder
	b.WriteString(pad)
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
		if s[i] == '\n' && i+1 < len(s) {
			b.WriteString(pad)
		}
	}
	return b.String()
}

// perlSplitNewlines is split(/\n/, $text)
func perlSplitNewlines(s string) []string {
	parts := strings.Split(s, "\n")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

var entityRe = regexp2.MustCompile(`&(?:[a-zA-Z][a-zA-Z0-9]*|#[0-9]+|#x[0-9a-fA-F]+);`, regexp2.None)

func visualLength(s string) int {
	r, _ := entityRe.Replace(s, "_", -1, -1)
	return utf8.RuneCountInString(r)
}

func truncateVisual(s string, max int) string {
	var b strings.Builder
	runes := []rune(s)
	v := 0
	for i := 0; i < len(runes); {
		if v >= max {
			break
		}
		if runes[i] == '&' {
			if m, _ := entityRe.FindStringMatch(string(runes[i:])); m != nil && m.Index == 0 {
				b.WriteString(m.String())
				i += m.Length
				v++
				continue
			}
		}
		b.WriteRune(runes[i])
		i++
		v++
	}
	return b.String()
}

// replaceLiteral is s{$search}{$replace}eg with TT's empty match rule
func replaceLiteral(re *regexp2.Regexp, s, replace string) (string, error) {
	runes := []rune(s)
	ms, err := reAll(re, s)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	pos, prevEnd := 0, -1
	for _, m := range ms {
		b.WriteString(string(runes[pos:m.start]))
		if !(m.start == m.end && m.start == prevEnd) {
			prevEnd = m.end
			b.WriteString(replace)
		}
		pos = m.end
	}
	b.WriteString(string(runes[pos:]))
	return b.String(), nil
}

// perlSprintf implements the common sprintf conversions with Perl's
// argument conversion rules.
func perlSprintf(format string, args []any) string {
	var b strings.Builder
	ai := 0
	next := func() any {
		if ai < len(args) {
			v := args[ai]
			ai++
			return v
		}
		ai++
		return nil
	}
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			b.WriteByte(format[i])
			continue
		}
		j := i + 1
		for j < len(format) && strings.IndexByte("-+ 0#", format[j]) >= 0 {
			j++
		}
		for j < len(format) && (isDigit(format[j]) || format[j] == '*') {
			j++
		}
		if j < len(format) && format[j] == '.' {
			j++
			for j < len(format) && isDigit(format[j]) {
				j++
			}
		}
		for j < len(format) && strings.IndexByte("hlqLV", format[j]) >= 0 {
			j++
		}
		if j >= len(format) {
			b.WriteString(format[i:])
			break
		}
		conv := format[j]
		spec := strings.Map(func(r rune) rune {
			if strings.ContainsRune("hlqLV", r) {
				return -1
			}
			return r
		}, format[i:j])
		switch conv {
		case '%':
			b.WriteByte('%')
		case 's':
			fmt.Fprintf(&b, spec+"s", Str(next()))
		case 'd', 'i':
			v := perlInt(next())
			switch n := v.(type) {
			case int64:
				fmt.Fprintf(&b, spec+"d", n)
			case float64:
				if math.IsInf(n, 0) || math.IsNaN(n) {
					fmt.Fprintf(&b, "%v", n)
				} else {
					fmt.Fprintf(&b, spec+".0f", n)
				}
			}
		case 'u':
			v, _ := perlInt(next()).(int64)
			fmt.Fprintf(&b, spec+"d", uint64(v)) // #nosec G115 -- Perl's %u of a negative number
		case 'f', 'F', 'e', 'E', 'g', 'G':
			fmt.Fprintf(&b, spec+string(conv), Float(next()))
		case 'x', 'X', 'o', 'b', 'B':
			v, _ := perlInt(next()).(int64)
			c := conv
			if c == 'B' {
				c = 'b'
			}
			fmt.Fprintf(&b, spec+string(c), uint64(v)) // #nosec G115 -- Perl's %x/%o/%b of a negative number
		case 'c':
			v, _ := perlInt(next()).(int64)
			b.WriteRune(rune(v)) // #nosec G115 -- Perl's %c; an invalid code point is written as U+FFFD
		default:
			b.WriteString(format[i : j+1])
		}
		i = j
	}
	return b.String()
}

// EncodeEntities is HTML::Entities::encode_entities with the default
// unsafe characters
func EncodeEntities(s string) string { return encodeEntities(s) }
