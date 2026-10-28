// Package lua embeds the reference Lua 5.3.6 interpreter (compiled with
// LUA_COMPAT_5_2, as per the Debian liblua5.3 build used by GADS) and
// reproduces the value-marshalling semantics of Perl's Inline::Lua (ctrlo
// fork) so that calculated and RAG fields evaluate identically to GADS.
package lua

/*
#cgo CFLAGS: -O2 -DLUA_COMPAT_5_2 -DLUA_USE_POSIX -DLUA_USE_DLOPEN -w
#cgo LDFLAGS: -lm -ldl
#include <stdlib.h>
#include <string.h>
#include "lua.h"
#include "lauxlib.h"
#include "lualib.h"

extern int goLuaCallback(lua_State *L, int id);

static int gads_trampoline(lua_State *L) {
	int id = (int)lua_tointeger(L, lua_upvalueindex(1));
	int r = goLuaCallback(L, id);
	if (r < 0) {
		return lua_error(L);
	}
	return r;
}

static void gads_push_callback(lua_State *L, int id) {
	lua_pushinteger(L, (lua_Integer)id);
	lua_pushcclosure(L, gads_trampoline, 1);
}

static int gads_loadstring(lua_State *L, const char *s, size_t len, const char *name) {
	return luaL_loadbuffer(L, s, len, name);
}

static void gads_pushlstring(lua_State *L, const char *s, size_t len) {
	lua_pushlstring(L, s, len);
}

static const char *gads_tolstring(lua_State *L, int idx, size_t *len) {
	return lua_tolstring(L, idx, len);
}

static int gads_pcall(lua_State *L, int nargs, int nresults) {
	return lua_pcall(L, nargs, nresults, 0);
}

static void gads_pop(lua_State *L, int n) { lua_pop(L, n); }
static void gads_newtable(lua_State *L) { lua_newtable(L); }
static int gads_isinteger(lua_State *L, int idx) { return lua_isinteger(L, idx); }
static void gads_getglobal(lua_State *L, const char *name) { lua_getglobal(L, name); }
*/
import "C"

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"unsafe"
)

// Int is a Perl integer scalar (IV). Pushed to Lua with lua_pushinteger.
type Int int64

// Num is a Perl floating point scalar (NV). Pushed with lua_pushnumber.
type Num float64

// Bool is the Inline::Lua::Boolean value returned from Lua.
type Bool bool

// Func is a Perl code reference callable from Lua. Arguments are converted
// from Lua using Inline::Lua rules (numbers become Num, etc).
type Func func(args []any) ([]any, error)

// The runner below is copied verbatim from GADS::Column::Code.
const luaRunSource = `
    function lua_run(string, vars, working_days_diff, working_days_add)
        local env = {}
        env["vars"] = vars

        env["working_days_diff"] = working_days_diff
        env["working_days_add"] = working_days_add

        env["ipairs"] = ipairs
        env["math"] = {
            abs = math.abs,
            acos = math.acos,
            asin = math.asin,
            atan = math.atan,
            atan2 = math.atan2,
            ceil = math.ceil,
            cos = math.cos,
            cosh = math.cosh,
            deg = math.deg,
            exp = math.exp,
            floor = math.floor,
            fmod = math.fmod,
            frexp = math.frexp,
            huge = math.huge,
            ldexp = math.ldexp,
            log = math.log,
            log10 = math.log10,
            max = math.max,
            min = math.min,
            modf = math.modf,
            pi = math.pi,
            pow = math.pow,
            rad = math.rad,
            random = math.random,
            sin = math.sin,
            sinh = math.sinh,
            sqrt = math.sqrt,
            tan = math.tan,
            tanh = math.tanh
        }
        env["next"] = next
        env["os"] = {
            clock = os.clock,
            date = os.date,
            difftime = os.difftime,
            time = os.time
        }
        env["pairs"] = pairs
        env["pcall"] = pcall
        env["select"] = select
        env["string"] = {
            byte = string.byte,
            char = string.char,
            find = string.find,
            format = string.format,
            gmatch = string.gmatch,
            gsub = string.gsub,
            len = string.len,
            lower = string.lower,
            match = string.match,
            rep = string.rep,
            reverse = string.reverse,
            sub = string.sub,
            upper = string.upper
        }
        env["table"] = {
            insert = table.insert,
            maxn = table.maxn,
            remove = table.remove,
            sort = table.sort,
            concat = table.concat
        }
        env["tonumber"] = tonumber
        env["tostring"] = tostring
        env["type"] = type
        env["unpack"] = unpack
        func, err = load(string, nil, 't', env)
        ret = {}
        if err then
            ret["success"] = 0
            ret["error"] = err
            return ret
        end
        ret["success"] = 1
        ret["return"] = func()
        return ret
    end
`

// Interpreter mirrors Inline::Lua's single global interpreter. GADS runs all
// code in one Lua state, so the same is done here (guarded by a mutex).
type Interpreter struct {
	mu        sync.Mutex
	L         *C.lua_State
	callbacks map[int]Func
	nextID    int
	cbErr     error
}

