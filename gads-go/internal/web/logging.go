package web

// Request and error logging, and the health check: Go only (Dancer2 logs
// through LogReport to the console, and GADS has no health route).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"sync"
	"time"
)

// statusWriter records the status and size of a response
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap gives http.ResponseController the original writer
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// requestLog is the log entry of one request, written when it ends
type requestLog struct {
	start time.Time
	id    string
	// user is the user logged in when the request came, and userAfter the
	// one when it ended (they differ for a login or a logout)
	user, userAfter string
	static          bool
}

// requestIDRe is a request ID that a proxy may pass in X-Request-ID
var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// newRequestLog starts the log entry of a request: its ID is the proxy's
// X-Request-ID behind a proxy, otherwise a new one, and is returned in the
// response's X-Request-ID
func (s *Server) newRequestLog(w http.ResponseWriter, req *http.Request) *requestLog {
	l := &requestLog{start: time.Now()}
	if id := req.Header.Get("X-Request-ID"); s.Config.BehindProxy && requestIDRe.MatchString(id) {
		l.id = id
	} else {
		b := make([]byte, 8)
		rand.Read(b)
		l.id = hex.EncodeToString(b)
	}
	w.Header().Set("X-Request-ID", l.id)
	return l
}

// secretPathRe finds the codes that some paths carry (password reset)
var secretPathRe = regexp.MustCompile(`^(/resetpw/)[^/]+`)

// logPath is the path of a request as logged: without its query string,
// which can hold tokens, and without the code of a reset link
func logPath(req *http.Request) string {
	return secretPathRe.ReplaceAllString(req.URL.Path, "${1}REDACTED")
}

// logRequest writes the log entry of a finished request. Static files that
// were served are not logged, as a page loads many of them.
func (s *Server) logRequest(l *requestLog, w *statusWriter, req *http.Request) {
	if s.Config.AccessLogger == nil {
		return
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	if l.static && status < 400 {
		return
	}
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelError
	}
	attrs := []slog.Attr{
		slog.String("id", l.id),
		slog.String("method", req.Method),
		slog.String("path", logPath(req)),
		slog.Int("status", status),
		slog.Int64("bytes", w.bytes),
		slog.Duration("duration", time.Since(l.start)),
		slog.String("client", s.clientAddr(req)),
		slog.String("user", l.user),
	}
	if l.userAfter != l.user {
		attrs = append(attrs, slog.String("user_after", l.userAfter))
	}
	s.Config.AccessLogger.LogAttrs(req.Context(), level, "request", attrs...)
}

// logUser is the user of a request as logged, from a session that the
// request has loaded (so that logging creates no session)
func (c *Ctx) logUser() string {
	if c.session == nil {
		return ""
	}
	u, _ := c.session.Data["logged_in_user"].(string)
	return u
}

// cookieUser is the user of the session of the request's cookie, before
// the request runs (without loading the session or marking it used)
func (c *Ctx) cookieUser() string {
	id, ok := c.Cookie(c.s.Config.Session.CookieName)
	if !ok {
		return ""
	}
	sess := c.s.sessions.peek(id)
	if sess == nil {
		return ""
	}
	u, _ := sess.Data["logged_in_user"].(string)
	return u
}

// logError logs an error of a route: an unexpected one (PANIC) as an
// error, with the Go stack of a runtime panic, and one shown to the user
// (ERROR, as a permission refused) as information
func (c *Ctx) logError(msg *Message, runtimePanic bool) {
	lg := c.s.Config.Logger
	if lg == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("id", c.requestID),
		slog.String("method", c.Req.Method),
		slog.String("path", logPath(c.Req)),
		slog.String("user", c.logUser()),
		slog.String("reason", msg.Reason),
		slog.String("message", msg.Text),
	}
	level := slog.LevelInfo
	if msg.Reason == "PANIC" {
		level = slog.LevelError
		if runtimePanic {
			attrs = append(attrs, slog.String("stack", string(debug.Stack())))
		}
	}
	lg.LogAttrs(c.Req.Context(), level, "error", attrs...)
}

// healthPath is the health check: GET or HEAD, without login. It shadows a
// table with the short name healthz.
const healthPath = "/healthz"

// healthRateLimit is how many health checks a client address may make in
// each rateWindow, hardened or not: Docker's make 2 a minute (30 while the
// container starts)
const healthRateLimit = 60

// healthTimeout is how long the health check waits for the engine and the
// database, and healthCache how long its result is kept
var (
	healthTimeout = 10 * time.Second
	healthCache   = time.Second
)

// healthState is the last health check, and the one running
type healthState struct {
	mu      sync.Mutex
	at      time.Time
	err     error
	running chan struct{}
}

// health answers the health check: 200 when the engine is free within
// healthTimeout and the database answers, otherwise 503. A request that
// holds the engine for longer (or a lost database) makes the service
// unhealthy. The route needs no login, so that anyone can call it: at
// most one check runs at a time, and its result is kept for healthCache,
// so that calls cannot queue for the engine.
func (s *Server) health(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !s.healthLimiter.allow(s.clientAddr(req), time.Now()) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		if req.Method != "HEAD" {
			w.Write([]byte("too many requests\n"))
		}
		return
	}
	err := s.checkHealth(req.Context())
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if req.Method != "HEAD" {
			w.Write([]byte("unavailable\n"))
		}
		return
	}
	if req.Method != "HEAD" {
		w.Write([]byte("ok\n"))
	}
}

// checkHealth is the result of a recent check, or of a new one (shared by
// the calls that wait for it)
func (s *Server) checkHealth(ctx context.Context) error {
	h := &s.healthState
	h.mu.Lock()
	if !h.at.IsZero() && time.Since(h.at) < healthCache {
		err := h.err
		h.mu.Unlock()
		return err
	}
	if h.running == nil {
		h.running = make(chan struct{})
		go s.runHealthCheck(h.running) // #nosec G118 -- the shared check outlives the request that started it
	}
	done := h.running
	h.mu.Unlock()
	select {
	case <-done:
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runHealthCheck checks the engine and the database, records the result
// and closes done
func (s *Server) runHealthCheck(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	res := make(chan error, 1)
	go func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if ctx.Err() != nil {
			res <- ctx.Err()
			return
		}
		res <- s.E.DB.Ping(ctx)
	}()
	var err error
	select {
	case err = <-res:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil && s.Config.Logger != nil {
		s.Config.Logger.Error("health check failed", "error", err.Error())
	}
	h := &s.healthState
	h.mu.Lock()
	h.at, h.err, h.running = time.Now(), err, nil
	h.mu.Unlock()
	close(done)
}
