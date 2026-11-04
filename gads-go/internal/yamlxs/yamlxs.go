// Package yamlxs writes YAML as YAML::XS (libyaml) Dump does. It is a port
// of the parts of libyaml's emitter (emitter.c, 0.2.4) that YAML::XS uses
// for one document of block collections: the indentation, the analysis of
// each scalar and the choice of its style, the plain, quoted and literal
// writers with their line folding at 80 columns, and YAML::XS's own rules
// (perl_libyaml.c dump_scalar) for which strings are quoted.
package yamlxs

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/perl"
)

// libyaml's defaults: YAML::XS sets a width of 2, which libyaml replaces
// with 80 as it is not more than twice the indent
const (
	bestIndent = 2
	bestWidth  = 80
)

type style int

const (
	plainStyle style = iota
	singleQuotedStyle
	doubleQuotedStyle
	literalStyle
)

// Dump is YAML::XS Dump of one document. Values are nil, bool, integers,
// floats, strings, []any and map[string]any.
func Dump(v any) string {
	e := &emitter{indent: -1, whitespace: true, indention: true}
	// Document start (not implicit)
	e.writeIndent()
	e.writeIndicator("---", true, false, false)
	e.node(v, true, false, false, false)
	// Document end (implicit)
	e.writeIndent()
	// Stream end: a literal scalar keeping its final line breaks leaves
	// the document open
	if e.openEnded == 2 {
		e.writeIndicator("...", true, false, false)
		e.writeIndent()
	}
	return e.b.String()
}

// emitter is the state of yaml_emitter_t used by block collections
type emitter struct {
	b          strings.Builder
	column     int
	indent     int
	indents    []int
	whitespace bool
	indention  bool
	openEnded  int
	flowLevel  int
	// The context of the node being written (yaml_emitter_emit_node)
	mappingContext   bool
	simpleKeyContext bool
}

func (e *emitter) put(c byte) {
	e.b.WriteByte(c)
	e.column++
}

func (e *emitter) putBreak() {
	e.b.WriteByte('\n')
	e.column = 0
}

// write copies a character (WRITE: one column whatever its width)
func (e *emitter) write(r rune) {
	e.b.WriteRune(r)
	e.column++
}

// writeBreak is WRITE_BREAK: a line feed is written as the line break, any
// other break is copied
func (e *emitter) writeBreak(r rune) {
	if r == '\n' {
		e.putBreak()
		return
	}
	e.b.WriteRune(r)
	e.column = 0
}

func (e *emitter) increaseIndent(flow, indentless bool) {
	e.indents = append(e.indents, e.indent)
	if e.indent < 0 {
		if flow {
			e.indent = bestIndent
		} else {
			e.indent = 0
		}
	} else if !indentless {
		e.indent += bestIndent
	}
}

func (e *emitter) popIndent() {
	e.indent = e.indents[len(e.indents)-1]
	e.indents = e.indents[:len(e.indents)-1]
}

func (e *emitter) writeIndent() {
	indent := max(e.indent, 0)
	if !e.indention || e.column > indent || (e.column == indent && !e.whitespace) {
		e.putBreak()
	}
	for e.column < indent {
		e.put(' ')
	}
	e.whitespace = true
	e.indention = true
}

func (e *emitter) writeIndicator(indicator string, needWhitespace, isWhitespace, isIndention bool) {
	if needWhitespace && !e.whitespace {
		e.put(' ')
	}
	for _, r := range indicator {
		e.write(r)
	}
	e.whitespace = isWhitespace
	e.indention = e.indention && isIndention
}

// node is yaml_emitter_emit_node
func (e *emitter) node(v any, root, sequence, mapping, simpleKey bool) {
	e.mappingContext = mapping
	e.simpleKeyContext = simpleKey
	switch t := v.(type) {
	case []any:
		if len(t) == 0 || e.flowLevel > 0 {
			e.emptyFlow("[", "]")
			return
		}
		e.blockSequence(t)
	case map[string]any:
		if len(t) == 0 || e.flowLevel > 0 {
			e.emptyFlow("{", "}")
			return
		}
		e.blockMapping(t)
	default:
		e.scalar(v)
	}
}

