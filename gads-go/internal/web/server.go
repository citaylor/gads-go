// Package web is a Go re-implementation of the GADS Dancer2 web
// application (lib/GADS.pm and lib/GADS/API.pm). Pages are rendered from
// the original Template Toolkit views with the tt package, so that the
// HTML produced is identical to that of the Perl application, as verified
// by the conformance suite.
package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// Config is the web application configuration (the Dancer2 config file).
type Config struct {
	// Gads is the "gads" section of the configuration
	Gads map[string]any
	// ShowErrors shows the text of unexpected errors to users (show_errors)
	ShowErrors bool
	// PasswordExpiryDays is the Auth::Extensible password_expiry_days
	PasswordExpiryDays int
	// BehindProxy uses X-Forwarded headers for the request base
	BehindProxy bool
	// Session is the cookie of the session engine
	Session SessionConfig
	// MaxBodyBytes limits the size of a request body (0 is
	// DefaultMaxBodyBytes). Larger requests are refused with 413.
	MaxBodyBytes int64
}

// DefaultMaxBodyBytes is the default limit of a request body: room for the
// largest upload GADS accepts (50 MB) with its encoding
const DefaultMaxBodyBytes = 100 << 20

// SessionConfig is the configuration of the Dancer2 session engine
// (engines: session: ENGINE:) that applies to its cookie.
type SessionConfig struct {
	CookieName   string // cookie_name
	CookiePath   string // cookie_path
	CookieDomain string // cookie_domain
	// CookieDuration (cookie_duration) gives the cookie an expiry; 0 makes
	// it a browser-session cookie
	CookieDuration time.Duration
	// IdleTimeout drops sessions unused for that long (session_duration,
	// otherwise cookie_duration, otherwise DefaultSessionIdle); 0 keeps
	// them for the life of the process
	IdleTimeout time.Duration
	Secure      bool          // is_secure
	HTTPOnly    bool          // is_http_only
	SameSite    http.SameSite // cookie_same_site
}

// DefaultConfig returns the configuration of config.yml-example.
func DefaultConfig() *Config {
	return &Config{
		Gads: map[string]any{
			"dateformat":     "yyyy-MM-dd",
			"login_instance": int64(1),
			"url":            "http://localhost/",
		},
		PasswordExpiryDays: 60,
		Session: SessionConfig{CookieName: "dancer.session", CookiePath: "/", HTTPOnly: true,
			IdleTimeout: DefaultSessionIdle},
	}
}

// DefaultSessionIdle is how long an unused session is kept without a
// session_duration. Deviation: Dancer2::Session::Simple keeps sessions for
// the life of the process, so that anyone could fill its memory.
const DefaultSessionIdle = 7 * 24 * time.Hour

// Server is the GADS web application.
type Server struct {
	E      *gads.Engine
	Config *Config
	Views  string
	Public string
	TT     *tt.Engine
	// ttDefault renders templates as a Template object with default options
	ttDefault *tt.Engine

	router   *Router
	sessions *sessionStore
	// GADS (as Perl Dancer2 with a single worker) processes one request at
	// a time: the engine is not safe for concurrent use
	mu sync.Mutex
}

// New creates the web application. views is the directory of the
// original GADS views (Template Toolkit files) and public that of the
// static files.
func New(e *gads.Engine, cfg *Config, views, public string) *Server {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	s := &Server{
		E:        e,
		Config:   cfg,
		Views:    views,
		Public:   public,
		router:   &Router{},
		sessions: newSessionStore(cfg.Session.IdleTimeout),
	}
	s.TT = tt.NewEngine(views)
	s.TT.Filters = map[string]func(string) string{}
	s.routes()
	return s
}

// ---------------------------------------------------------------------
// Request context
// ---------------------------------------------------------------------

