package conformance

import (
	"fmt"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
)

// stepFilterHeld operates on a filter object (GADS::Filter) kept between
// steps.
func (r *Runner) stepFilterHeld(args map[string]any) any {
	op := str(args["op"])
	if op == "" {
		op = "get"
	}
	name := str(args["as"])
	if name == "" {
		gads.Fail("Held filter alias (as) required")
	}
	if r.heldFilters == nil {
		r.heldFilters = map[string]*gads.Filter{}
	}
	if op == "new" {
		if v, ok := args["as_json"]; ok {
			r.heldFilters[name] = gads.NewFilterJSON(gads.EncodeJSON(v), nil)
		} else if v, ok := args["as_hash"]; ok {
			h, _ := v.(map[string]any)
			r.heldFilters[name] = gads.NewFilterHash(h, nil)
		} else {
			r.heldFilters[name] = gads.NewFilter(nil)
		}
	}
	f, ok := r.heldFilters[name]
	if !ok {
		gads.Fail("Unknown held filter %s", name)
	}
	switch op {
	case "set_json":
		f.SetJSON(gads.EncodeJSON(args["value"]))
	case "set_hash":
		h, _ := args["value"].(map[string]any)
		f.SetHash(h)
	case "new", "get":
	default:
		gads.Fail("Unknown held filter op %s", op)
	}
	obs := map[string]any{}
	for _, o := range strList(args["observe"]) {
		switch o {
		case "as_hash":
			obs[o] = f.AsHash()
		case "as_json":
			obs[o], _ = gads.DecodeJSON(f.AsJSON())
		case "changed":
			obs[o] = boolInt(f.Changed())
		case "column_ids":
			var ids []string
			for _, id := range f.ColumnIDs() {
				ids = append(ids, fmt.Sprint(id))
			}
			obs[o] = strings.Join(ids, " ")
		default:
			gads.Fail("Unknown filter observation %s", o)
		}
	}
	return obs
}
