package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// testServer is a server on a new in-memory database, with the views of
// GADS-dev
func testServer(t *testing.T) *Server {
	t.Helper()
	d, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	// The site of httptest's requests
	d.InsertMap("site", map[string]any{"id": int64(1), "host": "example.com"})
	e, err := gads.NewEngine(d, gads.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	gadsDir := filepath.Join("..", "..", "..", "upstream", "GADS-dev")
	return New(e, nil, filepath.Join(gadsDir, "views"), filepath.Join(gadsDir, "public"))
}

func TestHealth(t *testing.T) {
	s := testServer(t)
	get := func(method string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, healthPath, nil))
		return w
	}
	if w := get("GET"); w.Code != 200 || w.Body.String() != "ok\n" {
		t.Errorf("healthy: %d %q", w.Code, w.Body.String())
	}
	if w := get("HEAD"); w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("HEAD: %d %q", w.Code, w.Body.String())
	}
	// POST is not the health check
	if w := get("POST"); w.Body.String() == "ok\n" {
		t.Error("POST answered as the health check")
	}

	// A request that holds the engine makes it unhealthy
	defer func(d, c time.Duration) { healthTimeout, healthCache = d, c }(healthTimeout, healthCache)
	healthTimeout, healthCache = 50*time.Millisecond, 0
	s.mu.Lock()
	w := get("GET")
	s.mu.Unlock()
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("engine busy: %d", w.Code)
	}

	// So does a lost database
	s.E.DB.Close()
	if w := get("GET"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("database closed: %d", w.Code)
	}
}

// Requests are logged with their ID, without query strings or reset codes
func TestRequestLog(t *testing.T) {
	s := testServer(t)
	var out bytes.Buffer
	s.Config.AccessLogger = slog.New(slog.NewTextHandler(&out, nil))

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/resetpw/SECRETCODE?token=QSECRET", nil))
	line := out.String()
	id := w.Header().Get("X-Request-ID")
	if len(id) != 16 || !strings.Contains(line, "id="+id) {
		t.Errorf("request ID %q not logged: %s", id, line)
	}
	if strings.Contains(line, "SECRET") || !strings.Contains(line, "path=/resetpw/REDACTED") {
		t.Errorf("secret logged: %s", line)
	}
	if !strings.Contains(line, "msg=request") || !strings.Contains(line, "method=GET") || !strings.Contains(line, "status=") {
		t.Errorf("request line: %s", line)
	}

	// A proxy's request ID is used only behind a proxy
	for _, behind := range []bool{false, true} {
		s.Config.BehindProxy = behind
		req := httptest.NewRequest("GET", "/missing.css", nil)
		req.Header.Set("X-Request-ID", "proxy-id-1")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if got := w.Header().Get("X-Request-ID") == "proxy-id-1"; got != behind {
			t.Errorf("behind proxy %v: request ID %q", behind, w.Header().Get("X-Request-ID"))
		}
	}

	// Health checks are not logged
	out.Reset()
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", healthPath, nil))
	if out.Len() != 0 {
		t.Errorf("health check logged: %s", out.String())
	}

	// Without a logger, nothing is added to the response
	s.Config.AccessLogger = nil
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/resetpw/x", nil))
	if w.Header().Get("X-Request-ID") != "" {
		t.Error("request ID without a logger")
	}
}

// Unexpected errors are logged as errors, a runtime panic with its stack
func TestErrorLog(t *testing.T) {
	for _, tc := range []struct {
		err   any
		level string
		stack bool
	}{
		{&gads.Error{Level: gads.LevelError, Msg: "You do not have permission"}, "INFO", false},
		{&gads.Error{Level: gads.LevelPanic, Msg: "internal"}, "ERROR", false},
		{errors.New("runtime failure"), "ERROR", true},
	} {
		c := testCtx()
		var out bytes.Buffer
		c.s.Config.Logger = slog.New(slog.NewTextHandler(&out, nil))
		c.requestID = "abc"
		c.handleError(tc.err)
		line := out.String()
		if !strings.Contains(line, "level="+tc.level) || !strings.Contains(line, "id=abc") {
			t.Errorf("%v: %s", tc.err, line)
		}
		if got := strings.Contains(line, "stack="); got != tc.stack {
			t.Errorf("%v: stack logged %v: %s", tc.err, got, line)
		}
	}
}

// Health checks share one check, whose result is kept for healthCache, so
// that calls do not queue for the engine
func TestHealthShared(t *testing.T) {
	s := testServer(t)
	defer func(d, c time.Duration) { healthTimeout, healthCache = d, c }(healthTimeout, healthCache)
	healthTimeout, healthCache = 200*time.Millisecond, time.Hour
	if err := s.checkHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The kept result is answered while the engine is busy
	s.mu.Lock()
	start := time.Now()
	err := s.checkHealth(context.Background())
	if err != nil || time.Since(start) > 50*time.Millisecond {
		t.Errorf("kept result not used: %v after %s", err, time.Since(start))
	}
	// Once it is old, concurrent calls wait for one check
	healthCache = 0
	s.healthState.at = time.Time{}
	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = s.checkHealth(context.Background()) }()
	}
	wg.Wait()
	s.mu.Unlock()
	for _, err := range errs {
		if err == nil {
			t.Fatal("engine busy, but healthy")
		}
	}
}

// The request log has the user when the request came, and the user after
// it when that changed (a logout)
func TestRequestLogUsers(t *testing.T) {
	s := testServer(t)
	var out bytes.Buffer
	s.Config.AccessLogger = slog.New(slog.NewTextHandler(&out, nil))
	sess := &Session{ID: newSessionID(), Data: map[string]any{"logged_in_user": "alice@example.com"}}
	s.sessions.put(sess)
	get := func(path string) string {
		out.Reset()
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: s.Config.Session.CookieName, Value: sess.ID})
		s.ServeHTTP(httptest.NewRecorder(), req)
		return out.String()
	}
	if line := get("/healthz-not"); !strings.Contains(line, "user=alice@example.com") || strings.Contains(line, "user_after") {
		t.Errorf("request: %s", line)
	}
	if line := get("/logout"); !strings.Contains(line, "user=alice@example.com user_after=\"\"") {
		t.Errorf("logout: %s", line)
	}
}

// A request too large closes its connection, with the request log on
func TestRequestTooLarge(t *testing.T) {
	s := testServer(t)
	s.Config.MaxBodyBytes = 10
	s.Config.AccessLogger = slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(s)
	defer srv.Close()
	res, err := http.Post(srv.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader(strings.Repeat("a=b&", 100)))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge || !res.Close {
		t.Errorf("status %d, connection closed %v", res.StatusCode, res.Close)
	}
}
