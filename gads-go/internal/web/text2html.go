package web

import (
	"regexp"
	"strings"

	"github.com/ctrlo/gads-go/internal/tt"
)

// The regexps of HTML::FromText (urls) and Email::Find::addrspec (email)
var (
	hftURLRe   = regexp.MustCompile(`\b((?:afs|cid|ftp|gopher|http|https|mid|news|nntp|prospero|telnet|wais):[^\s<]+[\w/])`)
	hftEmailRe = func() *regexp.Regexp {
		esc := `\\`
		nonASCII := `\x{80}-\x{ff}`
		ctrl := `\x00-\x1f`
		crList := `\n\r`
		qtext := `[^` + esc + nonASCII + crList + `"]`
		dtext := `[^` + esc + nonASCII + crList + `\[\]]`
		quotedPair := esc + `[^` + nonASCII + `]`
		atomChar := `[^( )<>@,;:".` + esc + `\[\]` + ctrl + nonASCII + `]`
		// Perl's (?!atom_char) after the greedy atom cannot be expressed
		// in RE2; a greedy match is equivalent here
		atom := atomChar + `+`
		quotedStr := `"` + qtext + `*(?:` + quotedPair + qtext + `*)*"`
		word := `(?:` + atom + `|` + quotedStr + `)`
		localPart := word + `(?:\.` + word + `)*`
		label := `[A-Za-z\d](?:[A-Za-z\d-]*[A-Za-z\d])?`
		domainRef := label + `(?:\.` + label + `)*`
		domainLit := `\[(?:` + dtext + `|` + quotedPair + `)*\]`
		domain := `(?:` + domainRef + `|` + domainLit + `)`
		return regexp.MustCompile(`(` + localPart + `\s*@\s*` + domain + `)`)
	}()
)

// expandTabs is Text::Tabs expand (tab stops every 8 columns)
func expandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}
	var b strings.Builder
	col := 0
	for _, r := range line {
		if r == '\t' {
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

// text2html is HTML::FromText text2html with the options lines, urls,
// email and metachars (as used by GADS for string values)
func text2html(text string) string {
	// Perl split drops trailing empty fields
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = expandTabs(l)
	}
	html := tt.EncodeEntities(strings.Join(lines, "\n"))
	// lines: <br /> at the end of each line
	html = strings.Join(strings.Split(html, "\n"), "<br />\n") + "<br />"
	html = `<div class="hft-lines">` + html + `</div>`
	html = hftURLRe.ReplaceAllString(html, `<a href="$1" class="hft-urls">$1</a>`)
	html = hftEmailRe.ReplaceAllString(html, `<a href="mailto:$1" class="hft-email">$1</a>`)
	return html
}
