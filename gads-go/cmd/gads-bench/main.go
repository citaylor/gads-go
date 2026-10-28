// Command gads-bench measures and compares the performance of the Perl and
// Go implementations.
//
//	gads-bench units [-time 1s] [-fixture FILE] [-out units-go.json] [unit ...]
//	    runs the unit micro-benchmarks (internal/benchunits) and writes
//	    their times and output digests as JSON, in the format of
//	    conformance/perl/bench_units.pl (-fixture: the scenario of the
//	    database units, by default conformance/bench/fixtures/units_db.yaml)
//
//	gads-bench compare -perl DIR -go DIR [-units-perl FILE -units-go FILE] [-md]
//	    compares the step timings of the benchmark scenarios run by both
//	    runners with GADS_BENCH set (and the unit times, if given), and
//	    checks that the observations of each scenario, and the outputs of
//	    each unit, are the same
//
// conformance/bench_all.sh runs all of it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/conformance"
	"github.com/ctrlo/gads-go/internal/benchunits"
)

// stdout is where the results are written (a variable for the tests)
var stdout io.Writer = os.Stdout

func main() { os.Exit(run(os.Args[1:])) }

// run runs the command with its arguments, returning the exit status
func run(args []string) int {
	if len(args) < 1 {
		return usage()
	}
	switch args[0] {
	case "units":
		return units(args[1:])
	case "compare":
		return compare(args[1:])
	}
	return usage()
}

func usage() int {
	fmt.Fprintln(os.Stderr, "usage: gads-bench units [-time 1s] [-fixture FILE] [-out FILE] [unit ...]")
	fmt.Fprintln(os.Stderr, "       gads-bench compare -perl DIR -go DIR [-units-perl FILE -units-go FILE] [-md]")
	return 2
}

// unitsFile is the JSON of the unit times of a runner
type unitsFile struct {
	Runner   string              `json:"runner"`
	Go       string              `json:"go,omitempty"`
	Perl     string              `json:"perl,omitempty"`
	Backends map[string]string   `json:"backends,omitempty"`
	Units    []benchunits.Result `json:"units"`
}

func units(args []string) int {
	fs := flag.NewFlagSet("units", flag.ContinueOnError)
	d := fs.Duration("time", time.Second, "minimum time to run each unit")
	out := fs.String("out", "", "output file (default standard output)")
	fs.StringVar(&benchunits.FixtureFile, "fixture", "", "scenario of the database units")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	defer benchunits.CloseFixture()
	res := unitsFile{Runner: "go", Go: goVersion(), Units: []benchunits.Result{}}
	for _, u := range benchunits.Units {
		if !matches(u.Name, fs.Args()) {
			continue
		}
		r := benchunits.Measure(u, *d)
		fmt.Fprintf(os.Stderr, "%-24s %10d %16.1f ns/op %s\n", r.Name, r.Iterations, r.NsPerOp, spreadStr(r.NsPerOp, r.MinNs, r.MaxNs))
		round := func(f float64) float64 { return math.Round(f*10) / 10 }
		r.NsPerOp, r.MinNs, r.MaxNs = round(r.NsPerOp), round(r.MinNs), round(r.MaxNs)
		for i := range r.Samples {
			r.Samples[i] = round(r.Samples[i])
		}
		res.Units = append(res.Units, r)
	}
	js, _ := json.MarshalIndent(res, "", "   ")
	js = append(js, '\n')
	if *out == "" {
		stdout.Write(js)
		return 0
	}
	if err := os.WriteFile(*out, js, 0o644); err != nil {
		return fatal(err)
	}
	return 0
}

func matches(name string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if strings.Contains(name, f) {
			return true
		}
	}
	return false
}

// benchResult is the part of a runner's observation file used here
type benchResult struct {
	Scenario  string  `json:"scenario"`
	Fatal     string  `json:"fatal"`
	ElapsedMS float64 `json:"elapsed_ms"`
	Steps     []struct {
		ID     string `json:"id"`
		Pass   bool   `json:"pass"`
		Timing *struct {
			Runs     int     `json:"runs"`
			TotalMS  float64 `json:"total_ms"`
			MedianMS float64 `json:"median_ms"`
		} `json:"timing"`
	} `json:"steps"`
}

