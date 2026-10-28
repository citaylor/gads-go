package tt

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A lexItem is either a token (type, value) or a directive: the directive
// text, its line number(s) and its tokens. This mirrors the token list that
// Template::Parser::split_text produces, in which directives are array refs
// that the parser expands (followed by two ';' tokens) when it reaches them.
type lexItem struct {
	typ, val string // token

	dir     bool
	dirText string
	line    string // "" = unknown
	toks    []lexItem
}

func tok(typ, val string) lexItem { return lexItem{typ: typ, val: val} }

const (
	chompNone     = 0
	chompAll      = 1
	chompCollapse = 2
	chompGreedy   = 3
)

func chompFlag(c byte) (int, bool) {
	switch c {
	case '-':
		return chompAll, true
	case '=':
		return chompCollapse, true
	case '~':
		return chompGreedy, true
	case '+':
		return chompNone, true
	}
	return 0, false
}

// isSpace matches Perl's \s
func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return unicode.IsSpace(r)
}

// isHSpace matches [^\S\n]
func isHSpace(r rune) bool { return r != '\n' && isSpace(r) }

// isWord matches Perl's \w on character strings
func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) ||
		unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Pc, r) || unicode.Is(unicode.Nl, r)
}

func perlTrue(s string) bool { return s != "" && s != "0" }

// splitText is Template::Parser::split_text with the default tag style
// ([% %]) and no PRE_CHOMP/POST_CHOMP/INTERPOLATE.
func splitText(text string, anycase bool) []lexItem {
	var items []lexItem
	line := 1
	if text == "" {
		return items
	}
	for {
		start := strings.Index(text, "[%")
		if start < 0 {
			break
		}
		endRel := strings.Index(text[start+2:], "%]")
		if endRel < 0 {
			break
		}
		pre := text[:start]
		dir := text[start+2 : start+2+endRel]
		text = text[start+2+endRel+2:]

		prelines := strings.Count(pre, "\n")
		dirlines := strings.Count(dir, "\n")
		postlines := 0

		if strings.HasPrefix(dir, "#") {
			// comment out entire directive except for any end chomp
			// flag, consuming nested tag pairs
			nested := strings.Count(dir, "[%")
			for nested > 0 {
				e := strings.Index(text, "%]")
				if e < 0 {
					break
				}
				extra := text[:e]
				text = text[e+2:]
				dirlines += strings.Count(extra, "\n")
				nested--
				nested += strings.Count(extra, "[%")
			}
			d := strings.TrimSuffix(dir, "\n")
			if len(d) > 0 {
				if _, ok := chompFlag(d[len(d)-1]); ok {
					dir = d[len(d)-1:]
				} else {
					dir = ""
				}
			} else {
				dir = ""
			}
		} else {
			flag := byte(0)
			if len(dir) > 0 {
				if _, ok := chompFlag(dir[0]); ok {
					flag = dir[0]
					dir = dir[1:]
				}
			}
			ws := 0
			for ws < len(dir) {
				r, sz := utf8.DecodeRuneInString(dir[ws:])
				if !isSpace(r) {
					break
				}
				ws += sz
			}
			if ws > 0 {
				lc := strings.Count(dir[:ws], "\n")
				prelines += lc
				dirlines -= lc
				dir = dir[ws:]
			}
			chomp := chompNone
			if flag != 0 {
				chomp, _ = chompFlag(flag)
			}
			if chomp != chompNone && perlTrue(pre) {
				switch chomp {
				case chompAll:
					pre = chompPreAll(pre)
				case chompCollapse:
					if t := trimRightSpace(pre); len(t) != len(pre) {
						pre = t + " "
					}
				case chompGreedy:
					pre = trimRightSpace(pre)
				}
			}
		}

		// POST_CHOMP: s/\s*($CHOMP_FLAGS)?\s*$//
		var postFlag byte
		dir, postFlag = stripPostChomp(dir)
		chomp := chompNone
		if postFlag != 0 {
			chomp, _ = chompFlag(postFlag)
		}
		switch chomp {
		case chompAll:
			// s{ ^ ([^\S\n]* \n) }{}x
			i := 0
			for i < len(text) {
				r, sz := utf8.DecodeRuneInString(text[i:])
				if !isHSpace(r) {
					break
				}
				i += sz
			}
			if i < len(text) && text[i] == '\n' {
				text = text[i+1:]
				postlines++
			}
		case chompCollapse:
			if t := trimLeftSpace(text); len(t) != len(text) {
				postlines += strings.Count(text[:len(text)-len(t)], "\n")
				text = " " + t
			}
		case chompGreedy:
			if t := trimLeftSpace(text); len(t) != len(text) {
				postlines += strings.Count(text[:len(text)-len(t)], "\n")
				text = t
			}
		}

		if len(pre) > 0 {
			items = append(items, tok("TEXT", pre))
		}
		line += prelines

		if len(dir) > 0 {
			ls := strconv.Itoa(line)
			if dirlines != 0 {
				ls = strconv.Itoa(line) + "-" + strconv.Itoa(line+dirlines)
			}
			items = append(items, lexItem{dir: true, dirText: dir, line: ls, toks: tokeniseDirective(dir, ls, anycase)})
		}
		line += dirlines + postlines
	}
	if len(text) > 0 {
		items = append(items, tok("TEXT", text))
	}
	return items
}

