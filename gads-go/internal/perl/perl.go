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
func NumString(f float64) string {
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
	// Perl does not accept hex strings as numbers
	if strings.Contains(strings.ToLower(s), "0x") {
		return false
	}
	return lookslikeRe.MatchString(s)
}

var numPrefixRe = regexp.MustCompile(`^\s*([-+]?(?:\d+\.?\d*(?:[eE][-+]?\d+)?|\.\d+(?:[eE][-+]?\d+)?))`)

// Numify converts a string to a number as Perl does in numeric context
// (leading numeric portion, otherwise 0).
func Numify(s string) float64 {
	m := numPrefixRe.FindStringSubmatch(s)
	if m == nil {
		ls := strings.ToLower(strings.TrimSpace(s))
		switch {
		case strings.HasPrefix(ls, "inf") || strings.HasPrefix(ls, "+inf"):
			return math.Inf(1)
		case strings.HasPrefix(ls, "-inf"):
			return math.Inf(-1)
		case strings.HasPrefix(ls, "nan"):
			return math.NaN()
		}
		return 0
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	return f
}

var intLikeRe = regexp.MustCompile(`^\s*[-+]?\d+\s*$`)

// NumifyTyped converts a string to a number, returning whether the
// resulting Perl scalar would be an integer (IV) or a float (NV). Perl's
// grok_number produces an IV for integer strings that fit in 64 bits.
func NumifyTyped(s string) (any, bool) {
	if intLikeRe.MatchString(s) {
		t := strings.TrimSpace(s)
		if i, err := strconv.ParseInt(t, 10, 64); err == nil {
			return i, true
		}
	}
	return Numify(s), false
}

// True returns Perl truthiness of a string.
func True(s string) bool {
	return s != "" && s != "0"
}

// Lc is Perl lc for ASCII/Unicode.
func Lc(s string) string { return strings.ToLower(s) }

// Substr returns the first n characters (not bytes) of s, as Perl's
// substr on a character string.
func Substr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// RoundHalfAway rounds as Math::Round::round (half away from zero).
func RoundHalfAway(f float64) float64 {
	if f < 0 {
		return -math.Floor(-f + 0.5)
	}
	return math.Floor(f + 0.5)
}

// SprintfFixed implements sprintf("%.Nf", v).
func SprintfFixed(v float64, places int) string {
	return strconv.FormatFloat(v, 'f', places, 64)
}

// Int implements Perl's int() on a numeric value.
func Int(f float64) float64 {
	return math.Trunc(f)
}