// Ctx is the context of one request (the Dancer2 DSL in a route).
type Ctx struct {
	// tokenMessages is the messages token of the template being prepared
	tokenMessages *tt.List
	// colObjs are the template objects of the columns of the request
	colObjs map[*gads.Column]*ttColumnObj
	s       *Server
	E       *gads.Engine
	Req     *http.Request

	query   *multiValue
	body    *multiValue
	route   routeMatch
	uploads map[string][]*Upload
	rawBody []byte

	vars    map[string]any
	session *Session
	// session destroyed in this request
	destroyed *Session

	status  int
	headers http.Header
	cookies []*http.Cookie
	ctype   string
	content string
	file    []byte

	user       *gads.User
	userLoaded bool
}

// Upload is an uploaded file
type Upload struct {
	Filename string
	Type     string
	Content  []byte
}

// multiValue is Hash::MultiValue: ordered keys with multiple values
type multiValue struct {
	keys []string
	vals map[string][]string
}

func newMultiValue() *multiValue { return &multiValue{vals: map[string][]string{}} }

func (m *multiValue) add(k, v string) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = append(m.vals[k], v)
}

// get returns the last value (Hash::MultiValue->get)
func (m *multiValue) get(k string) (string, bool) {
	v := m.vals[k]
	if len(v) == 0 {
		return "", false
	}
	return v[len(v)-1], true
}

func (m *multiValue) getAll(k string) []string { return m.vals[k] }

func parseQuery(q string) *multiValue {
	m := newMultiValue()
	if q == "" {
		return m
	}
	for _, part := range strings.FieldsFunc(q, func(r rune) bool { return r == '&' || r == ';' }) {
		k, v, _ := strings.Cut(part, "=")
		k = unescapeForm(k)
		v = unescapeForm(v)
		m.add(k, v)
	}
	return m
}

func unescapeForm(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// flow control (Dancer2 halt etc)
type haltSignal struct{}

// passSignal is Dancer2 pass: try the next matching route
type passSignal struct{}

// Pass passes the request to the next matching route
func (c *Ctx) Pass() { panic(passSignal{}) }

// QueryParam is query_parameters->get
func (c *Ctx) QueryParam(k string) (string, bool) { return c.query.get(k) }

// BodyParam is body_parameters->get
func (c *Ctx) BodyParam(k string) (string, bool) { return c.body.get(k) }

// RouteParam is route_parameters->get
func (c *Ctx) RouteParam(k string) (string, bool) {
	v, ok := c.route.params[k]
	return v, ok && c.route.set[k]
}

// Param is Dancer2 param: route parameters, then body, then query
func (c *Ctx) Param(k string) (string, bool) {
	if v, ok := c.RouteParam(k); ok {
		return v, true
	}
	if v, ok := c.body.get(k); ok {
		return v, true
	}
	return c.query.get(k)
}

// ParamS returns a parameter or an empty string
func (c *Ctx) ParamS(k string) string {
	v, _ := c.Param(k)
	return v
}

// ParamAll returns all values of a parameter (body then query)
func (c *Ctx) ParamAll(k string) []string {
	if v, ok := c.RouteParam(k); ok {
		return []string{v}
	}
	if v := c.body.getAll(k); len(v) > 0 {
		return v
	}
	return c.query.getAll(k)
}

// Var is Dancer2 var
func (c *Ctx) Var(k string) any { return c.vars[k] }

// SetVar sets a Dancer2 var
func (c *Ctx) SetVar(k string, v any) { c.vars[k] = v }

// Path is request->path
func (c *Ctx) Path() string { return c.Req.URL.Path }

// URI is request->uri (path and query, as sent)
func (c *Ctx) URI() string { return c.Req.URL.RequestURI() }

// QueryString is request->query_string
func (c *Ctx) QueryString() string { return c.Req.URL.RawQuery }

// Method is request->method
func (c *Ctx) Method() string { return c.Req.Method }

// IsPost is request->is_post
func (c *Ctx) IsPost() bool { return c.Req.Method == "POST" }

// Host is request->base->host
func (c *Ctx) Host() string {
	h := c.Req.Host
	if c.s.Config.BehindProxy {
		if f := c.Req.Header.Get("X-Forwarded-Host"); f != "" {
			h = f
		}
	}
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return strings.ToLower(h)
}

// Scheme is request->scheme
func (c *Ctx) Scheme() string {
	if c.s.Config.BehindProxy {
		if p := c.Req.Header.Get("X-Forwarded-Proto"); p != "" {
			return p
		}
	}
	if c.Req.TLS != nil {
		return "https"
	}
	return "http"
}

// hostPort is the host (with any port) of the request
func (c *Ctx) hostPort() string {
	h := c.Req.Host
	if c.s.Config.BehindProxy {
		if f := c.Req.Header.Get("X-Forwarded-Host"); f != "" {
			h = f
		}
	}
	if h == "" {
		h = "localhost"
	}
	return h
}

// Base is request->base ("http://host/")
func (c *Ctx) Base() string { return c.Scheme() + "://" + c.hostPort() + "/" }

// URIFor is request->uri_for(path, params)
func (c *Ctx) URIFor(path string, params map[string][]string) string {
	u := strings.TrimSuffix(c.Base(), "/") + "/" + strings.TrimPrefix(path, "/")
	if len(params) > 0 {
		u += "?" + queryForm(params)
	}
	return u
}

// queryForm encodes parameters as URI->query_form with sorted keys (as a
// Perl hash is passed to uri_for, the key order is that of the hash: one
// key in all uses here)
func queryForm(params map[string][]string) string {
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		for _, v := range params[k] {
			parts = append(parts, uriEscapeForm(k)+"="+uriEscapeForm(v))
		}
	}
	return strings.Join(parts, "&")
}