// chompPreAll is s{ (\r?\n|^) [^\S\n]* \z }{}mx
func chompPreAll(pre string) string {
	// The match must end at the end of the string after horizontal
	// whitespace; find where the trailing horizontal whitespace starts
	i := len(pre)
	for i > 0 {
		r, sz := utf8.DecodeLastRuneInString(pre[:i])
		if !isHSpace(r) {
			break
		}
		i -= sz
	}
	// leftmost match: positions are tried from the left, so a newline
	// directly before the trailing whitespace (or the start of a line)
	// is required. Note [^\S\n]* includes \r, so "\r\n" before the
	// whitespace is matched by \r?\n starting at the \r.
	if i == 0 {
		return ""
	}
	if pre[i-1] == '\n' {
		j := i - 1
		if j > 0 && pre[j-1] == '\r' {
			j--
		}
		return pre[:j]
	}
	// ^ in /m mode also matches after a newline; the whitespace run may
	// contain \r characters only if preceded by a newline (handled
	// above). Otherwise there is no match.
	return pre
}

func trimRightSpace(s string) string {
	return strings.TrimRightFunc(s, isSpace)
}

func trimLeftSpace(s string) string {
	return strings.TrimLeftFunc(s, isSpace)
}

// stripPostChomp implements s/\s*($CHOMP_FLAGS)?\s*$//so, returning the
// directive and the chomp flag (0 if none).
func stripPostChomp(dir string) (string, byte) {
	t := trimRightSpace(dir)
	if len(t) > 0 {
		if _, ok := chompFlag(t[len(t)-1]); ok {
			flag := t[len(t)-1]
			return trimRightSpace(t[:len(t)-1]), flag
		}
	}
	return t, 0
}

var anycaseAfter = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`GET SET CALL DEFAULT INSERT INCLUDE PROCESS WRAPPER BLOCK USE
        PLUGIN FILTER MACRO IN TO STEP AND OR NOT DIV MOD DOT
        IF UNLESS ELSIF FOR WHILE SWITCH CASE META THROW CATCH VIEW
        CMPOP BINOP COMMA ( [ {`) {
		anycaseAfter[k] = true
	}
}

