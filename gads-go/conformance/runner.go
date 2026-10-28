// Package conformance executes the language-neutral GADS conformance
// scenarios (../../conformance/scenarios) against the Go engine, producing
// observation files in the same format as the Perl reference runner.
package conformance

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/perl"
)

// StepResult is the observation of one step.
type StepResult struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Observed any      `json:"observed"`
	Error    *string  `json:"error"`
	Messages []string `json:"messages,omitempty"`
	Pass     bool     `json:"pass"`
	Failures []string `json:"failures"`
	// Timing is the times of the runs of the step (GADS_BENCH)
	Timing *Timing `json:"timing,omitempty"`
}

// Timing is the times of the runs of a step, in milliseconds.
type Timing struct {
	Runs     int     `json:"runs"`
	TotalMS  float64 `json:"total_ms"`
	MinMS    float64 `json:"min_ms"`
	MedianMS float64 `json:"median_ms"`
	MaxMS    float64 `json:"max_ms"`
}

// bench is whether step timings are reported (GADS_BENCH)
var bench = os.Getenv("GADS_BENCH") != ""

func ms(d time.Duration) float64 {
	return math.Round(float64(d)/1e3) / 1e3
}

func newTiming(times []time.Duration) *Timing {
	if len(times) == 0 {
		return nil
	}
	t := append([]time.Duration(nil), times...)
	sort.Slice(t, func(i, j int) bool { return t[i] < t[j] })
	var total time.Duration
	for _, d := range t {
		total += d
	}
	n := len(t)
	median := t[n/2]
	if n%2 == 0 {
		median = (t[n/2-1] + t[n/2]) / 2
	}
	return &Timing{Runs: n, TotalMS: ms(total), MinMS: ms(t[0]),
		MedianMS: ms(median), MaxMS: ms(t[n-1])}
}

// Result is the observation of a scenario.
type Result struct {
	Scenario string       `json:"scenario"`
	Runner   string       `json:"runner"`
	Fatal    string       `json:"fatal,omitempty"`
	Steps    []StepResult `json:"steps"`
	// ElapsedMS is the time of the whole scenario (GADS_BENCH)
	ElapsedMS float64 `json:"elapsed_ms,omitempty"`
}

// Runner executes a scenario.
type Runner struct {
	// pgName is the PostgreSQL database of the scenario (GADS_TEST_PG)
	// caughtFatal is the first error caught by tryCol in the step
	caughtFatal *string
	pgName      string
	// uploads is the directory of uploaded files (gads.uploads), kept for
	// the whole scenario as by the Perl runner
	uploads     string
	cliDirs     map[string]string // the named directories of the cli steps
	webVars     map[string]string // values captured from web responses
	file        string
	e           *gads.Engine
	db          *db.DB
	sheets      map[string]*DataSheet
	first       string
	views       map[string]int64
	records     map[string]int64
	cols        map[string]int64
	held        map[string]*gads.Record
	heldCols    map[string]*gads.Column
	graphs      map[string]int64
	users       map[string]*gads.User
	groups      map[string]int64
	heldViews   map[string]*gads.View
	heldViewNew map[string]map[string]any
	savedGroups map[string][]int64
	heldFilters map[string]*gads.Filter
	topics      map[string]int64
	tables      map[string]*gads.Layout
	frozen      time.Time
	scenario    map[string]any
	webh        *webHarness
	tokens      int
	emails      []any // emails captured since the last emails step
}

// RunFile runs a scenario file and returns its result.
func RunFile(file string) (res *Result) {
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	res = &Result{Scenario: name, Runner: "go", Steps: []StepResult{}}
	started := time.Now()
	defer func() {
		if bench {
			res.ElapsedMS = ms(time.Since(started))
		}
		if r := recover(); r != nil {
			res.Fatal = fmt.Sprint(r)
		}
	}()
	scenario, err := loadScenario(file)
	if err != nil {
		res.Fatal = err.Error()
		return res
	}
	if n, ok := scenario["name"].(string); ok && n != "" {
		res.Scenario = n
	}
	r, err := newRunner(file, scenario)
	if err != nil {
		res.Fatal = err.Error()
		return res
	}
	defer r.close()
	res.Steps = r.runSteps()
	return res
}

