package web

import (
	"net/http"
	"regexp"
	"time"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

var mfaExemptRe = regexp.MustCompile(`^/(mfa|logout)$`)

// mfaBeforeHook is the before hook of GADS::MFA: a logged in user needing
// MFA is sent to /mfa unless recently verified, and the recent MFA cookie
// is refreshed
func (s *Server) mfaBeforeHook(c *Ctx) {
	user := c.LoggedInUser()
	if user == nil {
		return
	}
	mfaCookie, has := c.Cookie("MFATOKEN")
	if !mfaExemptRe.MatchString(c.URI()) && user.NeedMFA() && !user.RecentMFA(mfaCookie) {
		c.Redirect("/mfa")
	}
	if has {
		c.refreshRecentMFACookie(mfaCookie)
	}
}

// refreshRecentMFACookie is _refresh_recent_mfa_cookie
func (c *Ctx) refreshRecentMFACookie(key string) {
	c.SetCookie(&http.Cookie{Name: "MFATOKEN", Value: key, Path: "/", Secure: true, HttpOnly: true, // #nosec G124 -- Secure and HttpOnly; SameSite is the browser's default, as Perl
		Expires: c.E.Now().Add(48 * time.Hour)})
}

// any '/mfa' (GADS/MFA.pm)
func (s *Server) hMFA(c *Ctx) string {
	user := c.LoggedInUser()
	if !user.NeedMFA() {
		c.Redirect("/")
	}
	params := tt.NewHash()
	body := func(k string) string { v, _ := c.BodyParam(k); return v }
	if user.NeedMFASetup() {
		// Does user need to choose their MFA?
		if !truthy(user.MFATypeEffective()) {
			if submitted := body("choose_mfa"); truthy(submitted) {
				user.UpdateMFA(map[string]any{"mfa_type": submitted})
			} else {
				params.Set("choose_mfa", int64(1))
			}
		}
		if t := user.MFATypeEffective(); truthy(t) {
			switch t {
			case "otp":
				// The key is kept in the session until the user has
				// generated a correct token from it
				key, _ := c.SessionGet("otp_key").(string)
				if !truthy(key) {
					key = user.SeedKey()
				}
				c.SessionSet("otp_key", key)
				if submitted := body("get_key"); truthy(submitted) {
					if user.CheckToken(submitted, key) {
						user.UpdateMFA(map[string]any{"mfa_secret": key})
						c.Success("Key has been added successfully")
						c.Redirect("/mfa/")
					}
					gads.Fail("Incorrect key submitted")
				}
				params.Set("qr", user.KeyQRBase64(key))
				params.Set("key", key)
				params.Set("setup_mfa", "otp")
			case "yub":
				if submitted := body("get_key"); truthy(submitted) {
					if id := user.GetYubikey(submitted); truthy(id) {
						user.UpdateMFA(map[string]any{"mfa_secret": id})
						c.Success("Key has been added successfully")
						c.Redirect("/mfa/")
					}
					gads.Fail("Incorrect key submitted")
				}
				params.Set("setup_mfa", "yub")
			case "sms":
				if truthy(body("mobile")) {
					mobile := body("mobile")
					if c.Process(func() { user.UpdateMFA(map[string]any{"mobile": mobile}) }) {
						c.Success("Mobile number has been added successfully")
						c.Redirect("/mfa/")
					}
				} else if truthy(body("sms-not-received")) {
					c.Notice("Please re-enter your mobile number and try again")
					user.UpdateMFA(map[string]any{"mobile": nil})
					c.Redirect("/mfa/")
				} else if token := body("token"); truthy(token) {
					if user.VerifyMobile(token) {
						c.Success("Mobile number has been verified successfully")
						// The token is also a valid MFA validation
						s.mfaTokenSuccess(c, user, token)
					}
					c.ReportError("The token entered was not valid. Please re-enter your mobile number.")
					c.Redirect("/mfa/")
				}
				params.Set("setup_mfa", "sms")
			default:
				gads.Panic("Unexpected MFA type: %s", t)
			}
		}
	} else {
		// Ask user to authenticate
		if token := body("token"); truthy(token) {
			lastfail, _ := user.MFALastfail()
			if user.MFAFailcount() > 5 && lastfail.Add(15*time.Minute).After(s.E.Now()) {
				gads.Fail("Multi-factor authentication is currently unavailable, please try again shortly.")
			} else if user.CheckToken(token, "") {
				s.mfaTokenSuccess(c, user, token)
			} else {
				c.Warning("Incorrect or invalid token entered")
				user.UpdateMFA(map[string]any{
					"mfa_failcount": user.MFAFailcount() + 1,
					"mfa_lastfail":  s.E.Now(),
				})
			}
		}
		if user.MFATypeEffective() == "sms" {
			user.SendMFASMS()
		}
		params.Set("mfa_type", user.MFATypeEffective())
		params.Set("get_code", int64(1))
	}
	return c.Template("mfa", params, nil)
}

// mfaTokenSuccess is _mfa_token_success: the token recorded, with a random
// key in both a cookie and the database to allow it to be reused
func (s *Server) mfaTokenSuccess(c *Ctx, user *gads.User, token string) {
	if prev := user.Row["mfa_token_previous"]; truthy(prev) && fmtAny(prev) == token {
		if used, ok := user.MFATokenPreviousUsed(); ok && used.Add(5*time.Minute).After(s.E.Now()) {
			c.Warning("The authentication token has already been used in the last 5 minutes, possibly an attacker?")
		}
	}
	key := s.E.NewToken(32)
	user.UpdateMFA(map[string]any{
		"mfa_failcount":           0,
		"mfa_lastfail":            nil,
		"mfa_token_previous":      token,
		"mfa_token_previous_type": user.MFATypeEffective(),
		"mfa_token_previous_used": s.E.Now(),
		"mfa_token_previous_key":  key,
	})
	c.refreshRecentMFACookie(key)
	c.Redirect("/")
}
