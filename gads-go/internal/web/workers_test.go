package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// workersServer is a server with n engines on a new SQLite database file,
// with users u0@example.com ... (password "pw")
func workersServer(t *testing.T, n, users int) *Server {
	t.Helper()
	// (deployed in memory, then copied, which is much faster)
	path := filepath.Join(t.TempDir(), "gads.db")
	m, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Deploy(); err != nil {
		t.Fatal(err)
	}
	m.Exec(`VACUUM INTO ?`, path)
	m.Close()
	d, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.InsertMap("site", map[string]any{"id": int64(1), "host": "example.com"})
	for i := range users {
		name := fmt.Sprintf("u%d@example.com", i)
		d.InsertMap("user", map[string]any{"id": int64(i + 1), "site_id": int64(1), "username": name,
			"email": name, "firstname": "U", "surname": fmt.Sprint(i), "value": "U, " + fmt.Sprint(i),
			"password": HashPassword("pw"), "pwchanged": time.Now(), "account_request": int64(0)})
	}
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
		d2, err := db.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		w, err := e.Forked(d2)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { w.Close(); d2.Close() })
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
		s := workersServer(t, n, 1)
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
func TestWorkersConcurrent(t *testing.T) {
	const users = 6
	s := workersServer(t, 3, users)
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