// uriEscapeForm escapes as URI's query_form (spaces as +)
func uriEscapeForm(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == ' ':
			b.WriteByte('+')
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			strings.IndexByte("-_.!~*'()", ch) >= 0:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// Header sets a response header (response_header)
func (c *Ctx) Header(k, v string) { c.headers.Set(k, v) }

// Status sets the response status
func (c *Ctx) Status(n int) { c.status = n }

// ContentType sets the content type
func (c *Ctx) ContentType(t string) { c.ctype = t }

// Halt stops processing with the current response
func (c *Ctx) Halt(content string) {
	c.content = content
	panic(haltSignal{})
}

// Redirect is Dancer2 redirect (relative paths remain relative)
func (c *Ctx) Redirect(dest string) {
	c.status = http.StatusFound
	c.headers.Set("Location", dest)
	c.content = ""
	panic(haltSignal{})
}

// SendFile sends content as a file download (send_file with a scalar ref)
func (c *Ctx) SendFile(content []byte, ctype, filename string) string {
	c.ctype = ctype
	if filename != "" {
		c.headers.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	}
	c.file = content
	panic(haltSignal{})
}

// SetCookie adds a cookie to the response
func (c *Ctx) SetCookie(ck *http.Cookie) { c.cookies = append(c.cookies, ck) }

// Cookie returns a request cookie value
func (c *Ctx) Cookie(name string) (string, bool) {
	ck, err := c.Req.Cookie(name)
	if err != nil {
		return "", false
	}
	v, err := url.QueryUnescape(ck.Value)
	if err != nil {
		v = ck.Value
	}
	return v, true
}

// sessionCookie is the cookie of a session, or of a destroyed session
// (expired)
func (c *Ctx) sessionCookie(id string, destroyed bool) *http.Cookie {
	cfg := c.s.Config.Session
	ck := &http.Cookie{Name: cfg.CookieName, Value: id, Path: cfg.CookiePath, Domain: cfg.CookieDomain,
		Secure: cfg.Secure, HttpOnly: cfg.HTTPOnly, SameSite: cfg.SameSite}
	if destroyed {
		ck.Expires = c.E.Now().Add(-24 * time.Hour)
	} else if cfg.CookieDuration > 0 {
		ck.Expires = c.E.Now().Add(cfg.CookieDuration)
	}
	return ck
}

// Session returns the session, creating it if needed (Dancer2 session)
func (c *Ctx) Session() *Session {
	if c.session != nil {
		return c.session
	}
	if c.destroyed == nil {
		if id, ok := c.Cookie(c.s.Config.Session.CookieName); ok {
			if s := c.s.sessions.get(id); s != nil {
				c.session = s
				return s
			}
		}
	}
	c.session = &Session{ID: newSessionID(), Data: map[string]any{}}
	return c.session
}

// SessionGet reads a session value
func (c *Ctx) SessionGet(k string) any { return c.Session().Data[k] }

// SessionSet writes a session value
func (c *Ctx) SessionSet(k string, v any) {
	s := c.Session()
	s.Data[k] = v
	// Session::Simple flushes dirty sessions to its store
	c.s.sessions.put(s)
}

// SessionDelete removes a session value (session->delete)
func (c *Ctx) SessionDelete(k string) {
	s := c.Session()
	delete(s.Data, k)
	c.s.sessions.put(s)
}

// DestroySession is app->destroy_session
func (c *Ctx) DestroySession() {
	s := c.Session()
	c.s.sessions.remove(s.ID)
	c.destroyed = s
	c.session = nil
	c.user, c.userLoaded = nil, false
	delete(c.vars, "logged_in_user_hash")
}

// ChangeSessionID is app->change_session_id
func (c *Ctx) ChangeSessionID() {
	s := c.Session()
	c.s.sessions.remove(s.ID)
	s.ID = newSessionID()
	c.s.sessions.put(s)
}

// ---------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------

// ServeHTTP handles a request
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Only the dispatch holds the engine lock: static files, the request
	// body and the response are transferred without it, so that a slow
	// client does not hold up others
	if s.serveStatic(w, req) {
		return
	}
	c := &Ctx{
		s:       s,
		E:       s.E,
		Req:     req,
		vars:    map[string]any{},
		headers: http.Header{},
		status:  200,
		uploads: map[string][]*Upload{},
	}
	c.query = parseQuery(req.URL.RawQuery)
	c.body = newMultiValue()
	if !c.readBody(w) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return
	}
	res := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
	s.mu.Lock()
	func() {
		defer s.mu.Unlock()
		siteID := s.E.SiteID
		// Whatever a failed request left behind does not reach the next
		defer s.resetEngine(siteID)
		s.resetEngine(0)
		c.dispatch()
		c.flushReports()
		c.write(res)
	}()
	res.sendTo(w)
}

