// Package tt is a Go implementation of the Template Toolkit (TT2) language
// as used by GADS (Template 3.106 with the options set by
// Dancer2::Template::TemplateToolkit: ANYCASE, ABSOLUTE, ENCODING utf8).
//
// The parser is driven by the LALR(1) tables of Template::Grammar
// (grammar_tables.go is generated from them) so that exactly the same
// templates are accepted. Where TT compiles a template to Perl code and lets
// Perl evaluate it, this package reproduces the Perl semantics instead:
// expressions use Perl operator precedence, values follow Perl's scalar
// rules (stringification, numification, truthiness) and lists and hashes are
// references.
package tt

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/perl"
)

// Values handled by templates:
//
//	nil            Perl undef
//	string         a string
//	int64, float64 numbers (Perl IV and NV)
//	int            accepted as an IV
//	bool           accepted (true = 1, false = "")
//	*List          an array reference
//	*Hash          a hash reference
//	Code           a code reference (MACRO, root inc/dec)
//	Object         a blessed object: method calls are dispatched to it
//	*Iterator      the FOREACH loop iterator (the "loop" variable)
//
// Any other Go value is wrapped as an object by reflection (see
// reflect.go).

// List is a Perl array reference.
type List struct {
	Items []any
}

// NewList creates a list reference from values.
func NewList(items ...any) *List { return &List{Items: items} }

// Hash is a Perl hash reference. Keys retain insertion order only for
// determinism of output that would be random in Perl.
type Hash struct {
	M    map[string]any
	keys []string
}

// NewHash creates an empty hash reference.
func NewHash() *Hash { return &Hash{M: map[string]any{}} }

// HashOf creates a hash reference from key/value pairs.
func HashOf(kv ...any) *Hash {
	h := NewHash()
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(fmt.Sprint(kv[i]), kv[i+1])
	}
	return h
}

// Get returns the value of a key and whether it exists.
func (h *Hash) Get(k string) (any, bool) {
	v, ok := h.M[k]
	return v, ok
}

// Set sets a key.
func (h *Hash) Set(k string, v any) {
	if _, ok := h.M[k]; !ok {
		h.keys = append(h.keys, k)
	}
	h.M[k] = v
}

// Delete removes a key.
func (h *Hash) Delete(k string) {
	if _, ok := h.M[k]; !ok {
		return
	}
	delete(h.M, k)
	for i, kk := range h.keys {
		if kk == k {
			h.keys = append(h.keys[:i:i], h.keys[i+1:]...)
			break
		}
	}
}

// Keys returns the keys in insertion order. (Perl hash order is random;
// templates that depend on it are not deterministic in Perl either.)
func (h *Hash) Keys() []string {
	return append([]string(nil), h.keys...)
}

// Len returns the number of keys.
func (h *Hash) Len() int { return len(h.M) }

// Code is a Perl code reference called with positional arguments (a named
// argument hash, if any, is passed as the last argument). It returns a
// list of values as Perl subs return lists.
type Code func(c *Context, args []any) ([]any, error)

// Object is implemented by values that behave as blessed Perl objects in
// templates. TTMethod is called for obj.name(args): ok is false if the
// object has no such method (TT then falls back to virtual methods).
type Object interface {
	TTMethod(name string, args []any) (result []any, ok bool, err error)
}

// Stringer may be implemented by objects with overloaded stringification.
type Stringer interface {
	TTString() string
}

// Booler may be implemented by objects with overloaded boolean context.
type Booler interface {
	TTBool() bool
}

// HashLike may be implemented by objects whose Perl representation is a
// blessed hash, giving access to the underlying keys as TT does when no
// method of that name exists.
type HashLike interface {
	TTHash() *Hash
}

// ListLike may be implemented by objects that are blessed array refs.
type ListLike interface {
	TTList() *List
}

// Str stringifies a value as Perl does.
func Str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case float64:
		return perl.NumString(t)
	case bool:
		if t {
			return "1"
		}
		return ""
	case *List:
		return fmt.Sprintf("ARRAY(0x%x)", addr(t))
	case *Hash:
		return fmt.Sprintf("HASH(0x%x)", addr(t))
	case Code:
		return "CODE(0x1)"
	case Stringer:
		return t.TTString()
	case *Iterator:
		return fmt.Sprintf("Template::Iterator=HASH(0x%x)", addr(t))
	case fmt.Stringer:
		return t.String()
	}
	return fmt.Sprintf("%v", v)
}

func addr(p any) uintptr {
	return uintptr(reflectPointer(p))
}