func loadScenario(file string) (map[string]any, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var scenario map[string]any
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		return nil, err
	}
	return scenario, nil
}

// newRunner returns the runner of a scenario, with its fresh database and
// the clock at the scenario's fixed time.
func newRunner(file string, scenario map[string]any) (*Runner, error) {
	r := &Runner{file: file, scenario: scenario}
	if err := r.newDatabase(); err != nil {
		return nil, err
	}
	ft := "2014-10-10 01:00:00"
	if s, ok := scenario["fixed_time"].(string); ok {
		ft = s
	}
	r.setTime(ft)
	return r, nil
}

// runSteps runs the steps of the scenario.
func (r *Runner) runSteps() []StepResult {
	results := []StepResult{}
	steps, _ := r.scenario["steps"].([]any)
	for i, st := range steps {
		sm, ok := st.(map[string]any)
		if !ok || len(sm) != 1 {
			continue
		}
		var typ string
		var args map[string]any
		for k, v := range sm {
			typ = k
			args, _ = v.(map[string]any)
		}
		if args == nil {
			args = map[string]any{}
		}
		id := fmt.Sprintf("%d:%s", i+1, typ)
		if s, ok := args["id"].(string); ok {
			id = s
		}
		results = append(results, r.runStep(id, typ, args))
	}
	return results
}

// newDatabase creates a fresh in-memory database and engine, and resets all
// state referring to the previous one.
func (r *Runner) newDatabase() error {
	var d *db.DB
	var err error
	if pgParams() != "" {
		d, r.pgName, err = openPG()
	} else {
		d, err = db.OpenMemory()
	}
	if err != nil {
		return err
	}
	if os.Getenv("GADS_TRACE") != "" {
		d.Trace = func(q string, args []any) { fmt.Fprintf(os.Stderr, "SQL: %s %v\n", q, args) }
	}
	if err := d.Deploy(); err != nil {
		d.Close()
		return err
	}
	cfg := gads.DefaultConfig()
	if r.uploads == "" {
		if r.uploads, err = os.MkdirTemp("", "gads-uploads"); err != nil {
			d.Close()
			return err
		}
	}
	cfg.Uploads = r.uploads
	if c, ok := r.scenario["config"].(map[string]any); ok {
		if df, ok := c["dateformat"].(string); ok {
			cfg.DateFormat = df
		}
	}
	e, err := gads.NewEngine(d, cfg)
	if err != nil {
		d.Close()
		return err
	}
	e.Clock = func() time.Time { return r.frozen }
	// Predictable tokens, as the Perl runner's Session::Token
	e.TokenGenerator = func(n int) string {
		r.tokens++
		return fmt.Sprintf("T%0*d", n-1, r.tokens)
	}
	// Predictable SAML request IDs, as the Perl runner's generate_id
	e.SAMLIDGenerator = func() string {
		r.tokens++
		return fmt.Sprintf("NETSAML2_T%063d", r.tokens)
	}
	// Predictable passwords, as the Perl runner's XkcdPassword
	e.PasswordGenerator = func() string {
		r.tokens++
		return fmt.Sprintf("Pw%04d", r.tokens)
	}
	// As run_perl.sh sets GADS_NO_FORK
	e.NoFork = true
	// Emails are captured instead of sent (the emails step)
	r.emails = []any{}
	e.Emailer = func(m gads.Email) {
		r.emails = append(r.emails, map[string]any{
			"subject":  m.Subject,
			"to":       toAnyList(m.To),
			"text":     nilIfEmpty(m.Text),
			"html":     nilIfEmpty(m.HTML),
			"reply_to": nilIfEmpty(m.ReplyTo),
		})
	}
	r.db, r.e = d, e
	r.sheets = map[string]*DataSheet{}
	r.first = ""
	r.views = map[string]int64{}
	r.records = map[string]int64{}
	r.users = map[string]*gads.User{}
	r.groups = map[string]int64{}
	r.cols = nil
	r.held = nil
	r.heldCols = nil
	r.graphs = map[string]int64{}
	r.heldViews, r.heldViewNew, r.heldFilters = nil, nil, nil
	r.savedGroups = map[string][]int64{}
	r.topics = map[string]int64{}
	r.tables = map[string]*gads.Layout{}
	r.webh = nil
	return nil
}

