package lua

import (
	"fmt"
	"strings"
	"testing"
	"time"
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
	// A table key that Perl would turn into an array of 2^53 elements
	for _, code := range []string{`return {[2^53] = 1}`, `return {[1] = 1, [1e20] = 2}`, `return {x = {[1e7] = 1}}`} {
		_, err = it.Run(code, nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "is over the limit of 10000000") {
			t.Errorf("%s: %v", code, err)
		}
	}
	if r, err := it.Run(`return {[9999999] = 1}`, nil, nil, nil); err != nil || len(r.Return.([]any)) != 9999999 {
		t.Errorf("largest index: %v", err)
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

// The instructions of a call are counted only once it has run for
// CountAfter, and only during that call
func TestCountAfter(t *testing.T) {
	it, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	// working_days_diff(ms) sleeps, then reports whether the count hook is
	// set
	probe := Func(func(args []any) ([]any, error) {
		time.Sleep(time.Duration(args[0].(float64)) * time.Millisecond)
		if it.counting() {
			return []any{Int(1)}, nil
		}
		return []any{Int(0)}, nil
	})
	check := func(name string, ms int, want int64) {
		t.Helper()
		r, err := it.Run(`return working_days_diff(vars.ms)`, map[string]any{"ms": Int(ms)}, probe, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := fmt.Sprint(r.Return); got != fmt.Sprint(want) {
			t.Errorf("%s: counting %v, want %d", name, r.Return, want)
		}
	}
	it.CountAfter = 50 * time.Millisecond
	check("start of a call", 0, 0)
	check("after CountAfter", 120, 1)
	// The next call starts without the hook, and the timer of the last
	// call, stopped, does not set it
	check("next call", 0, 0)
	check("next call, later", 20, 0)
	// No limit: never counted
	it.MaxInstructions = 0
	check("no limit", 120, 0)
	// CountAfter 0: counted from the start
	it.MaxInstructions = 10_000_000
	it.CountAfter = 0
	check("CountAfter 0", 0, 1)
	it.CountAfter = 50 * time.Millisecond
	check("CountAfter again", 0, 0)

	// Code that never ends is stopped after CountAfter and the limit, and
	// many short calls in a row are not
	start := time.Now()
	if _, err := it.Run("while true do end", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "more than 10000000 instructions") {
		t.Errorf("endless code: %v", err)
	}
	if d := time.Since(start); d < it.CountAfter {
		t.Errorf("stopped after %s, before CountAfter", d)
	}
	for i := 0; i < 2000; i++ {
		if _, err := it.Run(`local n = 0 for i = 1, 1000 do n = n + i end return n`, nil, nil, nil); err != nil {
			t.Fatalf("short call %d: %v", i, err)
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