// resetEngine returns the engine to its state between requests: the
// permission overrides off, no transaction open, no reports
func (s *Server) resetEngine(siteID int64) {
	e := s.E
	e.SiteID = siteID
	e.Reports = nil
	e.IgnorePermissions = false
	e.IgnorePermissionsSearch = false
	e.DB.RollbackAll()
}

// bufferedResponse holds a response until the engine lock is released
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *bufferedResponse) Header() http.Header         { return r.header }
func (r *bufferedResponse) WriteHeader(status int)      { r.status = status }
func (r *bufferedResponse) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *bufferedResponse) sendTo(w http.ResponseWriter) {
	h := w.Header()
	for k, v := range r.header {
		h[k] = v
	}
	w.WriteHeader(r.status)
	w.Write(r.body.Bytes())
}

// serveStatic serves files of the public directory (Dancer2 serves these
// before routes)
func (s *Server) serveStatic(w http.ResponseWriter, req *http.Request) bool {
	if s.Public == "" || (req.Method != "GET" && req.Method != "HEAD") {
		return false
	}
	p := filepath.Clean("/" + req.URL.Path)
	if p == "/" {
		return false
	}
	full := filepath.Join(s.Public, p)
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		return false
	}
	http.ServeFile(w, req, full)
	return true
}

