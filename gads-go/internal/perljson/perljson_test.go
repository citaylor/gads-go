package perljson

import "testing"

// The expected documents are those of JSON::PP->new->canonical->pretty
func TestPretty(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{map[string]any{"b": int64(1), "a": "x\x01\x7f\"\\/", "c": []any{int64(1), map[string]any{"d": nil}}, "e": map[string]any{}, "f": []any{}},
			"{\n   \"a\" : \"x\\u0001\x7f\\\"\\\\/\",\n   \"b\" : 1,\n   \"c\" : [\n      1,\n      {\n         \"d\" : null\n      }\n   ],\n   \"e\" : {},\n   \"f\" : []\n}\n"},
		{[]any{}, "[]\n"},
		{map[string]any{}, "{}\n"},
		{"s", "\"s\"\n"},
	}
	for _, c := range cases {
		if got := Pretty(c.in); got != c.want {
			t.Errorf("Pretty(%v) =\n%q\nwant\n%q", c.in, got, c.want)
		}
	}
}
