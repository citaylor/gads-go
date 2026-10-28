package conformance

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/web"
)

// The password of users that log in (see the Perl GADS::Conformance::Web)
const (
	webPassword     = "password"
	webPasswordHash = "{SSHA512}+mohhbPgqahe9B/7Z+88H7b3SYD46/lw5OcuNT7ZU31ZMIPCAd/W5D4cinqsK8jbsRnH37fUuPExEROVvXDpf3NhbHQ="
)

// webClient is a browser: a cookie jar (by host and name, as HTTP::Cookies)
// and the CSRF token of its session
type webClient struct {
	cookies map[string]*http.Cookie
	csrf    string
}

type webHarness struct {
	srv     *web.Server
	clients map[string]*webClient
}

// gadsDir is the directory of the original GADS application (for its
// views), as the Perl runner uses
func (r *Runner) gadsDir() string {
	if d := os.Getenv("GADS_DIR"); d != "" {
		return d
	}
	abs, _ := filepath.Abs(r.file)
	return filepath.Join(filepath.Dir(abs), "..", "..", "upstream", "GADS-dev")
}

func (r *Runner) web() *webHarness {
	if r.webh != nil {
		return r.webh
	}
	// Loading the Perl web application sets the exception_action of the
	// schema, so later database errors are reported in full
	r.e.DB.FullErrors = true
	cfg := web.DefaultConfig()
	// As conformance/perl/webconf/config.yml
	cfg.Gads = map[string]any{
		"header":           "HEADER",
		"hostlocal":        int64(1),
		"dateformat":       "yyyy-MM-dd",
		"email_from":       `"Linkspace" <user@example.com>`,
		"aup":              int64(0),
		"login_instance":   int64(1),
		"default_instance": int64(1),
		"url":              "http://localhost/",
		"user_status":      int64(0),
	}
	if c, ok := r.scenario["config"].(map[string]any); ok {
		for k, v := range c {
			if i, ok := v.(int); ok {
				v = int64(i)
			}
			cfg.Gads[k] = v
		}
	}
	dir := r.gadsDir()
	srv := web.New(r.e, cfg, filepath.Join(dir, "views"), "")
	// ensure_site
	if n, _ := r.db.QueryInt64(`SELECT COUNT(*) FROM "site"`); n == 0 {
		r.db.InsertMap("site", map[string]any{"host": "localhost"})
	} else {
		r.db.Exec(`UPDATE "site" SET "host" = 'localhost' WHERE "host" IS NULL`)
	}
	r.webh = &webHarness{srv: srv, clients: map[string]*webClient{}}
	return r.webh
}

func (h *webHarness) client(name string) *webClient {
	if c, ok := h.clients[name]; ok {
		return c
	}
	c := &webClient{cookies: map[string]*http.Cookie{}}
	h.clients[name] = c
	return c
}

