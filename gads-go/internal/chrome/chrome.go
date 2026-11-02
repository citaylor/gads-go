// Package chrome drives headless Chrome as GADS does with
// WWW::Mechanize::Chrome (WMC): a page is loaded and printed to PDF or
// captured as a PNG. It speaks the DevTools protocol over a pipe
// (--remote-debugging-pipe) rather than WMC's port 9222, so that no other
// process can connect to the browser, and launches Chrome with WMC's
// default switches, so that a page renders as it does under Perl.
package chrome

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DefaultPath is the browser of GADS's _page_as_mech (launch_exe)
const DefaultPath = "/usr/bin/chromium"

// Options are those of a browser session
type Options struct {
	// Path is the browser executable (DefaultPath when empty)
	Path string
	// Allow, when set, is the only origin that the page may load from
	// (a prefix of the URLs allowed): any other request fails. Without
	// it the page loads what it asks for, as under WMC.
	Allow string
	// NoSandbox runs the browser without its sandbox, which WMC does only
	// as root: in a container that does not allow the sandbox
	NoSandbox bool
}

// Browser is a running headless Chrome with one page attached
type Browser struct {
	cmd     *exec.Cmd
	w       io.WriteCloser
	msgs    chan message
	readErr error
	dataDir string
	session string
	nextID  int
	pending []message
	// replies are the responses received while waiting for another
	replies map[int]message
	allow   string
	// stderr keeps the end of the browser's messages, for its errors
	stderr *tailWriter
}

// tailWriter keeps the last lines written to it
type tailWriter struct {
	mu   sync.Mutex
	data []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = append(w.data, p...)
	if len(w.data) > 8192 {
		w.data = w.data[len(w.data)-8192:]
	}
	return len(p), nil
}