// close ends the scenario
func (r *Runner) close() {
	r.closeDatabase()
	if r.uploads != "" {
		os.RemoveAll(r.uploads)
		r.uploads = ""
	}
}

func (r *Runner) closeDatabase() {
	for _, d := range r.cliDirs {
		os.RemoveAll(d)
	}
	r.cliDirs = nil
	if r.e != nil {
		r.e.Close()
	}
	if r.db != nil {
		r.db.Close()
	}
	if r.pgName != "" {
		dropPG(r.pgName)
		r.pgName = ""
	}
}

// stepReset starts again with a fresh database, as tests that create new
// DataSheets without passing a schema do.
func (r *Runner) stepReset(args map[string]any) any {
	r.closeDatabase()
	if err := r.newDatabase(); err != nil {
		gads.Panic("%s", err.Error())
	}
	return map[string]any{}
}

func (r *Runner) setTime(s string) {
	m := regexp.MustCompile(`^(\d{4})-(\d\d)-(\d\d)[ T](\d\d):(\d\d):(\d\d)`).FindStringSubmatch(s)
	if m == nil {
		panic("Invalid time " + s)
	}
	t, _ := time.Parse("2006-01-02 15:04:05", fmt.Sprintf("%s-%s-%s %s:%s:%s", m[1], m[2], m[3], m[4], m[5], m[6]))
	r.frozen = t.UTC()
}

