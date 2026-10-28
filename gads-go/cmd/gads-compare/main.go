// Command gads-compare compares the observation files of the Perl and Go
// runners and reports any difference.
//
//	gads-compare -perl ../conformance/results/perl -go ../conformance/results/go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/conformance"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

// run runs the command with its arguments, returning the exit status
func run(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("gads-compare", flag.ContinueOnError)
	perlDir := fs.String("perl", "../conformance/results/perl", "Perl results directory")
	goDir := fs.String("go", "../conformance/results/go", "Go results directory")
	max := fs.Int("max", 20, "maximum differences to show per scenario")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	files, _ := filepath.Glob(filepath.Join(*perlDir, "*.json"))
	sort.Strings(files)
	total, identical := 0, 0
	for _, pf := range files {
		name := strings.TrimSuffix(filepath.Base(pf), ".json")
		if len(fs.Args()) > 0 {
			match := false
			for _, a := range fs.Args() {
				if strings.Contains(name, a) {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		total++
		gf := filepath.Join(*goDir, name+".json")
		if _, err := os.Stat(gf); err != nil {
			fmt.Fprintf(stdout, "MISSING %s (no Go result)\n", name)
			continue
		}
		diffs, err := conformance.CompareFiles(pf, gf)
		if err != nil {
			fmt.Fprintf(stdout, "ERROR %s: %v\n", name, err)
			continue
		}
		if len(diffs) == 0 {
			identical++
			fmt.Fprintf(stdout, "same  %s\n", name)
			continue
		}
		fmt.Fprintf(stdout, "DIFF  %s (%d differences)\n", name, len(diffs))
		for i, d := range diffs {
			if i >= *max {
				fmt.Fprintf(stdout, "      ... %d more\n", len(diffs)-*max)
				break
			}
			fmt.Fprintf(stdout, "      %s\n", d)
		}
	}
	fmt.Fprintf(stdout, "\n%d scenarios compared, %d identical, %d different\n", total, identical, total-identical)
	if identical != total {
		return 1
	}
	return 0
}
