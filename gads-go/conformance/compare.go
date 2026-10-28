package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
)

// Diff is a difference between two observation files.
type Diff struct {
	Step   string
	Path   string
	Perl   any
	Go     any
	Detail string
}

func load(file string) (map[string]any, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// CompareFiles compares a Perl and a Go observation file.
func CompareFiles(perlFile, goFile string) ([]Diff, error) {
	p, err := load(perlFile)
	if err != nil {
		return nil, err
	}
	g, err := load(goFile)
	if err != nil {
		return nil, err
	}
	return CompareResults(p, g), nil
}

// CompareResults compares two decoded observation files.
func CompareResults(p, g map[string]any) []Diff {
	var diffs []Diff
	if pf, gf := p["fatal"], g["fatal"]; pf != nil || gf != nil {
		if fmt.Sprint(pf) != fmt.Sprint(gf) {
			diffs = append(diffs, Diff{Step: "-", Path: "fatal", Perl: pf, Go: gf})
		}
	}
	ps, _ := p["steps"].([]any)
	gs, _ := g["steps"].([]any)
	if len(ps) != len(gs) {
		diffs = append(diffs, Diff{Step: "-", Path: "steps", Detail: fmt.Sprintf("perl has %d steps, go has %d", len(ps), len(gs))})
	}
	n := len(ps)
	if len(gs) < n {
		n = len(gs)
	}
	for i := 0; i < n; i++ {
		pm, _ := ps[i].(map[string]any)
		gm, _ := gs[i].(map[string]any)
		id := fmt.Sprint(pm["id"])
		for _, k := range []string{"observed", "error", "messages"} {
			diffs = append(diffs, diffValues(id, k, pm[k], gm[k])...)
		}
	}
	return diffs
}

// Perl reference addresses (HASH(0x55b9b1433900)) are not reproducible
var reRefAddr = regexp.MustCompile(`\b(HASH|ARRAY|CODE|SCALAR)\(0x[0-9a-f]+\)`)

func diffValues(step, path string, a, b any) []Diff {
	if reflect.DeepEqual(a, b) {
		return nil
	}
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok && reRefAddr.ReplaceAllString(as, "$1(0x0)") == reRefAddr.ReplaceAllString(bs, "$1(0x0)") {
			return nil
		}
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		keys := map[string]bool{}
		for k := range am {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		var out []Diff
		for _, k := range ks {
			out = append(out, diffValues(step, path+"."+k, am[k], bm[k])...)
		}
		return out
	}
	al, aok := a.([]any)
	bl, bok := b.([]any)
	if aok && bok && len(al) == len(bl) {
		var out []Diff
		for i := range al {
			out = append(out, diffValues(step, fmt.Sprintf("%s[%d]", path, i), al[i], bl[i])...)
		}
		return out
	}
	return []Diff{{Step: step, Path: path, Perl: a, Go: b}}
}

// String formats a diff.
func (d Diff) String() string {
	if d.Detail != "" {
		return fmt.Sprintf("%s %s: %s", d.Step, d.Path, d.Detail)
	}
	pj, _ := json.Marshal(d.Perl)
	gj, _ := json.Marshal(d.Go)
	return fmt.Sprintf("%s %s: perl=%s go=%s", d.Step, d.Path, pj, gj)
}
