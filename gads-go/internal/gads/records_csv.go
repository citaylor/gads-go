package gads

import (
	"strconv"
	"strings"
)

// csvField formats a field as Text::CSV's combine does with its default
// attributes: a field is quoted if it contains the separator, a quote, a
// space (quote_space) or a control or binary character (quote_binary), and
// quotes are doubled. Empty fields are not quoted.
func csvField(s string) string {
	need := false
	for _, c := range s {
		if c == ',' || c == '"' || c == ' ' || c < 0x20 || (c >= 0x7f && c <= 0xa0) {
			need = true
			break
		}
	}
	if !need {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// CSVFormatLine is the line of CSV of the fields, as the downloads write it
// (Text::CSV combine).
func CSVFormatLine(fields []string) string { return csvLine(fields) }

func csvLine(fields []string) string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = csvField(f)
	}
	return strings.Join(out, ",") + "\n"
}

// CSVHeader returns the heading line of a CSV download of the records,
// preceded by any configured header (Records::csv_header).
func (r *Records) CSVHeader() string {
	if !r.Layout.UserCan("download") {
		Fail("You do not have permission to download data")
	}
	var names []string
	if r.HasChildren() {
		names = append(names, "Parent")
	}
	for _, c := range r.ColumnsRender() {
		names = append(names, c.Name)
	}
	out := csvLine(names)
	if h := r.e.Config.Header; h != "" {
		out = h + "\n" + out
	}
	return out
}

// CSVLine returns the next record as a line of CSV, or "" when all records
// have been returned (Records::csv_line).
func (r *Records) CSVLine() (string, bool) {
	if !r.Layout.UserCan("download") {
		Fail("You do not have permission to download data")
	}
	line := r.Single()
	if line == nil {
		return "", false
	}
	var items []string
	if r.HasChildren() {
		if p := line.ParentID(); p != 0 {
			items = append(items, strconv.FormatInt(p, 10))
		} else {
			items = append(items, "")
		}
	}
	for _, c := range r.ColumnsRender() {
		items = append(items, line.GetFieldValue(c).AsString())
	}
	return csvLine(items), true
}
