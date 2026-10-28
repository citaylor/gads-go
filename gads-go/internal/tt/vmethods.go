package tt

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

type vmethod func(c *Context, root any, args []any) ([]any, error)

type listMethod func(c *Context, l *List, args []any) ([]any, error)

type hashMethod func(c *Context, h *Hash, args []any) ([]any, error)

var scalarOps map[string]vmethod
var listOps map[string]listMethod
var hashOps map[string]hashMethod

func one(v any) []any { return []any{v} }

// ----------------------------------------------------------------------
// Perl regular expressions
// ----------------------------------------------------------------------

var reCache sync.Map

// PerlRegexp compiles a Perl regular expression.
func PerlRegexp(pat string) (*regexp2.Regexp, error) {
	if r, ok := reCache.Load(pat); ok {
		return r.(*regexp2.Regexp), nil
	}
	re, err := regexp2.Compile(pat, regexp2.None)
	if err != nil {
		return nil, err
	}
	reCache.Store(pat, re)
	return re, nil
}

type reMatch struct {
	start, end int // rune offsets
	groups     []*string
	gstart     []int
	gend       []int
}

func toMatch(m *regexp2.Match) reMatch {
	r := reMatch{start: m.Index, end: m.Index + m.Length}
	for i, g := range m.Groups() {
		if i == 0 {
			continue
		}
		if len(g.Captures) == 0 {
			r.groups = append(r.groups, nil)
			r.gstart = append(r.gstart, -1)
			r.gend = append(r.gend, -1)
			continue
		}
		s := g.String()
		r.groups = append(r.groups, &s)
		r.gstart = append(r.gstart, g.Index)
		r.gend = append(r.gend, g.Index+g.Length)
	}
	return r
}

func reFirst(re *regexp2.Regexp, s string) (*reMatch, error) {
	m, err := re.FindStringMatch(s)
	if err != nil || m == nil {
		return nil, err
	}
	r := toMatch(m)
	return &r, nil
}

// reAll returns all matches as Perl's //g does.
func reAll(re *regexp2.Regexp, s string) ([]reMatch, error) {
	var res []reMatch
	m, err := re.FindStringMatch(s)
	for err == nil && m != nil {
		res = append(res, toMatch(m))
		m, err = re.FindNextMatch(m)
	}
	return res, err
}

func runeSlice(s string, from, to int) string {
	r := []rune(s)
	if from < 0 {
		from = 0
	}
	if to > len(r) {
		to = len(r)
	}
	if from > to {
		return ""
	}
	return string(r[from:to])
}

// perlSplit implements split(/re/, $str, $limit) (awk is split(' ', ...))
func perlSplit(re *regexp2.Regexp, awk bool, s string, limit int) ([]any, error) {
	var fields []any
	if awk {
		s = strings.TrimLeftFunc(s, isSpace)
		var err error
		re, err = PerlRegexp(`\s+`)
		if err != nil {
			return nil, err
		}
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return []any{}, nil
	}
	matches, err := reAll(re, s)
	if err != nil {
		return nil, err
	}
	pos := 0
	for _, m := range matches {
		if limit > 0 && len(fields) >= limit-1 {
			break
		}
		if m.end == 0 && m.start == 0 {
			// zero-length match at the start produces no field
			continue
		}
		if m.start == m.end && m.start >= len(runes) {
			break
		}
		if m.start == m.end && m.start == pos && pos == 0 {
			continue
		}
		fields = append(fields, string(runes[pos:m.start]))
		for _, g := range m.groups {
			if g == nil {
				fields = append(fields, nil)
			} else {
				fields = append(fields, *g)
			}
		}
		pos = m.end
	}
	fields = append(fields, string(runes[pos:]))
	if limit == 0 {
		for len(fields) > 0 {
			last := fields[len(fields)-1]
			if last == nil || last == "" {
				fields = fields[:len(fields)-1]
				continue
			}
			break
		}
	}
	return fields, nil
}

// ----------------------------------------------------------------------
// scalar (text) virtual methods
// ----------------------------------------------------------------------

