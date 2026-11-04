package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ctrlo/gads-go/internal/chrome"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// The PDFs and images that GADS makes with headless Chrome (_page_as_mech
// and WWW::Mechanize::Chrome). The page is rendered while the request
// holds the engine; Chrome then loads and prints it after the engine is
// released, as it takes seconds (the sleep of _page_as_mech), and other
// requests go on meanwhile. A failure takes the engine again to report
// the error as one in the route would be.

const (
	// browserSettle is the sleep of _page_as_mech, for scripts to render
	browserSettle = 3 * time.Second
	// browserTimeout bounds a browser session, which WMC does not
	browserTimeout = 2 * time.Minute
	// maxBrowsers is the number of browsers run at once; other requests
	// wait for one to finish
	maxBrowsers = 2
)

// browsers limits the browsers running at once
var browsers = make(chan struct{}, maxBrowsers)

// mechOptions are the options of _page_as_mech, and what is then made of
// the page
type mechOptions struct {
	// PDF sets the viewport of an A3 page and loads the page with ?pdf=1
	PDF bool
	// Zoom is the zoom option (a percentage)
	Zoom any
	// HTML is a page given instead of a template (/print)
	HTML string
}

// browserJob is a page rendered for Chrome, and what to return of it
type browserJob struct {
	html   []byte
	pdf    bool
	public string
	// eval is JavaScript run in the page before it is captured
	eval string
	// png captures the page instead of printing it, scaled to fit
	// width x height when set (WMC content_as_png's target)
	png           bool
	width, height int
	ctype         string
	disposition   string
	// server serves the page and the public files to Chrome in hardened
	// mode
	server *pageServer
}

// pageAsMech is _page_as_mech: the page rendered with page_as_mech, for
// Chrome. Its links to static files go to the public directory (Perl:
// file://<appdir>/public/); hardened, to a server on the loopback
// interface that serves only them (H-2).
func (c *Ctx) pageAsMech(template string, params *tt.Hash, o mechOptions) *browserJob {
	public := c.s.AppPublic
	if public == "" {
		public = c.s.Public
	}
	public, err := filepath.Abs(public)
	if err != nil {
		gads.Panic("%s", err.Error())
	}
	job := &browserJob{pdf: o.PDF, public: public}
	base := "file://" + public + "/"
	if c.E.Config.Hardened {
		job.server, err = newPageServer(public)
		if err != nil {
			gads.Panic("%s", err.Error())
		}
		base = job.server.url
		defer func() {
			if r := recover(); r != nil {
				job.server.close()
				panic(r)
			}
		}()
	}
	if params == nil {
		params = tt.NewHash()
	}
	params.Set("scheme", "http")
	params.Set("base", base)
	params.Set("page_as_mech", int64(1))
	zoom := int64(100)
	if truthy(o.Zoom) {
		zoom = perlInt(tt.Str(o.Zoom))
	}
	params.Set("zoom", perlDivide(zoom, 100))
	if truthy(o.HTML) {
		job.html = []byte(o.HTML)
	} else {
		job.html = []byte(c.Template(template, params, nil))
	}
	return job
}

// perlDivide is a Perl division: an integer when it is exact
func perlDivide(a, b int64) any {
	if a%b == 0 {
		return a / b
	}
	return float64(a) / float64(b)
}

// sendBrowser ends the route with the job, which ServeHTTP runs once the
// engine is released
func (c *Ctx) sendBrowser(job *browserJob) {
	c.browser = job
	panic(haltSignal{})
}

// sendPDF is content_as_pdf of the page (A3 landscape), sent as a PDF
func (c *Ctx) sendPDF(job *browserJob) {
	job.ctype = "application/pdf"
	c.sendBrowser(job)
}

