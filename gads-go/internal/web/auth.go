package web

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"hash"
	"regexp"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// The Dancer2::Plugin::Auth::Extensible functions, with the DBIC provider
// as configured for GADS (user_as_object, password_expiry_days, lastlogin
// recorded).

// LoggedInUser is logged_in_user (cached for the request)
func (c *Ctx) LoggedInUser() *gads.User {
	if c.userLoaded {
		return c.user
	}
	username, _ := c.SessionGet("logged_in_user").(string)
	if username == "" {
		return nil
	}
	c.user = c.E.FindActiveUser(username)
	c.userLoaded = true
	return c.user
}

// clearUserCache forgets the cached logged in user
func (c *Ctx) clearUserCache() { c.user, c.userLoaded = nil, false }

var schemeRe = regexp.MustCompile(`^\{([^}]+)\}(.*)$`)

// MatchPassword is match_password: a hashed password ({SSHA512}...) is
// validated with Crypt::SaltedHash (4 byte salt), otherwise compared.
func MatchPassword(given, correct string) bool {
	if correct == "" {
		return false
	}
	m := schemeRe.FindStringSubmatch(correct)
	if m == nil || !strings.HasPrefix(correct, "{") {
		return subtle.ConstantTimeCompare([]byte(given), []byte(correct)) == 1
	}
	scheme := strings.ToUpper(m[1])
	salted := strings.HasPrefix(scheme, "S")
	name := scheme
	if salted {
		name = scheme[1:]
	}
	var h func() hash.Hash
	switch name {
	case "SHA", "SHA1":
		h = sha1.New
	case "SHA256":
		h = sha256.New
	case "SHA384":
		h = sha512.New384
	case "SHA512":
		h = sha512.New
	case "MD5":
		h = md5.New
	default:
		return false
	}
	bin, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return false
	}
	saltLen := 4
	if len(bin) < saltLen {
		return false
	}
	salt := bin[len(bin)-saltLen:]
	d := h()
	d.Write([]byte(given))
	d.Write(salt)
	gen := append(d.Sum(nil), salt...)
	return subtle.ConstantTimeCompare(gen, bin) == 1
}

// HashPassword hashes a password as Crypt::SaltedHash with SHA-512 and a
// random 4 byte salt
func HashPassword(pw string) string {
	salt := make([]byte, 4)
	rand.Read(salt)
	d := sha512.New()
	d.Write([]byte(pw))
	d.Write(salt)
	return "{SSHA512}" + base64.StdEncoding.EncodeToString(append(d.Sum(nil), salt...))
}

// authenticateUser is authenticate_user: returns whether the username and
// password are valid, recording the last login.
func (c *Ctx) authenticateUser(username, password string) bool {
	if username == "" || password == "" {
		return false
	}
	user := c.E.FindActiveUser(username)
	if user == nil {
		return false
	}
	pw, _ := db.AsString(user.Row["password"])
	if !MatchPassword(password, pw) {
		return false
	}
	if ll, ok := db.AsTime(user.Row["lastlogin"]); ok && user.Row["lastlogin"] != nil {
		c.SessionSet("logged_in_user_lastlogin", ll.Unix())
	}
	c.E.DB.UpdateMap("user", map[string]any{"lastlogin": c.E.Now()}, map[string]any{"id": user.ID})
	return true
}

// passwordExpired is logged_in_user_password_expired
func (c *Ctx) passwordExpired() bool {
	user := c.LoggedInUser()
	if user == nil {
		return false
	}
	days := c.s.Config.PasswordExpiryDays
	if days == 0 {
		return false
	}
	changed, ok := db.AsTime(user.Row["pwchanged"])
	if !ok || user.Row["pwchanged"] == nil {
		return true
	}
	return c.E.Now().Sub(changed) > time.Duration(days)*24*time.Hour
}

// RequireLogin wraps a route with require_login: users that are not
// logged in are redirected to the login page with a return URL.
func RequireLogin(h Handler) Handler {
	return func(c *Ctx) string {
		if c.LoggedInUser() == nil {
			forward := c.Path()
			if q := c.QueryString(); q != "" {
				forward += "?" + q
			}
			c.Redirect(c.URIFor("/login", map[string][]string{"return_url": {forward}}))
		}
		return h(c)
	}
}

// RequireAnyRole wraps a route with require_any_role
func RequireAnyRole(roles []string, h Handler) Handler {
	return func(c *Ctx) string {
		user := c.LoggedInUser()
		if user == nil {
			return RequireLogin(h)(c)
		}
		perms := user.Permission()
		for _, r := range roles {
			if perms[r] {
				return h(c)
			}
		}
		c.Status(403)
		return c.Template("403", nil, nil)
	}
}

// RequireRole wraps a route with require_role
func RequireRole(role string, h Handler) Handler { return RequireAnyRole([]string{role}, h) }

// requireAPIUser is require_api_user (API.pm)
func requireAPIUser(h Handler) Handler {
	return func(c *Ctx) string {
		if c.Var("api_user") != nil {
			return h(c)
		}
		c.Status(403)
		gads.Fail("Authentication needed to access this resource")
		return ""
	}
}
