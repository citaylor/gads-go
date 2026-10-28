package tt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Exception is a Template::Exception.
type Exception struct {
	Type string
	Info any
	text *strings.Builder
}

func (e *Exception) Error() string { return e.Type + " error - " + Str(e.Info) }

// AsString is Template::Exception::as_string.
func (e *Exception) AsString() string { return e.Error() }

func (e *Exception) TTString() string { return e.Error() }

func (e *Exception) TTMethod(name string, args []any) ([]any, bool, error) {
	switch name {
	case "type":
		return []any{e.Type}, true, nil
	case "info":
		return []any{e.Info}, true, nil
	case "text":
		if e.text == nil {
			return nil, true, nil
		}
		return []any{e.text.String()}, true, nil
	case "as_string":
		return []any{e.Error()}, true, nil
	}
	return nil, false, nil
}

// selectHandler is Template::Exception::select_handler
func (e *Exception) selectHandler(handlers []string) (string, bool) {
	hlut := map[string]bool{}
	for _, h := range handlers {
		hlut[h] = true
	}
	t := e.Type
	for t != "" {
		if hlut[t] {
			return t, true
		}
		i := strings.LastIndex(t, ".")
		if i < 0 {
			break
		}
		t = t[:i]
	}
	return "", false
}

func throwErr(typ string, info any) *Exception { return &Exception{Type: typ, Info: info} }

// loopCtl is a Perl loop control (next/last LABEL) propagating to the
// innermost (dynamically) enclosing loop with that label.
type loopCtl struct {
	last  bool
	label string
}

func (l *loopCtl) Error() string {
	kw := "next"
	if l.last {
		kw = "last"
	}
	return fmt.Sprintf("Label not found for \"%s %s\"", kw, l.label)
}

func (l *loopCtl) matches(label string) bool { return l.label == "" || l.label == label }

// ----------------------------------------------------------------------
// Engine (Template + Provider)
// ----------------------------------------------------------------------

// Engine compiles and caches templates found on its include path.
type Engine struct {
	IncludePath []string
	// Load reads a template file; defaults to os.ReadFile
	Load func(path string) ([]byte, error)
	// Filters are additional static filters (name => func(text) string)
	Filters map[string]func(string) string
	// NoAnyCase disables ANYCASE (directive keywords must be upper case),
	// as a Template object created with default options
	NoAnyCase bool

	mu    sync.Mutex
	cache map[string]*document
}

// NewEngine creates an engine with the given include path.
func NewEngine(includePath ...string) *Engine {
	return &Engine{IncludePath: includePath, cache: map[string]*document{}}
}

func (e *Engine) load(path string) ([]byte, error) {
	if e.Load != nil {
		return e.Load(path)
	}
	return os.ReadFile(path)
}

var relativePathRe = regexp.MustCompile(`(?:^|/)\.+/`)

// findFile resolves a template name to a path, as Template::Provider
func (e *Engine) findFile(name string) (string, []byte, bool) {
	if filepath.IsAbs(name) {
		b, err := e.load(name)
		if err != nil {
			return "", nil, false
		}
		return name, b, true
	}
	if relativePathRe.MatchString(name) {
		return "", nil, false
	}
	for _, dir := range e.IncludePath {
		p := filepath.Join(dir, name)
		if b, err := e.load(p); err == nil {
			return p, b, true
		}
	}
	return "", nil, false
}

func (e *Engine) fetch(name string) (*document, error) {
	path, data, ok := e.findFile(name)
	if !ok {
		return nil, nil
	}
	e.mu.Lock()
	if d, ok := e.cache[path]; ok {
		e.mu.Unlock()
		return d, nil
	}
	e.mu.Unlock()
	doc, err := parseTemplate(string(data), !e.NoAnyCase)
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			return nil, throwErr("file", "parse error - "+name+" "+pe.Msg)
		}
		return nil, throwErr("file", err.Error())
	}
	doc.name = name
	e.mu.Lock()
	if e.cache == nil {
		e.cache = map[string]*document{}
	}
	e.cache[path] = doc
	e.mu.Unlock()
	return doc, nil
}

// Process renders the named template with the given variables, as
// Template::process. Errors are *Exception values.
func (e *Engine) Process(name string, vars *Hash) (string, error) {
	c := &Context{eng: e, blocks: map[string]*tmplBody{}, filterCache: map[string]filterFunc{}}
	c.stash = newStash(vars)
	tmpl, err := c.template(name)
	if err != nil {
		return "", err
	}
	params := NewHash()
	if d, ok := tmpl.(*document); ok {
		params.Set("template", d.object())
	}
	c.localise(params)
	out, err := c.processTemplates([]any{tmpl}, nil, false)
	if err != nil {
		if ex, ok := err.(*Exception); ok && ex.Type == "stop" {
			if ex.text != nil {
				out = ex.text.String()
			}
			err = nil
		}
	}
	c.delocalise()
	if err != nil {
		return "", toException(err)
	}
	return out, nil
}

