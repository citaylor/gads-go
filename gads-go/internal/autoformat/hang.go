package autoformat

import (
	"strconv"
	"strings"
	"sync"

	"github.com/dlclark/regexp2"
)

// rx is a Perl regular expression (regexp2: lookarounds, backreferences,
// and Unicode \s, \w and \b as Perl's for character strings)
type rx struct{ re *regexp2.Regexp }

func mustRx(pat string, opts regexp2.RegexOptions) rx {
	return rx{regexp2.MustCompile(pat, opts)}
}

// find returns the groups of the first match (nil for none), and where the
// match ends, in runes
func (r rx) find(s string) ([]string, int) {
	m, err := r.re.FindStringMatch(s)
	if err != nil || m == nil {
		return nil, 0
	}
	g := make([]string, m.GroupCount())
	for i := range g {
		g[i] = m.GroupByNumber(i).String()
	}
	return g, m.Index + m.Length
}

func (r rx) match(s string) bool {
	ok, _ := r.re.MatchString(s)
	return ok
}

// cut is s =~ s/\A(...)//: the groups and the rest of s
func (r rx) cut(s *string) []string {
	g, end := r.find(*s)
	if g != nil {
		*s = string([]rune(*s)[end:])
	}
	return g
}

// Text::Autoformat::Hang (1.75): the marker of a list item or note, as
// levels of a number, Roman numeral or letter with their brackets and
// punctuation ("1.2)"), a bullet, or a PS. The empty hang is NullHang.