// tokeniseDirective is Template::Parser::tokenise_directive (optionally
// with ANYCASE).
func tokeniseDirective(text string, line string, anycase bool) []lexItem {
	var toks []lexItem
	pos := 0
	for pos < len(text) {
		r, sz := utf8.DecodeRuneInString(text[pos:])
		if isSpace(r) {
			pos += sz
			continue
		}
		c := text[pos]
		// comment to end of line
		if c == '#' {
			e := strings.IndexByte(text[pos:], '\n')
			if e < 0 {
				pos = len(text)
			} else {
				pos += e
			}
			continue
		}
		// quoted string
		if c == '"' || c == '\'' {
			if content, end, ok := matchQuoted(text, pos+1, c); ok {
				pos = end
				if c == '"' {
					if strings.ContainsAny(content, "$\\") {
						content = unescapeDouble(content)
						toks = append(toks, tok(`"`, `"`))
						toks = append(toks, interpolateText(content, anycase)...)
						toks = append(toks, tok(`"`, `"`))
						continue
					}
					toks = append(toks, tok("LITERAL", "'"+strings.ReplaceAll(content, "'", `\'`)+"'"))
				} else {
					toks = append(toks, tok("LITERAL", "'"+content+"'"))
				}
				continue
			}
		}
		// number -?\d+(?:\.\d+)?
		if n := matchNumber(text[pos:]); n > 0 {
			toks = append(toks, tok("NUMBER", text[pos:pos+n]))
			pos += n
			continue
		}
		// filename \/?\w+(?:(?:\/|::?)\w*)+ | \/\w+
		if n := matchFilename(text[pos:]); n > 0 {
			toks = append(toks, tok("FILENAME", text[pos:pos+n]))
			pos += n
			continue
		}
		// identifier \w+
		if isWord(r) {
			n := 0
			for pos+n < len(text) {
				r2, sz2 := utf8.DecodeRuneInString(text[pos+n:])
				if !isWord(r2) {
					break
				}
				n += sz2
			}
			word := text[pos : pos+n]
			pos += n
			uc := word
			if anycase && !((len(toks) > 0 && anycaseAfter[toks[len(toks)-1].typ]) || anycaseBefore(text[pos:])) {
				uc = strings.ToUpper(word)
			}
			if typ, ok := lexTable[uc]; ok {
				toks = append(toks, tok(typ, uc))
			} else {
				toks = append(toks, tok("IDENT", word))
			}
			continue
		}
		// symbols
		sym := matchSymbol(text[pos:])
		pos += len(sym)
		if typ, ok := lexTable[strings.ToUpper(sym)]; ok {
			toks = append(toks, tok(typ, strings.ToUpper(sym)))
		} else {
			toks = append(toks, tok("UNQUOTED", sym))
		}
	}
	return toks
}

// anycaseBefore is $ANYCASE_BEFORE: \G(?=\s*[=\.])
func anycaseBefore(rest string) bool {
	t := trimLeftSpace(rest)
	return len(t) > 0 && (t[0] == '=' || t[0] == '.')
}

// matchQuoted matches (?:\\\\|\\Q|.|\n)*?Q from position start (just after
// the opening quote Q) with Perl's backtracking order. It returns the
// content, the position after the closing quote and whether it matched.
func matchQuoted(text string, start int, q byte) (string, int, bool) {
	var try func(i, depth int) (int, bool)
	try = func(i, depth int) (int, bool) {
		for {
			if i >= len(text) {
				return 0, false
			}
			if text[i] == q {
				return i, true
			}
			if text[i] == '\\' && i+1 < len(text) && (text[i+1] == '\\' || text[i+1] == q) {
				// alternatives \\\\ or \\Q (two characters); on failure
				// fall back to consuming the backslash alone
				if depth < 64 {
					if e, ok := try(i+2, depth+1); ok {
						return e, true
					}
					i++
					continue
				}
				i += 2
				continue
			}
			_, sz := utf8.DecodeRuneInString(text[i:])
			i += sz
		}
	}
	e, ok := try(start, 0)
	if !ok {
		return "", 0, false
	}
	return text[start:e], e + 1, true
}

// unescapeDouble applies s/\\([^\$nrt])/$1/g then s/\\([nrt])/.../ge
func unescapeDouble(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			r, sz := utf8.DecodeRuneInString(s[i+1:])
			if r != '$' && r != 'n' && r != 'r' && r != 't' {
				b.WriteRune(r)
				i += sz
				continue
			}
		}
		b.WriteByte(s[i])
	}
	s = b.String()
	b.Reset()
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func matchNumber(s string) int {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	j := i
	for j < len(s) && isDigit(s[j]) {
		j++
	}
	if j == i {
		return 0
	}
	if j+1 < len(s) && s[j] == '.' && isDigit(s[j+1]) {
		j++
		for j < len(s) && isDigit(s[j]) {
			j++
		}
	}
	return j
}

func wordLen(s string) int {
	n := 0
	for n < len(s) {
		r, sz := utf8.DecodeRuneInString(s[n:])
		if !isWord(r) {
			break
		}
		n += sz
	}
	return n
}

