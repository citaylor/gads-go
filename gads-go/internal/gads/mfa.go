package gads

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- TOTP (RFC 6238) and Yubikey OTPs are HMAC-SHA1
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/ctrlo/gads-go/internal/db"
)

// The multi-factor authentication of a user (Result::User, "All the
// following for MFA")

// base32 of Convert::Base32: lower case without padding
var mfaBase32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

var reMobileValid = regexp.MustCompile(`^\+[0-9]{4,}$`)

// ValidateMobile is Result::User validate_mobile
func ValidateMobile(mobile string) bool { return reMobileValid.MatchString(mobile) }

// validateMFAFields is the mobile and MFA type checks of Result::User
// validate, for the values of a row
func validateMFAFields(row map[string]any) {
	if mobile := anyToString(row["mobile"]); truthy(row["mobile"]) && !ValidateMobile(mobile) {
		Fail("The mobile number %s is invalid. Please enter as international format (e.g. +1444555666)", mobile)
	}
	if t := anyToString(row["mfa_type"]); truthy(row["mfa_type"]) && !regexp.MustCompile(`^(otp|yub|sms)$`).MatchString(t) {
		Fail("Invalid MFA type: %s", t)
	}
}

// reload refreshes the user's row
func (u *User) reload() {
	if row := u.e.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, u.ID); row != nil {
		u.Row = row
	}
}

// updateMFA updates fields of the user's row ($user->update)
func (u *User) updateMFA(m map[string]any) {
	row := map[string]any{}
	for k, v := range u.Row {
		row[k] = v
	}
	if u.dirty == nil {
		u.dirty = map[string]any{}
	}
	for k, v := range m {
		u.dirty[k] = v
	}
	for k, v := range u.dirty {
		row[k] = v
	}
	validateMFAFields(row)
	u.e.DB.UpdateMap("user", u.dirty, map[string]any{"id": u.ID})
	u.dirty = nil
	u.reload()
}

// UpdateSessionSettings is the update of the user's session settings after
// rendering a page, validating as any update of the user (including any
// values left from a failed update).
func (u *User) UpdateSessionSettings(settings string) {
	if u.dirty != nil {
		u.updateMFA(map[string]any{"session_settings": settings})
		return
	}
	u.e.DB.UpdateMap("user", map[string]any{"session_settings": settings}, map[string]any{"id": u.ID})
}

func (u *User) rowStr(f string) string {
	s, _ := db.AsString(u.Row[f])
	return s
}

func (u *User) rowTime(f string) (time.Time, bool) {
	if u.Row[f] == nil {
		return time.Time{}, false
	}
	return db.AsTime(u.Row[f])
}

// site is the user's site
func (u *User) site() *Site {
	if id, ok := db.AsInt64(u.Row["site_id"]); ok && u.Row["site_id"] != nil {
		if row := u.e.DB.QueryRow(`SELECT * FROM "site" WHERE "id" = ?`, id); row != nil {
			return &Site{row: row}
		}
	}
	return u.e.Site()
}

// Mobile is the user's mobile number
func (u *User) Mobile() string { return u.rowStr("mobile") }

// MFAFailcount is the number of successive failed MFA attempts
func (u *User) MFAFailcount() int64 {
	n, _ := db.AsInt64(u.Row["mfa_failcount"])
	return n
}

// MFALastfail is the time of the last failed MFA attempt
func (u *User) MFALastfail() (time.Time, bool) { return u.rowTime("mfa_lastfail") }

// MFATypeEffective is mfa_type_effective: the site's forced MFA if
// applicable, otherwise the user's chosen MFA (which may be blank if the
// forced type is "any")
func (u *User) MFATypeEffective() string {
	if force := u.site().str("force_mfa"); force != "" && force != "0" && force != "any" {
		return force
	}
	return u.rowStr("mfa_type")
}

// NeedMFA is need_mfa
func (u *User) NeedMFA() bool {
	force := u.site().str("force_mfa")
	return (force != "" && force != "0") || truthy(u.MFATypeEffective())
}

// NeedMFASetup is need_mfa_setup
func (u *User) NeedMFASetup() bool {
	t := u.MFATypeEffective()
	if !truthy(t) {
		return true
	}
	if (t == "otp" || t == "yub") && truthy(u.Row["mfa_secret"]) {
		return false
	}
	if t == "sms" && truthy(u.Row["mobile"]) && truthy(u.Row["mobile_verified"]) {
		return false
	}
	return true
}

// SeedKey is seed_key: a random base32 key of 26 bytes
func (u *User) SeedKey() string {
	b := make([]byte, 26)
	if _, err := rand.Read(b); err != nil {
		Panic("Cannot read 26 from /dev/urandom")
	}
	return mfaBase32.EncodeToString(b)
}

