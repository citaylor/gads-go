package web

import (
	"math"
	"sort"
	"time"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// fromEpochMS is DateTime->from_epoch(epoch => ms / 1000)
func fromEpochMS(v any) time.Time {
	secs := perlNum(tt.Str(v)) / 1000
	whole := math.Floor(secs)
	return time.Unix(int64(whole), int64(math.Round((secs-whole)*1e9))).UTC()
}

// post '/:layout_name/edits' (GADS.pm:4039): the changes of dates made on
// the timeline
func (s *Server) hEdits(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	// Perl returns a bad request if the JSON is invalid, but $@ has been
	// cleared by the time that it is checked: invalid JSON is no records
	// (each of an undefined hash reference autovivifies it)
	decoded, err := gads.DecodeJSON(c.ParamS("q"))
	records, ok := decoded.(map[string]any)
	if err == nil && decoded != nil && !ok {
		if _, isList := decoded.([]any); isList {
			gads.Panic("Not a HASH reference")
		}
		gads.Panic(`Can't use string ("%s") as a HASH ref while "strict refs" in use`, tt.Str(decoded))
	}
	// Perl takes the records in the order of its hash
	var keys []string
	for k := range records {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	failed := false
	for _, k := range keys {
		values, _ := records[k].(map[string]any)
		record := s.E.NewRecord(layout, user)
		currentID, _ := toInt64(values["current_id"])
		record.FindCurrentID(currentID, gads.FindOptions{})
		// The layout may be that of another table
		layout = record.Layout()
		colID, _ := toInt64(values["column"])
		col := layout.Column(colID)
		if col == nil {
			gads.Panic(`Can't call method "type" on an undefined value`)
		}
		datum := record.Fields()[colID]
		if col.Type == "date" {
			toWrite := fromEpochMS(values["from"])
			if !c.Process(func() { datum.SetValue(toWrite, gads.SetValueOptions{}) }) {
				failed = true
				continue
			}
		} else {
			// The end date as reported by the timeline is a day later than
			// expected (midnight of the following day), so a day is
			// subtracted from it
			toWrite := []any{fromEpochMS(values["from"]), fromEpochMS(values["to"])}
			if !c.Process(func() { datum.SetValue(toWrite, gads.SetValueOptions{SubtractDaysEnd: 1}) }) {
				failed = true
				continue
			}
		}
		if !c.Process(func() { record.Write(gads.WriteOptions{}) }) {
			failed = true
		}
	}
	if failed {
		// Errors are already written to display
		c.Redirect("/data")
	}
	c.ForwardHome("success", "Submission has been completed successfully", layout.Identifier()+"/data")
	return ""
}
