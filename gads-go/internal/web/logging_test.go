package web

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	defer func(d time.Duration) { healthTimeout = d }(healthTimeout)
	healthTimeout = 50 * time.Millisecond
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
	s.Config.Logger = slog.New(slog.NewTextHandler(&out, nil))

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
	s.Config.Logger = nil
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