func ucfirst(s string) string {
	r, sz := utf8.DecodeRuneInString(s)
	if sz == 0 {
		return s
	}
	return string(unicode.ToTitle(r)) + s[sz:]
}

func lcfirst(s string) string {
	r, sz := utf8.DecodeRuneInString(s)
	if sz == 0 {
		return s
	}
	return string(unicode.ToLower(r)) + s[sz:]
}

func perlLc(s string) string { return strings.ToLower(s) }
func perlUc(s string) string { return strings.ToUpper(s) }

func collapse(s string) string {
	s = strings.TrimFunc(s, isSpace)
	return strings.Join(strings.FieldsFunc(s, isSpace), " ")
}

func textReplace(text, pattern, replace string, global bool) (string, error) {
	re, err := PerlRegexp(pattern)
	if err != nil {
		return "", err
	}
	runes := []rune(text)
	var matches []reMatch
	if global {
		matches, err = reAll(re, text)
	} else {
		var m *reMatch
		m, err = reFirst(re, text)
		if m != nil {
			matches = []reMatch{*m}
		}
	}
	if err != nil {
		return "", err
	}
	hasGroup, _ := dollarDigitRe.MatchString(replace)
	var b strings.Builder
	pos := 0
	prevEnd := -1
	for _, m := range matches {
		b.WriteString(string(runes[pos:m.start]))
		if global && m.start == m.end && m.start == prevEnd {
			// empty match directly after the previous match
		} else {
			prevEnd = m.end
			if hasGroup {
				b.WriteString(expandReplace(replace, runes, m))
			} else {
				b.WriteString(replace)
			}
		}
		pos = m.end
	}
	b.WriteString(string(runes[pos:]))
	return b.String(), nil
}

var dollarDigitRe = regexp2.MustCompile(`\$\d+`, regexp2.None)

// expandReplace: s{ \\(\\|\$) | \$ (\d+) }{...}exg
func expandReplace(chunk string, text []rune, m reMatch) string {
	var b strings.Builder
	for i := 0; i < len(chunk); i++ {
		if chunk[i] == '\\' && i+1 < len(chunk) && (chunk[i+1] == '\\' || chunk[i+1] == '$') {
			b.WriteByte(chunk[i+1])
			i++
			continue
		}
		if chunk[i] == '$' && i+1 < len(chunk) && isDigit(chunk[i+1]) {
			j := i + 1
			for j < len(chunk) && isDigit(chunk[j]) {
				j++
			}
			n, _ := strconv.Atoi(chunk[i+1 : j])
			if n > 0 && n <= len(m.groups) && m.groups[n-1] != nil {
				b.WriteString(string(text[m.gstart[n-1]:m.gend[n-1]]))
			}
			i = j - 1
			continue
		}
		b.WriteByte(chunk[i])
	}
	return b.String()
}

