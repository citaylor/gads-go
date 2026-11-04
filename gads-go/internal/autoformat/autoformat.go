// Package autoformat is Text::Autoformat 1.75 as GADS::Email send calls
// it on the plain text of every email: autoformat($text, {all => 1,
// break => break_wrap}). Paragraphs are filled to 72 columns, a word
// longer than the line is put on a line of its own, quoting (> :) and list
// markers (1. a) iv. * PS:) are kept and the lists renumbered, and block
// quotes with an attribution are laid out. The other options keep their
// defaults, so only their code is ported. Its Text::Reform form is in
// reform.go, and Text::Autoformat::Hang in hang.go.
package autoformat

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/dlclark/regexp2"
)

const (
	right       = 72 // $default_margin
	widow       = 10 // $default_widow
	widowSlack  = 0.1
	tabstop     = 8
	quotechar   = `[!#%=|:;]`
	quotechunk  = `(?:` + quotechar + `(?![a-z])|(?:[a-z]\w*)?>+)`
	quoter      = `(?:(?i)(?:` + quotechunk + `(?:[ \t]*` + quotechunk + `)*))`
	separatorRe = `(?:[-_]{2,}|[=#*]{3,}|[+~]{4,})`
)

var (
	reAnyNonSpace = mustRx(`\S`, 0)
	reQuoter      = mustRx(`\A([ \t]*)(`+quoter+`?)([ \t]*)`, 0)
	reLineText    = mustRx(`([ \t]*)(.*?)(\s*)$`, 0)
	reSeparator   = mustRx(`^`+separatorRe+`$`, 0)
	reWidow       = mustRx(`([^\n]*)$`, 0)
	reLeadingWS   = mustRx(`^\s+`, 0)
	reBlockquote  = mustRx(`\A(\s*)(["']|`+"``"+`)(.*)(''|\2)\s*?\n(\1[ ]+)(--|-)(.*?$)((\5.*?$)*)\s*\Z`, regexp2.Singleline|regexp2.Multiline)
	reQuotemeta   = regexp.MustCompile(`[^A-Za-z0-9_\x{80}-\x{10FFFF}]`)
)

type line struct {
	raw                                                   string
	prespace, quoter, quotespace, presig, hangspace, text string
	hang                                                  hangT
	empty, separator                                      bool
	hanglen                                               int
}