func (r *Runner) runStep(id, typ string, args map[string]any) StepResult {
	sr := StepResult{ID: id, Type: typ, Failures: []string{}}
	r.e.Reports = nil
	r.caughtFatal = nil
	handlers := map[string]func(map[string]any) any{
		"sheet":             r.stepSheet,
		"set_time":          r.stepSetTime,
		"create_records":    r.stepCreateRecords,
		"add_autocur":       r.stepAddAutocur,
		"column":            r.stepColumn,
		"layout":            r.stepLayout,
		"record":            r.stepRecord,
		"search":            r.stepSearch,
		"view":              r.stepView,
		"user":              r.stepUser,
		"lua":               r.stepLua,
		"db":                r.stepDB,
		"set_multivalue":    r.stepSetMultivalue,
		"held":              r.stepHeld,
		"reset":             r.stepReset,
		"graph":             r.stepGraph,
		"globe":             r.stepGlobe,
		"import":            r.stepImport,
		"graphs":            r.stepGraphs,
		"clear_layouts":     func(map[string]any) any { r.clearLayouts(); return map[string]any{} },
		"datum":             r.stepDatum,
		"alert":             r.stepAlert,
		"alert_send":        r.stepAlertSend,
		"emails":            r.stepEmails,
		"cli":               r.stepCLI,
		"alert_description": r.stepAlertDescription,
		"views":             r.stepViews,
		"view_held":         r.stepViewHeld,
		"sleep":             r.stepSleep,
		"template":          r.stepTemplate,
		"http":              r.stepHTTP,
		"login":             r.stepLogin,
		"file_access":       r.stepFileAccess,
		"repeat_single":     r.stepRepeatSingle,
		"table_permission":  r.stepTablePermission,
		"download":          r.stepDownload,
		"historic_purge":    r.stepHistoricPurge,
		"topic":             r.stepTopic,
		"site":              r.stepSite,
		"instances":         r.stepInstances,
		"filter_held":       r.stepFilterHeld,
	}
	h, ok := handlers[typ]
	if !ok {
		sr.Failures = []string{"Unknown step type " + typ}
		return sr
	}
	var observed any
	if truthy(args["ignore_permissions"]) {
		r.e.IgnorePermissions = true
	}
	// env: environment variables the Perl code checks, as engine flags
	env, _ := args["env"].(map[string]any)
	r.e.AllowBlankCurval = truthy(env["GADS_ALLOW_BLANK_CURVAL"])
	r.e.PanicOnCurvalBuildValues = truthy(env["PANIC_ON_CURVAL_BUILD_VALUES"])
	// runs: run the step several times (benchmarks), observing the last
	// run; the time of each run is reported with GADS_BENCH
	runs := 1
	if v, ok := args["runs"]; ok {
		if n, _ := strconv.Atoi(str(v)); n > 0 {
			runs = n
		}
	}
	var times []time.Duration
	err := gads.Try(func() {
		for i := 0; i < runs; i++ {
			t0 := time.Now()
			observed = h(args)
			times = append(times, time.Since(t0))
		}
	})
	r.e.IgnorePermissions = false
	r.e.AllowBlankCurval = false
	r.e.PanicOnCurvalBuildValues = false
	if err != nil {
		msg := err.Error()
		if r.caughtFatal != nil && typ != "http" && typ != "login" {
			msg = *r.caughtFatal
		}
		msg = normaliseError(msg)
		sr.Error = &msg
	}
	r.caughtFatal = nil
	r.e.PanicOnCode = false
	if typ == "http" || typ == "login" {
		// The web application reports to the session (as in Perl, where
		// web requests run outside the runner's try block)
		r.e.Reports = nil
	}
	for _, rep := range r.e.Reports {
		if rep.Level == gads.LevelWarning && truthy(args["capture_warnings"]) {
			// Strip parameters dump from code evaluation warnings
			msg := normaliseError(rep.Msg)
			if i := strings.Index(msg, " (code: "); i >= 0 {
				msg = msg[:i]
			}
			sr.Messages = append(sr.Messages, "WARNING: "+msg)
			continue
		}
		if rep.Level == gads.LevelError || rep.Level == gads.LevelMistake {
			sr.Messages = append(sr.Messages, string(rep.Level)+": "+normaliseError(rep.Msg))
		}
	}
	sr.Observed = plain(observed)
	if bench {
		sr.Timing = newTiming(times)
	}
	sr.Failures = check(args, &sr)
	if sr.Failures == nil {
		sr.Failures = []string{}
	}
	sr.Pass = len(sr.Failures) == 0
	return sr
}

var reLuaChunk = regexp.MustCompile(`\[string "(?:[^"\\]|\\.)*"\]`)
var reAtLine = regexp.MustCompile(`(?m) at \S+ line \d+\.?$`)
var reForStatement = regexp.MustCompile(`(?s) \[for Statement ".*$`)

func normaliseError(msg string) string {
	msg = strings.TrimRight(msg, " \t\n")
	msg = reLuaChunk.ReplaceAllString(msg, "[string]")
	msg = reAtLine.ReplaceAllString(msg, "")
	// The SQL of a DBI exception (generated differently by DBIx::Class and
	// the Go port)
	msg = reForStatement.ReplaceAllString(msg, "")
	return msg
}

// plain converts observations into strings/lists/maps.
func plain(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return x
	case bool:
		if x {
			return "1"
		}
		return ""
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return perl.NumString(x)
	case *int64:
		if x == nil {
			return nil
		}
		return strconv.FormatInt(*x, 10)
	case time.Time:
		return x.UTC().Format("2006-01-02T15:04:05Z")
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out
	case []int64:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = strconv.FormatInt(e, 10)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = plain(e)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	}
	return fmt.Sprint(v)
}

