package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

func hardenedCtx(hardened bool) *Ctx {
	c := testCtx()
	c.E.Config = &gads.Config{Hardened: hardened}
	c.E.Clock = time.Now
	return c
}

// Security headers and the SameSite cookie are sent only when hardened
func TestHardenedHeaders(t *testing.T) {
	for _, hardened := range []bool{false, true} {
		c := hardenedCtx(hardened)
		if hardened {
			c.securityHeaders()
		}
		if got := c.headers.Get("X-Content-Type-Options") == "nosniff"; got != hardened {
			t.Errorf("hardened %v: nosniff sent %v", hardened, got)
		}
		ck := c.sessionCookie("id", false)
		if got := ck.SameSite == http.SameSiteLaxMode; got != hardened {
			t.Errorf("hardened %v: SameSite=Lax %v", hardened, got)
		}
	}
	c := hardenedCtx(true)
	c.s.Config.Session.SameSite = http.SameSiteStrictMode
	if c.sessionCookie("id", false).SameSite != http.SameSiteStrictMode {
		t.Error("configured SameSite replaced")
	}
	c.securityHeaders()
	if !strings.Contains(c.headers.Get("Content-Security-Policy"), "object-src 'none'") {
		t.Errorf("CSP: %s", c.headers.Get("Content-Security-Policy"))
	}
}

// The public endpoints are limited per address
func TestRateLimit(t *testing.T) {
	c := hardenedCtx(true)
	s := c.s
	post := func(path, addr string) bool {
		req := httptest.NewRequest("POST", path, nil)
		req.RemoteAddr = addr + ":1234"
		return s.rateLimit(req)
	}
	for i := 0; i < rateLimit; i++ {
		if post("/login", "10.0.0.1") {
			t.Fatalf("request %d limited", i+1)
		}
	}
	if !post("/login", "10.0.0.1") {
		t.Error("request over the limit allowed")
	}
	if post("/login", "10.0.0.2") || post("/data", "10.0.0.1") {
		t.Error("other address or path limited")
	}
	if s.limiter.allow("10.0.0.1", time.Now().Add(rateWindow)) != true {
		t.Error("limit not reset after the window")
	}
	s.E.Config.Hardened = false
	if post("/login", "10.0.0.1") {
		t.Error("limited without hardened")
	}
}

func TestClampRows(t *testing.T) {
	c := hardenedCtx(true)
	for in, want := range map[int64]int64{10: 10, 1000: 1000, 5000: maxPageRows, -1: maxPageRows} {
		if got := c.s.clampRows(in); got != want {
			t.Errorf("clampRows(%d) = %d, want %d", in, got, want)
		}
	}
	c.E.Config.Hardened = false
	if c.s.clampRows(5000) != 5000 {
		t.Error("clamped without hardened")
	}
}

// Hardened passwords are PBKDF2, and Perl's are upgraded at login
func TestPBKDF2Passwords(t *testing.T) {
	h := hashPasswordPBKDF2("secret")
	if !strings.HasPrefix(h, "{PBKDF2-SHA512}210000$") || !MatchPassword("secret", h) || MatchPassword("other", h) {
		t.Errorf("PBKDF2 round trip failed: %s", h)
	}
	// The password column is varchar(128), which PostgreSQL enforces
	if len(h) > 128 {
		t.Errorf("hash of %d characters: %s", len(h), h)
	}
	for _, bad := range []string{"{PBKDF2-SHA512}x$a$b", "{PBKDF2-SHA512}1$$", "{PBKDF2-SHA512}99999999$YQ==$YQ=="} {
		if MatchPassword("secret", bad) {
			t.Errorf("malformed hash %q validated", bad)
		}
	}
	d, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	c := hardenedCtx(true)
	c.E.DB = d
	perl := HashPassword("secret")
	d.InsertMap("user", map[string]any{"id": int64(1), "username": "a@example.com", "password": perl})
	stored := func() string {
		v, _ := db.AsString(d.QueryRow(`SELECT "password" FROM "user" WHERE "id" = 1`)["password"])
		return v
	}
	c.E.Config.PerlPasswordHashes = true
	c.s.upgradePassword(1, "secret", perl)
	if stored() != perl {
		t.Error("upgraded with perl_password_hashes")
	}
	c.E.Config.PerlPasswordHashes = false
	c.s.upgradePassword(1, "secret", perl)
	if h := stored(); !strings.HasPrefix(h, "{PBKDF2-SHA512}") || !MatchPassword("secret", h) {
		t.Errorf("not upgraded: %s", h)
	}
}

// A GET request may make only the writes of showing a page
func TestReadOnly(t *testing.T) {
	d, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	c := hardenedCtx(true)
	c.E.DB = d
	sent := false
	c.E.Emailer = func(gads.Email) { sent = true }
	end := c.s.readOnly()
	if err := gads.Try(func() {
		d.InsertMap("audit", map[string]any{"description": "x"})
		d.Exec(`UPDATE "user" SET "session_settings" = ? WHERE "id" = ?`, "{}", 1)
	}); err != nil {
		t.Errorf("allowed write refused: %v", err)
	}
	for name, f := range map[string]func(){
		"delete": func() { d.Exec(`DELETE FROM "group" WHERE "id" = ?`, 1) },
		"update": func() { d.Exec(`UPDATE "user" SET "email" = ? WHERE "id" = ?`, "x", 1) },
		"email":  func() { c.E.Emailer(gads.Email{}) },
	} {
		if err := gads.Try(f); err == nil || !strings.Contains(err.Error(), "submitting a form") {
			t.Errorf("%s allowed: %v", name, err)
		}
	}
	end()
	if err := gads.Try(func() { d.Exec(`DELETE FROM "group" WHERE "id" = ?`, 1) }); err != nil {
		t.Errorf("write refused after the request: %v", err)
	}
	c.E.Emailer(gads.Email{})
	if !sent {
		t.Error("emailer not restored")
	}
}
