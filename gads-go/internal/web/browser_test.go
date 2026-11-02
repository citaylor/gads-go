package web

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPerlDivide(t *testing.T) {
	for _, c := range []struct {
		a    int64
		want any
	}{{100, int64(1)}, {80, 0.8}, {0, int64(0)}, {250, 2.5}} {
		if got := perlDivide(c.a, 100); got != c.want {
			t.Errorf("%d/100 = %v (%T), want %v", c.a, got, got, c.want)
		}
	}
}

func TestScalePNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1260, 1000))
	for y := range 1000 {
		for x := range 1260 {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 630 {
				c = color.RGBA{0, 0, 255, 255}
			}
			src.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	out, err := scalePNG(buf.Bytes(), 630, 400)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	// Imager's type min: fits in 630x400, keeping the proportions
	if s := img.Bounds().Size(); s.X != 504 || s.Y != 400 {
		t.Errorf("size %v, want 504x400", s)
	}
	if r, _, b, _ := img.At(10, 10).RGBA(); r>>8 != 255 || b != 0 {
		t.Errorf("left pixel %v", img.At(10, 10))
	}
	if r, _, b, _ := img.At(500, 10).RGBA(); r != 0 || b>>8 != 255 {
		t.Errorf("right pixel %v", img.At(500, 10))
	}
	if _, err := scalePNG([]byte("not a png"), 10, 10); err == nil {
		t.Error("no error for a bad PNG")
	}
}

func TestPageServer(t *testing.T) {
	public := t.TempDir()
	if err := os.MkdirAll(filepath.Join(public, "css"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(public, "css", "a.css"), []byte("body{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := newPageServer(public)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	get := func(url string) (int, string, http.Header) {
		t.Helper()
		res, err := http.Get(url) // #nosec G107 -- the test's own server
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body), res.Header
	}
	if !strings.HasPrefix(p.url, "http://127.0.0.1:") {
		t.Errorf("url %s", p.url)
	}
	// The page is not served before it is set, nor at a guessable path
	if code, _, _ := get(p.url + p.path[1:]); code != http.StatusNotFound {
		t.Errorf("page before it is set: %d", code)
	}
	url := p.setPage([]byte("<p>The page</p>"))
	code, body, h := get(url)
	if code != 200 || body != "<p>The page</p>" {
		t.Errorf("page: %d %q", code, body)
	}
	if !strings.Contains(h.Get("Content-Security-Policy"), "connect-src 'self'") || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers %v", h)
	}
	if code, _, _ := get(p.url + "page.html"); code != http.StatusNotFound {
		t.Errorf("page.html: %d", code)
	}
	if code, body, _ := get(p.url + "css/a.css"); code != 200 || body != "body{}" {
		t.Errorf("public file: %d %q", code, body)
	}
	// No listings, nothing outside the public directory
	for _, path := range []string{"css/", "", "../" + filepath.Base(public), "%2e%2e/etc/passwd"} {
		if code, body, _ := get(p.url + path); code == 200 {
			t.Errorf("%s served: %q", path, body)
		}
	}
	res, err := http.Post(p.url+"css/a.css", "text/plain", nil) // #nosec G107 -- the test's own server
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", res.StatusCode)
	}
}