func check(args map[string]any, sr *StepResult) []string {
	var failures []string
	if ee, ok := args["expect_error"]; ok {
		re := regexp.MustCompile(fmt.Sprint(ee))
		if sr.Error == nil {
			failures = append(failures, fmt.Sprintf("expected error matching /%v/ but step succeeded", ee))
		} else if !re.MatchString(*sr.Error) {
			failures = append(failures, fmt.Sprintf("error '%s' does not match /%v/", *sr.Error, ee))
		}
	} else if sr.Error != nil && !truthy(args["allow_error"]) {
		failures = append(failures, "unexpected error: "+*sr.Error)
	}
	if exp, ok := args["expect"]; ok && exp != nil {
		failures = append(failures, compare(exp, sr.Observed, "observed")...)
	}
	if exp, ok := args["expect_first"]; ok && exp != nil {
		var first any
		if m, ok := sr.Observed.(map[string]any); ok {
			if rows, ok := m["rows"].([]any); ok && len(rows) > 0 {
				first = rows[0]
			}
		}
		failures = append(failures, compare(exp, first, "observed.rows[0]")...)
	}
	if exp, ok := args["expect_messages"]; ok {
		var msgs any = []any{}
		if sr.Messages != nil {
			m := make([]any, len(sr.Messages))
			for i, s := range sr.Messages {
				m[i] = s
			}
			msgs = m
		}
		failures = append(failures, compare(exp, msgs, "messages")...)
	}
	if re, ok := args["expect_messages_match"]; ok {
		rx := regexp.MustCompile(str(re))
		found := false
		for _, m := range sr.Messages {
			if rx.MatchString(m) {
				found = true
			}
		}
		if !found {
			failures = append(failures, fmt.Sprintf("no message matching /%v/", re))
		}
	}
	return failures
}

func show(v any) string {
	if v == nil {
		return "undef"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func yamlScalar(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case float64:
		return perl.NumString(x), true
	case bool:
		if x {
			return "1", true
		}
		return "", true
	}
	return fmt.Sprint(v), true
}

func compare(exp, got any, path string) []string {
	switch e := exp.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: expected hash, got %s", path, show(got))}
		}
		keys := make([]string, 0, len(e))
		for k := range e {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var f []string
		for _, k := range keys {
			f = append(f, compare(e[k], g[k], path+"."+k)...)
		}
		return f
	case []any:
		g, ok := got.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: expected list %s, got %s", path, show(plain(e)), show(got))}
		}
		if len(e) != len(g) {
			return []string{fmt.Sprintf("%s: expected %s, got %s", path, show(plain(e)), show(got))}
		}
		var f []string
		for i := range e {
			f = append(f, compare(e[i], g[i], fmt.Sprintf("%s[%d]", path, i))...)
		}
		return f
	}
	es, eok := yamlScalar(exp)
	if !eok && got == nil {
		return nil
	}
	if eok && es == "@any" {
		if got == nil {
			return []string{fmt.Sprintf("%s: expected a value, got undef", path)}
		}
		return nil
	}
	if eok && strings.HasPrefix(es, "~") {
		re := regexp.MustCompile(es[1:])
		gs, ok := got.(string)
		if !ok || !re.MatchString(gs) {
			return []string{fmt.Sprintf("%s: expected match /%s/, got %s", path, es[1:], show(got))}
		}
		return nil
	}
	gs, gok := got.(string)
	if !eok || got == nil || !gok || es != gs {
		var ev any
		if eok {
			ev = es
		}
		return []string{fmt.Sprintf("%s: expected %s, got %s", path, show(ev), show(got))}
	}
	return nil
}

// WriteJSON writes a result file.
func WriteJSON(file string, res *Result) error {
	b, err := json.MarshalIndent(res, "", "   ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0o644)
}

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != "" && x != "0"
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	}
	return true
}

func str(v any) string {
	s, _ := yamlScalar(v)
	return s
}

func strList(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, a := range arr {
		out = append(out, str(a))
	}
	return out
}