func readJSON(file string, v any) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// table is a table printed as aligned text or as Markdown
type table struct {
	md   bool
	head []string
	rows [][]string
}

func (t *table) add(cells ...string) { t.rows = append(t.rows, cells) }

func (t *table) print() {
	if t.md {
		fmt.Fprintln(stdout, "| "+strings.Join(t.head, " | ")+" |")
		sep := make([]string, len(t.head))
		for i := range sep {
			sep[i] = "---"
			if i > 0 {
				sep[i] = "---:"
			}
		}
		fmt.Fprintln(stdout, "| "+strings.Join(sep, " | ")+" |")
		for _, r := range t.rows {
			fmt.Fprintln(stdout, "| "+strings.Join(r, " | ")+" |")
		}
		fmt.Fprintln(stdout)
		return
	}
	w := make([]int, len(t.head))
	for _, r := range append([][]string{t.head}, t.rows...) {
		for i, c := range r {
			if len(c) > w[i] {
				w[i] = len(c)
			}
		}
	}
	line := func(r []string) {
		var b strings.Builder
		for i, c := range r {
			if i == 0 {
				fmt.Fprintf(&b, "%-*s", w[i], c)
			} else {
				fmt.Fprintf(&b, "  %*s", w[i], c)
			}
		}
		fmt.Fprintln(stdout, strings.TrimRight(b.String(), " "))
	}
	line(t.head)
	for _, r := range t.rows {
		line(r)
	}
	fmt.Fprintln(stdout)
}

// ratio is how many times faster Go is than Perl ("perl / go")
func ratio(perl, goT float64) string {
	if perl <= 0 || goT <= 0 {
		return "-"
	}
	r := perl / goT
	if r >= 10 {
		return fmt.Sprintf("%.0fx", r)
	}
	return fmt.Sprintf("%.1fx", r)
}