var (
	unit = []string{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX"}
	ten  = []string{"", "X", "XX", "XXX", "XL", "L", "LX", "LXX", "LXXX", "XC"}
	hund = []string{"", "C", "CC", "CCC", "CD", "D", "DC", "DCC", "DCCC", "CM"}
	thou = []string{"", "M", "MM", "MMM"}
	rval = map[string]int{}
)

func reversed(l []string) string {
	r := make([]string, len(l))
	for i, s := range l {
		r[len(l)-1-i] = s
	}
	return strings.Join(r, "|")
}

var (
	rbpat = strings.Join([]string{reversed(thou), reversed(hund), reversed(ten), reversed(unit)}, ")(")
	rpat  = strings.Join([]string{reversed(thou), reversed(hund), reversed(ten), reversed(unit)}, ")(?:")
	rom   = `(?:(?=[MDCLXVI])(?:` + rpat + `))`
	// the abbreviations that end the levels of a hang
	abbrev = strings.Join([]string{`etc[.]`, `pp[.]`, `ph[.]?d[.]`,
		`(?!` + rom + `)(?:[A-Z][A-Za-z]+[.])+`, `(?:[A-Z][.])(?:[A-Z][.])+`}, "|")

	num   = `(?:[0-9]{1,3}\b(?!:[0-9][0-9]\b))` // Ignore 8:20 etc.
	let   = `[A-Za-z]`
	pbr   = `[\[(<]`
	ows   = `[ \t]*`
	close = map[string]string{"[": `\]`, "(": `\)`, "<": `>`, "": ``}

	hangPS     = `(?i:ps:|(?:p\.?)+s\b\.?(?:[ \t]*:)?)`
	hangNB     = `(?i:n\.?b\.?(?:[ \t]*:)?)`
	hangword   = `(?:(?:Note)[ \t]*:)`
	hangbullet = `[*.+-]`
	hang       = `(?:(?i)(?:` + hangNB + `|` + hangword + `|` + hangbullet + `)(?=[ \t]))`

	reRoman     = mustRx(`^.*?(`+rbpat+`).*$`, regexp2.IgnoreCase|regexp2.Singleline)
	reToRoman   = mustRx(`^([0-3]??)(\d??)(\d??)(\d)$`, 0)
	reHangPS    = mustRx(`\A(`+hangPS+`)`, 0)
	reHang      = mustRx(`\A(`+hang+`)`, 0)
	reParenWord = mustRx(`\A\([^\s)]+\s`, 0)
	reAbbrev    = mustRx(`\A(`+ows+`)(`+abbrev+`)`, 0)
	reBracketed = mustRx(`\A`+ows+pbr+`[^\])> \t]*\s`, 0)
	rePre       = mustRx(`\A(`+ows+pbr+ows+`)`, 0)
	reNum       = mustRx(`\A(`+num+`)`, 0)
	reRom       = mustRx(`\A(`+rom+`)\b`, regexp2.IgnoreCase)
	reLet       = mustRx(`\A(`+let+`(?!`+let+`))`, regexp2.IgnoreCase)
	rePost      = mustRx(`\A(`+ows+`()[\])>.:/])`, 0)
	reOrphan    = mustRx(`[\.>)]`, 0)
	// rePostClose caches postRx (the engines of a pool share it)
	rePostClose sync.Map
	rePSCount   = mustRx(`(p[.]?)`, regexp2.IgnoreCase)
	rePSParts   = mustRx(`^(p[.]?).*(s[.]?[:]?)`, 0)
	reUpper     = mustRx(`[A-Z]`, 0)
)

func init() {
	for k, l := range map[int][]string{1: unit, 10: ten, 100: hund, 1000: thou} {
		for i, s := range l {
			rval[s] = i * k
		}
	}
}

// postRx is the closing bracket of a level opened by pre: Perl's
// [$close{$pre}] is [][.:/] for a pre with spaces, which has no entry
func postRx(pre string) rx {
	if r, ok := rePostClose.Load(pre); ok {
		return r.(rx)
	}
	class := `[\]\[.:/]`
	if c, ok := close[pre]; ok {
		class = `[` + c + `]`
	}
	r := mustRx(`\A(`+ows+`()[.:/]?`+class+`[.:/]?)`, 0)
	rePostClose.Store(pre, r)
	return r
}

func fromRoman(s string) int {
	g, _ := reRoman.find(s)
	if g == nil {
		return 0
	}
	return rval[strings.ToUpper(g[1])] + rval[strings.ToUpper(g[2])] + rval[strings.ToUpper(g[3])] + rval[strings.ToUpper(g[4])]
}

func toRoman(n int, example string) string {
	g, _ := reToRoman.find(strconv.Itoa(n))
	if g == nil {
		return ""
	}
	d := func(s string) int { i, _ := strconv.Atoi(s); return i }
	roman := thou[d(g[1])] + hund[d(g[2])] + ten[d(g[3])] + unit[d(g[4])]
	if reUpper.match(example) {
		return strings.ToUpper(roman)
	}
	return strings.ToLower(roman)
}

// pscalar is a Perl scalar of a level: a string, which becomes a number
// once used as one (then ++ adds one instead of the magic increment)
type pscalar struct {
	s       string
	numeric bool
	defined bool
}

func str(s string) pscalar { return pscalar{s: s, defined: true} }

func intVal(n int) pscalar { return pscalar{s: strconv.Itoa(n), numeric: true, defined: true} }

// incr is Perl's ++
func (v *pscalar) incr() {
	if v.numeric || !v.defined || v.s == "" || !magicIncr(v.s) {
		n, _ := strconv.ParseFloat(strings.TrimSpace(v.s), 64)
		if !v.numeric {
			n = leadingNumber(v.s)
		}
		*v = pscalar{s: strconv.FormatFloat(n+1, 'f', -1, 64), numeric: true, defined: true}
		return
	}
	v.s = magicIncrement(v.s)
}

// magicIncr is whether ++ is the magic string increment: the string is
// letters then digits (/^[a-zA-Z]*[0-9]*$/)
func magicIncr(s string) bool {
	i := 0
	for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i == len(s)
}

func magicIncrement(s string) string {
	b := []byte(s)
	for i := len(b) - 1; i >= 0; i-- {
		switch {
		case b[i] == 'z':
			b[i] = 'a'
		case b[i] == 'Z':
			b[i] = 'A'
		case b[i] == '9':
			b[i] = '0'
		default:
			b[i]++
			return string(b)
		}
	}
	// carry out of the first character
	first := "1"
	switch {
	case s[0] >= 'a' && s[0] <= 'z':
		first = "a"
	case s[0] >= 'A' && s[0] <= 'Z':
		first = "A"
	}
	return first + string(b)
}

// leadingNumber numifies a string as Perl does (the leading number)
func leadingNumber(s string) float64 {
	t := strings.TrimLeft(s, " \t\n\r\f\v")
	end := 0
	for end < len(t) && (t[end] >= '0' && t[end] <= '9' || end == 0 && (t[end] == '+' || t[end] == '-')) {
		end++
	}
	n, _ := strconv.ParseFloat(t[:end], 64)
	return n
}

type level struct {
	typ       string
	val, nval pscalar
	pre, post string
	cut       int
}

// hangT is a Hang; nil is NullHang
type hangT []*level

// newHang is Hang->new($line, $lists_mode) with lists 1: the hang is cut
// from the start of *line
func newHang(line *string) hangT {
	orig := *line
	origlen := runes(orig)
	var vals []*level
	if g := reHangPS.cut(line); g != nil {
		vals = []*level{{typ: "ps", val: str(g[1])}}
	} else if g := reHang.cut(line); g != nil {
		vals = []*level{{typ: "bul", val: str(g[1])}}
	} else if reParenWord.match(*line) {
		vals = nil
	} else {
		for runes(*line) > 0 {
			if g, _ := reAbbrev.find(*line); g != nil && (g[1] != "" || len(vals) == 0) {
				break
			}
			if reBracketed.match(*line) {
				break
			}
			cut := origlen - runes(*line)
			pre := ""
			if g := rePre.cut(line); g != nil {
				pre = g[1]
			}
			var v *level
			if g := reNum.cut(line); g != nil {
				v = &level{typ: "num", val: str(g[1])}
			} else if g := reRom.cut(line); g != nil {
				v = &level{typ: "rom", val: str(g[1]), nval: intVal(fromRoman(g[1]))}
			} else if g := reLet.cut(line); g != nil {
				v = &level{typ: "let", val: str(g[1])}
			} else {
				v = &level{val: str("")}
			}
			if v.val.s == "" {
				// $_[1] = $pre.$_[1] and last: no last when that is false
				*line = pre + *line
				if *line != "" && *line != "0" {
					break
				}
			}
			if pre != "" {
				if g := postRx(pre).cut(line); g != nil && g[1] != "" && g[1] != "0" {
					v.post = g[1]
				}
			}
			if v.post == "" {
				if g := rePost.cut(line); g != nil && g[1] != "0" {
					v.post = g[1]
				}
			}
			v.pre = pre
			v.cut = cut
			vals = append(vals, v)
		}
		for len(vals) > 0 && vals[len(vals)-1].post == "" {
			*line = string([]rune(orig)[vals[len(vals)-1].cut:])
			vals = vals[:len(vals)-1]
		}
	}
	// check for orphaned years or unlikely Roman numerals...
	if len(vals) == 1 && reOrphan.match(vals[0].post) && vals[0].typ == "num" {
		// $v->{val} >= 1000 uses the value as a number
		vals[0].val.numeric = true
		if leadingNumber(vals[0].val.s) >= 1000 {
			*line = string([]rune(orig)[vals[0].cut:])
			vals = nil
		}
	}
	if len(vals) == 0 {
		return nil
	}
	return vals
}

func (h hangT) empty() bool { return h == nil }

func (h hangT) stringify() string {
	var b strings.Builder
	for _, l := range h {
		b.WriteString(l.pre + l.val.s + l.post)
	}
	return b.String()
}

func (h hangT) length() int { return runes(h.stringify()) }

func (h hangT) signature() string {
	var b strings.Builder
	for _, l := range h {
		t := l.typ
		if t == "rom" || t == "let" {
			t = "romlet"
		}
		b.WriteString(l.pre + t + l.post)
	}
	return b.String()
}

func (h hangT) field(i int) string {
	if h == nil || i >= len(h) {
		return ""
	}
	return h[i].typ
}

// incr is Hang incr: the hang continues the numbering of prev, or of
// prevsig (the last hang of the same signature) when prev is of another
// kind
func (h hangT) incr(prev, prevsig hangT) {
	if h == nil || prev.empty() {
		return
	}
	n := len(h)
	if len(prev) < n {
		n = len(prev)
	}
	for i := 0; i < n; i++ {
		if h[i].typ != prev[i].typ {
			if len(h) <= len(prev) {
				return // no incr if going up
			}
			prev = prevsig
			break
		}
	}
	if prev.empty() {
		return
	}
	switch {
	case h[0].typ == "ps":
		// 1 + s/(p[.]?)/$1/gi, then /^(p[.]?).*(s[.]?[:]?)/; when that
		// fails, $1 is the last p of the substitution and $2 undef
		pv := prev[0].val.s
		count := 1
		last := ""
		for m, _ := rePSCount.re.FindStringMatch(pv); m != nil; m, _ = rePSCount.re.FindNextMatch(m) {
			count++
			last = m.GroupByNumber(1).String()
		}
		p, s := last, ""
		if g, _ := rePSParts.find(pv); g != nil {
			p, s = g[1], g[2]
		}
		h[0].val = str(strings.Repeat(p, count) + s)
	case h[0].typ == "bul":
	case len(h) > len(prev): // going down level(s)
		for i := range prev {
			h[i].val, h[i].nval = prev[i].val, prev[i].nval
		}
		for _, l := range h[len(prev):] {
			l.reset()
		}
	default: // same level or going up
		for i := range h {
			h[i].val, h[i].nval = prev[i].val, prev[i].nval
		}
		h[len(h)-1].incrLevel()
	}
}

func (l *level) incrLevel() {
	switch {
	case l.typ == "rom":
		l.nval.incr()
		n, _ := strconv.Atoi(l.nval.s)
		l.val = str(toRoman(n, l.val.s))
	case l.typ == "let" && strings.ContainsAny(l.val.s, "Zz"):
	default:
		l.val.incr()
	}
}

func (l *level) reset() {
	switch l.typ {
	case "rom":
		l.nval = intVal(1)
		l.val = str(toRoman(1, l.val.s))
	case "let":
		if reUpper.match(l.val.s) {
			l.val = str("A")
		} else {
			l.val = str("a")
		}
	default:
		l.val = intVal(1)
	}
}
