package chrome

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// browserPath is the browser for the tests: GADS_CHROMIUM, GADS's own, or
// Chrome on the PATH
func browserPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GADS_CHROMIUM"); p != "" {
		return p
	}
	if _, err := os.Stat(DefaultPath); err == nil {
		return DefaultPath
	}
	for _, n := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	t.Skip("no Chrome or Chromium")
	return ""
}

func launch(t *testing.T, o Options) (*Browser, context.Context) {
	t.Helper()
	o.Path = browserPath(t)
	// In a container without the sandbox (as the Docker image)
	o.NoSandbox = os.Getenv("GADS_CHROMIUM_NO_SANDBOX") != ""
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	b, err := Launch(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b, ctx
}

func writePage(t *testing.T, html string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(file, []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}
	return "file://" + file
}

// pdfText is the text of a PDF (pdftotext)
func pdfText(t *testing.T, pdf []byte) string {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("no pdftotext")
	}
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdftotext", "-enc", "UTF-8", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestPDF(t *testing.T) {
	b, ctx := launch(t, Options{})
	if err := b.Viewport(ctx, 1550, 1085); err != nil {
		t.Fatal(err)
	}
	if err := b.Get(ctx, writePage(t, `<html><body><h1>A printed page</h1><p id="p">Some text</p></body></html>`)+"?pdf=1"); err != nil {
		t.Fatal(err)
	}
	if err := b.Eval(ctx, `document.getElementById("p").textContent += ", and more"`); err != nil {
		t.Fatal(err)
	}
	pdf, err := b.PDF(ctx, 16.5, 11.7)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", pdf[:min(20, len(pdf))])
	}
	if got := pdfText(t, pdf); !strings.Contains(got, "A printed page") || !strings.Contains(got, "Some text, and more") {
		t.Errorf("text %q", got)
	}
}

func TestPNG(t *testing.T) {
	b, ctx := launch(t, Options{})
	if err := b.Viewport(ctx, 400, 300); err != nil {
		t.Fatal(err)
	}
	// The whole page is captured, beyond the viewport
	if err := b.Get(ctx, writePage(t, `<html><body style="margin:0"><div style="width:200px;height:700px;background:red"></div></body></html>`)); err != nil {
		t.Fatal(err)
	}
	data, err := b.PNG(ctx)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// The width is the viewport's, without its vertical scroll bar
	if s := img.Bounds().Size(); s.X > 400 || s.X < 380 || s.Y != 700 {
		t.Errorf("size %v, want 400x700 less a scroll bar", s)
	}
}

func TestEvalException(t *testing.T) {
	b, ctx := launch(t, Options{})
	if err := b.Get(ctx, writePage(t, `<html><body></body></html>`)); err != nil {
		t.Fatal(err)
	}
	err := b.Eval(ctx, `(function(){ return $.jqplot.BarRenderer })()`)
	if err == nil || !strings.Contains(err.Error(), "ReferenceError: $ is not defined") {
		t.Errorf("error %v", err)
	}
}

// TestAllow checks that a page restricted to its origin loads from it and
// from nowhere else
func TestAllow(t *testing.T) {
	var outside atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outside.Add(1)
		w.Write([]byte("secret from elsewhere"))
	}))
	defer other.Close()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret file"), 0o600); err != nil {
		t.Fatal(err)
	}
	var own atomic.Int32
	page := `<html><body><p>The page</p><iframe src="/frame.html"></iframe>` +
		`<iframe src="` + other.URL + `/"></iframe><iframe src="file://` + secret + `"></iframe>` +
		`<img src="` + other.URL + `/img.png"></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		own.Add(1)
		if r.URL.Path == "/frame.html" {
			w.Write([]byte("<p>Own frame</p>"))
			return
		}
		w.Write([]byte(page))
	}))
	defer srv.Close()

	b, ctx := launch(t, Options{Allow: srv.URL + "/"})
	if err := b.Get(ctx, srv.URL+"/page.html"); err != nil {
		t.Fatal(err)
	}
	pdf, err := b.PDF(ctx, 8.27, 11.7)
	if err != nil {
		t.Fatal(err)
	}
	text := pdfText(t, pdf)
	if !strings.Contains(text, "The page") || !strings.Contains(text, "Own frame") {
		t.Errorf("own content missing: %q", text)
	}
	if strings.Contains(text, "secret") {
		t.Errorf("content from elsewhere: %q", text)
	}
	if n := outside.Load(); n != 0 {
		t.Errorf("%d requests reached another server", n)
	}
	if own.Load() < 2 {
		t.Errorf("own server had %d requests", own.Load())
	}
}

// TestUnrestricted is WMC's behaviour: a local page reads local files
func TestUnrestricted(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret file"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, ctx := launch(t, Options{})
	if err := b.Get(ctx, writePage(t, `<html><body><iframe src="file://`+secret+`"></iframe></body></html>`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	pdf, err := b.PDF(ctx, 8.27, 11.7)
	if err != nil {
		t.Fatal(err)
	}
	if text := pdfText(t, pdf); !strings.Contains(text, "secret file") {
		t.Errorf("local file not shown: %q", text)
	}
}

func TestLaunchMissing(t *testing.T) {
	_, err := Launch(context.Background(), Options{Path: filepath.Join(t.TempDir(), "none")})
	if err == nil || !strings.Contains(err.Error(), "Unable to start") {
		t.Errorf("error %v", err)
	}
}

func TestLaunchArgs(t *testing.T) {
	args := strings.Join(launchArgs("/tmp/x", false), " ")
	for _, want := range []string{"--remote-debugging-pipe", "--user-data-dir=/tmp/x", "--headless", "--disable-gpu", "--disable-web-resources", "--safebrowsing-disable-auto-update"} {
		if !strings.Contains(args, want) {
			t.Errorf("%s missing from %s", want, args)
		}
	}
	if strings.Contains(args, "remote-allow-origins") || strings.Contains(args, "remote-debugging-port") {
		t.Errorf("a debugging port: %s", args)
	}
	if os.Geteuid() != 0 && strings.Contains(args, "--no-sandbox") {
		t.Errorf("no sandbox: %s", args)
	}
	if args := strings.Join(launchArgs("/tmp/x", true), " "); !strings.Contains(args, "--no-sandbox") {
		t.Errorf("sandbox: %s", args)
	}
}

func TestTailWriter(t *testing.T) {
	w := &tailWriter{}
	w.Write([]byte("[1:1:ERROR:zygote.cc:130] No usable sandbox!\nother\n"))
	w.Write([]byte(strings.Repeat("x", 9000)))
	if len(w.data) != 8192 {
		t.Errorf("kept %d bytes", len(w.data))
	}
	if w.lastError() != "" {
		t.Errorf("error %q", w.lastError())
	}
	w.Write([]byte("\n[2:2:ERROR:a.cc:1] First\n[3:3:ERROR:b.cc:2] Last\ninfo\n"))
	if got := w.lastError(); got != "[3:3:ERROR:b.cc:2] Last" {
		t.Errorf("error %q", got)
	}
	// A fatal error comes before later errors
	w.Write([]byte("[4:4:FATAL:zygote.cc:228] Zygote exited\n[5:5:ERROR:file_io.cc:145] open cpufreq\n"))
	if got := w.lastError(); got != "[4:4:FATAL:zygote.cc:228] Zygote exited" {
		t.Errorf("error %q", got)
	}
}