func init() {
	scalarOps = map[string]vmethod{
		"item": func(c *Context, r any, a []any) ([]any, error) { return one(r), nil },
		"list": func(c *Context, r any, a []any) ([]any, error) { return one(NewList(r)), nil },
		"hash": func(c *Context, r any, a []any) ([]any, error) { return one(HashOf("value", r)), nil },
		"length": func(c *Context, r any, a []any) ([]any, error) {
			return one(int64(utf8.RuneCountInString(Str(r)))), nil
		},
		"size": func(c *Context, r any, a []any) ([]any, error) { return one(int64(1)), nil },
		"empty": func(c *Context, r any, a []any) ([]any, error) {
			if Str(r) == "" {
				return one(int64(1)), nil
			}
			return one(int64(0)), nil
		},
		"defined": func(c *Context, r any, a []any) ([]any, error) { return one(int64(1)), nil },
		"upper":   func(c *Context, r any, a []any) ([]any, error) { return one(perlUc(Str(r))), nil },
		"lower":   func(c *Context, r any, a []any) ([]any, error) { return one(perlLc(Str(r))), nil },
		"ucfirst": func(c *Context, r any, a []any) ([]any, error) { return one(ucfirst(Str(r))), nil },
		"lcfirst": func(c *Context, r any, a []any) ([]any, error) { return one(lcfirst(Str(r))), nil },
		"trim": func(c *Context, r any, a []any) ([]any, error) {
			return one(strings.TrimFunc(Str(r), isSpace)), nil
		},
		"collapse": func(c *Context, r any, a []any) ([]any, error) { return one(collapse(Str(r))), nil },
		"match": func(c *Context, r any, a []any) ([]any, error) {
			if r == nil || argN(a, 0) == nil {
				return one(r), nil
			}
			re, err := PerlRegexp(Str(a[0]))
			if err != nil {
				return nil, err
			}
			s := Str(r)
			var res []any
			if True(argN(a, 1)) {
				ms, err := reAll(re, s)
				if err != nil {
					return nil, err
				}
				for _, m := range ms {
					if len(m.groups) == 0 {
						res = append(res, runeSlice(s, m.start, m.end))
					}
					for _, g := range m.groups {
						if g == nil {
							res = append(res, nil)
						} else {
							res = append(res, *g)
						}
					}
				}
			} else {
				m, err := reFirst(re, s)
				if err != nil {
					return nil, err
				}
				if m != nil {
					if len(m.groups) == 0 {
						res = append(res, int64(1))
					}
					for _, g := range m.groups {
						if g == nil {
							res = append(res, nil)
						} else {
							res = append(res, *g)
						}
					}
				}
			}
			if len(res) == 0 {
				return one(""), nil
			}
			return one(NewList(res...)), nil
		},
		"search": func(c *Context, r any, a []any) ([]any, error) {
			if r == nil || argN(a, 0) == nil {
				return one(r), nil
			}
			re, err := PerlRegexp(Str(a[0]))
			if err != nil {
				return nil, err
			}
			ok, err := re.MatchString(Str(r))
			if err != nil {
				return nil, err
			}
			return one(Bool(ok)), nil
		},
		"repeat": func(c *Context, r any, a []any) ([]any, error) {
			n := argN(a, 0)
			if !True(n) {
				return one(""), nil
			}
			cnt, _ := perlInt(n).(int64)
			if cnt < 0 {
				cnt = 0
			}
			return one(strings.Repeat(Str(r), int(cnt))), nil
		},
		"replace": func(c *Context, r any, a []any) ([]any, error) {
			global := true
			if len(a) > 2 && a[2] != nil {
				global = True(a[2])
			}
			s, err := textReplace(Str(r), Str(argN(a, 0)), Str(argN(a, 1)), global)
			if err != nil {
				return nil, err
			}
			return one(s), nil
		},
		"remove": func(c *Context, r any, a []any) ([]any, error) {
			if r == nil || argN(a, 0) == nil {
				return one(r), nil
			}
			s, err := textReplace(Str(r), Str(a[0]), "", true)
			if err != nil {
				return nil, err
			}
			return one(s), nil
		},
		"split": func(c *Context, r any, a []any) ([]any, error) {
			s := Str(r)
			limit := 0
			if l := argN(a, 1); True(l) {
				n, _ := perlInt(l).(int64)
				limit = int(n)
			}
			var re *regexp2.Regexp
			awk := true
			if p := argN(a, 0); p != nil {
				if rr, err := PerlRegexp(Str(p)); err == nil {
					re, awk = rr, false
				}
			}
			f, err := perlSplit(re, awk, s, limit)
			if err != nil {
				return nil, err
			}
			return one(&List{Items: f}), nil
		},
		"chunk": func(c *Context, r any, a []any) ([]any, error) {
			s := []rune(Str(r))
			size := 1
			if n := argN(a, 0); True(n) {
				v, _ := perlInt(n).(int64)
				size = int(v)
			}
			l := &List{}
			if size < 0 {
				size = -size
				for end := len(s); end > 0; end -= size {
					start := end - size
					if start < 0 {
						start = 0
					}
					l.Items = append([]any{string(s[start:end])}, l.Items...)
				}
			} else if size > 0 {
				for i := 0; i < len(s); i += size {
					e := i + size
					if e > len(s) {
						e = len(s)
					}
					l.Items = append(l.Items, string(s[i:e]))
				}
			}
			return one(l), nil
		},
		"substr": func(c *Context, r any, a []any) ([]any, error) {
			s := []rune(Str(r))
			off := 0
			if n := argN(a, 0); True(n) {
				v, _ := perlInt(n).(int64)
				off = int(v)
			}
			if off < 0 {
				off += len(s)
				if off < 0 {
					off = 0
				}
			}
			if off > len(s) {
				return one(nil), nil
			}
			if len(a) > 1 && a[1] != nil {
				v, _ := perlInt(a[1]).(int64)
				ln := int(v)
				end := off + ln
				if ln < 0 {
					end = len(s) + ln
				}
				if end > len(s) {
					end = len(s)
				}
				if end < off {
					end = off
				}
				if len(a) > 2 && a[2] != nil {
					res := string(s[:off]) + Str(a[2]) + string(s[end:])
					return one(res), nil
				}
				return one(string(s[off:end])), nil
			}
			return one(string(s[off:])), nil
		},
		"squote": func(c *Context, r any, a []any) ([]any, error) {
			s := Str(r)
			s = strings.ReplaceAll(s, `\`, `\\`)
			s = strings.ReplaceAll(s, `'`, `\'`)
			return one(s), nil
		},
		"dquote": func(c *Context, r any, a []any) ([]any, error) {
			s := Str(r)
			s = strings.ReplaceAll(s, `\`, `\\`)
			s = strings.ReplaceAll(s, `"`, `\"`)
			s = strings.ReplaceAll(s, "\n", `\n`)
			return one(s), nil
		},
		"html": func(c *Context, r any, a []any) ([]any, error) { return one(htmlFilter(Str(r))), nil },
		"xml":  func(c *Context, r any, a []any) ([]any, error) { return one(xmlFilter(Str(r))), nil },
	}

	listOps = map[string]listMethod{
		"item": func(c *Context, l *List, a []any) ([]any, error) {
			i := 0
			if n := argN(a, 0); True(n) {
				v, _ := perlInt(n).(int64)
				i = int(v)
			}
			if i < 0 {
				i += len(l.Items)
			}
			if i < 0 || i >= len(l.Items) {
				return one(nil), nil
			}
			return one(l.Items[i]), nil
		},
		"list": func(c *Context, l *List, a []any) ([]any, error) { return one(l), nil },
		"hash": func(c *Context, l *List, a []any) ([]any, error) {
			h := NewHash()
			if len(a) > 0 {
				n := int64(0)
				if True(a[0]) {
					n, _ = perlInt(a[0]).(int64)
				}
				for _, v := range l.Items {
					h.Set(strconv.FormatInt(n, 10), v)
					n++
				}
				return one(h), nil
			}
			for i := 0; i < len(l.Items); i += 2 {
				var v any
				if i+1 < len(l.Items) {
					v = l.Items[i+1]
				}
				h.Set(Str(l.Items[i]), v)
			}
			return one(h), nil
		},
		"push": func(c *Context, l *List, a []any) ([]any, error) {
			l.Items = append(l.Items, a...)
			return one(""), nil
		},
		"pop": func(c *Context, l *List, a []any) ([]any, error) {
			if len(l.Items) == 0 {
				return one(nil), nil
			}
			v := l.Items[len(l.Items)-1]
			l.Items = l.Items[:len(l.Items)-1]
			return one(v), nil
		},
		"unshift": func(c *Context, l *List, a []any) ([]any, error) {
			l.Items = append(append([]any{}, a...), l.Items...)
			return one(""), nil
		},
		"shift": func(c *Context, l *List, a []any) ([]any, error) {
			if len(l.Items) == 0 {
				return one(nil), nil
			}
			v := l.Items[0]
			l.Items = l.Items[1:]
			return one(v), nil
		},
		"max":  func(c *Context, l *List, a []any) ([]any, error) { return one(int64(len(l.Items) - 1)), nil },
		"size": func(c *Context, l *List, a []any) ([]any, error) { return one(int64(len(l.Items))), nil },
		"empty": func(c *Context, l *List, a []any) ([]any, error) {
			if len(l.Items) == 0 {
				return one(int64(1)), nil
			}
			return one(int64(0)), nil
		},
		"defined": func(c *Context, l *List, a []any) ([]any, error) {
			if len(a) == 0 {
				return one(int64(1)), nil
			}
			if !looksLikeNumber(a[0]) {
				return nil, nil
			}
			i, _ := perlInt(a[0]).(int64)
			idx := int(i)
			if idx < 0 {
				idx += len(l.Items)
			}
			return one(Bool(idx >= 0 && idx < len(l.Items) && l.Items[idx] != nil)), nil
		},
		"first": func(c *Context, l *List, a []any) ([]any, error) {
			if len(a) == 0 {
				if len(l.Items) == 0 {
					return one(nil), nil
				}
				return one(l.Items[0]), nil
			}
			n, _ := perlInt(a[0]).(int64)
			return one(&List{Items: perlSliceRange(l.Items, 0, int(n)-1)}), nil
		},
		"last": func(c *Context, l *List, a []any) ([]any, error) {
			if len(a) == 0 {
				if len(l.Items) == 0 {
					return one(nil), nil
				}
				return one(l.Items[len(l.Items)-1]), nil
			}
			n, _ := perlInt(a[0]).(int64)
			return one(&List{Items: perlSliceRange(l.Items, -int(n), -1)}), nil
		},
		"reverse": func(c *Context, l *List, a []any) ([]any, error) {
			r := make([]any, len(l.Items))
			for i, v := range l.Items {
				r[len(l.Items)-1-i] = v
			}
			return one(&List{Items: r}), nil
		},
		"grep": func(c *Context, l *List, a []any) ([]any, error) {
			pat := ""
			if p := argN(a, 0); p != nil {
				pat = Str(p)
			}
			re, err := PerlRegexp(pat)
			if err != nil {
				return nil, err
			}
			res := &List{}
			for _, v := range l.Items {
				ok, err := re.MatchString(Str(v))
				if err != nil {
					return nil, err
				}
				if ok {
					res.Items = append(res.Items, v)
				}
			}
			return one(res), nil
		},
		"join": func(c *Context, l *List, a []any) ([]any, error) {
			sep := " "
			if j := argN(a, 0); j != nil {
				sep = Str(j)
			}
			parts := make([]string, len(l.Items))
			for i, v := range l.Items {
				parts[i] = Str(v)
			}
			return one(strings.Join(parts, sep)), nil
		},
		"sort":  listSort,
		"nsort": listNsort,
		"unique": func(c *Context, l *List, a []any) ([]any, error) {
			seen := map[string]bool{}
			res := &List{}
			for _, v := range l.Items {
				k := Str(v)
				if !seen[k] {
					seen[k] = true
					res.Items = append(res.Items, v)
				}
			}
			return one(res), nil
		},
		"import": func(c *Context, l *List, a []any) ([]any, error) {
			for _, v := range a {
				if o, ok := v.(*List); ok {
					l.Items = append(l.Items, o.Items...)
				}
			}
			return one(l), nil
		},
		"merge": func(c *Context, l *List, a []any) ([]any, error) {
			res := &List{Items: append([]any{}, l.Items...)}
			for _, v := range a {
				if o, ok := v.(*List); ok {
					res.Items = append(res.Items, o.Items...)
				}
			}
			return one(res), nil
		},
		"slice": func(c *Context, l *List, a []any) ([]any, error) {
			from := 0
			if f := argN(a, 0); True(f) {
				v, _ := perlInt(f).(int64)
				from = int(v)
			}
			to := len(l.Items) - 1
			if t := argN(a, 1); t != nil {
				v, _ := perlInt(t).(int64)
				to = int(v)
			}
			if from < 0 {
				from += len(l.Items)
			}
			if to < 0 {
				to += len(l.Items)
			}
			var res []any
			for i := from; i <= to; i++ {
				if i >= 0 && i < len(l.Items) {
					res = append(res, l.Items[i])
				} else {
					res = append(res, nil)
				}
			}
			return one(&List{Items: res}), nil
		},
		"splice": func(c *Context, l *List, a []any) ([]any, error) {
			n := len(l.Items)
			off := 0
			if len(a) > 0 && a[0] != nil {
				v, _ := perlInt(a[0]).(int64)
				off = int(v)
				if off < 0 {
					off += n
				}
			}
			if off > n {
				off = n
			}
			length := n - off
			if len(a) > 1 && a[1] != nil {
				v, _ := perlInt(a[1]).(int64)
				length = int(v)
				if length < 0 {
					length = n - off + length
					if length < 0 {
						length = 0
					}
				}
			}
			if off+length > n {
				length = n - off
			}
			var repl []any
			if len(a) > 2 {
				repl = a[2:]
				if len(repl) == 1 {
					if rl, ok := repl[0].(*List); ok {
						repl = rl.Items
					}
				}
			}
			removed := append([]any{}, l.Items[off:off+length]...)
			rest := append(append(append([]any{}, l.Items[:off]...), repl...), l.Items[off+length:]...)
			l.Items = rest
			return one(&List{Items: removed}), nil
		},
	}

	hashOps = map[string]hashMethod{
		"item": func(c *Context, h *Hash, a []any) ([]any, error) {
			k := ""
			if v := argN(a, 0); v != nil {
				k = Str(v)
			}
			if isPrivate(k) {
				return nil, nil
			}
			v, _ := h.Get(k)
			return one(v), nil
		},
		"hash": func(c *Context, h *Hash, a []any) ([]any, error) { return one(h), nil },
		"size": func(c *Context, h *Hash, a []any) ([]any, error) { return one(int64(h.Len())), nil },
		"empty": func(c *Context, h *Hash, a []any) ([]any, error) {
			if h.Len() == 0 {
				return one(int64(1)), nil
			}
			return one(int64(0)), nil
		},
		"each":  hashEach,
		"items": hashEach,
		"keys": func(c *Context, h *Hash, a []any) ([]any, error) {
			l := &List{}
			for _, k := range h.Keys() {
				l.Items = append(l.Items, k)
			}
			return one(l), nil
		},
		"values": func(c *Context, h *Hash, a []any) ([]any, error) {
			l := &List{}
			for _, k := range h.Keys() {
				v, _ := h.Get(k)
				l.Items = append(l.Items, v)
			}
			return one(l), nil
		},
		"pairs": func(c *Context, h *Hash, a []any) ([]any, error) { return one(hashPairs(h)), nil },
		"list": func(c *Context, h *Hash, a []any) ([]any, error) {
			what := ""
			if v := argN(a, 0); True(v) {
				what = Str(v)
			}
			switch what {
			case "keys":
				return hashOps["keys"](c, h, nil)
			case "values":
				return hashOps["values"](c, h, nil)
			case "each":
				return hashEach(c, h, nil)
			}
			return one(hashPairs(h)), nil
		},
		"exists": func(c *Context, h *Hash, a []any) ([]any, error) {
			_, ok := h.Get(Str(argN(a, 0)))
			return one(Bool(ok)), nil
		},
		"defined": func(c *Context, h *Hash, a []any) ([]any, error) {
			if len(a) == 0 {
				return one(int64(1)), nil
			}
			v, _ := h.Get(Str(a[0]))
			return one(Bool(v != nil)), nil
		},
		"delete": func(c *Context, h *Hash, a []any) ([]any, error) {
			for _, k := range a {
				h.Delete(Str(k))
			}
			return nil, nil
		},
		"import": func(c *Context, h *Hash, a []any) ([]any, error) {
			if imp, ok := argN(a, 0).(*Hash); ok {
				for _, k := range imp.Keys() {
					v, _ := imp.Get(k)
					h.Set(k, v)
				}
			}
			return one(""), nil
		},
		"sort": func(c *Context, h *Hash, a []any) ([]any, error) {
			keys := sortedKeys(h)
			sort.SliceStable(keys, func(i, j int) bool {
				vi, _ := h.Get(keys[i])
				vj, _ := h.Get(keys[j])
				return perlLc(Str(vi)) < perlLc(Str(vj))
			})
			return one(stringList(keys)), nil
		},
		"nsort": func(c *Context, h *Hash, a []any) ([]any, error) {
			keys := sortedKeys(h)
			sort.SliceStable(keys, func(i, j int) bool {
				vi, _ := h.Get(keys[i])
				vj, _ := h.Get(keys[j])
				return numCompare(vi, vj) < 0
			})
			return one(stringList(keys)), nil
		},
	}
}

func stringList(s []string) *List {
	l := &List{}
	for _, k := range s {
		l.Items = append(l.Items, k)
	}
	return l
}

func hashEach(c *Context, h *Hash, a []any) ([]any, error) {
	l := &List{}
	for _, k := range h.Keys() {
		v, _ := h.Get(k)
		l.Items = append(l.Items, k, v)
	}
	return one(l), nil
}

func hashPairs(h *Hash) *List {
	l := &List{}
	for _, k := range sortedKeys(h) {
		v, _ := h.Get(k)
		l.Items = append(l.Items, HashOf("key", k, "value", v))
	}
	return l
}

func looksLikeNumber(v any) bool {
	switch t := v.(type) {
	case int64, int, float64:
		return true
	case string:
		_, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return err == nil && strings.TrimLeft(t, " \t\n") == t || err == nil
	}
	return false
}

// perlSliceRange is @list[$from..$to] with negative indexes
func perlSliceRange(items []any, from, to int) []any {
	n := len(items)
	var res []any
	for i := from; i <= to; i++ {
		j := i
		if j < 0 {
			j += n
		}
		if j >= 0 && j < n {
			res = append(res, items[j])
		} else {
			res = append(res, nil)
		}
	}
	return res
}

// sortKey is _list_sort_make_key
func (c *Context) sortKey(item any, fields []any) string {
	var keys []string
	switch t := item.(type) {
	case *Hash:
		for _, f := range fields {
			v, _ := t.Get(Str(f))
			keys = append(keys, Str(v))
		}
	case Object:
		for _, f := range fields {
			res, ok, _ := t.TTMethod(Str(f), nil)
			if ok {
				if len(res) > 0 {
					keys = append(keys, Str(res[0]))
				} else {
					keys = append(keys, "")
				}
			} else {
				keys = append(keys, Str(item))
			}
		}
	default:
		keys = []string{Str(item)}
	}
	return perlLc(strings.Join(keys, "/*^UNLIKELY^*/"))
}

func listSort(c *Context, l *List, a []any) ([]any, error) {
	if len(l.Items) <= 1 {
		return one(l), nil
	}
	type kv struct {
		v any
		k string
	}
	items := make([]kv, len(l.Items))
	for i, v := range l.Items {
		if len(a) > 0 {
			items[i] = kv{v, c.sortKey(v, a)}
		} else {
			items[i] = kv{v, perlLc(Str(v))}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].k < items[j].k })
	res := &List{}
	for _, it := range items {
		res.Items = append(res.Items, it.v)
	}
	return one(res), nil
}

func listNsort(c *Context, l *List, a []any) ([]any, error) {
	if len(l.Items) <= 1 {
		return one(l), nil
	}
	items := append([]any{}, l.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		if len(a) > 0 {
			for _, f := range a {
				ka := c.sortKey(items[i], []any{f})
				kb := c.sortKey(items[j], []any{f})
				if r := numCompare(ka, kb); r != 0 {
					return r < 0
				}
			}
			return false
		}
		return numCompare(c.sortKey(items[i], nil), c.sortKey(items[j], nil)) < 0
	})
	return one(&List{Items: items}), nil
}