func (r *Runner) sheet(alias any) *DataSheet {
	a := str(alias)
	if a == "" {
		a = r.first
	}
	s, ok := r.sheets[a]
	if !ok {
		gads.Fail("Unknown sheet %s", a)
	}
	return s
}

func (r *Runner) firstSheet() *DataSheet { return r.sheets[r.first] }

var sheetUserRe = regexp.MustCompile(`^@sheet:(\w+)$`)

func (r *Runner) user(ref any) *gads.User {
	if ref == nil {
		return r.firstSheet().User()
	}
	s := str(ref)
	if s == "none" {
		return nil
	}
	// The user of a sheet (as $sheet->user)
	if m := sheetUserRe.FindStringSubmatch(s); m != nil {
		return r.sheet(m[1]).User()
	}
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		u := r.e.FindUser(id)
		if u == nil {
			gads.Fail("User %s not found", s)
		}
		return u
	}
	if u, ok := r.firstSheet().Users()[s]; ok {
		return u
	}
	if u, ok := r.users[s]; ok {
		return u
	}
	gads.Fail("Unknown user %s", s)
	return nil
}

func (r *Runner) layoutFor(sheet any, userRef any) *gads.Layout {
	s := r.sheet(sheet)
	if userRef == nil {
		return s.Layout()
	}
	return r.e.NewLayout(s.Layout().InstanceID, r.user(userRef))
}

func (r *Runner) layoutForStep(args map[string]any) *gads.Layout {
	if truthy(args["layout_user"]) {
		return r.layoutFor(args["sheet"], args["user"])
	}
	return r.sheet(args["sheet"]).Layout()
}

func (r *Runner) col(layout *gads.Layout, ref any) *gads.Column {
	s := str(ref)
	if s == "" {
		gads.Fail("Missing column reference")
	}
	if strings.HasPrefix(s, "@col:") {
		id, ok := r.cols[s[5:]]
		if !ok {
			gads.Fail("Unknown column alias %s", s[5:])
		}
		c := layout.Column(id)
		if c == nil {
			gads.Fail("Column %s not in layout", s[5:])
		}
		return c
	}
	if i := strings.Index(s, ":"); i >= 0 {
		alias, name := s[:i], s[i+1:]
		other := r.sheet(alias).Layout()
		if layout != nil && other.InstanceID != layout.InstanceID {
			other = layout.Clone(other.InstanceID)
		} else if layout != nil {
			other = layout
		}
		return r.col(other, name)
	}
	if isDigits(s) {
		id, _ := strconv.ParseInt(s, 10, 64)
		return layout.Column(id)
	}
	if strings.HasPrefix(s, "_") {
		c := layout.ColumnByNameShort(s)
		if c == nil {
			gads.Fail("Unknown internal column %s", s)
		}
		return c
	}
	if c := layout.ColumnByName(s); c != nil {
		return c
	}
	if c := layout.ColumnByNameShort(s); c != nil {
		return c
	}
	gads.Fail("Unknown column %s", s)
	return nil
}

// tryCol resolves a column as the Perl runner's eval of _col. An error
// caught there is still recorded by the step's try block (Log::Report), so
// is the error of the step should it fail (wasFatal is the first fatal
// exception).
func (r *Runner) tryCol(layout *gads.Layout, ref string) *gads.Column {
	var c *gads.Column
	if err := gads.Try(func() { c = r.col(layout, ref) }); err != nil {
		if ge, ok := err.(*gads.Error); ok && r.caughtFatal == nil {
			msg := ge.Error()
			r.caughtFatal = &msg
		}
		return nil
	}
	return c
}

func (r *Runner) filterID(layout *gads.Layout, ref any) string {
	s := str(ref)
	if regexp.MustCompile(`^[0-9]+(_[0-9]+)?$`).MatchString(s) {
		return s
	}
	if c := r.tryCol(layout, s); c != nil {
		return strconv.FormatInt(c.ID, 10)
	}
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		pn := strings.Join(parts[:i], "_")
		cn := strings.Join(parts[i:], "_")
		p := r.tryCol(layout, pn)
		if p == nil || !p.IsCurcommon() {
			continue
		}
		c := r.tryCol(p.LayoutParent(), cn)
		if c == nil {
			continue
		}
		return fmt.Sprintf("%d_%d", p.ID, c.ID)
	}
	gads.Fail("Unable to resolve filter column %s", s)
	return ""
}

