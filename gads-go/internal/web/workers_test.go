package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// testBackends are the databases that the tests of workers and stored
// sessions run on: SQLite, and PostgreSQL when GADS_TEST_PG is set
func testBackends() []string {
	if os.Getenv("GADS_TEST_PG") != "" {
		return []string{"sqlite", "postgres"}
	}
	return []string{"sqlite"}
}

// onBackends runs f as a subtest on each of testBackends
func onBackends(t *testing.T, f func(t *testing.T, backend string)) {
	for _, b := range testBackends() {
		t.Run(b, func(t *testing.T) { f(t, b) })
	}
}

// pgTestDBs numbers the PostgreSQL databases of the tests
var pgTestDBs atomic.Int64

// testDatabase is a new database with GADS's tables, on backend, and
// returns the function that opens a connection to it (closed at the end
// of the test). A PostgreSQL database is one of its own, dropped at the
// end of the test.
func testDatabase(t *testing.T, backend string) func() *db.DB {
	t.Helper()
	var driver, dsn string
	switch backend {
	case "postgres":
		params := os.Getenv("GADS_TEST_PG")
		admin, err := db.Open("pgx", params+" dbname=postgres")
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("gads_web_test_%d_%d", os.Getpid(), pgTestDBs.Add(1))
		admin.TryExec(`DROP DATABASE IF EXISTS ` + name)
		if _, err := admin.TryExec(`CREATE DATABASE ` + name); err != nil {
			admin.Close()
			t.Fatal(err)
		}
		// (the cleanups of the connections run before this one)
		t.Cleanup(func() {
			admin.TryExec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
			admin.Close()
		})
		driver, dsn = "pgx", params+" dbname="+name
		d, err := db.Open(driver, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		if err := d.Deploy(); err != nil {
			t.Fatal(err)
		}
	default:
		// (deployed in memory, then copied, which is much faster)
		driver, dsn = "sqlite", filepath.Join(t.TempDir(), "gads.db")
		m, err := db.OpenMemory()
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Deploy(); err != nil {
			t.Fatal(err)
		}
		m.Exec(`VACUUM INTO ?`, dsn)
		m.Close()
	}
	return func() *db.DB {
		t.Helper()
		d, err := db.Open(driver, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		return d
	}
}

// workersDB is a new database on backend with users u0@example.com ...
// (password "pw"): the function that opens a connection to it, and one
// connection
func workersDB(t *testing.T, backend string, users int) (func() *db.DB, *db.DB) {
	t.Helper()
	open := testDatabase(t, backend)
	d := open()
	d.InsertMap("site", map[string]any{"id": int64(1), "host": "example.com"})
	for i := range users {
		name := fmt.Sprintf("u%d@example.com", i)
		d.InsertMap("user", map[string]any{"id": int64(i + 1), "site_id": int64(1), "username": name,
			"email": name, "firstname": "U", "surname": fmt.Sprint(i), "value": "U, " + fmt.Sprint(i),
			"password": HashPassword("pw"), "pwchanged": time.Now(), "account_request": int64(0)})
	}
	return open, d
}

// workersServer is a server with n engines on a new database on backend,
// with users u0@example.com ... (password "pw")
func workersServer(t *testing.T, backend string, n, users int) *Server {
	t.Helper()
	open, d := workersDB(t, backend, users)
	e, err := gads.NewEngine(d, gads.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	gadsDir, err := filepath.Abs(filepath.Join("..", "..", "..", "upstream", "GADS-dev"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(e, nil, filepath.Join(gadsDir, "views"), filepath.Join(gadsDir, "public"))
	var more []*gads.Engine
	for range n - 1 {
		w, err := e.Forked(open())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(w.Close)
		more = append(more, w)
	}
	s.SetWorkers(more...)
	return s
}

// client is a browser of a workers test: it keeps the session cookie
type client struct {
	s      *Server
	cookie *http.Cookie
}

var csrfRe = regexp.MustCompile(`name="csrf_token"\s+value="([^"]+)"`)

func (cl *client) do(ctx context.Context, method, path string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body).WithContext(ctx)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cl.cookie != nil {
		req.AddCookie(cl.cookie)
	}
	w := httptest.NewRecorder()
	cl.s.ServeHTTP(w, req)
	for _, ck := range w.Result().Cookies() {
		if ck.Name == cl.s.Config.Session.CookieName {
			cl.cookie = ck
		}
	}
	return w
}

// login logs in as user, and is the error of a failure
func (cl *client) login(user string) error {
	w := cl.do(context.Background(), "GET", "/login", nil)
	m := csrfRe.FindStringSubmatch(w.Body.String())
	if m == nil {
		return fmt.Errorf("no CSRF token in the login page (%d)", w.Code)
	}
	w = cl.do(context.Background(), "POST", "/login", url.Values{"csrf_token": {m[1]},
		"username": {user}, "password": {"pw"}, "signin": {"1"}})
	if w.Code != http.StatusFound || strings.Contains(w.Header().Get("Location"), "login") {
		return fmt.Errorf("login of %s: %d to %s", user, w.Code, w.Header().Get("Location"))
	}
	return nil
}

// A request holding an engine holds up no other while another engine is
// free; with one engine, the others wait for it
func TestWorkersDispatch(t *testing.T) {
	for _, n := range []int{1, 2} {
		s := workersServer(t, "sqlite", n, 1)
		if s.Workers() != n {
			t.Fatalf("%d workers, want %d", s.Workers(), n)
		}
		e, _ := s.acquire(context.Background())
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		w := (&client{s: s}).do(ctx, "GET", "/login", nil)
		cancel()
		s.release(e)
		if got := w.Code == http.StatusOK; got != (n > 1) {
			t.Errorf("%d workers, one busy: status %d %s", n, w.Code, w.Header().Get("Location"))
		}
	}
}

// Users are served at once on several engines, and the requests of one
// session one at a time (run with -race)
func TestWorkersConcurrent(t *testing.T) { onBackends(t, testWorkersConcurrent) }

func testWorkersConcurrent(t *testing.T, backend string) {
	const users = 6
	s := workersServer(t, backend, 3, users)
	var wg sync.WaitGroup
	errs := make(chan error, users*4)
	for i := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cl := &client{s: s}
			user := fmt.Sprintf("u%d@example.com", i)
			if err := cl.login(user); err != nil {
				errs <- err
				return
			}
			// Requests of the same session at once
			var inner sync.WaitGroup
			for range 3 {
				inner.Add(1)
				go func() {
					defer inner.Done()
					c2 := &client{s: s, cookie: cl.cookie}
					for range 3 {
						w := c2.do(context.Background(), "GET", "/myaccount/", nil)
						if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), user) {
							errs <- fmt.Errorf("%s: account page %d", user, w.Code)
							return
						}
					}
				}()
			}
			inner.Wait()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	// Every engine is back in the pool
	if len(s.engines) != s.Workers() {
		t.Errorf("%d engines free of %d", len(s.engines), s.Workers())
	}
}

// A request waits for the one before it of the same session, holding no
// engine, and gives up when it ends; other sessions go on meanwhile
func TestWorkersSessionLock(t *testing.T) { onBackends(t, testWorkersSessionLock) }

func testWorkersSessionLock(t *testing.T, backend string) {
	s := workersServer(t, backend, 2, 2)
	a, b := &client{s: s}, &client{s: s}
	for i, cl := range []*client{a, b} {
		if err := cl.login(fmt.Sprintf("u%d@example.com", i)); err != nil {
			t.Fatal(err)
		}
	}
	sess := s.sessions.peek(a.cookie.Value)
	if !sess.acquire(context.Background()) {
		t.Fatal("lock not taken")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	w := a.do(ctx, "GET", "/myaccount/", nil)
	cancel()
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("request of a busy session: %d", w.Code)
	}
	if len(s.engines) != 2 {
		t.Errorf("a waiting request holds an engine: %d free", len(s.engines))
	}
	if w := b.do(context.Background(), "GET", "/myaccount/", nil); w.Code != http.StatusOK {
		t.Errorf("other session: %d", w.Code)
	}
	// Once released, the session's next request runs
	done := make(chan int)
	go func() { done <- a.do(context.Background(), "GET", "/myaccount/", nil).Code }()
	select {
	case code := <-done:
		t.Fatalf("ran while the session was busy: %d", code)
	case <-time.After(100 * time.Millisecond):
	}
	sess.release()
	if code := <-done; code != http.StatusOK {
		t.Errorf("after release: %d", code)
	}
}

// The health check needs one free engine, not all of them
func TestWorkersHealth(t *testing.T) {
	defer func(d, c time.Duration) { healthTimeout, healthCache = d, c }(healthTimeout, healthCache)
	healthTimeout, healthCache = 100*time.Millisecond, 0
	s := workersServer(t, "sqlite", 2, 0)
	e1, _ := s.acquire(context.Background())
	if err := s.checkHealth(context.Background()); err != nil {
		t.Errorf("one engine busy: %v", err)
	}
	e2, _ := s.acquire(context.Background())
	if err := s.checkHealth(context.Background()); err == nil {
		t.Error("every engine busy, but healthy")
	}
	s.release(e1)
	s.release(e2)
}

// WaitBackground waits for the background work of every engine
func TestServerWaitBackground(t *testing.T) {
	s := workersServer(t, "sqlite", 2, 0)
	fork := func() (*gads.Engine, error) {
		d, err := db.OpenMemory()
		if err != nil {
			return nil, err
		}
		return gads.NewEngine(d, gads.DefaultConfig())
	}
	stop := make(chan struct{})
	for _, e := range s.pool {
		e.Fork = fork
		e.Background(func(*gads.Engine) { <-stop })
	}
	if s.WaitBackground(50 * time.Millisecond) {
		t.Error("finished with work running")
	}
	close(stop)
	if !s.WaitBackground(5 * time.Second) {
		t.Error("work not finished")
	}
}

// The template engine of widgets is made once, whoever asks first
func TestWidgetTTOnce(t *testing.T) {
	s := workersServer(t, "sqlite", 1, 0)
	engines := make([]*tt.Engine, 8)
	var wg sync.WaitGroup
	for i := range engines {
		wg.Add(1)
		go func() { defer wg.Done(); engines[i] = s.widgetTT() }()
	}
	wg.Wait()
	for _, e := range engines {
		if e != engines[0] || !e.NoAnyCase {
			t.Fatal("widgetTT made more than one engine, or without NoAnyCase")
		}
	}
}
