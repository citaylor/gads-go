package web

import (
	"context"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// Session values come back with their Go types
func TestSessionEncoding(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 7, 1, 12, 30, 15, 123456789, london)
	in := map[string]any{
		"nil": nil, "s": "text ✓", "empty": "", "b": true, "n": 7, "i": int64(-1 << 62),
		"f": 0.1, "t": when, "utc": when.UTC(), "dt": &ttDateTime{t: when},
		"ls":       []string{"a", "b"},
		"messages": []*Message{{Reason: "NOTICE", Text: "Saved", Classes: []string{"x"}}},
		"l":        []any{"x", int64(1), nil, map[string]any{"k": nil}},
		"m":        map[string]any{"type": "asc", "id": "3", "deep": map[string]any{"f": 1.5}},
	}
	s, err := encodeSession(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodeSession(s)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range in {
		got := out[k]
		switch x := v.(type) {
		case time.Time:
			g, ok := got.(time.Time)
			if !ok || !g.Equal(x) || g.Location().String() != x.Location().String() {
				t.Errorf("%s: %#v, want %#v", k, got, v)
			}
		case *ttDateTime:
			g, ok := got.(*ttDateTime)
			if !ok || !g.t.Equal(x.t) || g.TTString() != x.TTString() {
				t.Errorf("%s: %#v, want %#v", k, got, v)
			}
		default:
			if !reflect.DeepEqual(got, v) {
				t.Errorf("%s: %#v, want %#v", k, got, v)
			}
		}
	}
	if _, ok := out["nil"]; !ok || len(out) != len(in) {
		t.Errorf("keys %v", out)
	}
	if _, err := encodeSession(map[string]any{"x": struct{}{}}); err == nil {
		t.Error("an unknown type was encoded")
	}
}

// sessionServers are two servers on one database with stored sessions,
// as the application before and after a restart
func sessionServers(t *testing.T, backend string) (*Server, *Server, *db.DB) {
	t.Helper()
	open, _ := workersDB(t, backend, 1)
	gadsDir, err := filepath.Abs(filepath.Join("..", "..", "..", "upstream", "GADS-dev"))
	if err != nil {
		t.Fatal(err)
	}
	server := func() *Server {
		e, err := gads.NewEngine(open(), gads.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		s := New(e, nil, filepath.Join(gadsDir, "views"), filepath.Join(gadsDir, "public"))
		if err := s.PersistSessions(open()); err != nil {
			t.Fatal(err)
		}
		return s
	}
	return server(), server(), open()
}

// A session is kept over a restart, and deleted with logout
func TestSessionPersisted(t *testing.T) { onBackends(t, testSessionPersisted) }

func testSessionPersisted(t *testing.T, backend string) {
	first, second, d := sessionServers(t, backend)
	count := func() int64 {
		n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "sessions"`)
		return n
	}
	cl := &client{s: first}
	if err := cl.login("u0@example.com"); err != nil {
		t.Fatal(err)
	}
	// Only the session after login is stored (its ID changed at login)
	if n := count(); n != 1 {
		t.Errorf("%d sessions stored after login", n)
	}
	w := cl.do(context.Background(), "GET", "/myaccount/", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("account page: %d", w.Code)
	}
	// The restarted application knows the session
	cl.s = second
	w = cl.do(context.Background(), "GET", "/myaccount/", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "u0@example.com") {
		t.Fatalf("after restart: %d %s", w.Code, w.Header().Get("Location"))
	}
	// Logging out deletes it
	w = cl.do(context.Background(), "GET", "/logout", nil)
	if n := count(); n != 0 {
		t.Errorf("%d sessions stored after logout (%d)", n, w.Code)
	}
	cl.s = first
	w = cl.do(context.Background(), "GET", "/myaccount/", nil)
	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "/login") {
		t.Errorf("logged out session still logged in: %d %s", w.Code, w.Header().Get("Location"))
	}
}

// Sessions unused for the idle timeout are deleted when the store starts
func TestSessionPersistedExpiry(t *testing.T) { onBackends(t, testSessionPersistedExpiry) }

func testSessionPersistedExpiry(t *testing.T, backend string) {
	first, _, d := sessionServers(t, backend)
	d.Exec(`INSERT INTO "sessions" ("id", "session_data", "last_active") VALUES ('old', '{}', ?), ('new', '{}', ?)`,
		time.Now().Add(-first.sessions.idle-time.Hour).Unix(), time.Now().Unix())
	if err := first.PersistSessions(d); err != nil {
		t.Fatal(err)
	}
	ids := d.QueryCol(`SELECT "id" FROM "sessions" ORDER BY "id"`)
	if len(ids) != 1 || ids[0] != "new" {
		t.Errorf("sessions left: %v", ids)
	}
}

// storedSessions are two session stores on one database (two processes),
// with the errors they log
func storedSessions(t *testing.T, backend string) (*sessionStore, *sessionStore, *db.DB, *[]string) {
	t.Helper()
	open, d := workersDB(t, backend, 0)
	if _, err := d.TryExec(sessionTable); err != nil {
		t.Fatal(err)
	}
	var errs []string
	store := func() *sessionStore {
		st := newSessionStore(time.Hour)
		st.backend = &sessionBackend{d: open()}
		st.logError = func(msg string, err error) { errs = append(errs, msg+": "+err.Error()) }
		return st
	}
	return store(), store(), d, &errs
}

func storedRow(d *db.DB, id string) (data string, active int64, ok bool) {
	r := d.QueryRow(`SELECT * FROM "sessions" WHERE "id" = ?`, rowKey(id))
	if r == nil {
		return "", 0, false
	}
	data, _ = db.AsString(r["session_data"])
	active, _ = db.AsInt64(r["last_active"])
	return data, active, true
}

// A session is written when its data changed, or to record its use once a
// sessionTouch has passed, and under a hash of its ID
func TestSessionStoreSave(t *testing.T) { onBackends(t, testSessionStoreSave) }

func testSessionStoreSave(t *testing.T, backend string) {
	st, _, d, errs := storedSessions(t, backend)
	s := &Session{ID: "secret-id", Data: map[string]any{"a": "1"}}
	st.save(s)
	if _, _, ok := storedRow(d, s.ID); ok {
		t.Error("a session not in the store was saved")
	}
	st.put(s)
	st.save(s)
	if _, _, ok := storedRow(d, s.ID); !ok {
		t.Fatal("not saved")
	}
	if n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "sessions" WHERE "id" = 'secret-id'`); n != 0 {
		t.Error("the session ID is stored as it is")
	}
	// Unchanged, it is not written again
	d.Exec(`UPDATE "sessions" SET "last_active" = 1`)
	st.save(s)
	if _, active, _ := storedRow(d, s.ID); active != 1 {
		t.Error("an unchanged session was written")
	}
	// Changed, it is
	s.Data["a"] = "2"
	st.save(s)
	if data, active, _ := storedRow(d, s.ID); active == 1 || !strings.Contains(data, `"2"`) {
		t.Errorf("a changed session was not written: %s", data)
	}
	// Unchanged but used a sessionTouch after its last write, it is
	d.Exec(`UPDATE "sessions" SET "last_active" = 1`)
	s.savedAt = s.used.Add(-2 * sessionTouch)
	st.save(s)
	if _, active, _ := storedRow(d, s.ID); active == 1 {
		t.Error("the use of a session was not recorded")
	}
	if len(*errs) != 0 {
		t.Errorf("errors: %v", *errs)
	}
}

// A session whose ID changes (at login) is stored under the new ID only
func TestSessionStoreChangeID(t *testing.T) { onBackends(t, testSessionStoreChangeID) }

func testSessionStoreChangeID(t *testing.T, backend string) {
	st, _, d, _ := storedSessions(t, backend)
	s := &Session{ID: "before", Data: map[string]any{"k": int64(1)}}
	st.put(s)
	st.save(s)
	// As ChangeSessionID
	st.remove(s.ID)
	s.ID = "after"
	st.put(s)
	st.save(s)
	if _, _, ok := storedRow(d, "before"); ok {
		t.Error("the old ID is still stored")
	}
	if _, _, ok := storedRow(d, "after"); !ok {
		t.Error("the new ID is not stored")
	}
	if n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "sessions"`); n != 1 {
		t.Errorf("%d rows", n)
	}
}

// Each request reads its session again: another process may have changed
// or deleted it
func TestSessionStoreRefresh(t *testing.T) { onBackends(t, testSessionStoreRefresh) }

func testSessionStoreRefresh(t *testing.T, backend string) {
	st1, st2, _, errs := storedSessions(t, backend)
	s := &Session{ID: "shared", Data: map[string]any{"page": int64(1)}}
	st1.put(s)
	st1.save(s)
	s2 := st2.get("shared")
	if s2 == nil || s2.Data["page"] != int64(1) {
		t.Fatalf("other process: %+v", s2)
	}
	s2.Data["page"] = int64(2)
	st2.save(s2)
	if got := st1.get("shared"); got != s || got.Data["page"] != int64(2) {
		t.Errorf("change of the other process not read: %+v", got)
	}
	st2.remove("shared")
	if got := st1.get("shared"); got != nil {
		t.Error("session deleted by the other process still found")
	}
	if len(*errs) != 0 {
		t.Errorf("errors: %v", *errs)
	}
}

// A value that cannot be stored is reported, and nothing is written
func TestSessionStoreEncodeFailure(t *testing.T) { onBackends(t, testSessionStoreEncodeFailure) }

func testSessionStoreEncodeFailure(t *testing.T, backend string) {
	st, _, d, errs := storedSessions(t, backend)
	s := &Session{ID: "bad", Data: map[string]any{"x": struct{}{}}}
	st.put(s)
	st.save(s)
	if _, _, ok := storedRow(d, "bad"); ok {
		t.Error("saved")
	}
	if len(*errs) != 1 || !strings.Contains((*errs)[0], "could not be saved") {
		t.Errorf("errors: %v", *errs)
	}
}

// A stored session read by several requests at once is one Session, whose
// lock they share (run with -race)
func TestSessionStoreLookupOnce(t *testing.T) { onBackends(t, testSessionStoreLookupOnce) }

func testSessionStoreLookupOnce(t *testing.T, backend string) {
	st1, st2, _, _ := storedSessions(t, backend)
	s := &Session{ID: "once", Data: map[string]any{}}
	st1.put(s)
	st1.save(s)
	got := make([]*Session, 8)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func() { defer wg.Done(); got[i] = st2.peek("once") }()
	}
	wg.Wait()
	for _, g := range got {
		if g == nil || g != got[0] {
			t.Fatal("several Sessions for one ID")
		}
	}
}