func toException(err error) *Exception {
	var ex *Exception
	if errors.As(err, &ex) {
		return ex
	}
	return throwErr("undef", err.Error())
}

func (d *document) object() *Hash {
	h := HashOf("name", d.name, "modtime", int64(time.Now().Unix()))
	for k, v := range d.metadata {
		h.Set(k, v)
	}
	return h
}

// ----------------------------------------------------------------------
// Stash
// ----------------------------------------------------------------------

// Stash is Template::Stash: a hash of variables with a parent link.
type Stash struct {
	vars   *Hash
	parent *Stash
}

var rootOps = map[string]Code{
	"inc": func(c *Context, args []any) ([]any, error) {
		v, _ := arith("+", argN(args, 0), int64(1))
		return []any{v}, nil
	},
	"dec": func(c *Context, args []any) ([]any, error) {
		v, _ := arith("-", argN(args, 0), int64(1))
		return []any{v}, nil
	},
}

func newStash(vars *Hash) *Stash {
	h := NewHash()
	h.Set("global", NewHash())
	if vars != nil {
		for _, k := range vars.Keys() {
			v, _ := vars.Get(k)
			h.Set(k, v)
		}
	}
	h.Set("inc", rootOps["inc"])
	h.Set("dec", rootOps["dec"])
	return &Stash{vars: h}
}

func (s *Stash) clone(params *Hash) *Stash {
	h := &Hash{M: make(map[string]any, len(s.vars.M)), keys: append([]string(nil), s.vars.keys...)}
	for k, v := range s.vars.M {
		h.M[k] = v
	}
	var imp *Hash
	if params != nil {
		if v, ok := params.Get("import"); ok {
			if ih, ok := v.(*Hash); ok {
				imp = ih
			}
		}
		for _, k := range params.Keys() {
			if k == "import" && imp != nil {
				continue
			}
			v, _ := params.Get(k)
			h.Set(k, v)
		}
	}
	if imp != nil {
		for _, k := range imp.Keys() {
			v, _ := imp.Get(k)
			h.Set(k, v)
		}
	}
	return &Stash{vars: h, parent: s}
}

func (s *Stash) declone() *Stash {
	if s.parent != nil {
		return s.parent
	}
	return s
}

func (s *Stash) update(params *Hash) {
	if params == nil {
		return
	}
	if v, ok := params.Get("import"); ok {
		if ih, ok := v.(*Hash); ok {
			for _, k := range ih.Keys() {
				iv, _ := ih.Get(k)
				s.vars.Set(k, iv)
			}
			params.Delete("import")
		}
	}
	for _, k := range params.Keys() {
		v, _ := params.Get(k)
		s.vars.Set(k, v)
	}
}

// ----------------------------------------------------------------------
// Context
// ----------------------------------------------------------------------

// Context is Template::Context for one processing run.
type Context struct {
	eng         *Engine
	stash       *Stash
	blocks      map[string]*tmplBody
	blkstack    []map[string]*tmplBody
	filterCache map[string]filterFunc
}

// Stash returns the current stash's variables.
func (c *Context) Vars() *Hash { return c.stash.vars }

func (c *Context) localise(params *Hash) *Stash {
	c.stash = c.stash.clone(params)
	return c.stash
}

func (c *Context) delocalise() *Stash {
	c.stash = c.stash.declone()
	return c.stash
}

// template finds a block or file template (Template::Context::template).
func (c *Context) template(name any) (any, error) {
	switch t := name.(type) {
	case *tmplBody, *document:
		return t, nil
	case Code:
		return t, nil
	}
	n := Str(name)
	if b, ok := c.blocks[n]; ok {
		return b, nil
	}
	for _, bl := range c.blkstack {
		if b, ok := bl[n]; ok {
			return b, nil
		}
	}
	if m := prefixRe.FindStringSubmatch(n); m != nil {
		return nil, throwErr("file", "no providers for template prefix '"+m[1]+"'")
	}
	doc, err := c.eng.fetch(n)
	if err != nil {
		return nil, err
	}
	if doc != nil {
		return doc, nil
	}
	return nil, throwErr("file", n+": not found")
}

var prefixRe = regexp.MustCompile(`^(\w+):`)

func (c *Context) processTemplates(names []any, params *Hash, localize bool) (string, error) {
	var compiled []any
	for _, n := range names {
		t, err := c.template(n)
		if err != nil {
			return "", err
		}
		compiled = append(compiled, t)
	}
	if localize {
		c.stash = c.stash.clone(params)
	} else {
		c.stash.update(params)
	}
	var out strings.Builder
	err := func() error {
		component, _ := c.stash.vars.Get("component")
		for i, t := range compiled {
			var element any
			switch tt := t.(type) {
			case *document:
				element = tt.object()
			default:
				nm := ""
				if s, ok := names[i].(string); ok {
					nm = s
				}
				element = HashOf("name", nm, "modtime", int64(time.Now().Unix()))
			}
			c.stash.vars.Set("component", element)
			var s string
			var err error
			switch tt := t.(type) {
			case *document:
				if !localize {
					for k, b := range tt.blocks {
						c.blocks[k] = b
					}
				}
				s, err = c.processDocument(tt)
			case *tmplBody:
				s, err = c.runBody(tt)
			case Code:
				var r []any
				r, err = tt(c, nil)
				if len(r) > 0 {
					s = Str(r[0])
				}
			}
			if err != nil {
				return err
			}
			out.WriteString(s)
		}
		c.stash.vars.Set("component", component)
		return nil
	}()
	if localize {
		c.stash = c.stash.declone()
	}
	if err != nil {
		var ex *Exception
		var lc *loopCtl
		if errors.As(err, &ex) || errors.As(err, &lc) {
			return "", err
		}
		return "", throwErr("file", err.Error())
	}
	return out.String(), nil
}

