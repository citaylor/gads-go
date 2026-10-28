package conformance

import (
	"fmt"
	"path"
	"sort"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// ttValue converts decoded YAML data into template values
func ttValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		h := tt.NewHash()
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.Set(k, ttValue(t[k]))
		}
		return h
	case []any:
		l := &tt.List{}
		for _, x := range t {
			l.Items = append(l.Items, ttValue(x))
		}
		return l
	case int:
		return int64(t)
	case bool:
		if t {
			return int64(1)
		}
		return ""
	}
	return v
}

// stepTemplate renders a template with the Go Template Toolkit
// implementation (see the Perl runner's step_template)
func (r *Runner) stepTemplate(args map[string]any) any {
	files := map[string]string{"_main.tt": str(args["template"])}
	if f, ok := args["files"].(map[string]any); ok {
		for k, v := range f {
			files[k] = str(v)
		}
	}
	eng := tt.NewEngine("/tt")
	eng.Load = func(p string) ([]byte, error) {
		rel := path.Clean(p)
		if len(rel) > 4 && rel[:4] == "/tt/" {
			rel = rel[4:]
		}
		if s, ok := files[rel]; ok {
			return []byte(s), nil
		}
		return nil, fmt.Errorf("not found")
	}
	vars, _ := ttValue(args["vars"]).(*tt.Hash)
	out, err := eng.Process("_main.tt", vars)
	if err != nil {
		gads.Fail("%s", err.Error())
	}
	return map[string]any{"output": out}
}