var (
	registryMu sync.Mutex
	registry   = map[*C.lua_State]*Interpreter{}
)

// New creates an interpreter with the standard libraries and the GADS
// lua_run function loaded.
func New() (*Interpreter, error) {
	L := C.luaL_newstate()
	if L == nil {
		return nil, fmt.Errorf("unable to create Lua state")
	}
	C.luaL_openlibs(L)
	it := &Interpreter{L: L, callbacks: map[int]Func{}}
	registryMu.Lock()
	registry[L] = it
	registryMu.Unlock()
	src := C.CString(luaRunSource)
	defer C.free(unsafe.Pointer(src))
	name := C.CString("_INLINED_LUA")
	defer C.free(unsafe.Pointer(name))
	if C.gads_loadstring(L, src, C.size_t(len(luaRunSource)), name) != 0 {
		return nil, fmt.Errorf("couldn't compile inline code")
	}
	if C.gads_pcall(L, 0, C.LUA_MULTRET) != 0 {
		return nil, fmt.Errorf("error: %s", it.tostring(-1))
	}
	C.lua_settop(L, 0)
	return it, nil
}

// Close releases the interpreter.
func (it *Interpreter) Close() {
	registryMu.Lock()
	delete(registry, it.L)
	registryMu.Unlock()
	C.lua_close(it.L)
}

func (it *Interpreter) tostring(idx int) string {
	var l C.size_t
	p := C.gads_tolstring(it.L, C.int(idx), &l)
	if p == nil {
		return ""
	}
	return C.GoStringN(p, C.int(l))
}

// CallbackError is returned when a Go (Perl in the original) callback
// raises an error. In Perl the die unwinds straight through Lua, so the
// resulting message is the callback's own message.
type CallbackError struct{ Msg string }

func (e *CallbackError) Error() string { return e.Msg }

// RuntimeError is the croak raised by Inline::Lua when lua_pcall fails.
type RuntimeError struct{ Msg string }

func (e *RuntimeError) Error() string { return e.Msg }

// RunResult is the Perl hash returned by lua_run.
type RunResult struct {
	Success   any
	Return    any
	HasReturn bool
	Error     any
}

// Run calls lua_run(code, vars, wdd, wda) in the same way that
// GADS::Column::Code::eval does.
func (it *Interpreter) Run(code string, vars map[string]any, wdd, wda Func) (*RunResult, error) {
	it.mu.Lock()
	defer it.mu.Unlock()
	L := it.L
	C.lua_settop(L, 0)
	it.cbErr = nil
	gname := C.CString("lua_run")
	C.gads_getglobal(L, gname)
	C.free(unsafe.Pointer(gname))
	it.pushVal(code)
	it.pushVal(vars)
	it.pushVal(wdd)
	it.pushVal(wda)
	status := C.gads_pcall(L, 4, C.LUA_MULTRET)
	// Remove callbacks registered for this call
	defer func() { it.callbacks = map[int]Func{} }()
	if status != 0 {
		msg := it.tostring(-1)
		C.lua_settop(L, 0)
		if it.cbErr != nil {
			return nil, &CallbackError{Msg: it.cbErr.Error()}
		}
		return nil, &RuntimeError{Msg: "error: " + msg + "\n"}
	}
	n := int(C.lua_gettop(L))
	if n == 0 {
		return &RunResult{}, nil
	}
	val := it.toPerl(1)
	C.lua_settop(L, 0)
	res := &RunResult{}
	if h, ok := val.(map[string]any); ok {
		res.Success = h["success"]
		res.Return, res.HasReturn = h["return"]
		res.Error = h["error"]
	}
	return res, nil
}

// pushVal implements Inline::Lua push_val for the Go representation of
// Perl values.
func (it *Interpreter) pushVal(v any) {
	L := it.L
	switch x := v.(type) {
	case nil:
		C.lua_pushnil(L)
	case Int:
		C.lua_pushinteger(L, C.lua_Integer(x))
	case int:
		C.lua_pushinteger(L, C.lua_Integer(x))
	case int64:
		C.lua_pushinteger(L, C.lua_Integer(x))
	case Num:
		C.lua_pushnumber(L, C.lua_Number(x))
	case float64:
		C.lua_pushnumber(L, C.lua_Number(x))
	case string:
		cs := C.CString(x)
		C.gads_pushlstring(L, cs, C.size_t(len(x)))
		C.free(unsafe.Pointer(cs))
	case Bool:
		if x {
			C.lua_pushboolean(L, 1)
		} else {
			C.lua_pushboolean(L, 0)
		}
	case []any:
		C.gads_newtable(L)
		for i, e := range x {
			C.lua_pushinteger(L, C.lua_Integer(i+1))
			it.pushVal(e)
			C.lua_settable(L, -3)
		}
	case map[string]any:
		C.gads_newtable(L)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			it.pushVal(k)
			it.pushVal(x[k])
			C.lua_settable(L, -3)
		}
	case Func:
		it.nextID++
		it.callbacks[it.nextID] = x
		C.gads_push_callback(L, C.int(it.nextID))
	default:
		panic(fmt.Sprintf("Attempt to pass unsupported type %T to Lua", v))
	}
}