// emptyFlow writes an empty sequence or mapping, which libyaml writes in
// flow style
func (e *emitter) emptyFlow(open, end string) {
	e.writeIndicator(open, true, true, false)
	e.increaseIndent(true, false)
	e.popIndent()
	e.writeIndicator(end, false, false, false)
}

func (e *emitter) blockSequence(items []any) {
	e.increaseIndent(false, e.mappingContext && !e.indention)
	for _, item := range items {
		e.writeIndent()
		e.writeIndicator("-", true, false, true)
		e.node(item, false, true, false, false)
	}
	e.popIndent()
}

func (e *emitter) blockMapping(m map[string]any) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// YAML::XS sorts the keys as strings (sv_cmp)
	sort.Strings(keys)
	e.increaseIndent(false, false)
	for _, k := range keys {
		e.writeIndent()
		if checkSimpleKey(k) {
			e.node(k, false, false, true, true)
			e.writeIndicator(":", false, false, false)
		} else {
			e.writeIndicator("?", true, false, true)
			e.node(k, false, false, true, false)
			e.writeIndent()
			e.writeIndicator(":", true, false, true)
		}
		e.node(m[k], false, false, true, false)
	}
	e.popIndent()
}

// checkSimpleKey is yaml_emitter_check_simple_key for a scalar key
func checkSimpleKey(k string) bool {
	a := analyze(k)
	return !a.multiline && len(k) <= 128
}

// scalarString is YAML::XS dump_scalar: the text of a value, and the style
// it asks for
func scalarString(v any) (string, style) {
	switch t := v.(type) {
	case nil:
		return "~", plainStyle
	case bool:
		// A copy of Perl's true or false (not &PL_sv_yes itself)
		if t {
			return "1", plainStyle
		}
		return "", singleQuotedStyle
	case int:
		return strconv.Itoa(t), plainStyle
	case int64:
		return strconv.FormatInt(t, 10), plainStyle
	case float64:
		return perl.NumString(t), plainStyle
	case string:
		return stringStyle(t)
	}
	return stringStyle(fmt.Sprint(v))
}

// stringStyle is the style YAML::XS asks for a string: quoted when it would
// read as another type, a literal block (over 30 bytes) or double quoted
// with a line feed, otherwise plain if libyaml allows it
func stringStyle(s string) (string, style) {
	if s == "" || s == "~" || s == "true" || s == "false" || s == "null" || perl.LooksLikeNumber(s) {
		return s, singleQuotedStyle
	}
	if strings.ContainsRune(s, '\n') {
		if len(s) > 30 {
			return s, literalStyle
		}
		return s, doubleQuotedStyle
	}
	return s, plainStyle
}

// analysis is yaml_scalar_data_t's results of yaml_emitter_analyze_scalar
type analysis struct {
	multiline           bool
	blockPlainAllowed   bool
	singleQuotedAllowed bool
	blockAllowed        bool
}

func isPrintable(r rune) bool {
	return r == '\n' || (r >= 0x20 && r <= 0x7E) || (r >= 0xA0 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD && r != 0xFEFF)
}

func isBreak(r rune) bool {
	return r == '\r' || r == '\n' || r == 0x85 || r == 0x2028 || r == 0x2029
}

// isBlankz is IS_BLANKZ: a space, tab, line break or the end
func isBlankz(runes []rune, i int) bool {
	if i >= len(runes) {
		return true
	}
	r := runes[i]
	return r == ' ' || r == '\t' || r == 0 || isBreak(r)
}

