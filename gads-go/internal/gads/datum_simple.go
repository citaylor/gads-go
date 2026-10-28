package gads

import (
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ctrlo/gads-go/internal/cldr"
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/lua"
	"github.com/ctrlo/gads-go/internal/perl"
)

// optString is a possibly-undef Perl string.
type optString struct {
	s  string
	ok bool
}

func someStr(s string) optString { return optString{s, true} }

// =====================================================================
// String
// =====================================================================

// StringDatum is GADS::Datum::String.
type StringDatum struct {
	datumBase
	values      []optString
	valuesBuilt bool
	textAll     []string
	textBuilt   bool
}

func (d *StringDatum) buildValues() {
	if d.valuesBuilt {
		return
	}
	d.valuesBuilt = true
	d.values = nil
	if !d.hasInitValue {
		return
	}
	for _, r := range d.initValue {
		if r.Value == nil {
			d.values = append(d.values, optString{})
		} else {
			s, _ := db.AsString(r.Value)
			d.values = append(d.values, someStr(s))
		}
	}
}

// Values returns the raw values.
func (d *StringDatum) Values() []optString {
	d.buildValues()
	return d.values
}

// TextAll returns all text values, sorted.
func (d *StringDatum) TextAll() []string {
	if !d.textBuilt {
		var t []string
		for _, v := range d.Values() {
			t = append(t, v.s)
		}
		d.textAll = sortedStrings(t)
		d.textBuilt = true
	}
	return d.textAll
}

var reTrailingH = regexp.MustCompile(`[\t \x{A0}\x{1680}\x{180E}\x{2000}-\x{200A}\x{202F}\x{205F}\x{3000}]+$`)
var reLeadingV = regexp.MustCompile(`^[\n\x0B\f\r\x{85}\x{2028}\x{2029}]+`)

// SetValue implements Datum::String set_value.
func (d *StringDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	in := valueList(value)
	var values []string
	for _, v := range in {
		if v == nil {
			continue
		}
		values = append(values, anyToString(v))
	}
	clone := d.Clone(nil, false)
	textAll := sortedStrings(values)
	oldTexts := d.TextAll()
	if joinSpace(textAll) != joinSpace(oldTexts) {
		for i := range values {
			values[i] = reTrailingH.ReplaceAllString(values[i], "")
			values[i] = reLeadingV.ReplaceAllString(values[i], "")
		}
	}
	changed := joinSpace(textAll) != joinSpace(oldTexts)
	if !opts.Draft {
		if re := d.column.ForceRegex; re != "" {
			rx := perlRegex("(?i)^(?:" + re + ")$")
			for _, val := range values {
				if perl.True(val) && (rx == nil || !rx.MatchString(val)) {
					msg := `Invalid value "` + val + `" for ` + d.column.Name
					if changed {
						Fail("%s", msg)
					} else {
						d.column.e.Warn("%s", msg)
					}
				}
			}
		}
		if maxlen := d.column.MaxLength(); maxlen != nil {
			for _, val := range values {
				if int64(utf8.RuneCountInString(val)) > *maxlen {
					msg := `Value "` + val + `" for ` + d.column.Name + ` exceeds maximum length of ` + itoa(*maxlen) + ` characters`
					if changed {
						Fail("%s", msg)
					} else {
						d.column.e.Warn("%s", msg)
					}
				}
			}
		}
	}
	if changed {
		d.changed = true
		d.values = nil
		for _, v := range values {
			d.values = append(d.values, someStr(v))
		}
		d.valuesBuilt = true
		d.textAll = textAll
		d.textBuilt = true
	}
	d.oldvalue = clone
}

// HTMLForm returns the values for the edit form.
func (d *StringDatum) HTMLForm() []any {
	var out []any
	for _, v := range d.Values() {
		out = append(out, v.s)
	}
	return out
}

