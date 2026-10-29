package web

import (
	"crypto/md5" // #nosec G501 -- checks the legacy Crypt::SaltedHash formats of Perl GADS
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- checks the legacy Crypt::SaltedHash formats of Perl GADS
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"hash"
	"regexp"
	"strconv"
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
	if scheme == pbkdf2Scheme {
		return matchPBKDF2(given, m[2])
	}
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

// Hardened password hashes (M-4): PBKDF2 with SHA-512, a 16 byte salt,
// pbkdf2Iterations and a 32 byte key, as
// {PBKDF2-SHA512}ITERATIONS$SALT$KEY (base64): 91 characters, within the
// 128 of the password column. Perl GADS cannot check them.
const (
	pbkdf2Scheme     = "PBKDF2-SHA512"
	pbkdf2Iterations = 210000
	pbkdf2KeyLen     = 32
)

// hashPasswordPBKDF2 is the hardened hash of a password
func hashPasswordPBKDF2(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key, err := pbkdf2.Key(sha512.New, pw, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		gads.Panic("%s", err.Error())
	}
	return fmt.Sprintf("{%s}%d$%s$%s", pbkdf2Scheme, pbkdf2Iterations,
		base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(key))
}

// matchPBKDF2 checks a password against the part of a PBKDF2 hash after
// its scheme
func matchPBKDF2(given, h string) bool {
	parts := strings.Split(h, "$")
	if len(parts) != 3 {
		return false
	}
	iter, err := strconv.Atoi(parts[0])
	if err != nil || iter < 1 || iter > 10_000_000 {
		return false
	}
	salt, err1 := base64.StdEncoding.DecodeString(parts[1])
	want, err2 := base64.StdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	key, err := pbkdf2.Key(sha512.New, given, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(key, want) == 1
}

// hashPassword hashes a new password: as Perl GADS does, or with PBKDF2 in
// hardened mode (unless the database is shared with Perl)
func (s *Server) hashPassword(pw string) string {
	if cfg := s.E.Config; cfg.Hardened && !cfg.PerlPasswordHashes {
		return hashPasswordPBKDF2(pw)
	}
	return HashPassword(pw)
}

// upgradePassword rehashes a password that has just been checked, if it
// is not stored with the hash of hashPassword
func (s *Server) upgradePassword(userID int64, pw, stored string) {
	if cfg := s.E.Config; !cfg.Hardened || cfg.PerlPasswordHashes ||
		strings.HasPrefix(stored, "{"+pbkdf2Scheme+"}") {
		return
	}
	s.E.DB.UpdateMap("user", map[string]any{"password": hashPasswordPBKDF2(pw)}, map[string]any{"id": userID})
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
	c.s.upgradePassword(user.ID, password, pw)
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