func (c *Context) processDocument(d *document) (string, error) {
	c.blkstack = append([]map[string]*tmplBody{d.blocks}, c.blkstack...)
	s, err := c.runBody(d.body)
	c.blkstack = c.blkstack[1:]
	if err != nil {
		var lc *loopCtl
		if errors.As(err, &lc) {
			return "", err
		}
		return "", c.catch(err, nil)
	}
	return s, nil
}

// catch is Template::Context::catch
func (c *Context) catch(err error, out *strings.Builder) *Exception {
	var ex *Exception
	if errors.As(err, &ex) {
		if out != nil && ex.text != nil {
			if ex.text != out {
				out.WriteString(ex.text.String())
			}
			ex.text = out
		}
		return ex
	}
	return &Exception{Type: "undef", Info: err.Error(), text: out}
}

// runBody runs a compiled template sub: eval { BLOCK: { ... } } catching
// RETURN.
func (c *Context) runBody(t *tmplBody) (string, error) {
	var out strings.Builder
	if t == nil {
		return "", nil
	}
	err := c.execBlock(t.body, &out)
	if err != nil {
		var lc *loopCtl
		if errors.As(err, &lc) {
			if lc.label == "" || lc.label == "BLOCK" {
				return out.String(), nil
			}
			return "", err
		}
		ex := c.catch(err, &out)
		if ex.Type != "return" {
			return "", ex
		}
	}
	return out.String(), nil
}

func (c *Context) include(names []any, params *Hash) (string, error) {
	return c.processTemplates(names, params, true)
}

func (c *Context) process(names []any, params *Hash) (string, error) {
	return c.processTemplates(names, params, false)
}

func (c *Context) insert(names []any) (string, error) {
	var out strings.Builder
	for _, n := range names {
		name := Str(n)
		if m := prefixRe.FindStringSubmatch(name); m != nil {
			return "", throwErr("file", "no providers for file prefix '"+m[1]+"'")
		}
		_, data, ok := c.eng.findFile(name)
		if !ok {
			return "", throwErr("file", name+": not found")
		}
		out.Write(data)
	}
	return out.String(), nil
}

// ----------------------------------------------------------------------
// Statement execution
// ----------------------------------------------------------------------

func (c *Context) execBlock(b block, out *strings.Builder) error {
	for _, s := range b {
		if err := c.execStmt(s, out); err != nil {
			return err
		}
	}
	return nil
}

func (c *Context) execStmt(s stmt, out *strings.Builder) error {
	switch t := s.(type) {
	case nil:
		return nil
	case *stmtText:
		out.WriteString(t.text)
	case *stmtGet:
		v, err := c.eval(t.e)
		if err != nil {
			return err
		}
		out.WriteString(Str(v))
	case *stmtCall:
		_, err := c.eval(t.e)
		return err
	case *stmtSet:
		for _, a := range t.assigns {
			v, err := c.eval(a.val)
			if err != nil {
				return err
			}
			if _, err := c.assign(a.target, v, t.def); err != nil {
				return err
			}
		}
	case *stmtInclude:
		names, err := c.evalNames(t.names)
		if err != nil {
			return err
		}
		params, err := c.evalNamed(t.named)
		if err != nil {
			return err
		}
		var r string
		if t.process {
			r, err = c.process(names, params)
		} else {
			r, err = c.include(names, params)
		}
		if err != nil {
			return err
		}
		out.WriteString(r)
	case *stmtInsert:
		names, err := c.evalNames(t.names)
		if err != nil {
			return err
		}
		r, err := c.insert(names)
		if err != nil {
			return err
		}
		out.WriteString(r)
	case *stmtThrow:
		return c.execThrow(t, out)
	case *stmtReturn:
		return &Exception{Type: "return", Info: "", text: out}
	case *stmtStop:
		return &Exception{Type: "stop", Info: "", text: out}
	case *stmtClear:
		out.Reset()
	case *stmtLast:
		return &loopCtl{last: true, label: t.label}
	case *stmtNext:
		if t.advance {
			return &loopCtl{label: t.label, last: false}
		}
		return &loopCtl{label: t.label}
	case *stmtIf:
		for _, br := range t.branches {
			v, err := c.eval(br.cond)
			if err != nil {
				return err
			}
			if True(v) {
				return c.execBlock(br.body, out)
			}
		}
		if t.hasElse {
			return c.execBlock(t.els, out)
		}
	case *stmtSwitch:
		return c.execSwitch(t, out)
	case *stmtForeach:
		return c.execForeach(t, out)
	case *stmtWhile:
		return c.execWhile(t, out)
	case *stmtWrapper:
		return c.execWrapper(t, out)
	case *stmtFilter:
		return c.execFilter(t, out)
	case *stmtAnon:
		r, err := c.runBody(t.body)
		if err != nil {
			return err
		}
		out.WriteString(r)
	case *stmtCapture:
		var inner strings.Builder
		if err := c.execBlock(t.body, &inner); err != nil {
			return err
		}
		_, err := c.assign(t.target, inner.String(), false)
		return err
	case *stmtMacro:
		c.defineMacro(t)
	case *stmtTry:
		return c.execTry(t, out)
	case *stmtThrowFixed:
		return &Exception{Type: t.typ, Info: t.info, text: out}
	case *stmtUse:
		n, err := c.eval(t.name)
		if err != nil {
			return err
		}
		return throwErr("plugin", Str(n)+": plugin not found")
	default:
		return fmt.Errorf("unknown statement %T", s)
	}
	return nil
}