// lastError is the browser's last line of error, if any
func (w *tailWriter) lastError() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(w.data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "ERROR") || strings.Contains(lines[i], "FATAL") {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

type message struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *protocolError  `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// launchArgs are WMC's switches for a headless browser (build_command_line
// with GADS's options): the --disable switches of its defaults, a fresh
// profile, and --no-sandbox only for root
func launchArgs(dataDir string, noSandbox bool) []string {
	args := []string{"--remote-debugging-pipe", "--user-data-dir=" + dataDir,
		"--no-first-run", "--mute-audio", "--no-default-browser-check"}
	if noSandbox || os.Geteuid() == 0 {
		args = append(args, "--no-sandbox")
	}
	args = append(args, "--safebrowsing-disable-auto-update")
	for _, o := range []string{"background-networking", "background-timer-throttling",
		"backgrounding-occluded-windows", "breakpad", "default-apps", "dev-shm-usage",
		"domain-reliability", "gpu", "ipc-flooding-protection", "client-side-phishing-detection",
		"component-update", "hang-monitor", "popup-blocking", "prompt-on-repost",
		"renderer-backgrounding", "sync", "translate", "web-resources"} {
		args = append(args, "--disable-"+o)
	}
	return append(args, "--headless")
}

// Launch starts the browser and attaches to its page (WMC new and
// _connect). The context bounds the whole session: when it ends, the
// browser is killed.
func Launch(ctx context.Context, o Options) (*Browser, error) {
	path := o.Path
	if path == "" {
		path = DefaultPath
	}
	dataDir, err := os.MkdirTemp("", "gads-chrome")
	if err != nil {
		return nil, err
	}
	// Chrome reads commands from fd 3 and writes to fd 4
	cmdR, cmdW, err := os.Pipe()
	if err != nil {
		os.RemoveAll(dataDir)
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		cmdR.Close()
		cmdW.Close()
		os.RemoveAll(dataDir)
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, launchArgs(dataDir, o.NoSandbox)...) // #nosec G204 -- the configured browser
	cmd.ExtraFiles = []*os.File{cmdR, outW}
	stderr := &tailWriter{}
	cmd.Stderr = stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		cmdR.Close()
		cmdW.Close()
		outR.Close()
		outW.Close()
		os.RemoveAll(dataDir)
		return nil, fmt.Errorf("Unable to start %s: %w", path, err)
	}
	cmdR.Close()
	outW.Close()
	b := &Browser{cmd: cmd, w: cmdW, stderr: stderr, msgs: make(chan message, 64), replies: map[int]message{}, dataDir: dataDir, allow: o.Allow}
	go b.read(outR)
	if err := b.attach(ctx); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

// read passes the browser's messages (NUL-terminated JSON) to msgs
func (b *Browser) read(r io.ReadCloser) {
	defer r.Close()
	br := bufio.NewReader(r)
	for {
		data, err := br.ReadBytes(0)
		if err != nil {
			b.readErr = err
			close(b.msgs)
			return
		}
		var m message
		if json.Unmarshal(data[:len(data)-1], &m) == nil {
			b.msgs <- m
		}
	}
}

// next is the next message from the browser
func (b *Browser) next(ctx context.Context) (message, error) {
	select {
	case m, ok := <-b.msgs:
		if !ok {
			if msg := b.stderr.lastError(); msg != "" {
				return message{}, fmt.Errorf("the browser closed: %s", msg)
			}
			return message{}, fmt.Errorf("the browser closed: %v", b.readErr)
		}
		return m, nil
	case <-ctx.Done():
		return message{}, ctx.Err()
	}
}

// Call sends a command to the page (to the browser before it is attached)
// and returns its result. Events that arrive meanwhile are kept for
// WaitEvent.
func (b *Browser) Call(ctx context.Context, method string, params any, result any) error {
	b.nextID++
	id := b.nextID
	if params == nil {
		params = struct{}{}
	}
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	data, err := json.Marshal(message{ID: id, Method: method, Params: p, SessionID: b.session})
	if err != nil {
		return err
	}
	if _, err := b.w.Write(append(data, 0)); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	for {
		m, ok := b.replies[id]
		if ok {
			delete(b.replies, id)
		} else {
			if m, err = b.next(ctx); err != nil {
				return fmt.Errorf("%s: %w", method, err)
			}
			if m.ID == 0 {
				if err := b.event(ctx, m); err != nil {
					return err
				}
				continue
			}
			if m.ID != id {
				b.replies[m.ID] = m
				continue
			}
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	}
}

// event keeps an event, or answers a request paused by Allow
func (b *Browser) event(ctx context.Context, m message) error {
	if m.Method != "Fetch.requestPaused" {
		b.pending = append(b.pending, m)
		return nil
	}
	var p struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return err
	}
	if b.allow != "" && strings.HasPrefix(p.Request.URL, b.allow) {
		return b.Call(ctx, "Fetch.continueRequest", map[string]any{"requestId": p.RequestID}, nil)
	}
	return b.Call(ctx, "Fetch.failRequest", map[string]any{"requestId": p.RequestID, "errorReason": "BlockedByClient"}, nil)
}

// WaitEvent waits for an event of the page that match accepts
func (b *Browser) WaitEvent(ctx context.Context, method string, match func(json.RawMessage) bool) error {
	for i, m := range b.pending {
		if m.Method == method && (match == nil || match(m.Params)) {
			b.pending = b.pending[i+1:]
			return nil
		}
	}
	b.pending = nil
	for {
		m, err := b.next(ctx)
		if err != nil {
			return fmt.Errorf("waiting for %s: %w", method, err)
		}
		if m.ID != 0 {
			b.replies[m.ID] = m
			continue
		}
		if m.Method == "Fetch.requestPaused" {
			if err := b.event(ctx, m); err != nil {
				return err
			}
			continue
		}
		if m.Method == method && (match == nil || match(m.Params)) {
			return nil
		}
	}
}

// attach attaches to the browser's page and enables its events (WMC
// _connect)
func (b *Browser) attach(ctx context.Context) error {
	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	var targetID string
	// The first page may appear only after a moment
	for range 50 {
		if err := b.Call(ctx, "Target.getTargets", nil, &targets); err != nil {
			return err
		}
		for _, t := range targets.TargetInfos {
			if t.Type == "page" {
				targetID = t.TargetID
				break
			}
		}
		if targetID != "" {
			break
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if targetID == "" {
		var created struct {
			TargetID string `json:"targetId"`
		}
		if err := b.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
			return err
		}
		targetID = created.TargetID
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := b.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true}, &attached); err != nil {
		return err
	}
	b.session = attached.SessionID
	for _, m := range []string{"DOM.enable", "Page.enable", "Network.enable", "Runtime.enable"} {
		if err := b.Call(ctx, m, nil, nil); err != nil {
			return err
		}
	}
	if b.allow != "" {
		return b.Call(ctx, "Fetch.enable", map[string]any{"patterns": []any{map[string]any{"urlPattern": "*"}}}, nil)
	}
	return nil
}

// Viewport sets the size of the page (WMC viewport_size)
func (b *Browser) Viewport(ctx context.Context, width, height int) error {
	return b.Call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": 0, "mobile": false,
	}, nil)
}

// Get loads a URL and waits until its main frame has stopped loading
// (WMC get)
func (b *Browser) Get(ctx context.Context, url string) error {
	var nav struct {
		FrameID   string `json:"frameId"`
		ErrorText string `json:"errorText"`
	}
	if err := b.Call(ctx, "Page.navigate", map[string]any{"url": url}, &nav); err != nil {
		return err
	}
	if nav.ErrorText != "" {
		return fmt.Errorf("Unable to load %s: %s", url, nav.ErrorText)
	}
	return b.WaitEvent(ctx, "Page.frameStoppedLoading", func(p json.RawMessage) bool {
		var f struct {
			FrameID string `json:"frameId"`
		}
		return json.Unmarshal(p, &f) == nil && f.FrameID == nav.FrameID
	})
}

// Eval runs JavaScript in the page (WMC eval_in_page): an exception is an
// error, as WMC croaks with it
func (b *Browser) Eval(ctx context.Context, js string) error {
	var res struct {
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := b.Call(ctx, "Runtime.evaluate", map[string]any{"expression": js, "returnByValue": true}, &res); err != nil {
		return err
	}
	if d := res.ExceptionDetails; d != nil {
		return errors.New(strings.TrimSpace(d.Text + "\n" + d.Exception.Description))
	}
	return nil
}

// PDF prints the page (WMC content_as_pdf), with the paper size in inches
func (b *Browser) PDF(ctx context.Context, paperWidth, paperHeight float64) ([]byte, error) {
	return b.data(ctx, "Page.printToPDF", map[string]any{"paperWidth": paperWidth, "paperHeight": paperHeight})
}

// PNG captures the whole page (WMC content_as_png)
func (b *Browser) PNG(ctx context.Context) ([]byte, error) {
	return b.data(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true})
}

func (b *Browser) data(ctx context.Context, method string, params any) ([]byte, error) {
	var res struct {
		Data string `json:"data"`
	}
	if err := b.Call(ctx, method, params, &res); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(res.Data)
}

// Close ends the browser and removes its profile
func (b *Browser) Close() {
	if b.cmd.Process != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		b.session = ""
		_ = b.Call(ctx, "Browser.close", nil, nil)
		cancel()
		done := make(chan struct{})
		go func() {
			_ = b.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = b.cmd.Process.Kill()
			<-done
		}
	}
	b.w.Close()
	os.RemoveAll(b.dataDir)
}