// Autoformat formats text as GADS's emails are.
func Autoformat(text string) string {
	if !reAnyNonSpace.match(text) {
		return text
	}

	// DETABIFY: split /\n/ leaves out the empty lines at the end
	rawlines := strings.Split(text, "\n")
	for len(rawlines) > 0 && rawlines[len(rawlines)-1] == "" {
		rawlines = rawlines[:len(rawlines)-1]
	}
	for i, l := range rawlines {
		rawlines[i] = expand(l)
	}

	// PARSE EACH LINE
	var lines []*line
	for _, s := range rawlines {
		l := &line{raw: s}
		g := reQuoter.cut(&s)
		l.prespace, l.quoter, l.quotespace = g[1], g[2], g[3]
		l.presig = l.prespace + l.quoter + l.quotespace
		l.hang = newHang(&s)
		g = reLineText.cut(&s)
		l.hangspace, l.text = g[1], g[2]
		l.empty = l.hang.empty() && !reAnyNonSpace.match(g[2])
		l.separator = reSeparator.match(l.text)
		lines = append(lines, l)
	}

	// SUBDIVIDE DOCUMENT INTO COHERENT SUBSECTIONS
	chunks := [][]*line{{lines[0]}}
	for _, l := range lines[1:] {
		last := chunks[len(chunks)-1]
		prev := last[len(last)-1]
		if l.separator || l.quoter != prev.quoter || l.empty || prev.empty {
			chunks = append(chunks, []*line{l})
		} else {
			chunks[len(chunks)-1] = append(last, l)
		}
	}
	// DETECT CENTRED PARAS: Perl quirk, none are, as the test for a hang
	// ($chunk->[0]{hang}) is of an object, which is always true

	// REDIVIDE INTO PARAGRAPHS
	var paras []*line
	for _, chunk := range chunks {
		first := true
		firstfrom := 0
		for _, l := range chunk {
			if first || l.quoter != paras[len(paras)-1].quoter || paras[len(paras)-1].separator || !l.hang.empty() {
				paras = append(paras, l)
				first = false
				firstfrom = runes(l.raw) - runes(l.text)
			} else {
				extra := runes(l.raw) - runes(l.text) - firstfrom
				p := paras[len(paras)-1]
				p.text += "\n" + spaces(extra) + l.text
				p.raw += "\n" + l.raw
			}
		}
	}

	// ALIGN QUOTERS
	lastquoted := false
	lastprespace := ""
	for _, p := range paras {
		if p.quoter != "" {
			if lastquoted {
				p.prespace = lastprespace
			} else {
				lastquoted = true
				lastprespace = p.prespace
			}
		} else {
			lastquoted = false
		}
	}

	// RENUMBER PARAGRAPHS
	type sigT struct {
		paras      []*line
		hangfields int
		hanglen    int
	}
	sigs := map[string]*sigT{}
	var sigOrder []string
	for _, p := range paras {
		sig := p.presig + p.hang.signature()
		s := sigs[sig]
		if s == nil {
			s = &sigT{hangfields: len(p.hang) - 1}
			sigs[sig] = s
			sigOrder = append(sigOrder, sig)
		}
		s.paras = append(s.paras, p)
	}
	for _, sig := range sigOrder {
		s := sigs[sig]
		if !strings.Contains(sig, "rom") {
			continue
		}
	field:
		for f := 0; f <= s.hangfields; f++ {
			romlen := 0
			for _, p := range s.paras {
				t := p.hang.field(f)
				if t != "rom" && t != "let" {
					continue field
				}
				if t == "let" {
					p.hang[f].typ = "let"
				} else {
					romlen += runes(p.hang[f].val.s)
				}
			}
			// NO ROMAN LETTER > 1 CHAR -> ALPHABETICS
			if romlen <= len(s.paras) {
				for _, p := range s.paras {
					p.hang[f].typ = "let"
				}
			}
		}
	}
	prev := map[string]hangT{}
	for _, p := range paras {
		sig := p.presig + p.hang.signature()
		if p.quoter == "" {
			p.hang.incr(prev[""], prev[sig])
			if !p.hang.empty() {
				prev[""], prev[sig] = p.hang, p.hang
			}
		}
		// COLLECT MAXIMAL HANG LENGTHS BY SIGNATURE
		if l := p.hang.length(); sigs[sig].hanglen == 0 || sigs[sig].hanglen < l {
			sigs[sig].hanglen = l
		}
	}
	// PROPAGATE MAXIMAL HANG LENGTH
	for _, s := range sigs {
		for _, p := range s.paras {
			p.hanglen = s.hanglen
		}
	}

	// BUILD FORMAT FOR EACH PARA THEN FILL IT
	// The gap option is 0, so that no blank lines are added between
	// paragraphs
	var out strings.Builder
	for _, p := range paras {
		leftmargin := p.prespace
		hlen := p.hanglen
		if hlen == 0 {
			hlen = p.hang.length()
		}
		hfield := strings.Repeat(">", hlen)
		if hlen == 1 {
			hfield = "~"
		}
		var hang []string
		if hlen != 0 {
			hang = []string{p.hang.stringify()}
		}
		format := leftmargin + quotemeta(p.quoter) + p.quotespace + hfield + p.hangspace
		rightslack := int(float64(right-runes(leftmargin)) * widowSlack)
		widowOK, rightindent := false, 0
		var firsttext, newtext string
		blockquoted := false
		for {
			tlen := right - rightindent - runes(leftmargin+p.quoter+p.quotespace+hfield+p.hangspace)
			if blockquote(&out, p, format, tlen, hang) {
				// next: out of the do block, to the next paragraph
				blockquoted = true
				break
			}
			tfield := strings.Repeat("[", max(tlen, 0))
			if tlen == 1 {
				tfield = "~"
			}
			args := []any{configFn(func(c *config) {
				c.squeeze, c.trim, c.brk, c.fill = true, true, breakWrap, true
			}), ptr(format + tfield)}
			for _, h := range hang {
				args = append(args, ptr(h))
			}
			args = append(args, ptr(p.text))
			newtext = form(args...)
			if firsttext == "" || firsttext == "0" {
				firsttext = newtext
			}
			g, _ := reWidow.find(newtext)
			w := ""
			if g != nil {
				w = g[1]
			}
			if g, end := reLeadingWS.find(w); g != nil {
				w = string([]rune(w)[end:])
			}
			widowOK = p.empty || runes(w) >= widow
			rightindent++
			if widowOK || rightindent > rightslack {
				break
			}
		}
		if blockquoted {
			continue
		}
		if widowOK {
			out.WriteString(newtext)
		} else {
			out.WriteString(firsttext)
		}
	}
	return out.String()
}

