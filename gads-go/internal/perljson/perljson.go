// Package perljson writes JSON as Perl's JSON::PP does with canonical
// and pretty: keys sorted, an indent of three spaces and " : " between
// keys and values.
package perljson

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Pretty is JSON->new->canonical->pretty->encode (with its final newline)
func Pretty(v any) string {
	var b strings.Builder
	value(&b, v, 0)
	b.WriteString("\n")
	return b.String()
}

func value(b *strings.Builder, v any, level int) {
	pad := strings.Repeat("   ", level+1)
	end := strings.Repeat("   ", level)
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case float64:
		b.WriteString(strconv.FormatFloat(t, 'g', 15, 64))
	case string:
		b.WriteString(Quote(t))
	case []byte:
		b.WriteString(Quote(string(t)))
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, x := range t {
			b.WriteString(pad)
			value(b, x, level+1)
			if i < len(t)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(end + "]")
	case map[string]any:
		if len(t) == 0 {
			b.WriteString("{}")
			return
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("{\n")
		for i, k := range keys {
			b.WriteString(pad + Quote(k) + " : ")
			value(b, t[k], level+1)
			if i < len(keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(end + "}")
	default:
		b.WriteString(Quote(fmt.Sprint(t)))
	}
}

// Quote is a JSON::PP string
func Quote(s string) string {
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
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\f':
			b.WriteString(`\f`)
		case '\b':
			b.WriteString(`\b`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteString(`"`)
	return b.String()
}
