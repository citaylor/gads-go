package gads

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"math/big"
	"strings"
)

// The EFF large word list, as used by GADS (CtrlO::Crypt::XkcdPassword
// with the eff_large word list)
//
//go:embed eff_large.txt
var effLarge string

var effWords = strings.Fields(effLarge)

func randInt(n int64) int64 {
	v, _ := rand.Int(rand.Reader, big.NewInt(n))
	return v.Int64()
}

// RandomPassword is GADS _random_pw: xkcd(words => 3, digits => 2), the
// capitalised words and a 2 digit number joined together. The engine's
// PasswordGenerator is used instead if set (tests).
func (e *Engine) RandomPassword() string {
	if e.PasswordGenerator != nil {
		return e.PasswordGenerator()
	}
	var b strings.Builder
	for i := 0; i < 3; i++ {
		w := effWords[randInt(int64(len(effWords)))]
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	b.WriteString(fmt.Sprintf("%02d", randInt(100)))
	return b.String()
}

// TextWrap is Text::Wrap wrap(”, ”, text) with the default 76 columns:
// lines are filled to at most 75 characters, breaking at whitespace (which
// is removed) and keeping existing newlines. Words longer than a line are
// broken. Tabs are expanded, and spaces are made tabs again in the lines
// (unexpand).
func TextWrap(text string) string {
	const ll = 75
	t := []rune(expandTabs(text))
	var r strings.Builder
	nl, remainder := "", ""
	p := 0
	for {
		// The loop ends when only whitespace is left
		rest := true
		for _, c := range t[p:] {
			if !isSpaceRune(c) {
				rest = false
				break
			}
		}
		if rest {
			break
		}
		// The longest run (up to the line length) without a newline that
		// is followed by whitespace or the end of the text
		max := 0
		for max < ll && p+max < len(t) && t[p+max] != '\n' {
			max++
		}
		k := -1
		for i := max; i >= 0; i-- {
			if p+i == len(t) || isSpaceRune(t[p+i]) {
				k = i
				break
			}
		}
		if k >= 0 {
			r.WriteString(unexpandTabs(nl + string(t[p:p+k])))
			p += k
			if p < len(t) {
				remainder = string(t[p])
				p++
			} else {
				remainder = ""
			}
		} else {
			r.WriteString(unexpandTabs(nl + string(t[p:p+ll])))
			p += ll
			remainder = "\n"
		}
		nl = "\n"
	}
	r.WriteString(remainder)
	return r.String()
}

func isSpaceRune(c rune) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// expandTabs is Text::Tabs expand with tab stops every 8 columns
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		var b strings.Builder
		col := 0
		for _, c := range line {
			if c == '\t' {
				n := 8 - col%8
				b.WriteString(strings.Repeat(" ", n))
				col += n
			} else {
				b.WriteRune(c)
				col++
			}
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}

// unexpandTabs is Text::Tabs unexpand: in each line, each 8 column chunk
// (apart from the last) ending in 2 or more spaces has them replaced by a
// tab, and a last chunk of 8 spaces becomes a tab
func unexpandTabs(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		r := []rune(expandTabs(line))
		var b strings.Builder
		n := len(r) / 8
		last := string(r[n*8:])
		for c := 0; c < n; c++ {
			chunk := string(r[c*8 : c*8+8])
			trimmed := strings.TrimRight(chunk, " ")
			if len(chunk)-len(trimmed) >= 2 {
				chunk = trimmed + "\t"
			}
			b.WriteString(chunk)
		}
		if last == "        " {
			last = "\t"
		}
		b.WriteString(last)
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}
