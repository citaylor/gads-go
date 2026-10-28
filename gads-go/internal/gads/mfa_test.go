package gads

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
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
	if got := yubikeyWebclient(otp, "1", api, "abcdefghijklmnopqrstuvwxyz123456", srv.URL); got != "OK" {
		t.Errorf("valid otp = %s", got)
	}
	status = "REPLAYED_OTP"
	if got := yubikeyWebclient(otp, "1", api, "abcdefghijklmnopqrstuvwxyz123456", srv.URL); got != "ERR_REPLAYED_OTP" {
		t.Errorf("replayed otp = %s", got)
	}
	if got := yubikeyWebclient("short", "1", api, "n", srv.URL); got != "ERR_BAD_OTP" {
		t.Errorf("bad otp = %s", got)
	}
}