func uriEscapeUTF8(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.IndexByte("-_.~", ch) >= 0 {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// request performs a request (GADS::Conformance::Web->request)
func (r *Runner) webRequest(args map[string]any) map[string]any {
	h := r.web()
	as := str(args["as"])
	if as == "" {
		as = "anon"
	}
	client := h.client(as)
	method := strings.ToUpper(str(args["method"]))
	if method == "" {
		method = "GET"
	}
	host := str(args["host"])
	if host == "" {
		host = "localhost"
	}
	target := "http://" + host + str(args["path"])
	csrf := truthy(args["csrf"])
	q := map[string]any{}
	if aq, ok := args["query"].(map[string]any); ok {
		for k, v := range aq {
			q[k] = v
		}
	}
	// A JSON list cannot hold the CSRF token, which is then sent in the
	// query (as the dashboard JavaScript does)
	_, isList := args["json"].([]any)
	_, isRaw := args["body"].(string)
	if (isList || isRaw) && csrf {
		if _, has := q["csrf-token"]; !has {
			q["csrf-token"] = client.csrf
		}
	}
	if len(q) > 0 {
		keys := sortedKeysAny(q)
		var parts []string
		for _, k := range keys {
			// A list repeats the parameter
			if l, ok := q[k].([]any); ok {
				for _, x := range l {
					parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(str(x)))
				}
				continue
			}
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(str(q[k])))
		}
		target += "?" + strings.Join(parts, "&")
	}
	var body string
	var ctype string
	if uploads, ok := args["upload"].(map[string]any); ok {
		// A multipart form: the form values, the CSRF token and the files
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		form, _ := args["form"].(map[string]any)
		hasCSRF := false
		for _, k := range sortedKeysAny(form) {
			if k == "csrf_token" {
				hasCSRF = true
			}
			if l, ok := form[k].([]any); ok {
				for _, x := range l {
					mw.WriteField(k, str(x))
				}
			} else {
				mw.WriteField(k, str(form[k]))
			}
		}
		if csrf && !hasCSRF {
			mw.WriteField("csrf_token", client.csrf)
		}
		for _, name := range sortedKeysAny(uploads) {
			u, _ := uploads[name].(map[string]any)
			var content []byte
			if b, ok := u["content_b64"]; ok {
				content, _ = base64.StdEncoding.DecodeString(str(b))
			} else {
				content = []byte(str(u["content"]))
			}
			ct := str(u["content_type"])
			if ct == "" {
				ct = "application/octet-stream"
			}
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, name, str(u["filename"])))
			h.Set("Content-Type", ct)
			w, _ := mw.CreatePart(h)
			w.Write(content)
		}
		mw.Close()
		body = buf.String()
		ctype = mw.FormDataContentType()
	} else if raw, ok := args["body"].(string); ok {
		body = raw
	} else if j, ok := args["json"].(map[string]any); ok {
		if _, has := j["csrf_token"]; csrf && !has {
			j["csrf_token"] = client.csrf
		}
		b, _ := json.Marshal(j)
		body = string(b)
		ctype = "application/json"
	} else if l, ok := args["json"].([]any); ok {
		b, _ := json.Marshal(l)
		body = string(b)
		ctype = "application/json"
	} else if method == "POST" || method == "PUT" || args["form"] != nil {
		form, _ := args["form"].(map[string]any)
		var pairs []string
		hasCSRF := false
		for _, k := range sortedKeysAny(form) {
			if k == "csrf_token" {
				hasCSRF = true
			}
			switch v := form[k].(type) {
			case []any:
				for _, x := range v {
					pairs = append(pairs, uriEscapeUTF8(k)+"="+uriEscapeUTF8(str(x)))
				}
			default:
				pairs = append(pairs, uriEscapeUTF8(k)+"="+uriEscapeUTF8(str(v)))
			}
		}
		if csrf && !hasCSRF {
			pairs = append(pairs, "csrf_token="+uriEscapeUTF8(client.csrf))
		}
		body = strings.Join(pairs, "&")
		ctype = "application/x-www-form-urlencoded"
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if hd, ok := args["headers"].(map[string]any); ok {
		for k, v := range hd {
			req.Header.Set(k, str(v))
		}
	}
	host = strings.ToLower(host)
	var names []string
	for k := range client.cookies {
		if ch, _, _ := strings.Cut(k, "\x00"); ch == host {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		ck := client.cookies[k]
		// Secure cookies are not sent over http
		if ck.Secure && req.URL.Scheme != "https" {
			continue
		}
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	rec := httptest.NewRecorder()
	siteID := r.e.SiteID
	h.srv.ServeHTTP(rec, req)
	r.e.SiteID = siteID
	res := rec.Result()
	for _, ck := range res.Cookies() {
		if !ck.Expires.IsZero() && ck.Expires.Before(r.e.Now()) || ck.MaxAge < 0 {
			delete(client.cookies, host+"\x00"+ck.Name)
			continue
		}
		client.cookies[host+"\x00"+ck.Name] = ck
	}
	return r.webObserve(client, res, rec.Body.String(), truthy(args["no_body"]))
}

var (
	csrfDataRe    = regexp.MustCompile(`data-csrf="([^"]+)"`)
	csrfInputRe   = regexp.MustCompile(`name="csrf_token"\s+value="([^"]+)"`)
	localhostRe   = regexp.MustCompile(`^https?://localhost`)
	perlAtLineRe  = regexp.MustCompile(` at \S+ line \d+\.\n`)
	perlRefAddrRe = regexp.MustCompile(`\b(HASH|ARRAY|CODE)\(0x[0-9a-f]+\)`)
	samlRequestRe = regexp.MustCompile(`[?&]SAMLRequest=([^&]+)`)
	xmlStartTagRe = regexp.MustCompile(`<([\w:]+)((?:\s+[\w:]+="[^"]*")+)(\s*/?)>`)
	xmlAttrRe     = regexp.MustCompile(`[\w:]+="[^"]*"`)
)

// decodeSAMLRequest is the XML of a SAML request of a redirect (URL
// escaped, base64 and deflated), with the attributes of each element sorted
func decodeSAMLRequest(enc string) string {
	unesc, err := url.QueryUnescape(enc)
	if err != nil {
		return "invalid escaping: " + err.Error()
	}
	raw, err := base64.StdEncoding.DecodeString(unesc)
	if err != nil {
		return "invalid base64: " + err.Error()
	}
	xml, err := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return "invalid deflate: " + err.Error()
	}
	return xmlStartTagRe.ReplaceAllStringFunc(string(xml), func(tag string) string {
		m := xmlStartTagRe.FindStringSubmatch(tag)
		attrs := xmlAttrRe.FindAllString(m[2], -1)
		sort.Strings(attrs)
		return "<" + m[1] + " " + strings.Join(attrs, " ") + m[3] + ">"
	})
}

func (r *Runner) webObserve(client *webClient, res *http.Response, content string, noBody bool) map[string]any {
	ct := res.Header.Get("Content-Type")
	if m := csrfDataRe.FindStringSubmatch(content); m != nil {
		client.csrf = m[1]
	} else if m := csrfInputRe.FindStringSubmatch(content); m != nil {
		client.csrf = m[1]
	}
	obs := map[string]any{
		"status":       fmt.Sprint(res.StatusCode),
		"content_type": ct,
	}
	samlEncoded := ""
	if loc := res.Header.Get("Location"); loc != "" {
		loc = localhostRe.ReplaceAllString(loc, "")
		// A SAML request of a redirect is deflated XML whose attributes are
		// in the order of a Perl hash: it is observed decoded, with the
		// attributes of each element sorted
		if m := samlRequestRe.FindStringSubmatch(loc); m != nil {
			samlEncoded = m[1]
			obs["saml_request"] = decodeSAMLRequest(m[1])
			loc = strings.Replace(loc, "SAMLRequest="+m[1], "SAMLRequest={saml_request}", 1)
		}
		obs["location"] = loc
	}
	headers := map[string]any{}
	for _, hname := range []string{"X-Frame-Options", "Content-Security-Policy", "Content-Disposition"} {
		if v := res.Header.Get(hname); v != "" {
			headers[hname] = v
		}
	}
	if len(headers) > 0 {
		obs["headers"] = headers
	}
	var cookies []any
	var names []string
	for _, ck := range client.cookies {
		names = append(names, ck.Name)
	}
	sort.Strings(names)
	for _, n := range names {
		cookies = append(cookies, n)
	}
	if cookies == nil {
		cookies = []any{}
	}
	obs["cookies"] = cookies
	if strings.Contains(ct, "json") && content != "" {
		var data any
		if err := json.Unmarshal([]byte(content), &data); err == nil {
			obs["json"] = normaliseJSON(data, client)
		} else {
			obs["json"] = content
		}
	} else if !noBody {
		if client.csrf != "" {
			content = strings.ReplaceAll(content, client.csrf, "{csrf}")
		}
		if samlEncoded != "" {
			content = strings.ReplaceAll(content, samlEncoded, "{saml_request}")
		}
		// Remove the source locations of Perl error messages
		content = perlAtLineRe.ReplaceAllString(content, "")
		// Stringified references (a template printing a hash) are addresses
		content = perlRefAddrRe.ReplaceAllString(content, "$1(0x)")
		obs["body"] = content
	}
	return obs
}

func normaliseJSON(v any, client *webClient) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, val := range x {
			m[k] = normaliseJSON(val, client)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, val := range x {
			l[i] = normaliseJSON(val, client)
		}
		return l
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return nil
	case float64:
		return perlNum(x)
	case string:
		if client.csrf != "" {
			return strings.ReplaceAll(x, client.csrf, "{csrf}")
		}
		return x
	}
	return fmt.Sprint(v)
}

