package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/web"
)

// appConfig is the Dancer2 configuration of GADS: config.yml, with the
// file of its environment (environments/<env>.yml) merged over it
type appConfig struct {
	raw map[string]any
}

// loadConfig reads config.yml of a directory and its environment file
func loadConfig(dir, environment string) (*appConfig, error) {
	raw := map[string]any{}
	files := []string{filepath.Join(dir, "config.yml")}
	if environment != "" {
		files = append(files, filepath.Join(dir, "environments", environment+".yml"))
	}
	found := false
	for i, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) && i > 0 {
				continue
			}
			return nil, err
		}
		found = true
		var m map[string]any
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		mergeMaps(raw, m)
	}
	if !found {
		return nil, fmt.Errorf("no configuration in %s", dir)
	}
	return &appConfig{raw: raw}, nil
}

// mergeMaps merges src over dst, as Dancer2 merges environment files
func mergeMaps(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeMaps(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func (c *appConfig) section(path ...string) map[string]any {
	m := c.raw
	for _, p := range path {
		n, ok := m[p].(map[string]any)
		if !ok {
			return map[string]any{}
		}
		m = n
	}
	return m
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func asInt(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case string:
		var n int64
		fmt.Sscan(t, &n)
		return n
	}
	return 0
}

func asBool(v any) bool {
	s := asString(v)
	return s != "" && s != "0" && s != "false"
}

// gadsSection is the "gads" section, with integers as int64 (as the web
// configuration expects)
func (c *appConfig) gadsSection() map[string]any {
	out := map[string]any{}
	for k, v := range c.section("gads") {
		if i, ok := v.(int); ok {
			v = int64(i)
		}
		out[k] = v
	}
	return out
}

// engineConfig is the configuration of the engine (GADS::Config)
func (c *appConfig) engineConfig() *gads.Config {
	cfg := gads.DefaultConfig()
	g := c.section("gads")
	if v := asString(g["dateformat"]); v != "" {
		cfg.DateFormat = v
	}
	if v := asString(g["url"]); v != "" {
		cfg.URL = v
	}
	if v := asInt(g["login_instance"]); v != 0 {
		cfg.LoginInstance = v
	}
	cfg.Header = asString(g["header"])
	cfg.Uploads = asString(g["uploads"])
	strMap := func(v any) map[string]string {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		out := map[string]string{}
		for k, x := range m {
			out[k] = asString(x)
		}
		return out
	}
	cfg.Yubi = strMap(g["yubi"])
	cfg.SMS = strMap(g["sms"])
	return cfg
}

// webConfig is the configuration of the web application
func (c *appConfig) webConfig() *web.Config {
	cfg := web.DefaultConfig()
	cfg.Gads = c.gadsSection()
	cfg.ShowErrors = asBool(c.raw["show_errors"])
	cfg.BehindProxy = asBool(c.raw["behind_proxy"])
	cfg.Session = c.sessionConfig(cfg.Session)
	auth := c.section("plugins", "Auth::Extensible")
	if v, ok := auth["password_expiry_days"]; ok {
		cfg.PasswordExpiryDays = int(asInt(v))
	} else if realms, ok := auth["realms"].(map[string]any); ok {
		for _, r := range realms {
			if rm, ok := r.(map[string]any); ok {
				if v, ok := rm["password_expiry_days"]; ok {
					cfg.PasswordExpiryDays = int(asInt(v))
				}
			}
		}
	}
	return cfg
}

// sessionConfig is the cookie configuration of the session engine named
// by "session" (engines: session: ENGINE:), over the Dancer2 defaults
func (c *appConfig) sessionConfig(cfg web.SessionConfig) web.SessionConfig {
	engine := asString(c.raw["session"])
	if engine == "" {
		engine = "Simple"
	}
	e := c.section("engines", "session", engine)
	if v := asString(e["cookie_name"]); v != "" {
		cfg.CookieName = v
	}
	if v := asString(e["cookie_path"]); v != "" {
		cfg.CookiePath = v
	}
	cfg.CookieDomain = asString(e["cookie_domain"])
	if v, ok := e["is_secure"]; ok {
		cfg.Secure = asBool(v)
	}
	if v, ok := e["is_http_only"]; ok {
		cfg.HTTPOnly = asBool(v)
	}
	switch strings.ToLower(asString(e["cookie_same_site"])) {
	case "strict":
		cfg.SameSite = http.SameSiteStrictMode
	case "lax":
		cfg.SameSite = http.SameSiteLaxMode
	case "none":
		cfg.SameSite = http.SameSiteNoneMode
	}
	cfg.CookieDuration = parseDuration(asString(e["cookie_duration"]))
	cfg.IdleTimeout = cfg.CookieDuration
	if d := parseDuration(asString(e["session_duration"])); d > 0 {
		cfg.IdleTimeout = d
	}
	return cfg
}

var (
	durationNumber = regexp.MustCompile(`^\s*([-+]?\d+(?:[.,]\d+)?)\s*$`)
	durationPart   = regexp.MustCompile(`^\s*([-+]?\d+(?:[.,]\d+)?)\s*([a-zA-Z]+)(?:\s*(?:,|and)\s*)*`)
	durationUnits  = map[string]float64{}
)

func init() {
	for secs, units := range map[float64]string{
		1: "s second seconds sec secs", 60: "m minute minutes min mins", 3600: "h hr hour hours",
		86400: "d day days", 7 * 86400: "w week weeks", 30 * 86400: "M month months", 365 * 86400: "y year years",
	} {
		for _, u := range strings.Fields(units) {
			durationUnits[u] = secs
		}
	}
}

// parseDuration is Dancer2::Core::Time's duration: seconds, or amounts
// with units ("2 hours", "1d 12h"); 0 if empty or not understood
func parseDuration(spec string) time.Duration {
	if m := durationNumber.FindStringSubmatch(spec); m != nil {
		spec = m[1] + "s"
	}
	var secs float64
	for strings.TrimSpace(spec) != "" {
		m := durationPart.FindStringSubmatch(spec)
		if m == nil {
			return 0
		}
		unit := m[2]
		if len(unit) > 1 {
			unit = strings.ToLower(unit)
		}
		mult, ok := durationUnits[unit]
		if !ok {
			return 0
		}
		n, _ := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
		secs += n * mult
		spec = spec[len(m[0]):]
	}
	return time.Duration(secs * float64(time.Second))
}

// database is the driver and data source of the DBIC plugin (its default
// connection)
func (c *appConfig) database() (driver, dsn string, err error) {
	d := c.section("plugins", "DBIC", "default")
	return mapDSN(asString(d["dsn"]), asString(d["user"]), asString(d["password"]))
}

// mapDSN maps a DBI data source to a Go driver and its data source:
// dbi:SQLite (sqlite) and dbi:Pg (pgx)
func mapDSN(dsn, user, password string) (string, string, error) {
	parts := strings.SplitN(dsn, ":", 3)
	if len(parts) != 3 || !strings.EqualFold(parts[0], "dbi") {
		return "", "", fmt.Errorf("unsupported data source %q", dsn)
	}
	attrs := map[string]string{}
	var order []string
	for _, kv := range strings.Split(parts[2], ";") {
		if kv == "" {
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			k, v = "dbname", kv
		}
		k = strings.ToLower(strings.TrimSpace(k))
		attrs[k] = strings.TrimSpace(v)
		order = append(order, k)
	}
	switch parts[1] {
	case "SQLite":
		name := attrs["dbname"]
		if name == "" {
			name = attrs["database"]
		}
		if name == "" {
			return "", "", fmt.Errorf("no database file in %q", dsn)
		}
		return "sqlite", name, nil
	case "Pg":
		var kv []string
		add := func(k, v string) {
			if v != "" {
				kv = append(kv, k+"="+quotePg(v))
			}
		}
		db := attrs["dbname"]
		if db == "" {
			db = attrs["database"]
		}
		add("dbname", db)
		add("host", attrs["host"])
		add("port", attrs["port"])
		add("user", user)
		add("password", password)
		for _, k := range order {
			switch k {
			case "dbname", "database", "host", "port":
			default:
				add(k, attrs[k])
			}
		}
		return "pgx", strings.Join(kv, " "), nil
	}
	return "", "", fmt.Errorf("unsupported database driver %q", parts[1])
}

// quotePg quotes a value of a libpq connection string
func quotePg(v string) string {
	if v != "" && !strings.ContainsAny(v, ` '\`) {
		return v
	}
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}
