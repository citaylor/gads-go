package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/web"
)

func TestMapDSN(t *testing.T) {
	cases := []struct {
		dsn, user, pass, driver, out string
	}{
		{"dbi:SQLite:dbname=gads.db", "", "", "sqlite", "gads.db"},
		{"dbi:SQLite:/var/gads.db", "", "", "sqlite", "/var/gads.db"},
		{"dbi:Pg:database=gads;host=127.0.0.1", "dbuser", "p w", "pgx", "dbname=gads host=127.0.0.1 user=dbuser password='p w'"},
		{"dbi:Pg:dbname=gads;port=5433;sslmode=disable", "", "", "pgx", "dbname=gads port=5433 sslmode=disable"},
	}
	for _, c := range cases {
		d, o, err := mapDSN(c.dsn, c.user, c.pass)
		if err != nil || d != c.driver || o != c.out {
			t.Errorf("mapDSN(%q) = %q, %q, %v; want %q, %q", c.dsn, d, o, err, c.driver, c.out)
		}
	}
	for _, bad := range []string{"", "dbi:mysql:database=x", "SQLite:x", "dbi:SQLite:"} {
		if _, _, err := mapDSN(bad, "", ""); err == nil {
			t.Errorf("mapDSN(%q): no error", bad)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yml"), []byte(`
show_errors: 0
gads:
  dateformat: dd-MM-yyyy
  login_instance: 2
  url: https://example.com/
plugins:
  DBIC:
    default:
      dsn: dbi:Pg:database=gads
      user: u
`), 0o600)
	os.Mkdir(filepath.Join(dir, "environments"), 0o700)
	os.WriteFile(filepath.Join(dir, "environments", "development.yml"), []byte(`
show_errors: 1
plugins:
  DBIC:
    default:
      dsn: dbi:SQLite:dbname=dev.db
`), 0o600)
	cfg, err := loadConfig(dir, "development")
	if err != nil {
		t.Fatal(err)
	}
	d, dsn, err := cfg.database()
	if err != nil || d != "sqlite" || dsn != "dev.db" {
		t.Errorf("database = %q %q %v", d, dsn, err)
	}
	if cfg.section("plugins", "DBIC", "default")["user"] != "u" {
		t.Errorf("merge lost the user of the base configuration")
	}
	ec := cfg.engineConfig()
	if ec.DateFormat != "dd-MM-yyyy" || ec.LoginInstance != 2 || ec.URL != "https://example.com/" {
		t.Errorf("engine config = %+v", ec)
	}
	if !ec.Hardened || ec.PerlPasswordHashes {
		t.Errorf("security fixes not on by default: %+v", ec)
	}
	cfg.raw["gads"].(map[string]any)["hardened"] = 0
	cfg.raw["gads"].(map[string]any)["perl_password_hashes"] = 1
	if ec := cfg.engineConfig(); ec.Hardened || !ec.PerlPasswordHashes {
		t.Errorf("hardened: 0, perl_password_hashes: 1 = %+v", ec)
	}
	wc := cfg.webConfig()
	if !wc.ShowErrors || wc.Gads["login_instance"] != int64(2) {
		t.Errorf("web config = %+v", wc)
	}
	if _, err := loadConfig(t.TempDir(), "development"); err == nil {
		t.Errorf("no error without config.yml")
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"":            0,
		"3600":        time.Hour,
		"90 minutes":  90 * time.Minute,
		"1d 12h":      36 * time.Hour,
		"2 weeks":     14 * 24 * time.Hour,
		"1M":          30 * 24 * time.Hour,
		"1.5 hours":   90 * time.Minute,
		"soon":        0,
		"3 fortnight": 0,
	}
	for spec, want := range cases {
		if got := parseDuration(spec); got != want {
			t.Errorf("parseDuration(%q) = %v; want %v", spec, got, want)
		}
	}
}

func TestSessionConfig(t *testing.T) {
	c := &appConfig{raw: map[string]any{
		"session": "YAML",
		"engines": map[string]any{"session": map[string]any{"YAML": map[string]any{
			"cookie_name": "linkspace.session", "is_secure": 1, "cookie_duration": 3600,
			"cookie_same_site": "lax", "session_duration": "8 hours",
		}}},
	}}
	got := c.webConfig().Session
	want := web.SessionConfig{CookieName: "linkspace.session", CookiePath: "/", HTTPOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, CookieDuration: time.Hour, IdleTimeout: 8 * time.Hour}
	if got != want {
		t.Errorf("session config %+v; want %+v", got, want)
	}
	// Without configuration, the Dancer2 defaults
	if got := (&appConfig{raw: map[string]any{}}).webConfig().Session; got != web.DefaultConfig().Session {
		t.Errorf("default session config %+v", got)
	}
}

// The log of serve takes the Dancer2 log level and gads: log_format
func TestLogger(t *testing.T) {
	for _, tc := range []struct {
		raw  map[string]any
		want string // what a warning and an info message write, or "error"
	}{
		{map[string]any{}, "level=WARN msg=w\nlevel=INFO msg=i\n"},
		{map[string]any{"log": "warning"}, "level=WARN msg=w\n"},
		{map[string]any{"log": "core"}, "level=WARN msg=w\nlevel=INFO msg=i\n"},
		{map[string]any{"gads": map[string]any{"log_format": "json"}}, `{"level":"WARN","msg":"w"}` + "\n" + `{"level":"INFO","msg":"i"}` + "\n"},
		{map[string]any{"log": "loud"}, "error"},
		{map[string]any{"gads": map[string]any{"log_format": "xml"}}, "error"},
	} {
		var out strings.Builder
		l, err := (&appConfig{raw: tc.raw}).logger(&out)
		if err != nil {
			if tc.want != "error" {
				t.Errorf("%v: %v", tc.raw, err)
			}
			continue
		}
		if tc.want == "error" {
			t.Errorf("%v: no error", tc.raw)
			continue
		}
		// Without the time, for a fixed output
		l = slog.New(withoutTime{l.Handler()})
		l.Warn("w")
		l.Info("i")
		if out.String() != tc.want {
			t.Errorf("%v: %q, want %q", tc.raw, out.String(), tc.want)
		}
	}
}

// withoutTime drops the time of log records
type withoutTime struct{ slog.Handler }

func (h withoutTime) Handle(ctx context.Context, r slog.Record) error {
	r.Time = time.Time{}
	return h.Handler.Handle(ctx, r)
}
