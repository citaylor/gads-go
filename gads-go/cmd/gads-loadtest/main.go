// Command gads-loadtest measures a running GADS server, Perl or Go, under
// load (gads-postgres-performance.md "Under load"): each client logs in
// with a session of its own, then sends a mix of requests in turn, each
// waiting for the previous answer, for a while. It reports the requests a
// second and the response times, in total and by request.
//
//	gads-loadtest -url http://127.0.0.1:3000 -user user1@example.com \
//	    -sheet table1 -record 51 -c 1,5,10,20 -d 60s
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// config is what a run needs
type config struct {
	base, host, user, password string
	reqs                       []request
}

// request is one of the mix
type request struct {
	name, method, path string
	form               url.Values
}

// mix is the requests of the measurements: the data table page, the
// records API (100 records, and a search) and a record page
func mix(sheet, record string) []request {
	return []request{
		{"data page", "GET", "/" + sheet + "/data", nil},
		{"api records", "POST", "/api/" + sheet + "/records", url.Values{"draw": {"1"}, "start": {"100"}, "length": {"100"}}},
		{"api search", "POST", "/api/" + sheet + "/records", url.Values{"draw": {"1"}, "start": {"0"}, "length": {"50"}, "search[value]": {"alpha"}}},
		{"record page", "GET", "/record/" + record, nil},
	}
}

var csrfRe = regexp.MustCompile(`data-csrf="([^"]+)"|name="csrf_token" value="([^"]+)"`)

// client is a browser: its own cookies (session) and CSRF token
type client struct {
	cfg  *config
	h    *http.Client
	csrf string
}

func newClient(cfg *config) *client {
	jar, _ := cookiejar.New(nil)
	return &client{cfg: cfg, h: &http.Client{Jar: jar, Timeout: 5 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// do sends a request (a form with the CSRF token), and takes the token of
// the page; it returns the status and the redirect
func (c *client) do(method, path string, form url.Values) (int, string, error) {
	var body io.Reader
	if form != nil {
		f := url.Values{}
		for k, v := range form {
			f[k] = v
		}
		if c.csrf != "" {
			f.Set("csrf_token", c.csrf)
		}
		body = strings.NewReader(f.Encode())
	}
	req, err := http.NewRequest(method, c.cfg.base+path, body)
	if err != nil {
		return 0, "", err
	}
	// GADS finds the site by the host name
	req.Host = c.cfg.host
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	if m := csrfRe.FindSubmatch(b); m != nil {
		c.csrf = string(m[1]) + string(m[2])
	}
	return resp.StatusCode, resp.Header.Get("Location"), nil
}

// login logs the client in, and fetches the first page of the mix for the
// CSRF token of its session
func (c *client) login() error {
	if _, _, err := c.do("GET", "/login", nil); err != nil {
		return fmt.Errorf("login page: %w", err)
	}
	st, _, err := c.do("POST", "/login", url.Values{"username": {c.cfg.user}, "password": {c.cfg.password}, "signin": {"1"}})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if st != http.StatusFound {
		return fmt.Errorf("login: status %d", st)
	}
	first := c.cfg.reqs[0]
	st, loc, err := c.do(first.method, first.path, first.form)
	if err != nil {
		return fmt.Errorf("%s: %w", first.name, err)
	}
	if st != http.StatusOK {
		// (/myaccount: the password has expired)
		return fmt.Errorf("%s: status %d %s", first.name, st, loc)
	}
	return nil
}

// result is the measurements of a run
type result struct {
	clients  int
	elapsed  time.Duration
	errors   int
	all      []time.Duration
	byReq    [][]time.Duration
	requests []request
}

// run logs in n clients and has them send the mix for d
func run(cfg *config, n int, d time.Duration) (*result, error) {
	clients := make([]*client, n)
	for i := range clients {
		clients[i] = newClient(cfg)
		if err := clients[i].login(); err != nil {
			return nil, err
		}
	}
	res := &result{clients: n, byReq: make([][]time.Duration, len(cfg.reqs)), requests: cfg.reqs}
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := time.Now()
	end := start.Add(d)
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *client) {
			defer wg.Done()
			// Each client starts at another request of the mix
			for k := i; time.Now().Before(end); k++ {
				r := cfg.reqs[k%len(cfg.reqs)]
				t := time.Now()
				st, _, err := c.do(r.method, r.path, r.form)
				took := time.Since(t)
				mu.Lock()
				res.all = append(res.all, took)
				res.byReq[k%len(cfg.reqs)] = append(res.byReq[k%len(cfg.reqs)], took)
				if err != nil || st != http.StatusOK {
					res.errors++
				}
				mu.Unlock()
			}
		}(i, c)
	}
	wg.Wait()
	res.elapsed = time.Since(start)
	return res, nil
}

// percentile is the p-th percentile (0 to 1) of ds, which it sorts
func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
	return ds[int(float64(len(ds)-1)*p)]
}

func ms(d time.Duration) string {
	return strconv.FormatFloat(float64(d.Microseconds())/1000, 'f', 0, 64)
}

func (r *result) print(w io.Writer) {
	fmt.Fprintf(w, "clients=%d requests=%d errors=%d req/s=%.1f p50=%sms p95=%sms p99=%sms\n",
		r.clients, len(r.all), r.errors, float64(len(r.all))/r.elapsed.Seconds(),
		ms(percentile(r.all, .5)), ms(percentile(r.all, .95)), ms(percentile(r.all, .99)))
	for k, q := range r.requests {
		fmt.Fprintf(w, "  %-12s n=%d p50=%sms p95=%sms\n", q.name, len(r.byReq[k]),
			ms(percentile(r.byReq[k], .5)), ms(percentile(r.byReq[k], .95)))
	}
}

// parseCounts reads the client counts of -c ("1,5,10,20")
func parseCounts(s string) ([]int, error) {
	var counts []int
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 1 {
			return nil, errors.New("-c: client counts such as 1,5,10,20")
		}
		counts = append(counts, n)
	}
	return counts, nil
}

func main() {
	cfg := &config{}
	flag.StringVar(&cfg.base, "url", "http://127.0.0.1:3000", "the server")
	flag.StringVar(&cfg.host, "host", "localhost", "the Host header (GADS's site host)")
	flag.StringVar(&cfg.user, "user", "", "the username of every client")
	flag.StringVar(&cfg.password, "password", "password", "its password")
	sheet := flag.String("sheet", "table1", "the table's short name (table<instance ID> without one)")
	record := flag.String("record", "51", "the record of the record page")
	counts := flag.String("c", "5", "client counts, run in turn (1,5,10,20)")
	d := flag.Duration("d", 60*time.Second, "how long each count runs")
	flag.Parse()
	if cfg.user == "" {
		fmt.Fprintln(os.Stderr, "gads-loadtest: -user is needed")
		os.Exit(2)
	}
	ns, err := parseCounts(*counts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gads-loadtest:", err)
		os.Exit(2)
	}
	cfg.reqs = mix(*sheet, *record)
	for _, n := range ns {
		res, err := run(cfg, n, *d)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gads-loadtest:", err)
			os.Exit(1)
		}
		res.print(os.Stdout)
	}
}
