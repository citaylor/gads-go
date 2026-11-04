package lua

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/ctrlo/gads-go/internal/perltest"
)

// codeRef stands for a Perl code reference: a Lua function is given to
// Perl as one (CODE(0x...), its address unknown), which Go gives as
// "CODE"
type codeRef struct{}

// codeRefs replaces the strings "CODE" that stand for functions
func codeRefs(v any) any {
	switch t := v.(type) {
	case string:
		if t == "CODE" {
			return codeRef{}
		}
	case []any:
		for i, e := range t {
			t[i] = codeRefs(e)
		}
	case map[string]any:
		for k, e := range t {
			t[k] = codeRefs(e)
		}
	}
	return v
}

// typed is the typed form of a value returned to Perl, as
// conformance/perl/gen_lua_golden.pl writes it. Strings are bytes, given
// as Latin-1 characters.
func typed(v any) any {
	switch t := v.(type) {
	case nil:
		return map[string]any{"n": nil}
	case float64:
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, math.Float64bits(t))
		return map[string]any{"f": hex.EncodeToString(b)}
	case string:
		r := make([]rune, len(t))
		for i := range len(t) {
			r[i] = rune(t[i])
		}
		return map[string]any{"s": string(r)}
	case codeRef:
		return map[string]any{"ref": "CODE"}
	case Bool:
		if t {
			return map[string]any{"bool": float64(1)}
		}
		return map[string]any{"bool": float64(0)}
	case []any:
		out := []any{}
		for _, e := range t {
			out = append(out, typed(e))
		}
		return map[string]any{"a": out}
	case map[string]any:
		out := map[string]any{}
		for k, e := range t {
			out[k] = typed(e)
		}
		return map[string]any{"h": out}
	}
	return map[string]any{"go": reflect.TypeOf(v).String()}
}

// perlPlaceRe is the place in Perl of a croak
var perlPlaceRe = regexp.MustCompile(` at \(eval \d+\) line \d+\.\n$`)

// TestGolden runs the code of testdata/golden.json (generated from GADS's
// lua_run by conformance/perl/gen_lua_golden.pl), each in a new
// interpreter as Perl ran each in its own process, and compares what is
// returned
func TestGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Cases []struct {
			Code   string         `json:"code"`
			Vars   map[string]any `json:"vars"`
			Result map[string]any `json:"result"`
			Die    *string        `json:"die"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	for _, c := range g.Cases {
		vars := map[string]any{}
		for k, v := range c.Vars {
			x, err := perltest.Build(v.(map[string]any))
			if err != nil {
				t.Fatal(err)
			}
			if i, ok := x.(int64); ok {
				x = Int(i)
			}
			vars[k] = x
		}
		it, err := New()
		if err != nil {
			t.Fatal(err)
		}
		r, err := it.Run(c.Code, vars, nil, nil)
		it.Close()
		if c.Die != nil {
			want := *c.Die
			// Inline::Lua's croak ends with its place in Perl
			want = perlPlaceRe.ReplaceAllString(want, "\n")
			if err == nil || err.Error() != want {
				t.Errorf("%s: error %v, Perl dies %q", c.Code, err, *c.Die)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: error %v, Perl %v", c.Code, err, c.Result)
			continue
		}
		res := map[string]any{"success": r.Success}
		if r.HasReturn && r.Return != nil {
			res["return"] = r.Return
		}
		if r.Error != nil {
			res["error"] = r.Error
		}
		if got := typed(codeRefs(res)); !reflect.DeepEqual(got, c.Result) {
			gj, _ := json.Marshal(got)
			pj, _ := json.Marshal(c.Result)
			t.Errorf("%s:\n  got  %s\n  Perl %s", c.Code, gj, pj)
		}
	}
}
