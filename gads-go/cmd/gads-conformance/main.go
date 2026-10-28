// Command gads-conformance runs the GADS conformance scenarios against the
// Go engine and writes observation files.
//
//	gads-conformance -out ../conformance/results/go ../conformance/scenarios/*.yaml
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"

	"github.com/ctrlo/gads-go/conformance"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

// run runs the command with its arguments, returning the exit status
func run(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("gads-conformance", flag.ContinueOnError)
	out := fs.String("out", "../conformance/results/go", "output directory")
	verbose := fs.Bool("verbose", false, "show passing steps")
	cpuprofile := fs.String("cpuprofile", "", "write a CPU profile to this file (go tool pprof)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "No scenario files specified")
		return 2
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	total, failed := 0, 0
	for _, f := range files {
		res := conformance.RunFile(f)
		if err := conformance.WriteJSON(filepath.Join(*out, res.Scenario+".json"), res); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		total++
		var fails []conformance.StepResult
		for _, s := range res.Steps {
			if !s.Pass {
				fails = append(fails, s)
			}
		}
		if len(fails) > 0 || res.Fatal != "" {
			failed++
			fatal := ""
			if res.Fatal != "" {
				fatal = " FATAL: " + res.Fatal
			}
			fmt.Fprintf(stdout, "FAIL %s (%d/%d steps failed)%s\n", res.Scenario, len(fails), len(res.Steps), fatal)
			for _, s := range fails {
				for _, m := range s.Failures {
					fmt.Fprintf(stdout, "    %s: %s\n", s.ID, m)
				}
			}
		} else {
			fmt.Fprintf(stdout, "ok   %s (%d steps)\n", res.Scenario, len(res.Steps))
		}
		if *verbose {
			for _, s := range res.Steps {
				fmt.Fprintf(stdout, "    [%s] pass=%v\n", s.ID, s.Pass)
			}
		}
	}
	fmt.Fprintf(stdout, "\n%d scenarios, %d failed\n", total, failed)
	if failed > 0 {
		return 1
	}
	return 0
}
