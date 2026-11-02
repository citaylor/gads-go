package conformance

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

// The requests that GADS makes to other services (the BulkSMS API of
// send_sms, the Yubico API of Auth::Yubikey_WebClient) are answered by the
// runner instead of sent, as the Perl runner replaces
// LWP::UserAgent::request (see the outgoing_http step)

// outgoingHTTP is the transport of the engine's HTTP client
type outgoingHTTP struct {
	r        *Runner
	requests []any
	answers  map[string]any
}

func (o *outgoingHTTP) reset() {
	o.requests = []any{}
	o.answers = map[string]any{}
}

func (o *outgoingHTTP) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body.Close()
	}
	obs := map[string]any{
		"method":        req.Method,
		"url":           req.URL.Scheme + "://" + req.URL.Host + req.URL.Path,
		"authorization": nilIfEmpty(req.Header.Get("Authorization")),
		"content_type":  nilIfEmpty(req.Header.Get("Content-Type")),
	}
	query := map[string]any{}
	for k, v := range req.URL.Query() {
		query[k] = v[len(v)-1]
	}
	obs["query"] = query
	var js any
	if len(body) > 0 && json.Unmarshal(body, &js) == nil {
		obs["json"] = js
	} else {
		obs["json"] = nil
	}
	o.requests = append(o.requests, obs)

	res := &http.Response{Request: req, Header: http.Header{}, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
	answer := func(status int, contentType, content string) (*http.Response, error) {
		res.StatusCode = status
		res.Status = http.StatusText(status)
		res.Header.Set("Content-Type", contentType)
		res.Body = io.NopCloser(bytes.NewReader([]byte(content)))
		res.ContentLength = int64(len(content))
		return res, nil
	}
	switch req.URL.Host {
	case "api.bulksms.com":
		status := 201
		if s, ok := o.answers["sms_status"]; ok {
			status = int(toInt64(s))
		}
		var content any = []any{map[string]any{"status": map[string]any{"type": "ACCEPTED"}}}
		if a, ok := o.answers["sms"]; ok {
			content = a
		}
		b, _ := json.Marshal(content)
		return answer(status, "application/json", string(b))
	case "api2.yubico.com":
		// A Yubico answer signed with the API key of the configuration
		status := "OK"
		if s, ok := o.answers["yubico"]; ok {
			status = str(s)
		}
		q := req.URL.Query()
		fields := map[string]string{
			"nonce":  q.Get("nonce"),
			"otp":    q.Get("otp"),
			"status": status,
			"t":      "2014-10-10T01:00:00Z0000",
		}
		var keys []string
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+fields[k])
		}
		key, _ := base64.StdEncoding.DecodeString(o.r.yubiKey())
		mac := hmac.New(sha1.New, key)
		mac.Write([]byte(strings.Join(parts, "&")))
		h := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		if str(o.answers["yubico_signature"]) == "bad" {
			h = "AAAAAAAAAAAAAAAAAAAAAAAAAAA="
		}
		lines := []string{"h=" + h}
		for _, k := range keys {
			lines = append(lines, k+"="+fields[k])
		}
		return answer(200, "text/plain", strings.Join(lines, "\r\n")+"\r\n\r\n")
	}
	return answer(404, "text/plain", "Not Found")
}

// yubiKey is the Yubikey API key of the scenario's configuration
func (r *Runner) yubiKey() string {
	if c, ok := r.scenario["config"].(map[string]any); ok {
		if y, ok := c["yubi"].(map[string]any); ok {
			return str(y["key"])
		}
	}
	return ""
}

// stepOutgoingHTTP sets the answers of later requests (sms, sms_status,
// yubico, yubico_signature), and observes the requests made since the
// scenario started or the last outgoing_http step
func (r *Runner) stepOutgoingHTTP(args map[string]any) any {
	for _, k := range []string{"sms", "sms_status", "yubico", "yubico_signature"} {
		if v, ok := args[k]; ok {
			r.outgoing.answers[k] = v
		}
	}
	out := r.outgoing.requests
	r.outgoing.requests = []any{}
	return out
}