// KeyQRBase64 is key_qr_base64: the otpauth URI of the key as a base64
// PNG QR code
func (u *User) KeyQRBase64(key string) string {
	issuer := "LinkSpace"
	if name := u.site().str("name"); name != "" {
		issuer += " – " + name
	}
	if key == "" {
		key = u.rowStr("mfa_secret")
	}
	uri := "otpauth://totp/" + uriEscape(u.Username) + "?secret=" + uriEscape(key) + "&issuer=" + issuer
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		Panic("Failed to write QR image")
	}
	code.Scale = 10
	return base64.StdEncoding.EncodeToString(code.PNG()) + "\n"
}

// totp is Authen::OATH totp (SHA1, 6 digits, 30 second steps)
func totp(secret []byte, now time.Time) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(now.Unix()/30)) // #nosec G115 -- the time step is positive
	h := hmac.New(sha1.New, secret)
	h.Write(msg[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 0xf
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1000000)
}

// TOTP is the current one-time password of a (base32) OTP secret, as an
// authenticator app would give it.
func (e *Engine) TOTP(key string) string {
	b, _ := mfaBase32.DecodeString(strings.ToLower(strings.TrimRight(key, "=")))
	return totp(b, e.Now())
}

// CheckToken is check_token: whether a submitted token is valid for the
// user's MFA (secret is a key not yet saved, for OTP setup)
func (u *User) CheckToken(token, secret string) bool {
	switch t := u.MFATypeEffective(); t {
	case "sms":
		if !truthy(u.Row["mfa_sms_token"]) {
			return false
		}
		if created, ok := u.rowTime("mfa_sms_created"); !ok || created.Before(u.e.Now().Add(-15*time.Minute)) {
			return false
		}
		return token == u.rowStr("mfa_sms_token")
	case "yub":
		if !truthy(u.Row["mfa_secret"]) {
			return false
		}
		return u.GetYubikey(token) == u.rowStr("mfa_secret")
	case "otp":
		key := u.rowStr("mfa_secret")
		if key == "" {
			key = secret
		}
		b, _ := mfaBase32.DecodeString(strings.ToLower(strings.TrimRight(key, "=")))
		return totp(b, u.e.Now()) == token
	default:
		Panic("Unknown MFA type %s", t)
	}
	return false
}

// RecentMFA is recent_mfa: whether the user has verified MFA in the last
// 7 days with the key of their MFATOKEN cookie
func (u *User) RecentMFA(keyFromCookie string) bool {
	used, ok := u.rowTime("mfa_token_previous_used")
	if keyFromCookie == "" || !ok || keyFromCookie != u.rowStr("mfa_token_previous_key") ||
		u.MFATypeEffective() != u.rowStr("mfa_token_previous_type") {
		return false
	}
	return used.After(u.e.Now().AddDate(0, 0, -7))
}

// SendMFASMS is send_mfa_sms: a new SMS token sent to the user's mobile
func (u *User) SendMFASMS() {
	b := make([]byte, 6)
	rand.Read(b)
	code := make([]byte, 6)
	for i := range b {
		code[i] = '0' + b[i]%10
	}
	u.updateMFA(map[string]any{"mfa_sms_token": string(code), "mfa_sms_created": u.e.Now()})
	u.e.SendSMS(u.Mobile(), "“"+string(code)+"” is your LinkSpace access code")
}

// NeedMobileVerification is need_mobile_verification (which sends the
// verification SMS)
func (u *User) NeedMobileVerification() bool {
	if m := u.Mobile(); m != "" && ValidateMobile(m) && !truthy(u.Row["mobile_verified"]) {
		u.SendMFASMS()
		return true
	}
	return false
}

// VerifyMobile is verify_mobile
func (u *User) VerifyMobile(token string) bool {
	if token == u.rowStr("mfa_sms_token") {
		u.updateMFA(map[string]any{"mobile_verified": 1})
		return true
	}
	u.updateMFA(map[string]any{"mobile": nil})
	return false
}

// UpdateMFA is $user->update with MFA related fields (as the MFA pages)
func (u *User) UpdateMFA(m map[string]any) { u.updateMFA(m) }

// ResetMFA is reset_mfa
func (u *User) ResetMFA() {
	u.updateMFA(map[string]any{
		"mobile": nil, "mfa_secret": nil, "mfa_sms_token": nil, "mfa_sms_created": nil,
		"mfa_token_previous": nil, "mfa_token_previous_type": nil, "mfa_token_previous_used": nil,
		"mfa_token_previous_key": nil, "mfa_failcount": 0,
	})
}