// matchFilename matches \/?\w+(?:(?:\/|::?)\w*)+ | \/\w+
func matchFilename(s string) int {
	i := 0
	if i < len(s) && s[i] == '/' {
		i++
	}
	if w := wordLen(s[i:]); w > 0 {
		j := i + w
		reps := 0
		for {
			k := j
			if k < len(s) && s[k] == '/' {
				k++
			} else if k < len(s) && s[k] == ':' {
				k++
				if k < len(s) && s[k] == ':' {
					k++
				}
			} else {
				break
			}
			k += wordLen(s[k:])
			j = k
			reps++
		}
		if reps > 0 {
			return j
		}
	}
	if len(s) > 1 && s[0] == '/' {
		if w := wordLen(s[1:]); w > 0 {
			return 1 + w
		}
	}
	return 0
}

// matchSymbol matches the $7 alternatives of the tokeniser regex.
func matchSymbol(s string) string {
	c := s[0]
	switch c {
	case '(', ')', '{', '}', '[', ']', ':', ';', ',', '/', '\\':
		return s[:1]
	case '+', '-', '*':
		return s[:1]
	case '$':
		if len(s) > 1 && s[1] == '{' {
			return s[:2]
		}
		return s[:1]
	}
	if strings.HasPrefix(s, "=>") {
		return "=>"
	}
	if (c == '=' || c == '!' || c == '<' || c == '>') && len(s) > 1 && s[1] == '=' {
		return s[:2]
	}
	if c == '=' {
		return s[:1]
	}
	if c == '!' || c == '<' || c == '>' {
		return s[:1]
	}
	if c == '&' {
		if len(s) > 1 && s[1] == '&' {
			return s[:2]
		}
		return s[:1]
	}
	if c == '|' {
		if len(s) > 1 && s[1] == '|' {
			return s[:2]
		}
		return s[:1]
	}
	if c == '.' {
		if len(s) > 1 && s[1] == '.' {
			return s[:2]
		}
		return s[:1]
	}
	// \S+
	n := 0
	for n < len(s) {
		r, sz := utf8.DecodeRuneInString(s[n:])
		if isSpace(r) {
			break
		}
		n += sz
	}
	return s[:n]
}

// interpolateText is Template::Parser::interpolate_text (called from the
// tokeniser for double quoted strings, so line numbers are unknown).
func interpolateText(text string, anycase bool) []lexItem {
	var toks []lexItem
	i := 0
	for i < len(text) {
		if text[i] != '$' {
			// ( (?: \\. | [^\$] ){1,3000} )
			var b strings.Builder
			n := 0
			for i < len(text) && n < 3000 {
				if text[i] == '\\' && i+1 < len(text) && text[i+1] != '\n' {
					_, sz := utf8.DecodeRuneInString(text[i+1:])
					b.WriteString(text[i : i+1+sz])
					i += 1 + sz
					n++
					continue
				}
				if text[i] == '$' {
					break
				}
				_, sz := utf8.DecodeRuneInString(text[i:])
				b.WriteString(text[i : i+sz])
				i += sz
				n++
			}
			pre := strings.ReplaceAll(b.String(), `\$`, `$`)
			if pre != "" {
				toks = append(toks, tok("TEXT", pre))
			}
			continue
		}
		// $ followed by {...} or [\w.]+
		rest := text[i+1:]
		if strings.HasPrefix(rest, "{") {
			if e := strings.IndexByte(rest, '}'); e >= 0 {
				v := rest[1:e]
				dir := text[i : i+1+e+1]
				i += 1 + e + 1
				if v != "" {
					toks = append(toks, lexItem{dir: true, dirText: dir, toks: tokeniseDirective(v, "", anycase)})
				} else {
					toks = append(toks, tok("TEXT", dir))
				}
				continue
			}
		}
		n := 0
		for n < len(rest) {
			r, sz := utf8.DecodeRuneInString(rest[n:])
			if !(isWord(r) || r == '.') {
				break
			}
			n += sz
		}
		if n > 0 {
			v := rest[:n]
			dir := text[i : i+1+n]
			i += 1 + n
			toks = append(toks, lexItem{dir: true, dirText: dir, toks: tokeniseDirective(v, "", anycase)})
			continue
		}
		// a lone '$' matches nothing and is skipped
		i++
	}
	return toks
}