func msStr(v float64) string {
	switch {
	case v >= 1000:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func nsStr(ns float64) string {
	switch {
	case ns >= 1e6:
		return fmt.Sprintf("%.2f ms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.1f µs", ns/1e3)
	}
	return fmt.Sprintf("%.0f ns", ns)
}

// geomean is the geometric mean of the ratios (the typical speed-up)
type geomean struct {
	sum float64
	n   int
}

func (g *geomean) add(perl, goT float64) {
	if perl > 0 && goT > 0 {
		g.sum += math.Log(perl / goT)
		g.n++
	}
}

func (g *geomean) String() string {
	if g.n == 0 {
		return "-"
	}
	return ratio(math.Exp(g.sum/float64(g.n)), 1)
}

func compare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	perlDir := fs.String("perl", "../conformance/results/bench-perl", "Perl results directory")
	goDir := fs.String("go", "../conformance/results/bench-go", "Go results directory")
	unitsPerl := fs.String("units-perl", "", "Perl unit times (bench_units.pl)")
	unitsGo := fs.String("units-go", "", "Go unit times (gads-bench units)")
	md := fs.Bool("md", false, "print Markdown tables")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	status := 0
	files, _ := filepath.Glob(filepath.Join(*perlDir, "*.json"))
	sort.Strings(files)
	var all geomean
	summary := &table{md: *md, head: []string{"scenario", "perl ms", "go ms", "go faster", "parity"}}
	var totalPerl, totalGo float64
	for _, pf := range files {
		name := strings.TrimSuffix(filepath.Base(pf), ".json")
		gf := filepath.Join(*goDir, name+".json")
		var p, g benchResult
		if err := readJSON(pf, &p); err != nil {
			return fatal(err)
		}
		if err := readJSON(gf, &g); err != nil {
			fmt.Fprintf(stdout, "%s: no Go result (%v)\n\n", name, err)
			status = 1
			continue
		}
		parity := "same"
		if diffs, err := conformance.CompareFiles(pf, gf); err != nil {
			parity = "error"
			status = 1
		} else if len(diffs) > 0 {
			parity = fmt.Sprintf("%d diffs", len(diffs))
			status = 1
		}
		if p.Fatal != "" || g.Fatal != "" {
			parity = "fatal"
			status = 1
		}
		goSteps := map[string]int{}
		for i, s := range g.Steps {
			goSteps[s.ID] = i
		}
		t := &table{md: *md, head: []string{name, "runs", "perl median ms", "go median ms", "go faster"}}
		var sc geomean
		for _, ps := range p.Steps {
			gi, ok := goSteps[ps.ID]
			if !ok || ps.Timing == nil || g.Steps[gi].Timing == nil {
				continue
			}
			gs := g.Steps[gi]
			pm, gm := ps.Timing.MedianMS, gs.Timing.MedianMS
			id := ps.ID
			if !ps.Pass || !gs.Pass {
				id += " (FAILED)"
				status = 1
			}
			t.add(id, fmt.Sprint(ps.Timing.Runs), msStr(pm), msStr(gm), ratio(pm, gm))
			sc.add(pm, gm)
			all.add(pm, gm)
		}
		t.add("geometric mean of steps", "", "", "", sc.String())
		if *md {
			fmt.Fprintf(stdout, "### %s\n\n", name)
		}
		t.print()
		summary.add(name, msStr(p.ElapsedMS), msStr(g.ElapsedMS), ratio(p.ElapsedMS, g.ElapsedMS), parity)
		totalPerl += p.ElapsedMS
		totalGo += g.ElapsedMS
	}
	if len(files) > 0 {
		summary.add("total", msStr(totalPerl), msStr(totalGo), ratio(totalPerl, totalGo), "")
		summary.add("geometric mean of all steps", "", "", all.String(), "")
		if *md {
			fmt.Fprintln(stdout, "### Scenarios")
			fmt.Fprintln(stdout)
		}
		summary.print()
	}

	if *unitsPerl != "" && *unitsGo != "" {
		var p, g unitsFile
		if err := readJSON(*unitsPerl, &p); err != nil {
			return fatal(err)
		}
		if err := readJSON(*unitsGo, &g); err != nil {
			return fatal(err)
		}
		goUnits := map[string]benchunits.Result{}
		for _, u := range g.Units {
			goUnits[u.Name] = u
		}
		t := &table{md: *md, head: []string{"unit", "perl", "spread", "go", "spread", "go faster", "output"}}
		var gm geomean
		for _, u := range p.Units {
			gu, ok := goUnits[u.Name]
			if !ok {
				continue
			}
			output := "same"
			switch {
			case u.OutputSHA256 == "" || gu.OutputSHA256 == "":
				output = "-"
			case u.OutputSHA256 != gu.OutputSHA256:
				output = "DIFFERENT"
				status = 1
			}
			t.add(u.Name, nsStr(u.NsPerOp), spreadStr(u.NsPerOp, u.MinNs, u.MaxNs),
				nsStr(gu.NsPerOp), spreadStr(gu.NsPerOp, gu.MinNs, gu.MaxNs), ratio(u.NsPerOp, gu.NsPerOp), output)
			gm.add(u.NsPerOp, gu.NsPerOp)
		}
		t.add("geometric mean", "", "", "", "", gm.String(), "")
		if *md {
			fmt.Fprintln(stdout, "### Units")
			fmt.Fprintln(stdout)
		}
		t.print()
		var notes []string
		if p.Perl != "" {
			notes = append(notes, "perl "+p.Perl)
		}
		keys := make([]string, 0, len(p.Backends))
		for k := range p.Backends {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			notes = append(notes, k+" "+p.Backends[k])
		}
		if g.Go != "" {
			notes = append(notes, g.Go)
		}
		if len(notes) > 0 {
			fmt.Fprintln(stdout, "("+strings.Join(notes, ", ")+")")
		}
	}
	return status
}

// spreadStr is the spread of a unit's samples: half their range, as a
// percentage of the median
func spreadStr(median, min, max float64) string {
	if median == 0 || max == 0 {
		return "-"
	}
	return fmt.Sprintf("±%.0f%%", (max-min)/2/median*100)
}

func fatal(err error) int {
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func goVersion() string { return runtime.Version() }