// numToString is LUA_NUMBER_FMT ("%.14g"), used for numeric table keys
// when converted into a Perl hash.
func numToString(n float64) string {
	return FormatG(n, 14)
}

// toPerl implements Inline::Lua luaval_to_perl.
func (it *Interpreter) toPerl(idx int) any {
	L := it.L
	switch C.lua_type(L, C.int(idx)) {
	case C.LUA_TNIL:
		return nil
	case C.LUA_TBOOLEAN:
		return Bool(C.lua_toboolean(L, C.int(idx)) != 0)
	case C.LUA_TNUMBER:
		return float64(C.lua_tonumberx(L, C.int(idx), nil))
	case C.LUA_TSTRING:
		return it.tostring(idx)
	case C.LUA_TTABLE:
		return it.tableToPerl(idx)
	case C.LUA_TFUNCTION:
		return "CODE"
	default:
		return nil
	}
}

func (it *Interpreter) tableToPerl(idx int) any {
	L := it.L
	if idx < 0 {
		idx = int(C.lua_gettop(L)) + idx + 1
	}
	isary := true
	var ary []any
	var aryExists []bool
	var hash map[string]any
	C.lua_pushnil(L)
	for C.lua_next(L, C.int(idx)) != 0 {
		// key at -2, value at -1
		if isary && C.lua_type(L, -2) != C.LUA_TNUMBER {
			hash = map[string]any{}
			for i, v := range ary {
				if aryExists[i] {
					hash[strconv.Itoa(i+1)] = v
				}
			}
			isary = false
		}
		top := int(C.lua_gettop(L))
		if isary {
			n := float64(C.lua_tonumberx(L, -2, nil))
			k := int(n) - 1
			val := it.toPerl(top)
			if k < 0 {
				k += len(ary)
			}
			if k >= 0 {
				for len(ary) <= k {
					ary = append(ary, nil)
					aryExists = append(aryExists, false)
				}
				ary[k] = val
				aryExists[k] = true
			}
		} else {
			var key string
			switch C.lua_type(L, -2) {
			case C.LUA_TNUMBER:
				key = numToString(float64(C.lua_tonumberx(L, -2, nil)))
			case C.LUA_TSTRING:
				key = it.tostring(-2)
			default:
				key = ""
			}
			hash[key] = it.toPerl(top)
		}
		C.gads_pop(L, 1)
	}
	if isary {
		if ary == nil {
			ary = []any{}
		}
		return ary
	}
	return hash
}

//export goLuaCallback
func goLuaCallback(L *C.lua_State, id C.int) C.int {
	registryMu.Lock()
	it := registry[L]
	registryMu.Unlock()
	if it == nil {
		return 0
	}
	f := it.callbacks[int(id)]
	nargs := int(C.lua_gettop(L))
	args := make([]any, 0, nargs)
	for i := 1; i <= nargs; i++ {
		args = append(args, it.toPerl(i))
	}
	C.lua_settop(L, 0)
	if f == nil {
		return 0
	}
	res, err := f(args)
	if err != nil {
		it.cbErr = err
		msg := err.Error()
		cs := C.CString(msg)
		C.gads_pushlstring(L, cs, C.size_t(len(msg)))
		C.free(unsafe.Pointer(cs))
		return -1
	}
	for _, r := range res {
		it.pushVal(r)
	}
	return C.int(len(res))
}

// FormatG formats a float in the style of C's printf("%.<prec>g").
func FormatG(f float64, prec int) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if math.IsNaN(f) {
		return "nan"
	}
	return cFormatG(f, prec)
}

// cFormatG reproduces glibc %g formatting.
func cFormatG(f float64, prec int) string {
	if prec == 0 {
		prec = 1
	}
	if f == 0 {
		if math.Signbit(f) {
			return "-0"
		}
		return "0"
	}
	// Determine exponent as printf would after rounding to prec digits
	e := strconv.FormatFloat(f, 'e', prec-1, 64)
	// e is like "-1.2345e+05"
	mant, exp := splitExp(e)
	_ = mant
	if exp < -4 || exp >= prec {
		// exponential style, strip trailing zeros
		m := stripZeros(mant)
		sign := "+"
		if exp < 0 {
			sign = "-"
			exp = -exp
		}
		es := strconv.Itoa(exp)
		if len(es) < 2 {
			es = "0" + es
		}
		return m + "e" + sign + es
	}
	s := strconv.FormatFloat(f, 'f', prec-1-exp, 64)
	return stripZeros(s)
}

func splitExp(s string) (string, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == 'e' {
			n, _ := strconv.Atoi(s[i+1:])
			return s[:i], n
		}
	}
	return s, 0
}

func stripZeros(s string) string {
	dot := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return s
	}
	i := len(s)
	for i > dot+1 && s[i-1] == '0' {
		i--
	}
	if i == dot+1 {
		i = dot
	}
	return s[:i]
}