// mfaHTTP is the client of the Yubikey and SMS web services
var mfaHTTP = &http.Client{Timeout: 10 * time.Second}

// GetYubikey is get_yubikey: the Yubikey ID of a valid OTP
// (Auth::Yubikey_WebClient against the Yubico web API)
func (u *User) GetYubikey(otp string) string {
	if otp == "" || otp == "0" {
		return ""
	}
	y := u.e.Config.Yubi
	if y == nil {
		Panic("Yubikey MFA is not configured")
	}
	if y["id"] == "" {
		Panic("Yubikey API ID is not defined")
	}
	if y["key"] == "" {
		Panic("Yubikey API ID is not defined")
	}
	nonce := u.e.NewToken(32)
	yubiID := otp
	if len(yubiID) > 12 {
		yubiID = yubiID[:12]
	}
	if yubikeyWebclient(otp, y["id"], y["key"], nonce, y["url"]) == "OK" {
		return yubiID
	}
	return ""
}

func yubikeyWebclient(otp, id, api, nonce, endpoint string) string {
	if endpoint == "" {
		endpoint = "https://api2.yubico.com/wsapi/2.0/verify"
	}
	if len(api)%4 != 0 {
		Panic("Your API key must be in 4 byte lengths")
	}
	otp = strings.TrimSuffix(otp, "\n")
	if !regexp.MustCompile(`(?i)[cbdefghijklnrtuv]`).MatchString(otp) || len(otp) < 32 {
		return "ERR_BAD_OTP"
	}
	key, _ := base64.StdEncoding.DecodeString(api)
	sign := func(s string) string {
		h := hmac.New(sha1.New, key)
		h.Write([]byte(s))
		return base64.StdEncoding.EncodeToString(h.Sum(nil))
	}
	params := "id=" + id + "&nonce=" + nonce + "&otp=" + uriEscape(otp) + "&timestamp=1"
	params += "&h=" + uriEscape(sign(params))
	var response string
	if res, err := mfaHTTP.Get(endpoint + "?" + params); err == nil {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode < 300 {
			response = string(b)
		}
	}
	response = strings.TrimSuffix(response, "\n")
	if !regexp.MustCompile(`(?i)status=ok`).MatchString(response) {
		status := ""
		if m := regexp.MustCompile(`status=(.+)`).FindStringSubmatch(response); m != nil {
			status = m[1]
		}
		return "ERR_" + regexp.MustCompile(`\s`).ReplaceAllString(status, "")
	}
	result := map[string]string{}
	for _, l := range strings.Split(response, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSuffix(l, "\n"), "="); ok {
			result[k] = regexp.MustCompile(`\s`).ReplaceAllString(v, "")
		}
	}
	sig := result["h"]
	delete(result, "h")
	var keys []string
	for k := range result {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+result[k])
	}
	if nonce != result["nonce"] || otp != result["otp"] {
		return "ERR_MSG_AUTH"
	}
	if sign(strings.Join(parts, "&")) == sig {
		return "OK"
	}
	return "ERR_HMAC"
}

// SendSMS is Result::User send_sms: a message sent through the BulkSMS
// JSON API
func (e *Engine) SendSMS(to, body string) {
	c := e.Config.SMS
	if c == nil {
		Panic("SMS not configured")
	}
	for _, f := range []struct{ key, name string }{{"username", "SMS username"}, {"password", "SMS password"}, {"from", "SMS sender configuration"}} {
		if c[f.key] == "" {
			Panic("%s not defined", f.name)
		}
	}
	endpoint := c["url"]
	if endpoint == "" {
		endpoint = "https://api.bulksms.com/v1/messages"
	}
	payload, _ := json.Marshal(map[string]string{"from": c["from"], "to": to, "body": body, "encoding": "UNICODE"})
	req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c["username"], c["password"])
	var ret any
	if res, err := mfaHTTP.Do(req); err == nil {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if json.Unmarshal(b, &ret) != nil {
			ret = nil
		}
	}
	switch r := ret.(type) {
	case nil:
		Panic("Failed to send SMS message - unknown response")
	case map[string]any:
		Panic("Failed to send SMS message: %s (%s)", perlFmt(r["title"]), perlFmt(r["detail"]))
	case []any:
		if len(r) > 0 {
			if m, ok := r[0].(map[string]any); ok {
				if st, ok := m["status"].(map[string]any); ok && st["type"] == "ACCEPTED" {
					return
				}
			}
		}
	}
	Panic("Failed to send SMS message - unknown reason")
}

// MFATokenPreviousUsed is the time the previous MFA token was used
func (u *User) MFATokenPreviousUsed() (time.Time, bool) { return u.rowTime("mfa_token_previous_used") }