// True is Perl truthiness.
func True(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != "" && t != "0"
	case int64:
		return t != 0
	case int:
		return t != 0
	case float64:
		return t != 0
	case bool:
		return t
	case Booler:
		return t.TTBool()
	case Stringer:
		if _, ok := v.(Object); ok {
			// References are true unless boolean context is overloaded.
			// Perl falls back to the stringification overload when only
			// "" is overloaded (fallback), as GADS's overloads do.
			s := t.TTString()
			return s != "" && s != "0"
		}
		s := t.TTString()
		return s != "" && s != "0"
	}
	return true
}

// Bool converts a Go boolean into the Perl result of a boolean operator
// (1 or "").
func Bool(b bool) any {
	if b {
		return int64(1)
	}
	return ""
}

// Num numifies a value as Perl does: the result is int64 or float64.
func Num(v any) any {
	switch t := v.(type) {
	case nil:
		return int64(0)
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return t
	case bool:
		if t {
			return int64(1)
		}
		return int64(0)
	case string:
		return numifyString(t)
	case *List, *Hash, Code, *Iterator:
		return int64(reflectPointer(t))
	}
	if s, ok := v.(Stringer); ok {
		return numifyString(s.TTString())
	}
	return int64(reflectPointer(v))
}

func numifyString(s string) any {
	t := strings.TrimLeft(s, " \t\n\r\f\v")
	// integer prefix
	i := 0
	if i < len(t) && (t[i] == '-' || t[i] == '+') {
		i++
	}
	j := i
	for j < len(t) && t[j] >= '0' && t[j] <= '9' {
		j++
	}
	if j > i {
		rest := t[j:]
		if rest == "" || !(rest[0] == '.' || rest[0] == 'e' || rest[0] == 'E') {
			if n, err := strconv.ParseInt(t[:j], 10, 64); err == nil {
				return n
			}
		}
	}
	return perl.Numify(s)
}

// Float returns the numeric value as float64.
func Float(v any) float64 {
	switch n := Num(v).(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

func arith(op string, a, b any) (any, error) {
	x, y := Num(a), Num(b)
	xi, xok := x.(int64)
	yi, yok := y.(int64)
	switch op {
	case "+":
		if xok && yok {
			s := xi + yi
			if (s > xi) == (yi > 0) || yi == 0 {
				return s, nil
			}
		}
		return Float(x) + Float(y), nil
	case "-":
		if xok && yok {
			s := xi - yi
			if (s < xi) == (yi > 0) || yi == 0 {
				return s, nil
			}
		}
		return Float(x) - Float(y), nil
	case "*":
		if xok && yok {
			if xi == 0 || yi == 0 {
				return int64(0), nil
			}
			p := xi * yi
			if p/yi == xi && !(xi == -1 && yi == math.MinInt64) && !(yi == -1 && xi == math.MinInt64) {
				return p, nil
			}
		}
		return Float(x) * Float(y), nil
	case "/":
		fy := Float(y)
		if fy == 0 {
			return nil, fmt.Errorf("Illegal division by zero")
		}
		if xok && yok && yi != 0 && xi%yi == 0 {
			return xi / yi, nil
		}
		return Float(x) / fy, nil
	case "%":
		// Perl integer modulus: operands are integers, result has the sign
		// of the right operand
		var ai, bi int64
		if xok {
			ai = xi
		} else {
			ai = int64(Float(x))
		}
		if yok {
			bi = yi
		} else {
			bi = int64(Float(y))
		}
		if bi == 0 {
			return nil, fmt.Errorf("Illegal modulus zero")
		}
		r := ai % bi
		if r != 0 && (r < 0) != (bi < 0) {
			r += bi
		}
		return r, nil
	}
	return nil, fmt.Errorf("unknown operator %s", op)
}

func perlInt(v any) any {
	switch n := Num(v).(type) {
	case int64:
		return n
	case float64:
		t := math.Trunc(n)
		if t >= -9.2e18 && t <= 9.2e18 {
			return int64(t)
		}
		return t
	}
	return int64(0)
}

func numCompare(a, b any) int {
	x, y := Num(a), Num(b)
	xi, xok := x.(int64)
	yi, yok := y.(int64)
	if xok && yok {
		switch {
		case xi < yi:
			return -1
		case xi > yi:
			return 1
		}
		return 0
	}
	fx, fy := Float(x), Float(y)
	switch {
	case fx < fy:
		return -1
	case fx > fy:
		return 1
	}
	return 0
}