// readBody reads the request body (parameters and uploads). It returns
// false if the body is over the size limit.
func (c *Ctx) readBody(w http.ResponseWriter) bool {
	if c.Req.Body == nil {
		return true
	}
	limit := c.s.Config.MaxBodyBytes
	if limit <= 0 {
		limit = DefaultMaxBodyBytes
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, c.Req.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return false
	}
	c.rawBody = b
	ct, params, _ := mime.ParseMediaType(c.Req.Header.Get("Content-Type"))
	switch ct {
	case "application/x-www-form-urlencoded":
		c.body = parseQuery(string(b))
	case "multipart/form-data":
		mr := multipart.NewReader(bytes.NewReader(b), params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(p)
			if p.FileName() != "" {
				c.uploads[p.FormName()] = append(c.uploads[p.FormName()], &Upload{
					Filename: p.FileName(),
					Type:     p.Header.Get("Content-Type"),
					Content:  data,
				})
				continue
			}
			c.body.add(p.FormName(), string(data))
		}
	}
	return true
}

// RequestContentType is request->content_type
func (c *Ctx) RequestContentType() string {
	ct, _, _ := mime.ParseMediaType(c.Req.Header.Get("Content-Type"))
	return ct
}

// Body is request->body
func (c *Ctx) Body() []byte { return c.rawBody }

// RequestHeader is request->header: the value of a request header, and
// whether it was sent
func (c *Ctx) RequestHeader(k string) (string, bool) {
	v, ok := c.Req.Header[http.CanonicalHeaderKey(k)]
	if !ok || len(v) == 0 {
		return "", false
	}
	return v[0], true
}

func (c *Ctx) dispatch() {
	method := c.Req.Method
	matches := c.s.router.match(method, c.Path())
	if len(matches) == 0 {
		c.notFound()
		return
	}
	for _, m := range matches {
		c.route = m
		passed := false
		c.run(func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(passSignal); ok {
						passed = true
						return
					}
					panic(r)
				}
			}()
			// As Dancer2, the before hooks run for each route tried, those
			// of API.pm and MFA.pm (loaded by GADS.pm) first
			c.s.apiBeforeHook(c)
			c.s.mfaBeforeHook(c)
			c.s.beforeHook(c)
			c.content = c.route.r.handler(c)
		})
		if !passed {
			return
		}
	}
	// No route accepted the request: a new 404 response
	c.route = routeMatch{}
	c.headers = http.Header{}
	c.notFound()
}

// run runs f, handling the flow control and errors of a route as Dancer2
// with Dancer2::Plugin::LogReport
func (c *Ctx) run(f func()) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if _, ok := r.(haltSignal); ok {
			return
		}
		c.handleError(r)
	}()
	f()
}

// handleError handles a fatal error in a route: the message is added to the
// session and the user is forwarded home (Dancer2::Plugin::LogReport)
func (c *Ctx) handleError(r any) {
	var msg *Message
	switch x := r.(type) {
	case *gads.Error:
		if x.Level == gads.LevelError {
			msg = &Message{Reason: "ERROR", Text: x.Msg}
		} else {
			msg = &Message{Reason: "PANIC", Text: x.Msg}
		}
	case *db.Error:
		msg = &Message{Reason: "PANIC", Text: x.Error()}
	default:
		if os.Getenv("GADS_STACK") != "" {
			fmt.Fprintf(os.Stderr, "%v\n%s\n", r, debug.Stack())
		}
		msg = &Message{Reason: "PANIC", Text: fmt.Sprint(r)}
	}
	if os.Getenv("GADS_WEB_ERRORS") != "" {
		fmt.Fprintf(os.Stderr, "web error (%s): %s\n", msg.Reason, msg.Text)
	}
	if apiURIRe.MatchString(c.URI()) {
		c.apiError(msg)
		return
	}
	c.forwardHomeMessage(msg)
}

