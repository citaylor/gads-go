// Package perl contains helpers that reproduce Perl semantics which GADS
// relies upon (number stringification, truthiness, string sorting,
// looks_like_number etc), so that the Go port produces identical output.
package perl

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/lua"
)

// NumString stringifies a floating point number as Perl does ("%.15g").
// Perl 5.32 prints negative zero as 0.
func NumString(f float64) string {
	if f == 0 {
		return "0"
	}
	if math.IsInf(f, 1) {
		return "Inf"
	}
	if math.IsInf(f, -1) {
		return "-Inf"
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	return lua.FormatG(f, 15)
}

var lookslikeRe = regexp.MustCompile(`^\s*[-+]?(?:(?:\d+\.?\d*(?:[eE][-+]?\d+)?)|(?:\.\d+(?:[eE][-+]?\d+)?)|0[xX][0-9a-fA-F]+|(?i:inf(?:inity)?|nan))\s*$`)

// LooksLikeNumber mirrors Scalar::Util::looks_like_number for strings.
// Note: leading whitespace is allowed, trailing whitespace too (perl >= 5.8
// allows trailing whitespace/newline).
func LooksLikeNumber(s string) bool {
	if s == "" {
		return false
	}
	// grok_number's special case
	if s == zeroButTrue {
		return true
	}
	// Perl does not accept hex strings as numbers
	if strings.Contains(strings.ToLower(s), "0x") {
		return false
	}
	return lookslikeRe.MatchString(s)
}

var numPrefixRe = regexp.MustCompile(`^(\d+\.?\d*(?:[eE][-+]?\d+)?|\.\d+(?:[eE][-+]?\d+)?)`)

// zeroButTrue is the string that Perl takes as the integer 0 without a
// warning
const zeroButTrue = "0 but true"

const spaces = " \t\n\r\f\v"

// cutSign removes the leading spaces and sign of a number, and returns
// whether it was negative
func cutSign(s string) (string, bool) {
	s = strings.TrimLeft(s, spaces)
	if s != "" && (s[0] == '-' || s[0] == '+') {
		return s[1:], s[0] == '-'
	}
	return s, false
}

// Numify converts a string to a number as Perl does in numeric context
// (leading numeric portion, otherwise 0). Perl's atof takes a sign, then
// the number as strtod reads it, which may have its own spaces and sign:
// "- 3" is -3 and "--1" is 1.
func Numify(s string) float64 {
	s, neg := cutSign(s)
	s, neg2 := cutSign(s)
	f := 0.0
	if m := numPrefixRe.FindStringSubmatch(s); m != nil {
		f, _ = strconv.ParseFloat(m[1], 64)
	} else {
		switch ls := strings.ToLower(s); {
		case strings.HasPrefix(ls, "inf"):
			f = math.Inf(1)
		case strings.HasPrefix(ls, "nan"):
			return math.NaN()
		}
	}
	if neg != neg2 {
		f = -f
	}
	return f
}

var intLikeRe = regexp.MustCompile(`^\s*[-+]?\d+\s*$`)

// NumifyTyped converts a string to a number as Perl's $s + 0, returning
// whether the result is an integer (IV) or a float (NV). Addition is done
// on integers when the string gives one (sv_2iuv_common): an integer
// string, "0 but true", or a number with an exponent whose value is whole
// ("1e5"). A decimal point ("3.0") makes a float, as does a string that is
// not a number ("12abc"). An unsigned integer beyond 2**63 (a UV in Perl)
// is a float here, as Lua's integers are signed.
func NumifyTyped(s string) (any, bool) {
	if intLikeRe.MatchString(s) {
		t := strings.TrimSpace(s)
		if i, err := strconv.ParseInt(t, 10, 64); err == nil {
			return i, true
		}
	}
	if s == zeroButTrue {
		return int64(0), true
	}
	f := Numify(s)
	if LooksLikeNumber(s) && strings.ContainsAny(s, "eE") && f == math.Trunc(f) && f >= -(1<<63) && f < 1<<63 {
		return int64(f), true
	}
	return f, false
}

// True returns Perl truthiness of a string.
func True(s string) bool {
	return s != "" && s != "0"
}

// Lc is Perl lc. Unicode lowercases capital I with dot above (U+0130) to
// i and a combining dot (SpecialCasing), where Go gives a plain i.
func Lc(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "\u0130", "i\u0307"))
}

// Substr returns the first n characters (not bytes) of s, as Perl's
// substr on a character string.
func Substr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// roundHalf is $Math::Round::half: a little more than one half, so that
// numbers just below a half (from floating point errors) round up
const roundHalf = 0.50000000000008

// RoundHalfAway rounds as Math::Round::round (half away from zero).
func RoundHalfAway(f float64) float64 {
	if f < 0 {
		return math.Ceil(f - roundHalf)
	}
	return math.Floor(f + roundHalf)
}

// SprintfFixed implements sprintf("%.Nf", v).
func SprintfFixed(v float64, places int) string {
	return strconv.FormatFloat(v, 'f', places, 64)
}

// Int implements Perl's int() on a numeric value. Perl gives an integer
// (IV) when it fits, so convert the result to int64 to print it.
func Int(f float64) float64 {
	return math.Trunc(f)
}
