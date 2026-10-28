package lua

import (
	"strings"
	"testing"
)

func TestSmoke(t *testing.T) {
	it, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	r, err := it.Run("x = vars[\"x\"]\n return x.year / 10", map[string]any{"x": map[string]any{"year": Int(2014)}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", r)
	r, err = it.Run("return {100, 200}", nil, nil, nil)
	t.Logf("%#v %v", r, err)
	r, err = it.Run("return nil + 1", nil, nil, nil)
	t.Logf("%#v %v", r, err)
	r, err = it.Run("return (", nil, nil, nil)
	t.Logf("%#v %v", r, err)
	r, err = it.Run("return f(1)", nil, nil, nil)
	t.Logf("%#v %v", r, err)
}

// Code that never ends, or that takes all memory, is stopped with an error
// and the interpreter remains usable
func TestLimits(t *testing.T) {
	it, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	it.MaxInstructions = 10_000_000
	it.MaxMemory = 64 << 20
	for _, code := range []string{
		"while true do end",
		// pcall in the code cannot keep it running
		"while true do pcall(function() while true do end end) end",
	} {
		_, err := it.Run(code, nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "code stopped after running more than 10000000 instructions") {
			t.Errorf("%s: %v", code, err)
		}
	}
	_, err = it.Run(`local s = string.rep("x", 1000000000) return #s`, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not enough memory") {
		t.Errorf("memory: %v", err)
	}
	_, err = it.Run(`local t = {} for i = 1, 100000000 do t[i] = i end return #t`, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not enough memory") {
		t.Errorf("memory of a table: %v", err)
	}
	// Callbacks still work, and the limits apply to each call
	double := Func(func(args []any) ([]any, error) { return []any{Int(2 * int64(args[0].(float64)))}, nil })
	for i := 0; i < 3; i++ {
		r, err := it.Run(`local n = 0 for i = 1, 1000000 do n = n + 1 end return working_days_diff(n)`, nil, double, nil)
		if err != nil || r.Return != float64(2000000) {
			t.Fatalf("run %d: %#v %v", i, r, err)
		}
	}
}

// A panic in a callback is an error of the code (it cannot unwind through
// Lua)
func TestCallbackPanic(t *testing.T) {
	it, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	bad := Func(func(args []any) ([]any, error) { panic("callback failed") })
	_, err = it.Run(`return working_days_diff(1)`, nil, bad, nil)
	if err == nil || !strings.Contains(err.Error(), "callback failed") {
		t.Errorf("error %v", err)
	}
}