func (c *Context) evalNames(names []Expr) ([]any, error) {
	var res []any
	for _, n := range names {
		v, err := c.eval(n)
		if err != nil {
			return nil, err
		}
		res = append(res, v)
	}
	return res, nil
}

// evalNamed evaluates named arguments into a hash (nil if there are none)
func (c *Context) evalNamed(named []namedArg) (*Hash, error) {
	if len(named) == 0 {
		return nil, nil
	}
	h := NewHash()
	for _, na := range named {
		if na.set != nil {
			v, err := c.eval(na.set)
			if err != nil {
				return nil, err
			}
			h.Set("", v)
			continue
		}
		k, err := c.eval(na.key)
		if err != nil {
			return nil, err
		}
		v, err := c.eval(na.val)
		if err != nil {
			return nil, err
		}
		h.Set(Str(k), v)
	}
	return h, nil
}

func (c *Context) execThrow(t *stmtThrow, out *strings.Builder) error {
	typ, err := c.eval(t.typ)
	if err != nil {
		return err
	}
	if t.info == nil {
		// $context->throw($type, undef): no info
		return &Exception{Type: "undef", Info: Str(typ), text: nil}
	}
	info, err := c.eval(t.info)
	if err != nil {
		return err
	}
	if len(t.pos) > 0 || len(t.named) > 0 {
		h := NewHash()
		all := []any{info}
		for _, p := range t.pos {
			v, err := c.eval(p)
			if err != nil {
				return err
			}
			all = append(all, v)
		}
		h.Set("args", NewList(all...))
		for i, v := range all {
			h.Set(strconv.Itoa(i), v)
		}
		named, err := c.evalNamed(t.named)
		if err != nil {
			return err
		}
		if named != nil {
			for _, k := range named.Keys() {
				v, _ := named.Get(k)
				h.Set(k, v)
			}
		}
		return &Exception{Type: Str(typ), Info: h, text: out}
	}
	if info == nil {
		return &Exception{Type: "undef", Info: Str(typ)}
	}
	return &Exception{Type: Str(typ), Info: info, text: out}
}

func (c *Context) execSwitch(t *stmtSwitch, out *strings.Builder) error {
	res, err := c.eval(t.e)
	if err != nil {
		return err
	}
	r := Str(res)
	err = func() error {
		for _, cs := range t.cases {
			m, err := c.eval(cs.match)
			if err != nil {
				return err
			}
			var items []any
			if l, ok := m.(*List); ok {
				items = l.Items
			} else {
				items = []any{m}
			}
			for _, it := range items {
				s := Str(it)
				if s == r || s == r+"\n" {
					return c.execBlock(cs.body, out)
				}
			}
		}
		if t.hasDef {
			return c.execBlock(t.def, out)
		}
		return nil
	}()
	if err != nil {
		var lc *loopCtl
		if errors.As(err, &lc) && lc.matches("SWITCH") {
			return nil
		}
	}
	return err
}

func (c *Context) execForeach(t *stmtForeach, out *strings.Builder) error {
	lv, err := c.eval(t.list)
	if err != nil {
		return err
	}
	it, ok := lv.(*Iterator)
	if !ok {
		it = NewIterator(lv)
	}
	value, done := it.getFirst()
	var oldLoop any
	if t.target != "" {
		oldLoop, _ = c.getVar("loop")
	} else {
		c.localise(nil)
	}
	c.stash.vars.Set("loop", it)
	err = func() error {
		for !done {
			if t.target != "" {
				c.stash.vars.Set(t.target, value)
			} else if h, ok := value.(*Hash); ok {
				for _, k := range h.Keys() {
					v, _ := h.Get(k)
					c.stash.vars.Set(k, v)
				}
			}
			if err := c.execBlock(t.body, out); err != nil {
				var lc *loopCtl
				if errors.As(err, &lc) && lc.label == t.label {
					if lc.last {
						return nil
					}
					value, done = it.getNext()
					continue
				}
				return err
			}
			value, done = it.getNext()
		}
		return nil
	}()
	if t.target != "" {
		if _, e := c.setVar("loop", oldLoop); e != nil && err == nil {
			err = e
		}
	} else {
		c.delocalise()
	}
	return err
}

