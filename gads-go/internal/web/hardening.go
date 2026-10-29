package web

// Hardened mode (gads.Config.Hardened): the fixes of the security findings
// of GADS (GADS-security-report-verified.md) in the web layer. Each is a
// deliberate deviation from Perl; without Hardened the server behaves as
// Perl GADS does.

import (
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/microcosm-cc/bluemonday"
)

// noticePolicy is the HTML allowed in a notice widget: formatting, links,
// images and tables, without scripts, event handlers or styles (H-5)
var noticePolicy = bluemonday.UGCPolicy().AllowAttrs("class").Globally()

// safeMethod is whether a request method may not change anything
func safeMethod(m string) bool { return m == "GET" || m == "HEAD" || m == "OPTIONS" }

// readOnlyWrites are the writes that GADS makes when only showing a page:
// the audit log, the user's session settings, and the defaults it creates
// on first use (dashboards, RAG settings, submission tokens, the OAuth
// client) or clears when expired (API tokens)
var readOnlyWrites = []*regexp.Regexp{
	regexp.MustCompile(`^(?i)INSERT\s+INTO\s+"?(audit|dashboard|widget|instance_rag|submission|oauthclient)"?\s`),
	regexp.MustCompile(`^(?i)UPDATE\s+"?user"?\s+SET\s+"?session_settings"?\s*=\s*\?\s+WHERE\s+"?id"?\s*=\s*\?$`),
	regexp.MustCompile(`^(?i)DELETE\s+FROM\s+"?oauthtoken"?\s`),
}

// errReadOnly is the error of a change requested without a form
// submission
const errReadOnly = "This change can only be made by submitting a form. Please go back and try again."

// readOnly makes the request unable to change data (H-1, M-9): a GET
// request that asks for a change, as any link on another site can, is
// refused rather than carried out without a CSRF token. It returns the
// function that ends it.
func (s *Server) readOnly() func() {
	e := s.E
	check, emailer := e.DB.WriteCheck, e.Emailer
	e.DB.WriteCheck = func(table, q string) {
		q = strings.Join(strings.Fields(q), " ")
		for _, re := range readOnlyWrites {
			if re.MatchString(q) {
				return
			}
		}
		gads.Fail(errReadOnly)
	}
	e.Emailer = func(gads.Email) { gads.Fail(errReadOnly) }
	return func() { e.DB.WriteCheck, e.Emailer = check, emailer }
}

// securityHeaders are sent with every response (M-8, H-5)
func (c *Ctx) securityHeaders() {
	c.Header("Content-Security-Policy", "script-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'self'")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	if c.s.Config.Session.Secure {
		c.Header("Strict-Transport-Security", "max-age=31536000")
	}
}

// maxPageRows is the largest page of records or users in hardened mode
// (M-7): Perl serves any number asked for, all built in memory
const maxPageRows = 1000

// clampRows limits the size of a page asked for (a negative size would
// be all of them)
func (s *Server) clampRows(n int64) int64 {
	if s.E.Config.Hardened && (n > maxPageRows || n < 0) {
		return maxPageRows
	}
	return n
}

// Rate limit of the public endpoints that send email or check passwords
// (M-7, M-9): requests per client address in each window
const (
	rateLimit  = 20
	rateWindow = time.Minute
)

// rateLimited are the endpoints limited (the posts to them)
var rateLimited = regexp.MustCompile(`^/(login|register|resetpw(/.*)?|api/token)$`)

type rateLimiter struct {
	// limit is the number of requests allowed in a window (0: rateLimit)
	limit  int
	mu     sync.Mutex
	start  time.Time
	counts map[string]int
}

// allow counts a request from addr and is whether it is within the limit
func (r *rateLimiter) allow(addr string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now.Sub(r.start) >= rateWindow || r.counts == nil {
		r.start, r.counts = now, map[string]int{}
	}
	r.counts[addr]++
	limit := r.limit
	if limit == 0 {
		limit = rateLimit
	}
	return r.counts[addr] <= limit
}

// clientAddr is the address of the client (the one the proxy reports,
// behind a proxy)
func (s *Server) clientAddr(req *http.Request) string {
	if s.Config.BehindProxy {
		if f := req.Header.Get("X-Forwarded-For"); f != "" {
			parts := strings.Split(f, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

// rateLimit is whether a request is refused for too many attempts
func (s *Server) rateLimit(req *http.Request) bool {
	if s.E == nil || !s.E.Config.Hardened || safeMethod(req.Method) || !rateLimited.MatchString(req.URL.Path) {
		return false
	}
	return !s.limiter.allow(s.clientAddr(req), time.Now())
}