// SetValuesForChild returns values for copying to a child.
func (d *StringDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns values used for unique checking.
func (d *StringDatum) SearchValuesUnique() []string {
	var out []string
	for _, v := range d.HTMLForm() {
		out = append(out, anyToString(v))
	}
	return out
}

// Blank reports whether there is no value.
func (d *StringDatum) Blank() bool {
	for _, v := range d.Values() {
		if v.ok && v.s != "" {
			return false
		}
	}
	return true
}

// AsString returns the values joined.
func (d *StringDatum) AsString() string { return strings.Join(d.TextAll(), ", ") }

// HTML returns the HTML-escaped string.
func (d *StringDatum) HTML() string { return htmlEscape(d.AsString()) }

// HasValue reports whether a value was set.
func (d *StringDatum) HasValue() bool { return len(d.Values()) > 0 || d.initNoValue }

// RawValues returns the values.
func (d *StringDatum) RawValues() []any {
	var out []any
	for _, v := range d.Values() {
		if v.ok {
			out = append(out, v.s)
		} else {
			out = append(out, nil)
		}
	}
	return out
}

// ForCode returns the value passed to Lua.
func (d *StringDatum) ForCode(ctx ForCodeContext) any {
	if d.forCodeBuilt {
		return d.forCodeCache
	}
	var vals []any
	for _, t := range d.TextAll() {
		if t == "" {
			vals = append(vals, nil)
		} else {
			vals = append(vals, t)
		}
	}
	var ret any
	if d.column.MultivalueFn() || len(vals) > 1 {
		if vals == nil {
			vals = []any{}
		}
		ret = vals
	} else if len(vals) > 0 {
		ret = vals[0]
	}
	d.forCodeCache, d.forCodeBuilt = ret, true
	return ret
}

// ValueRegexTest returns the text values.
func (d *StringDatum) ValueRegexTest(string) []string { return d.TextAll() }

// FilterValue returns the value for filters.
func (d *StringDatum) FilterValue() string { return d.AsString() }

// Clone copies the datum.
func (d *StringDatum) Clone(record *Record, fresh bool) Datum {
	n := &StringDatum{datumBase: d.cloneBase(record)}
	n.values = append([]optString(nil), d.Values()...)
	n.valuesBuilt = true
	n.textAll = append([]string(nil), d.TextAll()...)
	n.textBuilt = true
	return n
}

// =====================================================================
// Integer
// =====================================================================

// IntegerDatum is GADS::Datum::Integer. Values are kept as the strings
// that Perl would hold (user input or database values).
type IntegerDatum struct {
	datumBase
	values      []any // string, int64, float64 or nil
	valuesBuilt bool
}

func (d *IntegerDatum) Values() []any {
	if !d.valuesBuilt {
		d.valuesBuilt = true
		d.values = nil
		if d.hasInitValue {
			for _, r := range d.initValue {
				d.values = append(d.values, r.Value)
			}
		}
	}
	return d.values
}

var reArith = regexp.MustCompile(`^\s*\(\s*([\*\+\-/])\s*([0-9]+)\s*\)\s*$`)
var reZeros = regexp.MustCompile(`^0+$`)

func intValueString(v any) string { return anyToString(v) }

// SetValue implements Datum::Integer set_value.
func (d *IntegerDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	var in []string
	for _, v := range valueList(value) {
		if v == nil {
			continue
		}
		in = append(in, anyToString(v))
	}
	in = sortedStrings(in)
	clone := d.Clone(nil, false)
	var oldInts []string
	for _, v := range d.Values() {
		if v != nil {
			oldInts = append(oldInts, intValueString(v))
		}
	}
	oldInts = sortedStrings(oldInts)
	var values2 []any
	for _, s := range in {
		var val any = s
		if !perl.True(s) && !reZeros.MatchString(s) {
			val = nil
		}
		if val != nil && perl.True(s) {
			if m := reArith.FindStringSubmatch(s); m != nil {
				op := m[1]
				amount := perl.Numify(m[2])
				for _, v := range d.Values() {
					if v == nil {
						continue
					}
					cur := perl.Numify(intValueString(v))
					var r float64
					switch op {
					case "*":
						r = cur * amount
					case "+":
						r = cur + amount
					case "-":
						r = cur - amount
					case "/":
						r = cur / amount
					}
					values2 = append(values2, perl.NumString(r))
				}
				continue
			}
		}
		d.column.Validate(val, true)
		if val != nil {
			values2 = append(values2, val)
		}
	}
	var v2s []string
	for _, v := range values2 {
		v2s = append(v2s, intValueString(v))
	}
	changed := (len(values2) > 0 || len(oldInts) > 0) && (len(values2) != len(oldInts) || joinSpace(v2s) != joinSpace(oldInts))
	d.changed = changed
	if changed {
		d.values = values2
		d.valuesBuilt = true
	}
	d.oldvalue = clone
}

// HTMLForm returns the form values.
func (d *IntegerDatum) HTMLForm() []any {
	vals := d.Values()
	if len(vals) == 0 {
		return []any{""}
	}
	var out []any
	for _, v := range vals {
		if v == nil {
			out = append(out, "")
		} else {
			out = append(out, intValueString(v))
		}
	}
	return out
}

// SetValuesForChild returns values for child records.
func (d *IntegerDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns values used for unique checks.
func (d *IntegerDatum) SearchValuesUnique() []string {
	var out []string
	for _, v := range d.HTMLForm() {
		out = append(out, anyToString(v))
	}
	return out
}

// Blank reports a blank value.
func (d *IntegerDatum) Blank() bool {
	for _, v := range d.Values() {
		if v != nil && intValueString(v) != "" {
			return false
		}
	}
	return true
}

// TextAll returns the values as strings.
func (d *IntegerDatum) TextAll() []string { return []string{d.AsString()} }

// AsString joins the defined values.
func (d *IntegerDatum) AsString() string {
	var out []string
	for _, v := range d.Values() {
		if v != nil {
			out = append(out, intValueString(v))
		}
	}
	return strings.Join(out, ", ")
}

// HTML escapes the string.
func (d *IntegerDatum) HTML() string { return htmlEscape(d.AsString()) }

// HasValue reports a set value.
func (d *IntegerDatum) HasValue() bool { return len(d.Values()) > 0 || d.initNoValue }

// RawValues returns the values.
func (d *IntegerDatum) RawValues() []any { return d.Values() }

// ForCode returns the Lua value.
func (d *IntegerDatum) ForCode(ctx ForCodeContext) any {
	if d.forCodeBuilt {
		return d.forCodeCache
	}
	var vals []any
	for _, v := range d.Values() {
		if v == nil {
			continue
		}
		vals = append(vals, lua.Int(int64(perl.Int(perl.Numify(intValueString(v))))))
	}
	if len(vals) == 0 {
		vals = []any{nil}
	}
	var ret any
	if d.column.MultivalueFn() || len(vals) > 1 {
		ret = vals
	} else {
		ret = vals[0]
	}
	d.forCodeCache, d.forCodeBuilt = ret, true
	return ret
}

// ValueRegexTest returns text values.
func (d *IntegerDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the filter value.
func (d *IntegerDatum) FilterValue() string { return d.AsString() }

// Clone copies the datum.
func (d *IntegerDatum) Clone(record *Record, fresh bool) Datum {
	n := &IntegerDatum{datumBase: d.cloneBase(record)}
	n.values = append([]any(nil), d.Values()...)
	n.valuesBuilt = true
	return n
}

// =====================================================================
// Date
// =====================================================================

// DateDatum is GADS::Datum::Date.
type DateDatum struct {
	datumBase
	values      []time.Time
	valuesBuilt bool
	textAll     []string
	textBuilt   bool
}

// Values returns the dates.
func (d *DateDatum) Values() []time.Time {
	if !d.valuesBuilt {
		d.valuesBuilt = true
		d.values = nil
		if d.record != nil && d.record.NewEntry() && d.column.DefaultToday() {
			d.values = []time.Time{d.column.e.Now()}
			return d.values
		}
		for _, r := range d.initValue {
			if t, ok := dtFromDB(r.Value); ok {
				d.values = append(d.values, t)
			}
		}
	}
	return d.values
}

func dtFromDB(v any) (time.Time, bool) {
	switch x := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return time.Date(x.Year(), x.Month(), x.Day(), x.Hour(), x.Minute(), x.Second(), 0, time.UTC), true
	case string:
		if x == "" {
			return time.Time{}, false
		}
		return db.ParseDBDateTime(x)
	}
	return time.Time{}, false
}

func (d *DateDatum) asStringOne(t time.Time) string {
	return FormatDate(t, d.column.DateFormat())
}

// TextAll returns the formatted dates.
func (d *DateDatum) TextAll() []string {
	if !d.textBuilt {
		d.textAll = nil
		for _, v := range d.Values() {
			d.textAll = append(d.textAll, d.asStringOne(v))
		}
		d.textBuilt = true
	}
	return d.textAll
}

// SetValue implements Datum::Date set_value.
func (d *DateDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	all := valueList(value)
	clone := d.Clone(nil, false)
	if len(all)%2 == 1 && !truthy(all[0]) {
		all = all[1:]
	}
	var values []time.Time
	for _, v := range all {
		values = append(values, d.toDT(v, opts)...)
	}
	var text []string
	for _, v := range values {
		text = append(text, d.asStringOne(v))
	}
	text = sortedStrings(text)
	if joinSpace(text) != joinSpace(d.TextAll()) {
		d.changed = true
		d.values = values
		d.valuesBuilt = true
		d.textAll = text
		d.textBuilt = true
	}
	d.oldvalue = clone
}

func (d *DateDatum) toDT(v any, opts SetValueOptions) []time.Time {
	if t, ok := v.(time.Time); ok {
		return []time.Time{t.UTC()}
	}
	s := anyToString(v)
	if !truthy(v) || s == "" {
		return nil
	}
	if !d.column.Validate(s, false) && opts.Bulk {
		if dur, ok := parseDuration(s); ok {
			var out []time.Time
			for _, cur := range d.Values() {
				out = append(out, dur.addTo(cur))
			}
			return out
		}
	}
	d.column.Validate(s, true)
	t, _ := d.column.ParseDate(s)
	return []time.Time{t}
}

// HTMLForm returns the formatted values.
func (d *DateDatum) HTMLForm() []any {
	var out []any
	for _, v := range d.Values() {
		out = append(out, d.asStringOne(v))
	}
	return out
}

// SetValuesForChild returns values for child records.
func (d *DateDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns values for unique checks.
func (d *DateDatum) SearchValuesUnique() []string {
	var out []string
	for _, v := range d.HTMLForm() {
		out = append(out, anyToString(v))
	}
	return out
}

// Blank reports no dates.
func (d *DateDatum) Blank() bool { return len(d.Values()) == 0 }

// AsString joins the formatted dates.
func (d *DateDatum) AsString() string { return strings.Join(d.TextAll(), ", ") }

// HTML escapes the string.
func (d *DateDatum) HTML() string { return htmlEscape(d.AsString()) }

// HasValue reports a value.
func (d *DateDatum) HasValue() bool { return len(d.Values()) > 0 }

// RawValues returns the dates.
func (d *DateDatum) RawValues() []any {
	var out []any
	for _, v := range d.Values() {
		out = append(out, v)
	}
	return out
}

// ForCode returns the Lua value.
func (d *DateDatum) ForCode(ctx ForCodeContext) any {
	if d.forCodeBuilt {
		return d.forCodeCache
	}
	var ret any
	if !d.column.MultivalueFn() && d.Blank() {
		ret = nil
	} else {
		var vals []any
		for _, v := range d.Values() {
			v := v
			vals = append(vals, dateForCode(&v))
		}
		if d.column.MultivalueFn() || len(vals) > 1 {
			if vals == nil {
				vals = []any{}
			}
			ret = vals
		} else if len(vals) > 0 {
			ret = vals[0]
		}
	}
	d.forCodeCache, d.forCodeBuilt = ret, true
	return ret
}

// ValueRegexTest returns text values.
func (d *DateDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the filter value.
func (d *DateDatum) FilterValue() string { return d.AsString() }

// Clone copies the datum.
func (d *DateDatum) Clone(record *Record, fresh bool) Datum {
	n := &DateDatum{datumBase: d.cloneBase(record)}
	n.values = append([]time.Time(nil), d.Values()...)
	n.valuesBuilt = true
	return n
}

// =====================================================================
// Daterange
// =====================================================================

// DateSpan is a DateTime::Span.
type DateSpan struct{ Start, End time.Time }

// DaterangeDatum is GADS::Datum::Daterange.
type DaterangeDatum struct {
	datumBase
	values      []DateSpan
	valuesBuilt bool
	textAll     []string
	textBuilt   bool
}

// Values returns the spans.
func (d *DaterangeDatum) Values() []DateSpan {
	if !d.valuesBuilt {
		d.valuesBuilt = true
		d.values = nil
		for _, r := range d.initValue {
			f, ok1 := dtFromDB(r.From)
			t, ok2 := dtFromDB(r.To)
			if !ok1 && !ok2 {
				continue
			}
			d.values = append(d.values, DateSpan{f, t})
		}
	}
	return d.values
}

// DaterangeAsString formats a span.
func DaterangeAsString(s DateSpan, format string) string {
	return FormatDate(s.Start, format) + " to " + FormatDate(s.End, format)
}

// TextAll returns formatted ranges.
func (d *DaterangeDatum) TextAll() []string {
	if !d.textBuilt {
		d.textAll = nil
		for _, v := range d.Values() {
			d.textAll = append(d.textAll, DaterangeAsString(v, d.column.DateFormat()))
		}
		d.textBuilt = true
	}
	return d.textAll
}

// SetValue implements Datum::Daterange set_value.
func (d *DaterangeDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	all := valueList(value)
	clone := d.Clone(nil, false)
	if len(all)%2 == 1 && !truthy(all[0]) {
		all = all[1:]
	}
	var values []DateSpan
	for len(all) > 0 {
		first := all[0]
		all = all[1:]
		var start, end any
		if arr, ok := first.([]any); ok {
			if len(arr) > 0 {
				start = arr[0]
			}
			if len(arr) > 1 {
				end = arr[1]
			}
		} else if m, ok := first.(map[string]any); ok {
			start, end = m["from"], m["to"]
		} else {
			start = first
			if len(all) > 0 {
				end = all[0]
				all = all[1:]
			}
		}
		values = append(values, d.parseUser(start, end, opts)...)
	}
	var text []string
	for _, v := range values {
		text = append(text, DaterangeAsString(v, d.column.DateFormat()))
	}
	text = sortedStrings(text)
	if joinSpace(text) != joinSpace(d.TextAll()) {
		d.changed = true
		d.values = values
		d.valuesBuilt = true
		d.textAll = text
		d.textBuilt = true
	}
	d.oldvalue = clone
}

func (d *DaterangeDatum) parseUser(start, end any, opts SetValueOptions) []DateSpan {
	subtract := func(spans []DateSpan) []DateSpan {
		for i := range spans {
			spans[i].End = spans[i].End.AddDate(0, 0, -opts.SubtractDaysEnd)
		}
		return spans
	}
	if st, ok := start.(time.Time); ok {
		if et, ok := end.(time.Time); ok {
			return subtract([]DateSpan{{st, et}})
		}
	}
	from, to := anyToString(start), anyToString(end)
	if !truthyStr(from) && !truthyStr(to) {
		return nil
	}
	orig := map[string]any{"from": from, "to": to}
	if d.column.Validate(orig, !opts.Bulk) {
		f, _ := d.column.ParseDate(from)
		t, _ := d.column.ParseDate(to)
		return subtract([]DateSpan{{f, t}})
	} else if opts.Bulk {
		fd, okf := parseDuration(from)
		td, okt := parseDuration(to)
		if okf || okt {
			var out []DateSpan
			for _, v := range d.Values() {
				f, t := v.Start, v.End
				if okf {
					f = fd.addTo(f)
				}
				if okt {
					t = td.addTo(t)
				}
				out = append(out, DateSpan{f, t})
			}
			return out
		}
		d.column.Validate(orig, true)
	}
	return nil
}

// HTMLForm returns start/end pairs formatted in UTC.
func (d *DaterangeDatum) HTMLForm() []any {
	var out []any
	f := d.column.DateFormat()
	for _, v := range d.Values() {
		out = append(out, formatUTC(v.Start, f), formatUTC(v.End, f))
	}
	return out
}

// SetValuesForChild returns the form values.
func (d *DaterangeDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns the text values.
func (d *DaterangeDatum) SearchValuesUnique() []string { return d.TextAll() }

// Blank reports no complete range.
func (d *DaterangeDatum) Blank() bool { return len(d.Values()) == 0 }

// AsString joins the ranges.
func (d *DaterangeDatum) AsString() string { return strings.Join(d.TextAll(), ", ") }

// HTML escapes.
func (d *DaterangeDatum) HTML() string { return htmlEscape(d.AsString()) }

// HasValue reports a value.
func (d *DaterangeDatum) HasValue() bool { return len(d.Values()) > 0 }

// RawValues returns spans.
func (d *DaterangeDatum) RawValues() []any {
	var out []any
	for _, v := range d.Values() {
		out = append(out, v)
	}
	return out
}

// ForCode returns the Lua value.
func (d *DaterangeDatum) ForCode(ctx ForCodeContext) any {
	if d.forCodeBuilt {
		return d.forCodeCache
	}
	var ret any
	if !d.column.MultivalueFn() && d.Blank() {
		ret = nil
	} else {
		var vals []any
		for _, v := range d.Values() {
			s, e := v.Start, v.End
			vals = append(vals, map[string]any{
				"from":  dateForCode(&s),
				"to":    dateForCode(&e),
				"value": DaterangeAsString(v, d.column.DateFormat()),
			})
		}
		if d.column.MultivalueFn() || len(vals) > 1 {
			if vals == nil {
				vals = []any{}
			}
			ret = vals
		} else if len(vals) > 0 {
			ret = vals[0]
		}
	}
	d.forCodeCache, d.forCodeBuilt = ret, true
	return ret
}

// ValueRegexTest returns text values.
func (d *DaterangeDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the first text value.
func (d *DaterangeDatum) FilterValue() string {
	t := d.TextAll()
	if len(t) == 0 {
		return ""
	}
	return t[0]
}

// Clone copies the datum.
func (d *DaterangeDatum) Clone(record *Record, fresh bool) Datum {
	n := &DaterangeDatum{datumBase: d.cloneBase(record)}
	n.values = append([]DateSpan(nil), d.Values()...)
	n.valuesBuilt = true
	return n
}

// =====================================================================
// Enum
// =====================================================================

type enumValueHash struct {
	ids     []*int64
	text    []string
	deleted []bool
}

// EnumDatum is GADS::Datum::Enum.
type EnumDatum struct {
	datumBase
	vh       *enumValueHash
	id       []int64 // effective ids (single: 0 or 1 entries)
	idSet    bool
	hasID    bool
	text     *string
	textAllV []string
	textSet  bool
}

func (d *EnumDatum) valueHash() *enumValueHash {
	if d.vh != nil {
		return d.vh
	}
	vh := &enumValueHash{}
	for _, r := range d.initValue {
		if r.NullJoin {
			// A hash of undefined values: no ID and an empty text
			vh.ids = append(vh.ids, nil)
			vh.text = append(vh.text, "")
			vh.deleted = append(vh.deleted, false)
			continue
		}
		if r.Value == nil && !r.HasText {
			// NULL value row
			continue
		}
		if r.HasText {
			id := r.ID
			vh.ids = append(vh.ids, &id)
			vh.text = append(vh.text, r.Text)
			vh.deleted = append(vh.deleted, r.Deleted)
			continue
		}
		id, _ := anyToInt64(r.Value)
		e := d.column.Enumval(id)
		if e == nil {
			continue
		}
		eid, _ := anyToInt64(e["id"])
		vh.ids = append(vh.ids, &eid)
		vh.text = append(vh.text, anyToString(e["value"]))
		vh.deleted = append(vh.deleted, truthy(e["deleted"]))
	}
	for _, id := range vh.ids {
		if id != nil {
			d.hasID = true
		}
	}
	if d.initNoValue {
		d.hasID = true
	}
	d.vh = vh
	return vh
}

// IDs returns the selected enumval IDs.
func (d *EnumDatum) IDs() []int64 {
	if !d.idSet {
		vh := d.valueHash()
		var ids []int64
		for _, id := range vh.ids {
			if id != nil {
				ids = append(ids, *id)
			}
		}
		if !d.column.MultivalueFn() && len(vh.ids) > 0 {
			if vh.ids[0] != nil {
				ids = []int64{*vh.ids[0]}
			} else {
				ids = nil
			}
		}
		d.id = ids
		d.idSet = true
	}
	return d.id
}

// SetValue implements Datum::Enum set_value.
func (d *EnumDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	clone := d.Clone(nil, false)
	var vals []string
	for _, v := range valueList(value) {
		if truthy(v) {
			vals = append(vals, anyToString(v))
		}
	}
	vals = sortedStrings(vals)
	var old []string
	for _, id := range d.IDs() {
		old = append(old, itoa(id))
	}
	old = sortedStrings(old)
	changed := joinSpace(vals) != joinSpace(old)
	var ids []int64
	for _, v := range vals {
		id, _ := anyToInt64(v)
		ids = append(ids, id)
	}
	if changed {
		vh := &enumValueHash{}
		for _, v := range vals {
			d.column.Validate(v, true)
			id, _ := anyToInt64(v)
			e := d.column.Enumval(id)
			idc := id
			vh.ids = append(vh.ids, &idc)
			vh.text = append(vh.text, anyToString(e["value"]))
			vh.deleted = append(vh.deleted, truthy(e["deleted"]))
		}
		d.text = nil
		d.textSet = false
		d.vh = vh
	}
	d.changed = changed
	d.oldvalue = clone
	if d.column.MultivalueFn() {
		d.id = ids
	} else if len(ids) > 0 {
		d.id = ids[:1]
	} else {
		d.id = nil
	}
	d.idSet = true
}

// TextAll returns the text values.
func (d *EnumDatum) TextAll() []string {
	if !d.textSet {
		d.textAllV = append([]string(nil), d.valueHash().text...)
		d.textSet = true
	}
	return d.textAllV
}

// AsString joins the text values.
func (d *EnumDatum) AsString() string {
	if d.text != nil {
		return *d.text
	}
	s := strings.Join(d.TextAll(), ", ")
	d.text = &s
	return s
}

// HTML escapes.
func (d *EnumDatum) HTML() string { return htmlEscape(d.AsString()) }

// Blank reports no selection.
func (d *EnumDatum) Blank() bool { return len(d.IDs()) == 0 }

// HasValue reports a value.
func (d *EnumDatum) HasValue() bool { d.valueHash(); return d.hasID }

// HTMLForm returns the ids.
func (d *EnumDatum) HTMLForm() []any {
	ids := d.IDs()
	if !d.column.MultivalueFn() && len(ids) == 0 {
		return []any{""}
	}
	var out []any
	for _, id := range ids {
		if id == 0 {
			out = append(out, "")
		} else {
			out = append(out, id)
		}
	}
	return out
}

// SetValuesForChild returns ids.
func (d *EnumDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns ids.
func (d *EnumDatum) SearchValuesUnique() []string {
	var out []string
	for _, v := range d.HTMLForm() {
		out = append(out, anyToString(v))
	}
	return out
}

// RawValues returns the ids.
func (d *EnumDatum) RawValues() []any {
	var out []any
	for _, id := range d.IDs() {
		out = append(out, id)
	}
	return out
}

// ForCode returns the Lua value.
func (d *EnumDatum) ForCode(ctx ForCodeContext) any {
	if d.forCodeBuilt {
		return d.forCodeCache
	}
	vh := d.valueHash()
	texts := append([]string(nil), vh.text...)
	var values []any
	for _, id := range vh.ids {
		var t any
		if len(texts) > 0 {
			t = texts[len(texts)-1]
			texts = texts[:len(texts)-1]
		}
		var idv any
		if id != nil {
			idv = lua.Int(*id)
		}
		values = append(values, map[string]any{"id": idv, "value": t})
	}
	var ret any
	if !d.column.MultivalueFn() && len(values) <= 1 {
		if d.Blank() {
			ret = nil
		} else {
			ret = d.AsString()
		}
	} else {
		if values == nil {
			values = []any{}
		}
		ret = map[string]any{"text": d.AsString(), "values": values}
	}
	d.forCodeCache, d.forCodeBuilt = ret, true
	return ret
}

// ValueRegexTest returns text values.
func (d *EnumDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the string.
func (d *EnumDatum) FilterValue() string { return d.AsString() }

// DeletedValues returns values that have since been deleted.
func (d *EnumDatum) DeletedValues() []map[string]any {
	vh := d.valueHash()
	var out []map[string]any
	for i, id := range vh.ids {
		if !vh.deleted[i] || id == nil {
			continue
		}
		out = append(out, map[string]any{"id": *id, "value": vh.text[i]})
	}
	return out
}

// Clone copies the datum.
func (d *EnumDatum) Clone(record *Record, fresh bool) Datum {
	n := &EnumDatum{datumBase: d.cloneBase(record)}
	vh := d.valueHash()
	n.vh = &enumValueHash{
		ids:     append([]*int64(nil), vh.ids...),
		text:    append([]string(nil), vh.text...),
		deleted: append([]bool(nil), vh.deleted...),
	}
	n.id = append([]int64(nil), d.IDs()...)
	n.idSet = true
	n.hasID = d.hasID
	s := d.AsString()
	n.text = &s
	return n
}

// =====================================================================
// Tree
// =====================================================================

// TreeDatum is GADS::Datum::Tree.
type TreeDatum struct {
	datumBase
	ids      []int64
	idsSet   bool
	texts    []string
	textsSet bool
	vhBuilt  bool
	vhIDs    []int64
	vhText   []string
}

func (d *TreeDatum) valueHash() ([]int64, []string) {
	if !d.vhBuilt {
		d.vhBuilt = true
		d.vhIDs, d.vhText = nil, nil
		for _, r := range d.initValue {
			if r.HasText {
				if r.ID == 0 {
					continue
				}
				d.vhIDs = append(d.vhIDs, r.ID)
				d.vhText = append(d.vhText, r.Text)
				continue
			}
			if r.Value == nil {
				continue
			}
			id, _ := anyToInt64(r.Value)
			n := d.column.Node(id)
			if n == nil {
				continue
			}
			d.vhIDs = append(d.vhIDs, n.id)
			d.vhText = append(d.vhText, n.value)
		}
	}
	return d.vhIDs, d.vhText
}

// IDs returns the selected node ids.
func (d *TreeDatum) IDs() []int64 {
	if !d.idsSet {
		ids, _ := d.valueHash()
		d.ids = append([]int64(nil), ids...)
		d.idsSet = true
	}
	return d.ids
}

// TextAll returns node texts.
func (d *TreeDatum) TextAll() []string {
	if !d.textsSet {
		_, t := d.valueHash()
		d.texts = append([]string(nil), t...)
		d.textsSet = true
	}
	return d.texts
}

// SetValue implements Datum::Tree set_value.
func (d *TreeDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	clone := d.Clone(nil, false)
	var vals []string
	for _, v := range valueList(value) {
		if truthy(v) {
			vals = append(vals, anyToString(v))
		}
	}
	vals = sortedStrings(vals)
	var old []string
	for _, id := range d.IDs() {
		old = append(old, itoa(id))
	}
	old = sortedStrings(old)
	changed := joinSpace(vals) != joinSpace(old)
	var ids []int64
	for _, v := range vals {
		id, _ := anyToInt64(v)
		ids = append(ids, id)
	}
	if changed {
		var text []string
		for _, v := range vals {
			d.column.Validate(v, true)
			id, _ := anyToInt64(v)
			text = append(text, d.column.Node(id).value)
		}
		d.texts = text
		d.textsSet = true
	}
	d.changed = changed
	d.oldvalue = clone
	d.ids = ids
	d.idsSet = true
}

// Blank reports no selection.
func (d *TreeDatum) Blank() bool {
	for _, id := range d.IDs() {
		if id != 0 {
			return false
		}
	}
	return true
}

// HasValue reports a value.
func (d *TreeDatum) HasValue() bool { return !d.Blank() || d.initNoValue }

// AsString joins the texts.
func (d *TreeDatum) AsString() string { return strings.Join(d.TextAll(), ", ") }

// HTML escapes.
func (d *TreeDatum) HTML() string { return htmlEscape(d.AsString()) }

// HTMLForm returns the ids.
func (d *TreeDatum) HTMLForm() []any {
	var out []any
	for _, id := range d.IDs() {
		out = append(out, id)
	}
	return out
}

// SetValuesForChild returns ids.
func (d *TreeDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns ids.
func (d *TreeDatum) SearchValuesUnique() []string {
	var out []string
	for _, v := range d.HTMLForm() {
		out = append(out, anyToString(v))
	}
	return out
}

// RawValues returns ids.
func (d *TreeDatum) RawValues() []any { return d.HTMLForm() }

// Ancestors returns the ancestors of each selected node.
func (d *TreeDatum) ancestors(id int64) []*treeNode {
	n := d.column.Node(id)
	if n == nil {
		return nil
	}
	var out []*treeNode
	for p := n.parent; p != nil && p.parent != nil; p = p.parent {
		out = append(out, p)
	}
	// Ancestors are returned nearest first; keep root-most last
	return out
}

// FullPath returns the full path of each value ("a#b#c").
func (d *TreeDatum) FullPath() []string {
	var out []string
	texts := d.TextAll()
	for i, id := range d.IDs() {
		var path []string
		anc := d.ancestors(id)
		for j := len(anc) - 1; j >= 0; j-- {
			path = append(path, anc[j].value)
		}
		text := ""
		if i < len(texts) {
			text = texts[i]
		}
		p := strings.Join(path, "#")
		if p != "" {
			p = p + "#" + text
		} else {
			p = text
		}
		out = append(out, p)
	}
	return out
}

// ValueRegexTest returns full paths.
func (d *TreeDatum) ValueRegexTest(string) []string { return d.FullPath() }

// FilterValue returns the string.
func (d *TreeDatum) FilterValue() string { return d.AsString() }

// ForCode returns the Lua value.
func (d *TreeDatum) ForCode(ctx ForCodeContext) any {
	if d.forCodeBuilt {
		return d.forCodeCache
	}
	var values []any
	for _, id := range d.IDs() {
		parents := map[string]any{}
		node := d.column.Node(id)
		var anc []*treeNode
		if node != nil {
			// DAG ancestors: mother first ... root last; root removed
			for p := node.parent; p != nil; p = p.parent {
				anc = append(anc, p)
			}
			if len(anc) > 0 {
				anc = anc[:len(anc)-1]
			}
		}
		count := 0
		for i := len(anc) - 1; i >= 0; i-- {
			count++
			parents["parent"+itoa(int64(count))] = d.column.Node(anc[i].id).value
		}
		var v any
		if !d.Blank() && node != nil {
			v = node.value
		}
		values = append(values, map[string]any{"value": v, "parents": parents})
	}
	var ret any
	if d.column.MultivalueFn() || len(values) > 1 {
		if values == nil {
			values = []any{}
		}
		ret = values
	} else if len(values) > 0 {
		ret = values[0]
	} else {
		ret = map[string]any{"value": nil, "parents": map[string]any{}}
	}
	d.forCodeCache, d.forCodeBuilt = ret, true
	return ret
}

// Clone copies the datum.
func (d *TreeDatum) Clone(record *Record, fresh bool) Datum {
	n := &TreeDatum{datumBase: d.cloneBase(record)}
	ids, text := d.valueHash()
	n.vhIDs = append([]int64(nil), ids...)
	n.vhText = append([]string(nil), text...)
	n.vhBuilt = true
	n.ids = append([]int64(nil), d.IDs()...)
	n.idsSet = true
	n.texts = append([]string(nil), d.TextAll()...)
	n.textsSet = true
	return n
}

// =====================================================================
// Person
// =====================================================================

// PersonDatum is GADS::Datum::Person.
type PersonDatum struct {
	datumBase
	valueHashV []map[string]any
	vhBuilt    bool
	ids        []int64
	idsSet     bool
	textAll    []string
	textBuilt  bool
}

func (d *PersonDatum) orgHash(kind string, v map[string]any) map[string]any {
	key := kind
	if kind != "organisation" {
		key = kind + "_id"
	}
	if m, ok := v[kind].(map[string]any); ok {
		return m
	}
	idv := v[key]
	if !truthy(idv) {
		return map[string]any{}
	}
	id, _ := anyToInt64(idv)
	row := d.column.e.DB.QueryRow(`SELECT "id", "name" FROM `+q(kind)+` WHERE "id" = ?`, id)
	if row == nil {
		return map[string]any{}
	}
	rid, _ := db.AsInt64(row["id"])
	name, _ := db.AsString(row["name"])
	return map[string]any{"id": rid, "name": name}
}

func (d *PersonDatum) idToHash(id int64) map[string]any {
	if id == 0 {
		return nil
	}
	u := d.column.e.FindUser(id)
	if u == nil {
		return nil
	}
	return u.Hash()
}

// DeletedValues returns the values of users that have since been deleted
// (deleted_values).
func (d *PersonDatum) DeletedValues() []map[string]any {
	var out []map[string]any
	for _, v := range d.ValueHash() {
		if truthy(v["deleted"]) {
			out = append(out, v)
		}
	}
	return out
}

// ValueHash returns the person values.
func (d *PersonDatum) ValueHash() []map[string]any {
	if d.vhBuilt {
		return d.valueHashV
	}
	d.vhBuilt = true
	var raw []any
	if d.hasInitValue {
		for _, r := range d.initValue {
			if r.User != nil {
				raw = append(raw, r.User)
			} else if r.Value != nil {
				raw = append(raw, r.Value)
			}
		}
	} else if d.idsSet {
		for _, id := range d.ids {
			raw = append(raw, id)
		}
	}
	var out []map[string]any
	for _, v := range raw {
		var h map[string]any
		switch x := v.(type) {
		case map[string]any:
			h = x
		default:
			id, _ := anyToInt64(x)
			h = d.idToHash(id)
		}
		if h == nil || !truthy(h["id"]) {
			continue
		}
		nh := map[string]any{}
		for _, k := range []string{"id", "email", "username", "firstname", "surname", "freetext1", "freetext2", "department_id", "team_id", "title", "value", "deleted"} {
			nh[k] = h[k]
		}
		nh["organisation"] = d.orgHash("organisation", h)
		nh["department"] = d.orgHash("department", h)
		nh["team"] = d.orgHash("team", h)
		out = append(out, nh)
	}
	d.valueHashV = out
	return out
}

// IDs returns the user ids.
func (d *PersonDatum) IDs() []int64 {
	if !d.idsSet {
		var ids []int64
		for _, v := range d.ValueHash() {
			id, _ := anyToInt64(v["id"])
			ids = append(ids, id)
		}
		d.ids = ids
		d.idsSet = true
	}
	return d.ids
}

// ID returns the single id.
func (d *PersonDatum) ID() int64 {
	ids := d.IDs()
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}

func (d *PersonDatum) hasIDValue(id int64) bool {
	for _, i := range d.IDs() {
		if i == id {
			return true
		}
	}
	return false
}

// SetValue implements Datum::Person set_value.
func (d *PersonDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	var vals []any
	for _, v := range valueList(value) {
		if truthy(v) {
			vals = append(vals, v)
		}
	}
	// Perl sort of mixed values: sort by string value
	sort.SliceStable(vals, func(i, j int) bool { return anyToString(vals[i]) < anyToString(vals[j]) })
	clone := d.Clone(nil, false)
	var old []string
	for _, id := range d.IDs() {
		old = append(old, itoa(id))
	}
	old = sortedStrings(old)
	var values2 []int64
	for _, v := range vals {
		var id int64
		if m, ok := v.(map[string]any); ok {
			if truthy(m["email"]) {
				id = d.column.e.findOrCreateUser(m)
				d.column.ClearPeople()
			}
		} else {
			s := anyToString(v)
			if s != "" && !reDigits.MatchString(s) {
				orig := s
				if !strings.Contains(s, ",") {
					s = rePersonSwap.ReplaceAllString(s, "$2, $1")
				}
				var found *User
				for _, p := range d.column.People() {
					if p.Value == s {
						found = p
						break
					}
				}
				if found == nil {
					Fail("Invalid name '%s'", orig)
				}
				id = found.ID
			} else {
				id, _ = anyToInt64(s)
			}
			if id != 0 && !opts.NoValidation && !d.column.validPerson(id) && !d.hasIDValue(id) {
				Fail("'%s' is not a valid person ID", itoa(id))
			}
		}
		values2 = append(values2, id)
	}
	var v2s []string
	for _, v := range values2 {
		v2s = append(v2s, itoa(v))
	}
	changed := (len(values2) > 0 || len(old) > 0) && (len(values2) != len(old) || joinSpace(v2s) != joinSpace(old))
	if changed {
		d.changed = true
		d.ids = values2
		d.idsSet = true
		d.vhBuilt = false
		d.valueHashV = nil
		d.hasInitValue = false
		d.initValue = nil
		d.textBuilt = false
	}
	d.oldvalue = clone
}

var rePersonSwap = regexp.MustCompile(`^(.*)[\t ]+(.*)$`)

// TextAll returns sorted names.
func (d *PersonDatum) TextAll() []string {
	if !d.textBuilt {
		var t []string
		for _, v := range d.ValueHash() {
			t = append(t, anyToString(v["value"]))
		}
		d.textAll = sortedStrings(t)
		d.textBuilt = true
	}
	return d.textAll
}

// AsString joins names.
func (d *PersonDatum) AsString() string { return strings.Join(d.TextAll(), ", ") }

// HTML escapes.
func (d *PersonDatum) HTML() string { return htmlEscape(d.AsString()) }

// Blank reports no person.
func (d *PersonDatum) Blank() bool { return len(d.IDs()) == 0 }

// HasValue reports a value.
func (d *PersonDatum) HasValue() bool { return !d.Blank() }

// HTMLForm returns ids.
func (d *PersonDatum) HTMLForm() []any {
	ids := d.IDs()
	if len(ids) == 0 {
		return []any{""}
	}
	var out []any
	for _, id := range ids {
		out = append(out, id)
	}
	return out
}

// SetValuesForChild returns ids.
func (d *PersonDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns names.
func (d *PersonDatum) SearchValuesUnique() []string { return d.TextAll() }

// RawValues returns ids.
func (d *PersonDatum) RawValues() []any {
	var out []any
	for _, id := range d.IDs() {
		out = append(out, id)
	}
	return out
}

// ForCode returns the Lua value.
func (d *PersonDatum) ForCode(ctx ForCodeContext) any {
	if d.forCodeBuilt {
		return d.forCodeCache
	}
	var values []any
	for _, v := range d.ValueHash() {
		values = append(values, map[string]any{
			"id":           luaID(v["id"]),
			"surname":      luaStr(v["surname"]),
			"firstname":    luaStr(v["firstname"]),
			"email":        luaStr(v["email"]),
			"freetext1":    luaStr(v["freetext1"]),
			"freetext2":    luaStr(v["freetext2"]),
			"organisation": luaOrg(v["organisation"]),
			"department":   luaOrg(v["department"]),
			"team":         luaOrg(v["team"]),
			"title":        luaID(v["title"]),
			"text":         luaStr(v["value"]),
			"deleted":      luaDeleted(v["deleted"]),
		})
	}
	var ret any
	if d.column.MultivalueFn() || len(values) > 1 {
		if values == nil {
			values = []any{}
		}
		ret = values
	} else if len(values) > 0 {
		ret = values[0]
	}
	d.forCodeCache, d.forCodeBuilt = ret, true
	return ret
}

func luaID(v any) any {
	if v == nil {
		return nil
	}
	if i, ok := anyToInt64(v); ok {
		return lua.Int(i)
	}
	return anyToString(v)
}

// luaDeleted is the epoch of the deletion of a person (or the false value
// of a person not deleted)
func luaDeleted(v any) any {
	if !truthy(v) {
		if v == nil {
			return nil
		}
		return anyToString(v)
	}
	t, ok := dtFromDB(v)
	if !ok {
		Panic(`Can't call method "epoch" on an undefined value`)
	}
	return lua.Int(t.Unix())
}

func luaStr(v any) any {
	if v == nil {
		return nil
	}
	return anyToString(v)
}

func luaOrg(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{}
	if id, ok := m["id"]; ok {
		out["id"] = luaID(id)
	}
	if n, ok := m["name"]; ok {
		out["name"] = luaStr(n)
	}
	return out
}

// ValueRegexTest returns the string.
func (d *PersonDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the string.
func (d *PersonDatum) FilterValue() string { return d.AsString() }

// Clone copies the datum.
func (d *PersonDatum) Clone(record *Record, fresh bool) Datum {
	n := &PersonDatum{datumBase: d.cloneBase(record)}
	for _, v := range d.ValueHash() {
		c := map[string]any{}
		for k, vv := range v {
			c[k] = vv
		}
		n.valueHashV = append(n.valueHashV, c)
	}
	n.vhBuilt = true
	n.hasInitValue = true
	for _, v := range n.valueHashV {
		n.initValue = append(n.initValue, rawValue{User: v})
	}
	return n
}

func (e *Engine) findOrCreateUser(m map[string]any) int64 {
	if id, ok := anyToInt64(m["id"]); ok && id != 0 {
		if u := e.FindUser(id); u != nil {
			return id
		}
		e.InsertUser(m)
		return id
	}
	return e.InsertUser(m)
}

// =====================================================================
// File
// =====================================================================

// FileDatum is GADS::Datum::File.
type FileDatum struct {
	datumBase
	ids      []int64
	idsSet   bool
	hasIDs   bool
	files    []map[string]any
	filesSet bool
}

// IDs returns file ids.
func (d *FileDatum) IDs() []int64 {
	if !d.idsSet {
		var ids []int64
		for _, f := range d.Files() {
			id, _ := anyToInt64(f["id"])
			ids = append(ids, id)
		}
		d.ids = ids
		d.idsSet = true
	}
	return d.ids
}

// Files returns the file details.
func (d *FileDatum) Files() []map[string]any {
	if d.filesSet {
		return d.files
	}
	d.filesSet = true
	if d.isPurged() {
		d.files = []map[string]any{{"id": int64(-1), "name": "Purged", "mimetype": "text/plain"}}
		return d.files
	}
	var out []map[string]any
	if d.hasInitValue {
		for _, r := range d.initValue {
			if r.HasText {
				if r.ID == 0 {
					continue
				}
				out = append(out, map[string]any{"id": r.ID, "name": r.Text, "mimetype": r.Mimetype})
				continue
			}
			if r.Value == nil {
				continue
			}
			id, _ := anyToInt64(r.Value)
			out = append(out, d.idsToFiles([]int64{id})...)
		}
		if len(d.initValue) > 0 || d.initNoValue {
			d.hasIDs = true
		}
	} else if d.idsSet {
		out = d.idsToFiles(d.ids)
	}
	d.files = out
	return out
}

func (d *FileDatum) isPurged() bool {
	if d.recordID == 0 {
		return false
	}
	n, _ := d.column.e.DB.QueryInt64(`SELECT COUNT(*) FROM "file" WHERE "record_id" = ? AND "layout_id" = ? AND "purged_on" IS NOT NULL`, d.recordID, d.column.ID)
	return n > 0
}

func (d *FileDatum) idsToFiles(ids []int64) []map[string]any {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var out []map[string]any
	for _, r := range d.column.e.DB.Query(`SELECT "id", "name", "mimetype" FROM "fileval" WHERE "id" IN (`+db.Placeholders(len(ids))+`)`, args...) {
		id, _ := db.AsInt64(r["id"])
		n, _ := db.AsString(r["name"])
		m, _ := db.AsString(r["mimetype"])
		out = append(out, map[string]any{"id": id, "name": n, "mimetype": m})
	}
	return out
}

// SetValue implements Datum::File set_value.
func (d *FileDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	clone := d.Clone(nil, false)
	var in []any
	for _, v := range valueList(value) {
		if truthy(v) {
			in = append(in, v)
		}
	}
	sort.SliceStable(in, func(i, j int) bool { return anyToString(in[i]) < anyToString(in[j]) })
	var values []int64
	for _, v := range in {
		if m, ok := v.(map[string]any); ok {
			// The content is written to the uploads directory
			id := d.column.e.FilevalCreate(map[string]any{
				"name":     m["name"],
				"mimetype": m["mimetype"],
			}, contentBytes(m["content"]))
			values = append(values, id)
		} else {
			id, _ := anyToInt64(v)
			values = append(values, id)
		}
	}
	var vs, old []string
	for _, v := range values {
		vs = append(vs, itoa(v))
	}
	for _, v := range d.IDs() {
		old = append(old, itoa(v))
	}
	old = sortedStrings(old)
	changed := joinSpace(vs) != joinSpace(old)
	if changed {
		for _, v := range values {
			d.column.Validate(itoa(v), true)
		}
		if len(values) == 1 && len(old) == 1 {
			e := d.column.e
			oldRow := e.DB.QueryRow(`SELECT "id", "name" FROM "fileval" WHERE "id" = ?`, old[0])
			if oldRow == nil {
				Fail(`Can't call method "content" on an undefined value`)
			}
			oldID, _ := db.AsInt64(oldRow["id"])
			oldContent := e.FilevalContent(oldID)
			if fl := e.DB.QueryRow(`SELECT "id" FROM "fileval" WHERE "id" = ? AND "name" = ?`, values[0], oldRow["name"]); fl != nil {
				if string(e.FilevalContent(values[0])) == string(oldContent) {
					changed = false
				}
			}
		}
	}
	if changed {
		d.filesSet = false
		d.files = nil
		d.hasInitValue = false
		d.initValue = nil
	}
	d.changed = changed
	d.oldvalue = clone
	d.hasIDs = true
	d.ids = values
	d.idsSet = true
}

// Blank reports no files.
func (d *FileDatum) Blank() bool { return len(d.IDs()) == 0 }

// HasValue reports set ids.
func (d *FileDatum) HasValue() bool { d.Files(); return d.hasIDs }

// AsString joins file names.
func (d *FileDatum) AsString() string {
	var names []string
	for _, f := range d.Files() {
		names = append(names, anyToString(f["name"]))
	}
	return strings.Join(names, ", ")
}

// TextAll returns names.
func (d *FileDatum) TextAll() []string { return []string{d.AsString()} }

// HTML escapes.
func (d *FileDatum) HTML() string { return htmlEscape(d.AsString()) }

// HTMLForm returns ids.
func (d *FileDatum) HTMLForm() []any {
	var out []any
	for _, id := range d.IDs() {
		out = append(out, id)
	}
	return out
}

// SetValuesForChild returns ids.
func (d *FileDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns names.
func (d *FileDatum) SearchValuesUnique() []string {
	var out []string
	for _, f := range d.Files() {
		out = append(out, anyToString(f["name"]))
	}
	return out
}

// RawValues returns ids.
func (d *FileDatum) RawValues() []any { return d.HTMLForm() }

// ForCode returns the Lua value.
func (d *FileDatum) ForCode(ctx ForCodeContext) any {
	var names []any
	for _, f := range d.Files() {
		names = append(names, anyToString(f["name"]))
	}
	if d.column.MultivalueFn() {
		if names == nil {
			names = []any{}
		}
		return names
	}
	if len(names) > 0 {
		return names[0]
	}
	return nil
}

// ValueRegexTest returns the string.
func (d *FileDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the string.
func (d *FileDatum) FilterValue() string { return d.AsString() }

// Clone copies the datum.
func (d *FileDatum) Clone(record *Record, fresh bool) Datum {
	n := &FileDatum{datumBase: d.cloneBase(record)}
	n.files = d.Files()
	n.filesSet = true
	n.ids = append([]int64(nil), d.IDs()...)
	n.idsSet = true
	n.hasIDs = d.hasIDs
	return n
}

// =====================================================================
// ID, Serial, Count
// =====================================================================

// IDDatum is GADS::Datum::ID.
type IDDatum struct{ datumBase }

// SetValue is not applicable.
func (d *IDDatum) SetValue(value any, opts SetValueOptions) {}

// AsString returns the current ID.
func (d *IDDatum) AsString() string {
	if d.currentID == 0 {
		return ""
	}
	return itoa(d.currentID)
}

// TextAll returns the ID.
func (d *IDDatum) TextAll() []string { return []string{d.AsString()} }

// Blank reports no ID.
func (d *IDDatum) Blank() bool { return d.currentID == 0 }

// HTMLForm returns the ID.
func (d *IDDatum) HTMLForm() []any { return []any{d.AsString()} }

// SetValuesForChild returns the ID.
func (d *IDDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns the ID.
func (d *IDDatum) SearchValuesUnique() []string { return []string{d.AsString()} }

// ForCode returns the ID as an integer.
func (d *IDDatum) ForCode(ctx ForCodeContext) any {
	if d.currentID == 0 {
		return nil
	}
	return lua.Int(d.currentID)
}

// ValueRegexTest returns the ID.
func (d *IDDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the ID.
func (d *IDDatum) FilterValue() string { return d.AsString() }

// Clone copies.
func (d *IDDatum) Clone(record *Record, fresh bool) Datum {
	return &IDDatum{datumBase: d.cloneBase(record)}
}

// HasValue reports an ID.
func (d *IDDatum) HasValue() bool { return d.currentID != 0 }

// RawValues returns the ID.
func (d *IDDatum) RawValues() []any { return []any{d.currentID} }

// HTML returns the ID.
func (d *IDDatum) HTML() string { return d.AsString() }

// SetCurrentID sets the ID.
func (d *IDDatum) SetCurrentID(id int64) { d.currentID = id }

// SerialDatum is GADS::Datum::Serial.
type SerialDatum struct {
	datumBase
	serial    *int64
	serialSet bool
}

func (d *SerialDatum) value() *int64 {
	if !d.serialSet {
		d.serialSet = true
		if d.record != nil {
			d.serial = d.record.Serial()
		}
	}
	return d.serial
}

// SetValue is not applicable.
func (d *SerialDatum) SetValue(value any, opts SetValueOptions) {}

// AsString returns the serial.
func (d *SerialDatum) AsString() string {
	if v := d.value(); v != nil {
		return itoa(*v)
	}
	return ""
}

// TextAll returns the serial.
func (d *SerialDatum) TextAll() []string { return []string{d.AsString()} }

// Blank reports no serial.
func (d *SerialDatum) Blank() bool { v := d.value(); return v == nil || *v == 0 }

// HTMLForm returns the serial.
func (d *SerialDatum) HTMLForm() []any { return []any{d.AsString()} }

// SetValuesForChild returns the serial.
func (d *SerialDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns the serial.
func (d *SerialDatum) SearchValuesUnique() []string { return []string{d.AsString()} }

// ForCode returns the serial (as_string in Perl, which is an integer from
// the database).
func (d *SerialDatum) ForCode(ctx ForCodeContext) any {
	if v := d.value(); v != nil {
		return lua.Int(*v)
	}
	return nil
}

// ValueRegexTest returns the serial.
func (d *SerialDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the serial.
func (d *SerialDatum) FilterValue() string { return d.AsString() }

// Clone copies.
func (d *SerialDatum) Clone(record *Record, fresh bool) Datum {
	return &SerialDatum{datumBase: d.cloneBase(record), serial: d.serial, serialSet: d.serialSet}
}

// HasValue reports a serial.
func (d *SerialDatum) HasValue() bool { return !d.Blank() }

// RawValues returns the serial.
func (d *SerialDatum) RawValues() []any {
	if v := d.value(); v != nil {
		return []any{*v}
	}
	return nil
}

// HTML returns the serial.
func (d *SerialDatum) HTML() string { return d.AsString() }

// CountDatum is GADS::Datum::Count (grouped views).
type CountDatum struct {
	datumBase
	val any
}

// SetValue is not applicable.
func (d *CountDatum) SetValue(value any, opts SetValueOptions) {}

// AsString returns "N unique".
func (d *CountDatum) AsString() string {
	if d.val == nil {
		return ""
	}
	i, _ := anyToInt64(d.val)
	return itoa(i) + " unique"
}

// TextAll returns the string.
func (d *CountDatum) TextAll() []string { return []string{d.AsString()} }

// Blank reports a defined value (sic, as in GADS).
func (d *CountDatum) Blank() bool { return d.val != nil }

// HTMLForm returns the string.
func (d *CountDatum) HTMLForm() []any { return []any{d.AsString()} }

// SetValuesForChild returns the string.
func (d *CountDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns the string.
func (d *CountDatum) SearchValuesUnique() []string { return []string{d.AsString()} }

// ForCode returns nil.
func (d *CountDatum) ForCode(ctx ForCodeContext) any { return nil }

// ValueRegexTest returns the string.
func (d *CountDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the string.
func (d *CountDatum) FilterValue() string { return d.AsString() }

// Clone copies.
func (d *CountDatum) Clone(record *Record, fresh bool) Datum {
	return &CountDatum{datumBase: d.cloneBase(record), val: d.val}
}

// HasValue reports a value.
func (d *CountDatum) HasValue() bool { return d.val != nil }

// RawValues returns the count.
func (d *CountDatum) RawValues() []any { return []any{d.val} }

// HTML returns the string.
func (d *CountDatum) HTML() string { return htmlEscape(d.AsString()) }

// formatUTC formats in UTC (DateTime::format_cldr without a time zone
// change, as used by Daterange html_form).
func formatUTC(t time.Time, format string) string {
	return cldr.Format(t.UTC(), format)
}