const whileMax = 1000

func (c *Context) execWhile(t *stmtWhile, out *strings.Builder) error {
	failsafe := whileMax
	for {
		v, err := c.eval(t.cond)
		if err != nil {
			return err
		}
		if !True(v) {
			break
		}
		failsafe--
		if failsafe < 0 {
			break
		}
		if err := c.execBlock(t.body, out); err != nil {
			var lc *loopCtl
			if errors.As(err, &lc) && lc.matches(t.label) {
				if lc.last {
					break
				}
				continue
			}
			return err
		}
	}
	if failsafe < 0 {
		return throwErr("while", fmt.Sprintf("WHILE loop terminated (> %d iterations)", whileMax))
	}
	return nil
}

func (c *Context) execWrapper(t *stmtWrapper, out *strings.Builder) error {
	var inner strings.Builder
	if err := c.execBlock(t.body, &inner); err != nil {
		return err
	}
	names, err := c.evalNames(t.names)
	if err != nil {
		return err
	}
	content := inner.String()
	// multiple wrappers are applied innermost (last) first
	for i := len(names) - 1; i >= 0; i-- {
		params, err := c.evalNamed(t.named)
		if err != nil {
			return err
		}
		if params == nil {
			params = NewHash()
		}
		params.Set("content", content)
		r, err := c.include([]any{names[i]}, params)
		if err != nil {
			return err
		}
		content = r
	}
	out.WriteString(content)
	return nil
}

func (c *Context) execFilter(t *stmtFilter, out *strings.Builder) error {
	name, err := c.eval(t.name)
	if err != nil {
		return err
	}
	var args []any
	if t.args != nil {
		args, err = c.evalArgs(t.args)
		if err != nil {
			return err
		}
	}
	var alias string
	if t.alias != nil {
		a, err := c.eval(t.alias)
		if err != nil {
			return err
		}
		alias = Str(a)
	}
	f, err := c.filter(name, args, t.args != nil, alias)
	if err != nil {
		return err
	}
	var inner strings.Builder
	if err := c.execBlock(t.body, &inner); err != nil {
		return err
	}
	r, err := f(inner.String())
	if err != nil {
		return err
	}
	out.WriteString(r)
	return nil
}

func (c *Context) defineMacro(t *stmtMacro) {
	var code Code = func(cc *Context, args []any) ([]any, error) {
		var params *Hash
		if t.hasArgs {
			margs := NewHash()
			n := len(t.args)
			for i, a := range t.args {
				if i < len(args) {
					margs.Set(a, args[i])
				} else {
					margs.Set(a, nil)
				}
			}
			rest := args
			if len(rest) > n {
				rest = rest[n:]
			} else {
				rest = nil
			}
			var p *Hash
			if len(rest) > 0 {
				p, _ = rest[0].(*Hash)
			}
			params = margs
			if p != nil {
				for _, k := range p.Keys() {
					v, _ := p.Get(k)
					params.Set(k, v)
				}
			}
		} else if len(args) > 0 {
			params, _ = args[0].(*Hash)
		}
		var out strings.Builder
		cc.localise(params)
		err := cc.execBlock(t.body, &out)
		cc.delocalise()
		if err != nil {
			return nil, err
		}
		return []any{out.String()}, nil
	}
	c.stash.vars.Set(t.name, code)
}

func (c *Context) execTry(t *stmtTry, out *strings.Builder) error {
	var inner strings.Builder
	err := c.execBlock(t.body, &inner)
	var tterr *Exception
	if err != nil {
		var lc *loopCtl
		if errors.As(err, &lc) {
			out.WriteString(inner.String())
			return err
		}
		tterr = c.catch(err, &inner)
		if tterr.Type == "return" || tterr.Type == "stop" {
			return tterr
		}
		c.stash.vars.Set("error", tterr)
		c.stash.vars.Set("e", tterr)
		var handlers []string
		var deflt block
		hasDefault := false
		for _, cb := range t.catches {
			if cb.typ == "" {
				if !hasDefault {
					deflt, hasDefault = cb.body, true
				}
				continue
			}
			handlers = append(handlers, cb.typ)
		}
		if h, ok := tterr.selectHandler(handlers); ok {
			for _, cb := range t.catches {
				if cb.typ == h {
					if err := c.execBlock(cb.body, &inner); err != nil {
						return err
					}
					break
				}
			}
			tterr = nil
		} else if hasDefault {
			if err := c.execBlock(deflt, &inner); err != nil {
				return err
			}
			tterr = nil
		}
	}
	if t.hasFinal {
		if err := c.execBlock(t.final, &inner); err != nil {
			return err
		}
	}
	if tterr != nil {
		return tterr
	}
	out.WriteString(inner.String())
	return nil
}