func ptr(s string) *string { return &s }

// quotemeta is Perl's quotemeta: every ASCII character other than a word
// character is escaped (the formats of form only need those)
func quotemeta(s string) string {
	return reQuotemeta.ReplaceAllStringFunc(s, func(c string) string { return `\` + c })
}

// blockquote is Text::Autoformat blockquote: a paragraph that is a quote
// then an attribution ("-- Author") is laid out as one, and appended to
// out
func blockquote(out *strings.Builder, p *line, format string, tlen int, hang []string) bool {
	g, _ := reBlockquote.find(p.text)
	if g == nil {
		return false
	}
	indent := runes(g[1])
	text := g[2] + g[3] + g[4]
	qindent := runes(g[2])
	aindent := runes(g[5])
	intro := g[6]
	attrib := g[7] + g[8]
	text = strings.ReplaceAll(text, "\n", " ")

	// The hang and the text are each one variable, which the formats
	// consume in turn
	var h *string
	if len(hang) > 0 {
		h = ptr(hang[0])
	}
	withHang := func(args ...any) []any {
		if h == nil {
			return args
		}
		return append([]any{h}, args...)
	}
	tp := &text
	args := []any{configFn(func(c *config) { c.squeeze, c.trim, c.fill = true, true, false }),
		ptr(format + spaces(indent) + strings.Repeat("<", max(tlen, 0)))}
	args = append(args, withHang(tp)...)
	args = append(args, ptr(format+spaces(qindent)+strings.Repeat("[", max(tlen-qindent, 0))))
	args = append(args, withHang(tp)...)
	args = append(args, configFn(func(c *config) { c.squeeze = false }))
	args = append(args, ptr(format+spaces(aindent)+">> "+strings.Repeat("[", max(tlen-aindent-3, 0))))
	args = append(args, withHang(ptr(intro), ptr(attrib))...)
	out.WriteString(form(args...))
	return true
}

// expand is Text::Tabs expand (2013.0523): a tab goes to the next multiple
// of 8 columns, counting grapheme clusters
func expand(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	prevRune := rune(-1)
	for _, r := range s {
		if r == '\t' {
			pad := tabstop - col%tabstop
			b.WriteString(spaces(pad))
			col += pad
			prevRune = r
			continue
		}
		if !extendsCluster(prevRune, r) {
			col++
		}
		b.WriteRune(r)
		prevRune = r
	}
	return b.String()
}

// extendsCluster is whether r continues the grapheme cluster of prev (\X):
// combining and spacing marks, joiners, variation selectors and emoji
// modifiers, after ZWJ, and CR LF
func extendsCluster(prev, r rune) bool {
	if prev < 0 {
		return false
	}
	switch {
	case prev == '\r' && r == '\n':
		return true
	case unicode.Is(unicode.Cc, prev) || unicode.Is(unicode.Cc, r):
		return false
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc), r == 0x200D, r == 0x200C,
		r >= 0xFE00 && r <= 0xFE0F, r >= 0x1F3FB && r <= 0x1F3FF, r >= 0xE0020 && r <= 0xE007F:
		return true
	case prev == 0x200D && unicode.In(r, unicode.So):
		return true
	}
	return false
}