// forwardHomeMessage is LogReport's _forward_home
func (c *Ctx) forwardHomeMessage(msg *Message) {
	msg = c.addMessage(msg)
	c.file = nil
	if c.URI() == "/" && c.Req.Method == "GET" {
		c.status = 200
		c.ctype = "text/plain"
		c.content = msg.Text
		return
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(haltSignal); !ok {
					panic(r)
				}
			}
		}()
		c.Redirect("/")
	}()
}

// notFound renders the 404 page (Dancer2::Core::Error)
func (c *Ctx) notFound() {
	c.status = 404
	c.run(func() {
		tokens := tt.HashOf(
			"title", "Error 404 - Not Found",
			"content", c.Path(),
			"status", int64(404),
		)
		c.content = c.Template("404", tokens, nil)
	})
}

func (c *Ctx) write(w http.ResponseWriter) {
	h := w.Header()
	for k, v := range c.headers {
		h[k] = v
	}
	// Session cookie (Dancer2 after-response)
	if c.session != nil {
		http.SetCookie(w, c.sessionCookie(c.session.ID, false))
	} else if c.destroyed != nil {
		http.SetCookie(w, c.sessionCookie(c.destroyed.ID, true))
	} else if id, ok := c.Cookie(c.s.Config.Session.CookieName); ok {
		http.SetCookie(w, c.sessionCookie(id, false))
	}
	for _, ck := range c.cookies {
		http.SetCookie(w, ck)
	}
	var body []byte
	if c.file != nil {
		body = c.file
	} else {
		body = []byte(c.content)
	}
	ct := c.ctype
	if ct == "" {
		ct = "text/html"
	}
	// Dancer2 encodes text responses with a content (Response
	// encode_content): only these have the charset added
	if c.file == nil && !strings.Contains(ct, "charset") && strings.HasPrefix(ct, "text") && len(body) > 0 && string(body) != "0" {
		ct += "; charset=utf-8"
	}
	if c.status >= 300 && c.status < 400 && c.status != 304 && len(body) == 0 {
		// Plack::Middleware::FixMissingBodyInRedirect
		loc := c.headers.Get("Location")
		body = []byte(`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">
    <html xmlns="http://www.w3.org/1999/xhtml">
    <head>
    <title>Moved</title>
    </head>
    <body>
   <p>This item has moved <a href="` + tt.EncodeEntities(loc) + `">here</a>.</p>
</body>
</html>
`)
		ct = "text/html; charset=utf-8"
	}
	// A status without an entity body has no content type or body
	// (Plack::Util::status_with_no_entity_body, as the Perl response)
	if c.status < 200 || c.status == 204 || c.status == 304 {
		h.Del("Content-Type")
		w.WriteHeader(c.status)
		return
	}
	h.Set("Content-Type", ct)
	w.WriteHeader(c.status)
	if c.Req.Method != "HEAD" {
		w.Write(body)
	}
}

// apiURIRe matches the URIs of the API (API.pm fatal_handler)
var apiURIRe = regexp.MustCompile(`(?i)^/([0-9a-z]+/)?api/`)

// apiError is the fatal handler of the API: errors are returned as JSON
func (c *Ctx) apiError(msg *Message) {
	c.file = nil
	text := msg.Text
	if msg.Reason == "PANIC" {
		c.status = http.StatusInternalServerError
		text = "An unexexpected error has occurred"
	} else {
		c.status = http.StatusBadRequest
	}
	c.ctype = "application/json; charset=UTF-8"
	c.content = gads.EncodeJSON(map[string]any{"is_error": true, "message": text})
}

// Upload is request->upload: the first file uploaded with a name, or nil
func (c *Ctx) Upload(name string) *Upload {
	if u := c.uploads[name]; len(u) > 0 {
		return u[0]
	}
	return nil
}