// analyze is yaml_emitter_analyze_scalar (in block context, so only what
// decides the block styles)
func analyze(s string) analysis {
	if s == "" {
		return analysis{blockPlainAllowed: true, singleQuotedAllowed: true}
	}
	runes := []rune(s)
	var blockIndicators, lineBreaks, special bool
	var leadingSpace, leadingBreak, trailingSpace, trailingBreak, breakSpace, spaceBreak bool
	var previousSpace, previousBreak bool
	if strings.HasPrefix(s, "---") || strings.HasPrefix(s, "...") {
		blockIndicators = true
	}
	precededByWhitespace := true
	for i, r := range runes {
		followedByWhitespace := isBlankz(runes, i+1)
		if i == 0 {
			if strings.ContainsRune("#,[]{}&*!|>'\"%@`", r) {
				blockIndicators = true
			}
			if (r == '?' || r == ':' || r == '-') && followedByWhitespace {
				blockIndicators = true
			}
		} else {
			if r == ':' && followedByWhitespace {
				blockIndicators = true
			}
			if r == '#' && precededByWhitespace {
				blockIndicators = true
			}
		}
		if !isPrintable(r) {
			special = true
		}
		if isBreak(r) {
			lineBreaks = true
		}
		last := i == len(runes)-1
		switch {
		case r == ' ':
			leadingSpace = leadingSpace || i == 0
			trailingSpace = trailingSpace || last
			breakSpace = breakSpace || previousBreak
			previousSpace, previousBreak = true, false
		case isBreak(r):
			leadingBreak = leadingBreak || i == 0
			trailingBreak = trailingBreak || last
			spaceBreak = spaceBreak || previousSpace
			previousSpace, previousBreak = false, true
		default:
			previousSpace, previousBreak = false, false
		}
		precededByWhitespace = isBlankz(runes, i)
	}
	a := analysis{multiline: lineBreaks, blockPlainAllowed: true, singleQuotedAllowed: true, blockAllowed: true}
	if leadingSpace || leadingBreak || trailingSpace || trailingBreak {
		a.blockPlainAllowed = false
	}
	if trailingSpace {
		a.blockAllowed = false
	}
	if breakSpace {
		a.blockPlainAllowed = false
		a.singleQuotedAllowed = false
	}
	if spaceBreak || special {
		a.blockPlainAllowed = false
		a.singleQuotedAllowed = false
		a.blockAllowed = false
	}
	if lineBreaks || blockIndicators {
		a.blockPlainAllowed = false
	}
	return a
}

// scalar is yaml_emitter_emit_scalar
func (e *emitter) scalar(v any) {
	s, st := scalarString(v)
	a := analyze(s)
	// yaml_emitter_select_scalar_style
	if e.simpleKeyContext && a.multiline {
		st = doubleQuotedStyle
	}
	if st == plainStyle && (!a.blockPlainAllowed || (s == "" && e.simpleKeyContext)) {
		st = singleQuotedStyle
	}
	if st == singleQuotedStyle && !a.singleQuotedAllowed {
		st = doubleQuotedStyle
	}
	if st == literalStyle && (!a.blockAllowed || e.simpleKeyContext) {
		st = doubleQuotedStyle
	}
	e.increaseIndent(true, false)
	allowBreaks := !e.simpleKeyContext
	switch st {
	case plainStyle:
		e.writePlain(s, allowBreaks)
	case singleQuotedStyle:
		e.writeSingleQuoted(s, allowBreaks)
	case doubleQuotedStyle:
		e.writeDoubleQuoted(s, allowBreaks)
	case literalStyle:
		e.writeLiteral(s)
	}
	e.popIndent()
}

func (e *emitter) writePlain(s string, allowBreaks bool) {
	runes := []rune(s)
	spaces, breaks := false, false
	if !e.whitespace {
		e.put(' ')
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == ' ':
			if allowBreaks && !spaces && e.column > bestWidth && !(i+1 < len(runes) && runes[i+1] == ' ') {
				e.writeIndent()
			} else {
				e.write(r)
			}
			spaces = true
		case isBreak(r):
			if !breaks && r == '\n' {
				e.putBreak()
			}
			e.writeBreak(r)
			e.indention = true
			breaks = true
		default:
			if breaks {
				e.writeIndent()
			}
			e.write(r)
			e.indention = false
			spaces, breaks = false, false
		}
	}
	e.whitespace = false
	e.indention = false
}

