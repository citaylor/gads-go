package gads

// Hooks are the extension points of GADS::Hooks (GADS-dev): code that
// extensions can add to be run at points of the processing. Only the hooks
// known are allowed, and each can be added once.
type Hooks struct {
	hooks map[string]func(args ...any)
}

// HooksInstance is the hooks of the process (as the singleton instance of
// GADS::Hooks)
var HooksInstance = &Hooks{}

// The known hooks: record.write.before_write_values is run (with the
// record and its layout) before a record's values are written
var availableHooks = map[string]bool{
	"record.write.before_write_values": true,
}

// HookExists is hook_exists
func (h *Hooks) HookExists(name string) bool { return availableHooks[name] }

// AddHook is add_hook
func (h *Hooks) AddHook(name string, code func(args ...any)) {
	if !h.HookExists(name) {
		Panic("Hook %s not known", name)
	}
	if h.hooks[name] != nil {
		Panic("Hook %s already exists", name)
	}
	if h.hooks == nil {
		h.hooks = map[string]func(args ...any){}
	}
	h.hooks[name] = code
}

// RunHook is run_hook
func (h *Hooks) RunHook(name string, args ...any) {
	if !h.HookExists(name) {
		Panic("Hook %s not known", name)
	}
	if code := h.hooks[name]; code != nil {
		code(args...)
	}
}