func (r *Runner) convertFilter(layout *gads.Layout, f any) map[string]any {
	m, ok := f.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	if rules, ok := m["rules"].([]any); ok {
		var nr []any
		for _, rule := range rules {
			nr = append(nr, r.convertFilter(layout, rule))
		}
		if nr == nil {
			nr = []any{}
		}
		out["rules"] = nr
		return out
	}
	if n, ok := out["name"]; ok && n != nil {
		delete(out, "name")
		out["id"] = r.filterIDTyped(layout, n)
	} else if id, ok := out["id"]; ok && id != nil {
		out["id"] = r.filterIDTyped(layout, id)
	}
	if id, ok := out["id"]; ok {
		if _, ok := out["field"]; !ok {
			out["field"] = id
		}
		if _, ok := out["type"]; !ok {
			out["type"] = "string"
		}
	}
	if s, ok := out["value"].(string); ok {
		if mm := regexp.MustCompile(`^@record:(\w+)$`).FindStringSubmatch(s); mm != nil {
			id, ok := r.records[mm[1]]
			if !ok {
				gads.Fail("Unknown record alias %s", mm[1])
			}
			out["value"] = strconv.FormatInt(id, 10)
		}
	}
	// YAML numbers become strings in Perl; keep values as strings
	switch v := out["value"].(type) {
	case int:
		out["value"] = strconv.Itoa(v)
	case float64:
		out["value"] = perl.NumString(v)
	case []any:
		var nv []any
		for _, x := range v {
			if s, ok := yamlScalar(x); ok {
				nv = append(nv, s)
			} else {
				nv = append(nv, nil)
			}
		}
		out["value"] = nv
	}
	return out
}

// filterIDTyped returns the id as a number (single) or string (pair), as
// the Perl runner produces.
func (r *Runner) filterIDTyped(layout *gads.Layout, ref any) any {
	s := r.filterID(layout, ref)
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	return s
}

func (r *Runner) convertValue(c *gads.Column, v any) any {
	if v == nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		var out []any
		for _, x := range arr {
			out = append(out, r.convertSingle(c, x))
		}
		if out == nil {
			out = []any{}
		}
		return out
	}
	if m, ok := v.(map[string]any); ok && c.Type == "daterange" {
		return []any{r.convertSingle(c, m["from"]), r.convertSingle(c, m["to"])}
	}
	return r.convertSingle(c, v)
}

var fieldRefRe = regexp.MustCompile(`@field:(\w+)`)

func (r *Runner) convertSingle(c *gads.Column, v any) any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	s, _ := yamlScalar(v)
	if mm := regexp.MustCompile(`^@record:(\w+)$`).FindStringSubmatch(s); mm != nil {
		id, ok := r.records[mm[1]]
		if !ok {
			gads.Fail("Unknown record alias %s", mm[1])
		}
		return strconv.FormatInt(id, 10)
	}
	// Query values of curvals: @field:name is the field name of a column in
	// the referred sheet
	if c.IsCurcommon() {
		s = fieldRefRe.ReplaceAllStringFunc(s, func(m string) string {
			return r.col(c.LayoutParent(), fieldRefRe.FindStringSubmatch(m)[1]).Field()
		})
	}
	if (c.Type == "enum" || c.Type == "tree") && s != "" && !isDigits(s) {
		for _, e := range c.Enumvals() {
			if str(e["value"]) == s {
				return strconv.FormatInt(e["id"].(int64), 10)
			}
		}
		gads.Fail("Unknown enum value %s for %s", s, c.Name)
	}
	return s
}