func (e *emitter) writeSingleQuoted(s string, allowBreaks bool) {
	runes := []rune(s)
	spaces, breaks := false, false
	e.writeIndicator("'", true, false, false)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == ' ':
			if allowBreaks && !spaces && e.column > bestWidth && i != 0 && i != len(runes)-1 && runes[i+1] != ' ' {
				e.writeIndent()
			} else {
				e.write(r)
			}
			spaces = true
		case isBreak(r):
			if !breaks && r == '\n' {
				e.putBreak()
			}
			e.writeBreak(r)
			e.indention = true
			breaks = true
		default:
			if breaks {
				e.writeIndent()
			}
			if r == '\'' {
				e.put('\'')
			}
			e.write(r)
			e.indention = false
			spaces, breaks = false, false
		}
	}
	if breaks {
		e.writeIndent()
	}
	e.writeIndicator("'", false, false, false)
	e.whitespace = false
	e.indention = false
}

// escapes are the short escapes of libyaml's double quoted scalars
var escapes = map[rune]byte{
	0x00: '0', 0x07: 'a', 0x08: 'b', 0x09: 't', 0x0A: 'n', 0x0B: 'v', 0x0C: 'f',
	0x0D: 'r', 0x1B: 'e', '"': '"', '\\': '\\', 0x85: 'N', 0xA0: '_',
	0x2028: 'L', 0x2029: 'P',
}

func (e *emitter) writeDoubleQuoted(s string, allowBreaks bool) {
	runes := []rune(s)
	spaces := false
	e.writeIndicator(`"`, true, false, false)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case !isPrintable(r) || r == 0xFEFF || isBreak(r) || r == '"' || r == '\\':
			e.put('\\')
			if c, ok := escapes[r]; ok {
				e.put(c)
			} else {
				var hex string
				switch {
				case r <= 0xFF:
					hex = fmt.Sprintf("x%02X", r)
				case r <= 0xFFFF:
					hex = fmt.Sprintf("u%04X", r)
				default:
					hex = fmt.Sprintf("U%08X", r)
				}
				for j := range len(hex) {
					e.put(hex[j])
				}
			}
			spaces = false
		case r == ' ':
			if allowBreaks && !spaces && e.column > bestWidth && i != 0 && i != len(runes)-1 {
				e.writeIndent()
				if runes[i+1] == ' ' {
					e.put('\\')
				}
			} else {
				e.write(r)
			}
			spaces = true
		default:
			e.write(r)
			spaces = false
		}
	}
	e.writeIndicator(`"`, false, false, false)
	e.whitespace = false
	e.indention = false
}

// writeBlockScalarHints writes the indentation hint (for a text starting
// with a space or break) and the chomping hint: "-" without a final line
// break, "+" with more than one
func (e *emitter) writeBlockScalarHints(runes []rune) {
	if len(runes) > 0 && (runes[0] == ' ' || isBreak(runes[0])) {
		e.writeIndicator(strconv.Itoa(bestIndent), false, false, false)
	}
	e.openEnded = 0
	chomp := ""
	n := len(runes)
	switch {
	case n == 0 || !isBreak(runes[n-1]):
		chomp = "-"
	case n == 1 || isBreak(runes[n-2]):
		chomp = "+"
		e.openEnded = 2
	}
	if chomp != "" {
		e.writeIndicator(chomp, false, false, false)
	}
}

func (e *emitter) writeLiteral(s string) {
	runes := []rune(s)
	breaks := true
	e.writeIndicator("|", true, false, false)
	e.writeBlockScalarHints(runes)
	e.putBreak()
	e.indention = true
	e.whitespace = true
	for _, r := range runes {
		if isBreak(r) {
			e.writeBreak(r)
			e.indention = true
			breaks = true
		} else {
			if breaks {
				e.writeIndent()
			}
			e.write(r)
			e.indention = false
			breaks = false
		}
	}
}