// ----------------------------------------------------------------------
// Variables: get, set, dotop
// ----------------------------------------------------------------------

func (c *Context) getVar(name string) (any, error) {
	return c.stashGet([]any{name}, [][]any{nil})
}

func (c *Context) setVar(name string, v any) (any, error) {
	return c.stashSet([]any{name}, [][]any{nil}, v, false)
}

func splitDotted(s string) []any {
	var res []any
	for _, p := range strings.Split(s, ".") {
		if i := strings.IndexByte(p, '('); i >= 0 {
			p = p[:i]
		}
		res = append(res, p)
	}
	return res
}

// stashGet is Template::Stash::get with an evaluated identifier
func (c *Context) stashGet(names []any, args [][]any) (any, error) {
	var root any = c.stash
	var result any
	for i := range names {
		r, err := c.dotop(root, names[i], args[i], false)
		if err != nil {
			return nil, err
		}
		result = r
		if r == nil {
			break
		}
		root = r
	}
	if result == nil {
		return "", nil
	}
	return result, nil
}

func (c *Context) stashSet(names []any, args [][]any, value any, def bool) (any, error) {
	var root any = c.stash
	n := len(names)
	for i := 0; i < n-1; i++ {
		r, err := c.dotop(root, names[i], args[i], true)
		if err != nil {
			return nil, err
		}
		if r == nil {
			return "", nil
		}
		root = r
	}
	r, err := c.assignItem(root, names[n-1], args[n-1], value, def)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return "", nil
	}
	return r, nil
}

func isPrivate(s string) bool { return strings.HasPrefix(s, "_") || strings.HasPrefix(s, ".") }

var intItemRe = regexp.MustCompile(`^-?\d+$`)

func (c *Context) assignItem(root any, item any, args []any, value any, def bool) (any, error) {
	if !True(root) || item == nil {
		return nil, nil
	}
	key := Str(item)
	if isPrivate(key) {
		return nil, nil
	}
	switch r := root.(type) {
	case *Stash:
		if def {
			if cur, ok := r.vars.Get(key); ok && True(cur) {
				return nil, nil
			}
		}
		r.vars.Set(key, value)
		return value, nil
	case *Hash:
		if def {
			if cur, ok := r.Get(key); ok && True(cur) {
				return nil, nil
			}
		}
		r.Set(key, value)
		return value, nil
	case *List:
		if intItemRe.MatchString(key) {
			idx, _ := strconv.Atoi(key)
			if idx < 0 {
				idx += len(r.Items)
				if idx < 0 {
					return nil, fmt.Errorf("Modification of non-creatable array value attempted, subscript %s", key)
				}
			}
			for len(r.Items) <= idx {
				r.Items = append(r.Items, nil)
			}
			r.Items[idx] = value
			return value, nil
		}
	case Object:
		if def {
			res, ok, err := r.TTMethod(key, nil)
			if err != nil {
				return nil, err
			}
			if ok && len(res) > 0 && True(res[0]) {
				return nil, nil
			}
		}
		res, ok, err := r.TTMethod(key, append(append([]any{}, args...), value))
		if err != nil {
			return nil, err
		}
		if !ok {
			// No such method: as Template::Stash::XS, a hash-based object
			// has the key set
			if hl, isHash := r.(HashLike); isHash {
				hl.TTHash().Set(key, value)
				return value, nil
			}
			return nil, nil
		}
		if len(res) > 0 {
			return res[0], nil
		}
		return nil, nil
	}
	return nil, fmt.Errorf("don't know how to assign to [%s].[%s]\n", Str(root), key)
}

