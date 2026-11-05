package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A fake GADS: a login form, a session cookie, and pages that need both
// the session and (for POSTs) the CSRF token
func fakeGADS(t *testing.T) (*httptest.Server, *atomic.Int64) {
	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "localhost" {
			http.Error(w, "wrong host "+r.Host, http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/login" {
			if r.Method == "POST" {
				if r.FormValue("password") != "secret" || r.FormValue("csrf_token") != "tok" {
					http.Error(w, "bad login", http.StatusForbidden)
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "s", Value: "1", Path: "/"})
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			w.Write([]byte(`<body data-csrf="tok">`))
			return
		}
		if c, err := r.Cookie("s"); err != nil || c.Value != "1" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if r.Method == "POST" && r.FormValue("csrf_token") != "tok" {
			http.Error(w, "csrf", http.StatusForbidden)
			return
		}
		served.Add(1)
		w.Write([]byte(`<body data-csrf="tok">ok`))
	}))
	t.Cleanup(srv.Close)
	return srv, &served
}

func TestRun(t *testing.T) {
	srv, served := fakeGADS(t)
	cfg := &config{base: srv.URL, host: "localhost", user: "u", password: "secret", reqs: mix("table1", "51")}
	res, err := run(cfg, 3, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res.errors != 0 || len(res.all) == 0 {
		t.Fatalf("errors %d of %d", res.errors, len(res.all))
	}
	// (the first page of each login is not counted)
	if int(served.Load()) != len(res.all)+3 {
		t.Errorf("served %d, counted %d", served.Load(), len(res.all))
	}
	for k := range cfg.reqs {
		if len(res.byReq[k]) == 0 {
			t.Errorf("no %s", cfg.reqs[k].name)
		}
	}
	var out bytes.Buffer
	res.print(&out)
	if !strings.HasPrefix(out.String(), "clients=3 ") || !strings.Contains(out.String(), "record page") {
		t.Errorf("%s", out.String())
	}

	cfg.password = "wrong"
	if _, err := run(cfg, 1, time.Millisecond); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Errorf("bad password: %v", err)
	}
}

func TestPercentile(t *testing.T) {
	ds := []time.Duration{5, 1, 4, 2, 3}
	if p := percentile(ds, .5); p != 3 {
		t.Errorf("median %d", p)
	}
	if p := percentile(ds, 1); p != 5 {
		t.Errorf("max %d", p)
	}
	if p := percentile(nil, .5); p != 0 {
		t.Errorf("empty %d", p)
	}
}

func TestParseCounts(t *testing.T) {
	if c, err := parseCounts("1, 5,20"); err != nil || len(c) != 3 || c[2] != 20 {
		t.Errorf("%v %v", c, err)
	}
	for _, bad := range []string{"", "0", "a", "1,,2"} {
		if _, err := parseCounts(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
