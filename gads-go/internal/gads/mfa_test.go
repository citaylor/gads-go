package gads

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestTOTP(t *testing.T) {
	// RFC 6238 SHA1 vectors (last 6 of the 8 digits)
	secret := []byte("12345678901234567890")
	for _, c := range []struct {
		t    int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		if got := totp(secret, time.Unix(c.t, 0)); got != c.want {
			t.Errorf("totp(%d) = %s, want %s", c.t, got, c.want)
		}
	}
	// Convert::Base32 is lower case without padding
	if got := mfaBase32.EncodeToString([]byte("foobar")); got != "mzxw6ytboi" {
		t.Errorf("base32 = %s", got)
	}
}

func TestValidateMobile(t *testing.T) {
	for m, want := range map[string]bool{"+4471234": true, "+123": false, "07123456789": false, "+44 712": false} {
		if ValidateMobile(m) != want {
			t.Errorf("ValidateMobile(%q) != %v", m, want)
		}
	}
	if err := Try(func() { validateMFAFields(map[string]any{"mfa_type": "foo"}) }); err == nil || !strings.Contains(err.Error(), "Invalid MFA type: foo") {
		t.Errorf("mfa_type error = %v", err)
	}
	if err := Try(func() { validateMFAFields(map[string]any{"mobile": "123"}) }); err == nil || !strings.Contains(err.Error(), "The mobile number 123 is invalid") {
		t.Errorf("mobile error = %v", err)
	}
}

func TestYubikeyWebclient(t *testing.T) {
	api := base64.StdEncoding.EncodeToString([]byte("secretkey123"))
	key, _ := base64.StdEncoding.DecodeString(api)
	otp := "ccccccbcgujhingjrdejhgfnuetrgigvejhhgbkugded"
	status := "OK"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		res := map[string]string{"otp": q.Get("otp"), "nonce": q.Get("nonce"), "status": status, "t": "2026-10-25T10:00:00Z0123"}
		var keys []string
		for k := range res {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+res[k])
		}
		h := hmac.New(sha1.New, key)
		h.Write([]byte(strings.Join(parts, "&")))
		fmt.Fprintf(w, "h=%s\r\n", base64.StdEncoding.EncodeToString(h.Sum(nil)))
		for _, k := range keys {
			fmt.Fprintf(w, "%s=%s\r\n", k, res[k])
		}
	}))
	defer srv.Close()
	if got := yubikeyWebclient(mfaHTTP, otp, "1", api, "abcdefghijklmnopqrstuvwxyz123456", srv.URL); got != "OK" {
		t.Errorf("valid otp = %s", got)
	}
	status = "REPLAYED_OTP"
	if got := yubikeyWebclient(mfaHTTP, otp, "1", api, "abcdefghijklmnopqrstuvwxyz123456", srv.URL); got != "ERR_REPLAYED_OTP" {
		t.Errorf("replayed otp = %s", got)
	}
	if got := yubikeyWebclient(mfaHTTP, "short", "1", api, "n", srv.URL); got != "ERR_BAD_OTP" {
		t.Errorf("bad otp = %s", got)
	}
}

func TestYubikeyWebclientSignature(t *testing.T) {
	api := base64.StdEncoding.EncodeToString([]byte("secretkey123"))
	otp := "ccccccbcgujhingjrdejhgfnuetrgigvejhhgbkugded"
	var answer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, answer)
	}))
	defer srv.Close()
	// A signature made with another key
	answer = "h=AAAAAAAAAAAAAAAAAAAAAAAAAAA=\r\nnonce=n0123456789\r\notp=" + otp + "\r\nstatus=OK\r\n"
	if got := yubikeyWebclient(srv.Client(), otp, "1", api, "n0123456789", srv.URL); got != "ERR_HMAC" {
		t.Errorf("bad signature = %s", got)
	}
	// An answer for another request
	if got := yubikeyWebclient(srv.Client(), otp, "1", api, "other", srv.URL); got != "ERR_MSG_AUTH" {
		t.Errorf("other nonce = %s", got)
	}
	// No answer
	answer = ""
	if got := yubikeyWebclient(srv.Client(), otp, "1", api, "n0123456789", srv.URL); got != "ERR_" {
		t.Errorf("no answer = %s", got)
	}
	if err := Try(func() { yubikeyWebclient(srv.Client(), otp, "1", "abc", "n", srv.URL) }); err == nil || err.Error() != "Your API key must be in 4 byte lengths" {
		t.Errorf("short key: %v", err)
	}
}

func TestSendSMS(t *testing.T) {
	var status int
	var answer, gotAuth, gotType string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotType = r.Header.Get("Content-Type")
		gotBody = nil
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(status)
		fmt.Fprint(w, answer)
	}))
	defer srv.Close()
	e := &Engine{Config: &Config{SMS: map[string]string{"username": "u", "password": "p", "from": "GADS", "url": srv.URL}}, HTTPClient: srv.Client()}
	send := func() error { return Try(func() { e.SendSMS("+441234567890", "code") }) }

	status, answer = 201, `[{"status":{"type":"ACCEPTED"}}]`
	if err := send(); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if gotAuth != "Basic dTpw" || gotType != "application/json" {
		t.Errorf("headers %q %q", gotAuth, gotType)
	}
	want := map[string]string{"from": "GADS", "to": "+441234567890", "body": "code", "encoding": "UNICODE"}
	if fmt.Sprint(gotBody) != fmt.Sprint(want) {
		t.Errorf("body %v", gotBody)
	}
	for _, c := range []struct {
		status int
		answer string
		msg    string
	}{
		{401, `{"title":"Unauthorized","detail":"Invalid credentials"}`, "Failed to send SMS message: Unauthorized (Invalid credentials)"},
		{201, `[{"status":{"type":"FAILED"}}]`, "Failed to send SMS message - unknown reason"},
		{201, `[]`, "Failed to send SMS message - unknown reason"},
		{500, `not json`, "Failed to send SMS message - unknown response"},
	} {
		status, answer = c.status, c.answer
		err := send()
		if err == nil || err.Error() != c.msg || err.(*Error).Level != LevelPanic {
			t.Errorf("%s: %v", c.answer, err)
		}
	}
	// The configuration is checked before sending
	for key, msg := range map[string]string{"username": "SMS username not defined", "password": "SMS password not defined", "from": "SMS sender configuration not defined"} {
		e.Config.SMS = map[string]string{"username": "u", "password": "p", "from": "GADS"}
		delete(e.Config.SMS, key)
		if err := send(); err == nil || err.Error() != msg {
			t.Errorf("without %s: %v", key, err)
		}
	}
	e.Config.SMS = nil
	if err := send(); err == nil || err.Error() != "SMS not configured" {
		t.Errorf("not configured: %v", err)
	}
}

func TestNewDigits(t *testing.T) {
	e := &Engine{}
	for i := 0; i < 20; i++ {
		d := e.NewDigits(6)
		if len(d) != 6 || strings.Trim(d, "0123456789") != "" {
			t.Fatalf("NewDigits = %q", d)
		}
	}
	// The runners' predictable tokens
	e.TokenGenerator = func(n int) string { return fmt.Sprintf("T%05d", n) }
	if d := e.NewDigits(6); d != "T00006" {
		t.Errorf("NewDigits with TokenGenerator = %q", d)
	}
	if e.httpClient() != mfaHTTP || mfaHTTP.Timeout != 10*time.Second {
		t.Errorf("default client")
	}
	c := &http.Client{}
	e.HTTPClient = c
	if e.httpClient() != c {
		t.Errorf("HTTPClient not used")
	}
}