// dotop is Template::Stash::_dotop
func (c *Context) dotop(root any, item any, args []any, lvalue bool) (any, error) {
	if root == nil || item == nil {
		return nil, nil
	}
	key := Str(item)
	if isPrivate(key) {
		return nil, nil
	}
	var result []any
	var err error
	switch r := root.(type) {
	case *Stash:
		if v, ok := r.vars.Get(key); ok && v != nil {
			if code, ok := v.(Code); ok {
				result, err = code(c, args)
				break
			}
			return v, nil
		} else if lvalue {
			h := NewHash()
			r.vars.Set(key, h)
			return h, nil
		} else if key == "import" {
			result, err = hashOps["import"](c, r.vars, args)
		}
	case *Hash:
		if v, ok := r.Get(key); ok && v != nil {
			if code, ok := v.(Code); ok {
				result, err = code(c, args)
				break
			}
			return v, nil
		} else if lvalue {
			h := NewHash()
			r.Set(key, h)
			return h, nil
		} else if op, ok := hashOps[key]; ok {
			result, err = op(c, r, args)
		}
	case *List:
		if op, ok := listOps[key]; ok {
			result, err = op(c, r, args)
		} else if intItemRe.MatchString(key) {
			idx, _ := strconv.Atoi(key)
			if idx < 0 {
				idx += len(r.Items)
			}
			if idx < 0 || idx >= len(r.Items) {
				return nil, nil
			}
			v := r.Items[idx]
			if code, ok := v.(Code); ok {
				result, err = code(c, args)
				break
			}
			return v, nil
		}
	case *Iterator:
		result, err = r.method(key)
	case Object:
		var ok bool
		result, ok, err = r.TTMethod(key, args)
		if err != nil {
			return nil, err
		}
		if !ok {
			result, err = c.objectFallback(root, key, args, lvalue)
		}
	default:
		if op, ok := scalarOps[key]; ok && !lvalue {
			result, err = op(c, root, args)
		} else if op, ok := listOps[key]; ok && !lvalue {
			result, err = op(c, NewList(root), args)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(result) > 0 && result[0] != nil {
		if len(result) > 1 {
			return NewList(result...), nil
		}
		return result[0], nil
	}
	if len(result) > 1 && result[1] != nil {
		if e, ok := result[1].(error); ok {
			return nil, e
		}
		return nil, errors.New(Str(result[1]))
	}
	return nil, nil
}

func (c *Context) objectFallback(root any, key string, args []any, lvalue bool) ([]any, error) {
	if hl, ok := root.(HashLike); ok {
		h := hl.TTHash()
		if op, ok := hashOps[key]; ok {
			return op(c, h, args)
		}
		if v, ok := h.Get(key); ok && v != nil {
			r, err := c.dotop(h, key, args, lvalue)
			return []any{r}, err
		}
		return nil, nil
	}
	if ll, ok := root.(ListLike); ok {
		l := ll.TTList()
		if op, ok := listOps[key]; ok {
			return op(c, l, args)
		}
		if intItemRe.MatchString(key) {
			r, err := c.dotop(l, key, args, lvalue)
			return []any{r}, err
		}
		return nil, nil
	}
	if op, ok := scalarOps[key]; ok {
		return op(c, root, args)
	}
	if op, ok := listOps[key]; ok {
		return op(c, NewList(root), args)
	}
	return nil, nil
}

// ----------------------------------------------------------------------
// Expression evaluation
// ----------------------------------------------------------------------

func (c *Context) evalArgs(a *argList) ([]any, error) {
	if a == nil {
		return nil, nil
	}
	var res []any
	for _, p := range a.pos {
		v, err := c.eval(p)
		if err != nil {
			return nil, err
		}
		res = append(res, v)
	}
	if len(a.named) > 0 {
		h, err := c.evalNamed(a.named)
		if err != nil {
			return nil, err
		}
		res = append(res, h)
	}
	return res, nil
}

// evalIdent evaluates the names and arguments of an identifier
func (c *Context) evalIdent(parts []identPart) ([]any, [][]any, error) {
	if len(parts) == 1 && parts[0].args == nil {
		n, err := c.eval(parts[0].name)
		if err != nil {
			return nil, nil, err
		}
		if s, ok := n.(string); ok && strings.Contains(s, ".") {
			names := splitDotted(s)
			return names, make([][]any, len(names)), nil
		}
		return []any{n}, [][]any{nil}, nil
	}
	names := make([]any, len(parts))
	args := make([][]any, len(parts))
	for i, p := range parts {
		n, err := c.eval(p.name)
		if err != nil {
			return nil, nil, err
		}
		names[i] = n
		if p.args != nil {
			a, err := c.evalArgs(p.args)
			if err != nil {
				return nil, nil, err
			}
			args[i] = a
		}
	}
	return names, args, nil
}

func (c *Context) assign(t assignTarget, v any, def bool) (any, error) {
	if t.ident != nil {
		names, args, err := c.evalIdent(t.ident.parts)
		if err != nil {
			return nil, err
		}
		return c.stashSet(names, args, v, def)
	}
	n, err := c.eval(t.literal)
	if err != nil {
		return nil, err
	}
	if s, ok := n.(string); ok && strings.Contains(s, ".") {
		names := splitDotted(s)
		return c.stashSet(names, make([][]any, len(names)), v, def)
	}
	return c.stashSet([]any{n}, [][]any{nil}, v, def)
}

func (c *Context) eval(e Expr) (any, error) {
	switch t := e.(type) {
	case nil:
		return nil, nil
	case *exprLiteral:
		return t.v, nil
	case *exprIdent:
		if t.ref {
			parts := t.parts
			var code Code = func(cc *Context, args []any) ([]any, error) {
				names, a, err := cc.evalIdent(parts)
				if err != nil {
					return nil, err
				}
				if len(args) > 0 {
					a[len(a)-1] = args
				}
				v, err := cc.stashGet(names, a)
				return []any{v}, err
			}
			return code, nil
		}
		names, args, err := c.evalIdent(t.parts)
		if err != nil {
			return nil, err
		}
		return c.stashGet(names, args)
	case *exprQuoted:
		var b strings.Builder
		for _, p := range t.parts {
			v, err := c.eval(p)
			if err != nil {
				return nil, err
			}
			b.WriteString(Str(v))
		}
		return b.String(), nil
	case *exprList:
		l := &List{}
		for _, it := range t.items {
			if r, ok := it.(*exprRange); ok {
				items, err := c.evalRange(r)
				if err != nil {
					return nil, err
				}
				l.Items = append(l.Items, items...)
				continue
			}
			v, err := c.eval(it)
			if err != nil {
				return nil, err
			}
			l.Items = append(l.Items, v)
		}
		return l, nil
	case *exprHash:
		h := NewHash()
		for _, p := range t.pairs {
			k, err := c.eval(p.key)
			if err != nil {
				return nil, err
			}
			v, err := c.eval(p.val)
			if err != nil {
				return nil, err
			}
			h.Set(Str(k), v)
		}
		return h, nil
	case *exprAssign:
		v, err := c.eval(t.val)
		if err != nil {
			return nil, err
		}
		return c.assign(t.target, v, t.def)
	case *exprBinary:
		l, err := c.eval(t.l)
		if err != nil {
			return nil, err
		}
		switch t.op {
		case "&&":
			if !True(l) {
				return l, nil
			}
			return c.eval(t.r)
		case "||":
			if True(l) {
				return l, nil
			}
			return c.eval(t.r)
		}
		r, err := c.eval(t.r)
		if err != nil {
			return nil, err
		}
		if t.op == "." {
			return Str(l) + Str(r), nil
		}
		return arith(t.op, l, r)
	case *exprChain:
		l, err := c.eval(t.terms[0])
		if err != nil {
			return nil, err
		}
		var res any
		for i, op := range t.ops {
			r, err := c.eval(t.terms[i+1])
			if err != nil {
				return nil, err
			}
			var ok bool
			switch op {
			case "eq":
				ok = Str(l) == Str(r)
			case "ne":
				ok = Str(l) != Str(r)
			case "<":
				ok = numCompare(l, r) < 0
			case ">":
				ok = numCompare(l, r) > 0
			case "<=":
				ok = numCompare(l, r) <= 0
			case ">=":
				ok = numCompare(l, r) >= 0
			}
			res = Bool(ok)
			if !ok {
				return res, nil
			}
			l = r
		}
		return res, nil
	case *exprNot:
		v, err := c.eval(t.e)
		if err != nil {
			return nil, err
		}
		return Bool(!True(v)), nil
	case *exprTernary:
		v, err := c.eval(t.c)
		if err != nil {
			return nil, err
		}
		if True(v) {
			return c.eval(t.t)
		}
		return c.eval(t.f)
	case *exprInt:
		v, err := c.eval(t.e)
		if err != nil {
			return nil, err
		}
		return perlInt(v), nil
	case *exprParen:
		return c.eval(t.e)
	case *exprRange:
		items, err := c.evalRange(t)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, nil
		}
		return items[len(items)-1], nil
	}
	return nil, fmt.Errorf("unknown expression %T", e)
}

func (c *Context) evalRange(r *exprRange) ([]any, error) {
	a, err := c.eval(r.from)
	if err != nil {
		return nil, err
	}
	b, err := c.eval(r.to)
	if err != nil {
		return nil, err
	}
	return perlRange(a, b), nil
}

var magicIncRe = regexp.MustCompile(`^[a-zA-Z]*[0-9]*$`)

// perlRange implements the list-context range operator
func perlRange(a, b any) []any {
	as, aIsStr := a.(string)
	bs, _ := b.(string)
	if aIsStr && as != "" && !looksNum(as) && magicIncRe.MatchString(as) {
		if _, bstr := b.(string); bstr || b == nil {
			var res []any
			cur := as
			for {
				res = append(res, cur)
				if cur == bs || len(cur) > len(bs) || len(res) > 1000000 {
					break
				}
				cur = magicIncrement(cur)
				if len(cur) > len(bs) {
					break
				}
			}
			return res
		}
	}
	x := perlInt(a)
	y := perlInt(b)
	xi, _ := x.(int64)
	yi, _ := y.(int64)
	var res []any
	for i := xi; i <= yi; i++ {
		res = append(res, i)
	}
	return res
}

func looksNum(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

func magicIncrement(s string) string {
	b := []byte(s)
	i := len(b) - 1
	for i >= 0 {
		switch {
		case b[i] == 'z':
			b[i] = 'a'
		case b[i] == 'Z':
			b[i] = 'A'
		case b[i] == '9':
			b[i] = '0'
		default:
			b[i]++
			return string(b)
		}
		i--
	}
	first := s[0]
	var p byte = '1'
	if first >= 'a' && first <= 'z' {
		p = 'a'
	} else if first >= 'A' && first <= 'Z' {
		p = 'A'
	}
	return string(p) + string(b)
}

func argN(args []any, n int) any {
	if n < len(args) {
		return args[n]
	}
	return nil
}

func sortedKeys(h *Hash) []string {
	k := h.Keys()
	sort.Strings(k)
	return k
}
