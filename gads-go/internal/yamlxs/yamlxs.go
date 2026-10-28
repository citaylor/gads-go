// Package yamlxs writes YAML as YAML::XS (libyaml) Dump does: block
// style, mapping keys sorted, sequences in mappings not indented, and
// strings quoted when they would otherwise read as another type.
package yamlxs

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Dump is YAML::XS Dump of one document. Values are nil, bool, integers,
// floats, strings, []any and map[string]any.
func Dump(v any) string {
	var b strings.Builder
	b.WriteString("---")
	if isCollection(v) && !isEmpty(v) {
		b.WriteString("\n")
		node(&b, v, 0, false)
	} else {
		b.WriteString(" " + scalar(v) + "\n")
	}
	return b.String()
}

func isCollection(v any) bool {
	switch v.(type) {
	case []any, map[string]any:
		return true
	}
	return false
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// node writes a collection at an indent. inline is whether its first line
// follows a "- " already written.
func node(b *strings.Builder, v any, indent int, inline bool) {
	pad := strings.Repeat(" ", indent)
	switch t := v.(type) {
	case []any:
		for i, item := range t {
			if !(inline && i == 0) {
				b.WriteString(pad)
			}
			b.WriteString("-")
			if isCollection(item) && !isEmpty(item) {
				if _, ok := item.([]any); ok {
					b.WriteString(" ")
					node(b, item, indent+2, true)
				} else {
					b.WriteString(" ")
					node(b, item, indent+2, true)
				}
			} else {
				b.WriteString(" " + scalar(item) + "\n")
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if !(inline && i == 0) {
				b.WriteString(pad)
			}
			b.WriteString(scalar(k) + ":")
			item := t[k]
			if isCollection(item) && !isEmpty(item) {
				b.WriteString("\n")
				if _, ok := item.([]any); ok {
					// Sequences in mappings are not indented
					node(b, item, indent, false)
				} else {
					node(b, item, indent+2, false)
				}
			} else {
				b.WriteString(" " + scalar(item) + "\n")
			}
		}
	}
}

// looksLikeNumber is Perl's looks_like_number of a string
var looksLikeNumber = regexp.MustCompile(`^\s*[-+]?(?:(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][-+]?[0-9]+)?|(?i:inf(?:inity)?|nan))\s*$`)

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "~"
	case []any:
		return "[]"
	case map[string]any:
		return "{}"
	case bool:
		if t {
			return "1"
		}
		return "''"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', 15, 64)
	case string:
		return str(t)
	}
	return str(fmt.Sprint(v))
}

func str(s string) string {
	if s == "" || s == "~" || s == "true" || s == "false" || s == "null" || looksLikeNumber.MatchString(s) {
		return singleQuoted(s)
	}
	switch plainStyle(s) {
	case "plain":
		return s
	case "single":
		return singleQuoted(s)
	}
	return doubleQuoted(s)
}

func singleQuoted(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func doubleQuoted(s string) string {
	var b strings.Builder
	b.WriteString(`"`)
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case 0:
			b.WriteString(`\0`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteString(`"`)
	return b.String()
}

// plainStyle is libyaml's choice for a string in block context: plain if
// it allows it, else single quoted if it can be, else double quoted
func plainStyle(s string) string {
	runes := []rune(s)
	special := false
	for _, r := range runes {
		if (r < 0x20 && r != '\n') || r == 0x7f || (!unicode.IsPrint(r) && r != '\n' && r != ' ') {
			special = true
		}
	}
	if special {
		return "double"
	}
	if strings.ContainsRune(s, '\n') {
		return "double"
	}
	if runes[0] == ' ' || runes[len(runes)-1] == ' ' {
		return "single"
	}
	if strings.HasPrefix(s, "---") || strings.HasPrefix(s, "...") {
		return "single"
	}
	first := runes[0]
	if strings.ContainsRune(",[]{}#&*!|>'\"%@`", first) {
		return "single"
	}
	if strings.ContainsRune("-?:", first) && (len(runes) == 1 || runes[1] == ' ') {
		return "single"
	}
	if strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.HasSuffix(s, ":") {
		return "single"
	}
	return "plain"
}
