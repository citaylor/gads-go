// Package perltest reads the values of the golden files that the Perl
// generators in conformance/perl write (gen_json_golden.pl and
// gen_yaml_golden.pl), for the tests that compare Go with Perl.
package perltest

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
)

// Build makes a value from its typed form, as the generators build it in
// Perl: {"f": "<IEEE 754 bits>"} a float, {"i": "<digits>"} an integer,
// {"s": "..."} a string, {"b": 0|1} a boolean, {"n": null}, {"a": [...]}
// an array and {"h": {...}} a hash.
func Build(c map[string]any) (any, error) {
	for k, v := range c {
		switch k {
		case "f":
			s, _ := v.(string)
			b, err := hex.DecodeString(s)
			if err != nil || len(b) != 8 {
				return nil, fmt.Errorf("bad float %q", s)
			}
			return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
		case "i":
			s, _ := v.(string)
			return strconv.ParseInt(s, 10, 64)
		case "s":
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("bad string %v", v)
			}
			return s, nil
		case "b":
			f, _ := v.(float64)
			return f != 0, nil
		case "n":
			return nil, nil
		case "a":
			l, _ := v.([]any)
			out := []any{}
			for _, e := range l {
				m, _ := e.(map[string]any)
				x, err := Build(m)
				if err != nil {
					return nil, err
				}
				out = append(out, x)
			}
			return out, nil
		case "h":
			h, _ := v.(map[string]any)
			out := map[string]any{}
			for hk, e := range h {
				m, _ := e.(map[string]any)
				x, err := Build(m)
				if err != nil {
					return nil, err
				}
				out[hk] = x
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("bad value %v", c)
}
