package yamlxs

import "testing"

// The expected documents are those of YAML::XS Dump (perl -MYAML::XS)
func TestDump(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{map[string]any{"b": int64(1), "a": "x", "c": []any{int64(1), int64(2), map[string]any{"d": nil}}, "e": map[string]any{}, "f": []any{}},
			"---\na: x\nb: 1\nc:\n- 1\n- 2\n- d: ~\ne: {}\nf: []\n"},
		{[]any{"", "~", "true", "1.5", " lead", "a: b", "- x", "multi\nline", "it's", "#c", "plain text", "-1", "0x1F"},
			"---\n- ''\n- '~'\n- 'true'\n- '1.5'\n- ' lead'\n- 'a: b'\n- '- x'\n- \"multi\\nline\"\n- it's\n- '#c'\n- plain text\n- '-1'\n- 0x1F\n"},
		{[]any{[]any{int64(1), int64(2)}, []any{int64(3)}}, "---\n- - 1\n  - 2\n- - 3\n"},
		{"scalar", "--- scalar\n"},
		{map[string]any{}, "--- {}\n"},
	}
	for _, c := range cases {
		if got := Dump(c.in); got != c.want {
			t.Errorf("Dump(%v) =\n%q\nwant\n%q", c.in, got, c.want)
		}
	}
}
