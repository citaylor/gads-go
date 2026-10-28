package conformance

import (
	"strconv"

	"github.com/ctrlo/gads-go/internal/gads"
)

// stepAlert writes an alert on a view (GADS::Alert). frequency null removes
// the alert.
func (r *Runner) stepAlert(args map[string]any) any {
	layout := r.sheet(args["sheet"]).Layout()
	vid, ok := r.views[str(args["view"])]
	if !ok {
		gads.Fail("Unknown view %s", str(args["view"]))
	}
	a := r.e.NewAlert(layout, r.user(args["user"]), vid)
	a.SetFrequency(args["frequency"])
	a.Write()
	if args["frequency"] == nil {
		return map[string]any{"id": nil}
	}
	return map[string]any{"id": a.ID}
}

// stepAlertSend processes alerts for changes to records, as GADS does after
// updating code values.
func (r *Runner) stepAlertSend(args map[string]any) any {
	layout := r.sheet(args["sheet"]).Layout()
	var cols []int64
	for _, n := range strList(args["columns"]) {
		cols = append(cols, r.col(layout, n).ID)
	}
	r.e.NewAlertSend(layout, int64List(args["current_ids"]), cols, false).Process()
	return map[string]any{}
}

// stepAlertDescription observes the descriptions and links of records for
// alert emails, for each user in turn (sharing the description cache).
func (r *Runner) stepAlertDescription(args map[string]any) any {
	layout := r.sheet(args["sheet"]).Layout()
	ad := r.e.NewAlertDescription()
	cids := int64List(args["current_ids"])
	out := []any{}
	for _, u := range strList(args["users"]) {
		user := r.user(u)
		out = append(out, map[string]any{
			"description": ad.Description(layout.InstanceID, cids, user),
			"link":        ad.Link(layout.InstanceID, cids, user),
		})
	}
	return out
}

// int64List converts a scalar or list of IDs.
func int64List(v any) []int64 {
	var out []int64
	switch x := v.(type) {
	case nil:
	case []any:
		for _, e := range x {
			id, _ := strconv.ParseInt(str(e), 10, 64)
			out = append(out, id)
		}
	default:
		id, _ := strconv.ParseInt(str(x), 10, 64)
		out = append(out, id)
	}
	return out
}

// stepEmails observes the emails sent since the last emails step.
func (r *Runner) stepEmails(map[string]any) any {
	out := r.emails
	r.emails = []any{}
	return out
}

func toAnyList(l []string) []any {
	out := []any{}
	for _, s := range l {
		out = append(out, s)
	}
	return out
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
