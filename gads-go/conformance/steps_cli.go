package conformance

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/cli"
	"github.com/ctrlo/gads-go/internal/gads"
)

var (
	cliFileRe   = regexp.MustCompile(`\{file:([^}]+)\}`)
	cliAtLineRe = regexp.MustCompile(` at \S+ line \d+(?:, <\S+> (?:line|chunk) \d+)?\.?\n?`)
)

// stepCLI runs a script of the bin directory (the Go port of it), against
// the scenario's database. Files given are written to a temporary
// directory, referred to in arguments as {file:<name>}; that directory is
// {tmp} in the observed output.
func (r *Runner) stepCLI(args map[string]any) any {
	script := str(args["script"])
	if script == "" {
		gads.Fail("script required")
	}
	r.web() // as the Perl runner, the site of the web harness exists
	// A named directory is kept for the later scripts of the scenario
	name := str(args["dir"])
	tmp := r.cliDirs[name]
	if tmp == "" {
		var err error
		tmp, err = os.MkdirTemp("", "gads-cli")
		if err != nil {
			panic(err)
		}
		if name != "" {
			if r.cliDirs == nil {
				r.cliDirs = map[string]string{}
			}
			r.cliDirs[name] = tmp
		} else {
			defer os.RemoveAll(tmp)
		}
	}
	if files, ok := args["files"].(map[string]any); ok {
		var names []string
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(tmp, n)), 0o700); err != nil {
				panic(err)
			}
			if err := os.WriteFile(filepath.Join(tmp, n), []byte(str(files[n])), 0o600); err != nil {
				panic(err)
			}
		}
	}
	var argv []string
	for _, a := range asList(args["args"]) {
		v := cliFileRe.ReplaceAllString(str(a), tmp+"/$1")
		v = str(r.webValues(v))
		argv = append(argv, v)
	}
	var stdout, stderr bytes.Buffer
	env := cli.NewEnv(r.e, argv, strings.NewReader(str(args["stdin"])), &stdout, &stderr)
	env.Dir = tmp
	env.SortedDirs = true
	siteID := r.e.SiteID
	res := cli.Run(script, env)
	r.e.SiteID = siteID
	clean := func(s string) string {
		s = strings.ReplaceAll(s, tmp, "{tmp}")
		return cliAtLineRe.ReplaceAllString(s, "")
	}
	obs := map[string]any{
		"exit":   strconv.Itoa(res.Exit),
		"stdout": clean(stdout.String()),
		"stderr": clean(stderr.String()),
	}
	if res.Died != "" || res.Exit == 255 {
		obs["died"] = clean(res.Died)
	}
	// The files written by the script: their contents, or (for random
	// contents) their number of lines and first line
	if mode := str(args["observe_files"]); mode != "" {
		given := map[string]bool{}
		if files, ok := args["files"].(map[string]any); ok {
			for n := range files {
				given[n] = true
			}
		}
		files := map[string]any{}
		filepath.WalkDir(tmp, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(tmp, p)
			if given[rel] {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				panic(err)
			}
			content := string(b)
			if mode == "lines" {
				lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
				if content == "" {
					lines = nil
				}
				first := any(nil)
				if len(lines) > 0 {
					first = lines[0]
				}
				files[rel] = map[string]any{"lines": strconv.Itoa(len(lines)), "first": first}
			} else {
				files[rel] = content
			}
			return nil
		})
		obs["files"] = files
	}
	return obs
}