func perlNum(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprint(int64(f))
	}
	return fmt.Sprint(f)
}

func sortedKeysAny(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// stepHTTP makes a request to the web application
func (r *Runner) stepHTTP(args map[string]any) any {
	req := map[string]any{}
	for k, v := range args {
		req[k] = v
	}
	if args["query"] != nil {
		req["query"] = r.webValues(args["query"])
	}
	if args["form"] != nil {
		req["form"] = r.webValues(args["form"])
	}
	if args["json"] != nil {
		req["json"] = r.webValues(args["json"])
	}
	if args["headers"] != nil {
		req["headers"] = r.webValues(args["headers"])
	}
	req["path"] = r.webPath(str(args["path"]))
	if _, ok := args["csrf"]; !ok {
		req["csrf"] = true
	}
	delete(req, "capture")
	obs := r.webRequest(req)
	// Remember values of the JSON response for later steps ({var:<name>})
	if capture, ok := args["capture"].(map[string]any); ok {
		j, _ := obs["json"].(map[string]any)
		for _, name := range sortedKeysAny(capture) {
			// A missing value leaves the variable unset (using it later fails)
			if v, ok := j[str(capture[name])].(string); ok && v != "" {
				if r.webVars == nil {
					r.webVars = map[string]string{}
				}
				r.webVars[name] = v
			}
		}
	}
	return r.webUnvar(obs)
}

// webUnvar replaces the values of captured variables (e.g. random OAuth
// tokens) in an observation by their references
func (r *Runner) webUnvar(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, val := range x {
			m[k] = r.webUnvar(val)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, val := range x {
			l[i] = r.webUnvar(val)
		}
		return l
	case []string:
		l := make([]string, len(x))
		for i, val := range x {
			l[i] = r.webUnvar(val).(string)
		}
		return l
	case string:
		names := make([]string, 0, len(r.webVars))
		for n := range r.webVars {
			names = append(names, n)
		}
		// Longest values first, as Perl
		sort.Slice(names, func(i, j int) bool {
			a, b := r.webVars[names[i]], r.webVars[names[j]]
			if len(a) != len(b) {
				return len(a) > len(b)
			}
			return names[i] < names[j]
		})
		for _, n := range names {
			x = strings.ReplaceAll(x, r.webVars[n], "{var:"+n+"}")
		}
		return x
	}
	return v
}

// stepLogin logs in to the web application as a user
func (r *Runner) stepLogin(args map[string]any) any {
	r.web()
	user := r.user(args["user"])
	r.db.UpdateMap("user", map[string]any{"password": webPasswordHash, "pwchanged": r.e.Now()}, map[string]any{"id": user.ID})
	as := str(args["as"])
	if as == "" {
		as = str(args["user"])
	}
	r.webRequest(map[string]any{"as": as, "method": "GET", "path": "/login"})
	path := str(args["path"])
	if path == "" {
		path = "/login"
	}
	pw := webPassword
	if p, ok := args["password"]; ok {
		pw = str(p)
	}
	return r.webRequest(map[string]any{
		"as":     as,
		"method": "POST",
		"path":   r.webPath(path),
		"csrf":   true,
		"form": map[string]any{
			"username": user.Username,
			"password": pw,
			"signin":   int64(1),
		},
		"no_body": true,
	})
}

var webRefRe = regexp.MustCompile(`\{(\w+):([^}]+)\}`)

func (r *Runner) webPath(p string) string {
	return webRefRe.ReplaceAllStringFunc(p, func(m string) string {
		sm := webRefRe.FindStringSubmatch(m)
		return r.webRef(sm[1], sm[2])
	})
}

func (r *Runner) webRef(typ, ref string) string {
	switch typ {
	case "user":
		return fmt.Sprint(r.user(ref).ID)
	case "record":
		return fmt.Sprint(r.records[ref])
	case "view":
		return fmt.Sprint(r.views[ref])
	case "var":
		v, ok := r.webVars[ref]
		if !ok {
			gads.Fail("Unknown variable %s", ref)
		}
		return v
	case "sheet":
		return r.sheet(ref).Layout().Identifier()
	case "instance":
		return fmt.Sprint(r.sheet(ref).Layout().InstanceID)
	case "totp":
		// The current OTP token of an MFA secret
		return r.e.TOTP(ref)
	case "column":
		// <sheet alias>:<name> is a column of that sheet
		if i := strings.Index(ref, ":"); i >= 0 {
			return fmt.Sprint(r.col(r.sheet(ref[:i]).Layout(), ref[i+1:]).ID)
		}
		return fmt.Sprint(r.col(r.sheet(nil).Layout(), ref).ID)
	}
	gads.Fail("Unknown reference type %s", typ)
	return ""
}

func (r *Runner) webValues(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		// Keys may also hold references (e.g. column IDs as parameter names)
		for k, val := range x {
			m[r.webPath(k)] = r.webValues(val)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, val := range x {
			l[i] = r.webValues(val)
		}
		return l
	case nil:
		return nil
	case string:
		return r.webPath(x)
	}
	return v
}

var _ = db.Q