// runBrowser loads the page in Chrome and returns what the job asks for
func (s *Server) runBrowser(ctx context.Context, job *browserJob) ([]byte, error) {
	if job.server != nil {
		defer job.server.close()
	}
	ctx, cancel := context.WithTimeout(ctx, browserTimeout)
	defer cancel()
	select {
	case browsers <- struct{}{}:
		defer func() { <-browsers }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var url string
	if job.server != nil {
		url = job.server.setPage(job.html)
	} else {
		f, err := os.CreateTemp("", "*.html")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(job.html); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		url = "file://" + f.Name()
	}
	if job.pdf {
		url += "?pdf=1"
	}
	o := chrome.Options{Path: s.chromiumPath(), NoSandbox: truthy(s.Config.Gads["chromium_no_sandbox"])}
	if job.server != nil {
		o.Allow = job.server.url
	}
	b, err := chrome.Launch(ctx, o)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	// A3 is 29.7 × 42 cm, less the borders: 1085 x 1550 pixels
	if job.pdf {
		if err := b.Viewport(ctx, 1550, 1085); err != nil {
			return nil, err
		}
	}
	if err := b.Get(ctx, url); err != nil {
		return nil, err
	}
	select {
	case <-time.After(browserSettle):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if job.eval != "" {
		if err := b.Eval(ctx, job.eval); err != nil {
			return nil, err
		}
	}
	if !job.png {
		return b.PDF(ctx, 16.5, 11.7)
	}
	out, err := b.PNG(ctx)
	if err != nil || job.width == 0 {
		return out, err
	}
	return scalePNG(out, job.width, job.height)
}

// chromiumPath is the browser: gads: chromium, else that of GADS
func (s *Server) chromiumPath() string {
	if p, ok := s.Config.Gads["chromium"].(string); ok && p != "" {
		return p
	}
	return chrome.DefaultPath
}

// finishBrowser runs the browser job of a request after the engine is
// released, and writes its response: the file, or the error as the route
// would have reported it
func (s *Server) finishBrowser(c *Ctx, req *http.Request) *bufferedResponse {
	out, err := s.runBrowser(req.Context(), c.browser)
	res := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
	// The response is written with an engine, as the route would have
	e, ok := s.acquire(context.WithoutCancel(req.Context()))
	if !ok {
		res.status = http.StatusServiceUnavailable
		return res
	}
	defer s.release(e)
	c.E = e
	siteID := e.SiteID
	defer resetEngine(e, siteID)
	resetEngine(e, 0)
	if err != nil {
		c.status = http.StatusOK
		c.headers.Del("Content-Disposition")
		// WMC croaks: a PANIC
		c.handleError(&gads.Error{Msg: err.Error(), Level: gads.LevelPanic})
		c.flushReports()
	} else {
		c.file = out
		c.ctype = c.browser.ctype
		if c.browser.disposition != "" {
			c.headers.Set("Content-Disposition", c.browser.disposition)
		}
	}
	c.write(res)
	return res
}

// scalePNG is Imager scale with xpixels and ypixels (type min): the image
// scaled to fit within width x height, keeping its proportions
func scalePNG(data []byte, width, height int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	sb := src.Bounds()
	f := min(float64(width)/float64(sb.Dx()), float64(height)/float64(sb.Dy()))
	w, h := max(1, int(float64(sb.Dx())*f+0.5)), max(1, int(float64(sb.Dy())*f+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// Each pixel is the average of the source pixels that it covers
	for y := range h {
		y0, y1 := sb.Min.Y+y*sb.Dy()/h, sb.Min.Y+max((y+1)*sb.Dy()/h, y*sb.Dy()/h+1)
		for x := range w {
			x0, x1 := sb.Min.X+x*sb.Dx()/w, sb.Min.X+max((x+1)*sb.Dx()/w, x*sb.Dx()/w+1)
			var r, g, b, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, b, a, n = r+uint64(cr), g+uint64(cg), b+uint64(cb), a+uint64(ca), n+1
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(b/n>>8), uint8(a/n>>8) // #nosec G115 -- averages of 16-bit values
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pageServer serves a page and the public files to Chrome on the loopback
// interface (hardened, H-2): the page loads from there and from nowhere
// else, instead of from file://, where it could read any file
type pageServer struct {
	url string
	// path is the page's, which no other process can guess
	path string
	page atomic.Pointer[[]byte]
	srv  *http.Server
}

// pageCSP keeps the page's scripts and connections to its own origin, as
// WebSockets are not requests that the browser can stop
const pageCSP = "default-src 'self' data: 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; frame-src 'self'"

func newPageServer(public string) (*pageServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("Unable to listen for the browser: %w", err)
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		ln.Close()
		return nil, err
	}
	p := &pageServer{url: "http://" + ln.Addr().String() + "/", path: "/" + hex.EncodeToString(token) + ".html"}
	files := http.FileServer(publicFiles{http.Dir(public)})
	p.srv = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", pageCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if page := p.page.Load(); page != nil && r.URL.Path == p.path {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(*page)
			return
		}
		files.ServeHTTP(w, r)
	})}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// setPage sets the page, and returns its URL
func (p *pageServer) setPage(html []byte) string {
	p.page.Store(&html)
	return p.url + p.path[1:]
}

func (p *pageServer) close() { _ = p.srv.Close() }

// publicFiles are the public files, without directory listings
type publicFiles struct{ fs http.FileSystem }

func (f publicFiles) Open(name string) (http.File, error) {
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}
	if st, err := file.Stat(); err != nil || st.IsDir() {
		file.Close()
		return nil, os.ErrNotExist
	}
	return file, nil
}
