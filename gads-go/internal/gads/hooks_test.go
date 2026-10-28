package gads

import "testing"

func TestHooks(t *testing.T) {
	h := &Hooks{}
	if !h.HookExists("record.write.before_write_values") || h.HookExists("nope") {
		t.Fatal("HookExists")
	}
	// Running a hook not added does nothing
	h.RunHook("record.write.before_write_values", 1)
	var got []any
	h.AddHook("record.write.before_write_values", func(args ...any) { got = args })
	h.RunHook("record.write.before_write_values", 1, 2)
	if len(got) != 2 {
		t.Fatalf("hook args %v", got)
	}
	for _, f := range []func(){
		func() { h.AddHook("record.write.before_write_values", func(...any) {}) },
		func() { h.AddHook("nope", func(...any) {}) },
		func() { h.RunHook("nope") },
	} {
		if err := Try(f); err == nil {
			t.Fatal("expected panic")
		}
	}
}
