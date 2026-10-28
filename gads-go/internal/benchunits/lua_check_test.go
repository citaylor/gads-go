package benchunits

import (
	"testing"

	"github.com/ctrlo/gads-go/internal/lua"
)

// TestLuaUnits checks that the code of the Lua units evaluates (rather than
// timing an error)
func TestLuaUnits(t *testing.T) {
	it, err := lua.New()
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	cb := lua.Func(func([]any) ([]any, error) { return nil, nil })
	for _, c := range []struct {
		code string
		vars map[string]any
		want any
	}{
		{luaSimple, map[string]any{"L1integer1": int64(21)}, float64(42)},
		{luaLoop, map[string]any{"L1integer1": int64(7)}, float64(59998)},
		{luaStrings, map[string]any{"L1string1": "the quick brown fox"}, "THE-QUICK-BROWN-FOX"},
	} {
		res, err := it.Run(c.code, c.vars, cb, cb)
		if err != nil {
			t.Fatal(err)
		}
		if res.Return != c.want {
			t.Errorf("%q: got %v (error %v), want %v", c.code, res.Return, res.Error, c.want)
		}
	}
}
