package web

import (
	"regexp"
	"strings"
)

// Handler handles a matched route. Control flow that Dancer2 implements
// with exceptions (redirect, halt, send_as, Log::Report errors) is
// implemented with panics recovered by the dispatcher.
type Handler func(c *Ctx) string

// route is a Dancer2::Core::Route
type route struct {
	methods map[string]bool
	re      *regexp.Regexp
	params  []string
	capture bool // regexp route: captures are the splat
	handler Handler
	spec    string
}

var (
	tokenRe = regexp.MustCompile(`:([^/.?]+)`)
)

// buildRoute compiles a route path as Dancer2's _build_regexp_from_string
func buildRoute(spec string) *route {
	r := &route{spec: spec}
	s := spec
	if strings.Contains(s, ":") {
		for _, m := range tokenRe.FindAllStringSubmatch(s, -1) {
			r.params = append(r.params, m[1])
		}
		s = tokenRe.ReplaceAllString(s, "\x00")
	}
	s = strings.ReplaceAll(s, "**", "\x01")
	s = strings.ReplaceAll(s, "*", "\x02")
	s = strings.ReplaceAll(s, ".", `\.`)
	s = strings.ReplaceAll(s, "\x00", `([^/]+)`)
	s = strings.ReplaceAll(s, "\x01", `([^\n]+)`)
	s = strings.ReplaceAll(s, "\x02", `([^/]+)`)
	r.re = regexp.MustCompile("^" + s + "$")
	return r
}

// Router holds routes in the order registered (the first match wins).
type Router struct {
	routes []*route
	prefix string
}

func methodSet(methods []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range methods {
		x = strings.ToUpper(x)
		m[x] = true
		if x == "GET" {
			m["HEAD"] = true
		}
	}
	return m
}

// Add registers a route for the given methods ("get", "post", "put", "del")
func (rt *Router) Add(methods []string, path string, h Handler) {
	if rt.prefix == "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	r := buildRoute(rt.prefix + path)
	for i, m := range methods {
		if m == "del" {
			methods[i] = "delete"
		}
	}
	r.methods = methodSet(methods)
	r.handler = h
	rt.routes = append(rt.routes, r)
}

// AddRegexp registers a regexp route. As in Dancer2, a regexp without a
// prefix is not anchored.
func (rt *Router) AddRegexp(methods []string, re string, h Handler) {
	if rt.prefix != "" {
		re = "^" + regexp.QuoteMeta(rt.prefix) + "(?:" + re + ")$"
	}
	r := &route{re: regexp.MustCompile(re), capture: true, handler: h, spec: re}
	r.methods = methodSet(methods)
	rt.routes = append(rt.routes, r)
}

// Prefix runs f with routes prefixed (Dancer2 prefix)
func (rt *Router) Prefix(p string, f func()) {
	old := rt.prefix
	rt.prefix = old + p
	f()
	rt.prefix = old
}

func (rt *Router) Get(path string, h Handler)  { rt.Add([]string{"get"}, path, h) }
func (rt *Router) Post(path string, h Handler) { rt.Add([]string{"post"}, path, h) }
func (rt *Router) Put(path string, h Handler)  { rt.Add([]string{"put"}, path, h) }
func (rt *Router) Del(path string, h Handler)  { rt.Add([]string{"delete"}, path, h) }
func (rt *Router) Any(path string, h Handler)  { rt.Add([]string{"get", "post"}, path, h) }

// match finds the routes matching a request, in order, with their params.
type routeMatch struct {
	r      *route
	params map[string]string
	set    map[string]bool // params with a defined value
	splat  []string
}

func (rt *Router) match(method, path string) []routeMatch {
	var res []routeMatch
	for _, r := range rt.routes {
		if !r.methods[method] {
			continue
		}
		m := r.re.FindStringSubmatchIndex(path)
		if m == nil {
			continue
		}
		rm := routeMatch{r: r, params: map[string]string{}, set: map[string]bool{}}
		for i := 1; i*2 < len(m); i++ {
			var v string
			def := m[i*2] >= 0
			if def {
				v = path[m[i*2]:m[i*2+1]]
			}
			if r.capture {
				rm.splat = append(rm.splat, v)
			} else if i-1 < len(r.params) {
				rm.params[r.params[i-1]] = v
				rm.set[r.params[i-1]] = def
			}
		}
		res = append(res, rm)
	}
	return res
}
