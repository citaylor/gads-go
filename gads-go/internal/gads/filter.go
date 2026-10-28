package gads

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Filter is a port of GADS::Filter: a JSON filter as produced by the
// jQuery QueryBuilder, with helpers to traverse its rules.
type Filter struct {
	asHash         map[string]any
	hasValue       bool
	changed        bool
	layout         *Layout
	IsPeopleFilter bool
	filters        []map[string]any
	filtersBuilt   bool
	columnsInSubs  []*Column
	subsBuilt      bool
}

// DecodeJSON decodes JSON keeping integral numbers as int64.
func DecodeJSON(s string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normaliseJSON(v), nil
}

func normaliseJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, vv := range x {
			x[k] = normaliseJSON(vv)
		}
		return x
	case []any:
		for i, vv := range x {
			x[i] = normaliseJSON(vv)
		}
		return x
	}
	return v
}

// EncodeJSON encodes as compact JSON with sorted keys (Perl's key order
// is random, so any order is equivalent).
func EncodeJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}

// NewFilterJSON creates a filter from JSON (invalid JSON becomes {}).
func NewFilterJSON(js string, layout *Layout) *Filter {
	f := &Filter{layout: layout}
	f.setJSON(js, false)
	return f
}

// NewFilterHash creates a filter from a hash.
func NewFilterHash(h map[string]any, layout *Layout) *Filter {
	if h == nil {
		h = map[string]any{}
	}
	return &Filter{asHash: h, layout: layout, hasValue: true}
}

// NewFilter creates an empty filter.
func NewFilter(layout *Layout) *Filter {
	return &Filter{asHash: map[string]any{}, layout: layout}
}

func (f *Filter) setJSON(js string, trigger bool) {
	if js == "" {
		js = "{}"
	}
	v, err := DecodeJSON(js)
	h, ok := v.(map[string]any)
	if err != nil || !ok {
		h = map[string]any{}
	}
	if trigger && f.hasValue && !compareIgnoringColumnID(f.asHash, h) {
		f.changed = true
	}
	f.asHash = h
	f.clearLazy()
	f.hasValue = true
}

// SetJSON sets the filter from JSON (tracking changes).
func (f *Filter) SetJSON(js string) { f.setJSON(js, true) }

// SetHash sets the filter from a hash (tracking changes).
func (f *Filter) SetHash(h map[string]any) {
	if h == nil {
		h = map[string]any{}
	}
	if f.hasValue && !compareIgnoringColumnID(f.AsHash(), h) {
		f.changed = true
	}
	f.asHash = h
	f.clearLazy()
	f.hasValue = true
}

func (f *Filter) clearLazy() {
	f.filters = nil
	f.filtersBuilt = false
}

// Changed reports whether the filter has changed since creation.
func (f *Filter) Changed() bool { return f.changed }

// Layout returns the filter layout.
func (f *Filter) Layout() *Layout { return f.layout }

// SetLayout sets the layout (used to validate columns).
func (f *Filter) SetLayout(l *Layout) {
	f.layout = l
	f.clearLazy()
}

// AsHash returns the filter as a hash.
func (f *Filter) AsHash() map[string]any {
	if f.asHash == nil {
		f.asHash = map[string]any{}
	}
	f.hasValue = true
	return f.asHash
}

// AsJSON returns the filter as JSON.
func (f *Filter) AsJSON() string {
	f.hasValue = true
	return EncodeJSON(f.AsHash())
}

// Filters returns all the rules in a flat list. As in GADS, each rule is
// annotated with column_id, and rules referencing columns that do not exist
// in the layout are emptied.
func (f *Filter) Filters() []map[string]any {
	if f.filtersBuilt {
		return f.filters
	}
	var out []map[string]any
	f.filterTables(f.AsHash(), &out)
	f.filters = out
	f.filtersBuilt = true
	return out
}

var reParentChild = regexp.MustCompile(`^([0-9]+)_([0-9]+)$`)

func (f *Filter) filterTables(filter map[string]any, out *[]map[string]any) {
	if rules, ok := filter["rules"]; ok && truthy(rules) {
		if rl, ok := rules.([]any); ok {
			for _, r := range rl {
				if rm, ok := r.(map[string]any); ok {
					f.filterTables(rm, out)
				}
			}
		}
		return
	}
	id, ok := filter["id"]
	if !ok || !truthy(id) {
		return
	}
	ids := anyToString(id)
	if m := reParentChild.FindStringSubmatch(ids); m != nil {
		cid, _ := strconv.ParseInt(m[2], 10, 64)
		filter["column_id"] = cid
	} else {
		filter["column_id"] = id
	}
	if f.layout != nil {
		cid, _ := anyToInt64(filter["column_id"])
		if f.layout.Column(cid) == nil {
			for k := range filter {
				delete(filter, k)
			}
		}
	}
	*out = append(*out, filter)
}

// ColumnIDs returns the IDs referred to by the filter rules.
func (f *Filter) ColumnIDs() []any {
	var out []any
	for _, r := range f.Filters() {
		out = append(out, r["id"])
	}
	return out
}

var reSubValue = regexp.MustCompile(`(?i)^\$([_0-9a-z]+)$`)

// ColumnsInSubs returns the columns whose values will be substituted
// into the filter ($short_name values).
func (f *Filter) ColumnsInSubs() []*Column {
	if f.subsBuilt {
		return f.columnsInSubs
	}
	if f.layout == nil {
		Panic("layout has not been set in filter")
	}
	var cols []*Column
	for _, r := range f.Filters() {
		v, ok := r["value"].(string)
		if !ok || v == "" {
			continue
		}
		m := reSubValue.FindStringSubmatch(v)
		if m == nil {
			continue
		}
		if c := f.layout.ColumnByNameShort(m[1]); c != nil {
			cols = append(cols, c)
		}
	}
	f.columnsInSubs = cols
	f.subsBuilt = true
	return cols
}

// SubValues substitutes values from the layout's record into the filter.
// Returns false if the record is not ready.
func (f *Filter) SubValues(layout *Layout) bool {
	filter := f.AsHash()
	subs := f.ColumnsInSubs()
	if layout.Record() == nil && len(subs) > 0 {
		filter = map[string]any{}
	} else {
		if rules, ok := filter["rules"].([]any); ok {
			for _, r := range rules {
				if rm, ok := r.(map[string]any); ok {
					if !f.subFilterSingle(rm, layout) {
						return false
					}
				}
			}
		}
	}
	f.SetHash(filter)
	return true
}

func (f *Filter) subFilterSingle(single map[string]any, layout *Layout) bool {
	record := layout.Record()
	if rules, ok := single["rules"]; ok && truthy(rules) {
		for _, r := range rules.([]any) {
			if rm, ok := r.(map[string]any); ok {
				if !f.subFilterSingle(rm, layout) {
					return false
				}
			}
		}
		return true
	}
	v, ok := single["value"].(string)
	if !ok || v == "" {
		return true
	}
	m := reSubValue.FindStringSubmatch(v)
	if m == nil {
		return true
	}
	col := layout.ColumnByNameShort(m[1])
	if col == nil {
		return true
	}
	datum := record.Fields()[col.ID]
	if col.Type == "curval" {
		ids := datum.(*CurcommonDatum).IDs()
		arr := make([]any, len(ids))
		for i, id := range ids {
			arr[i] = id
		}
		single["value"] = arr
	} else if col.Multivalue {
		ta := datum.TextAll()
		arr := make([]any, len(ta))
		for i, t := range ta {
			arr[i] = t
		}
		single["value"] = arr
	} else {
		if !col.Userinput() {
			if cd, ok := datum.(CodeDatum); ok {
				cd.ReEvaluate(ReEvalOptions{})
			}
		}
		if col.Numeric() {
			vals := datum.RawValues()
			if len(vals) > 0 {
				single["value"] = vals[0]
			} else {
				single["value"] = nil
			}
		} else {
			single["value"] = datum.AsString()
		}
	}
	return true
}

// Base64 returns the filter encoded for the front-end builder, with data
// values inserted for typeahead fields.
func (f *Filter) Base64() string {
	f.AsHash()
	if !f.IsPeopleFilter {
		for _, filter := range f.Filters() {
			if f.layout == nil {
				Panic("layout has not been set in filter")
			}
			cid, _ := anyToInt64(filter["column_id"])
			col := f.layout.Column(cid)
			if col == nil {
				continue
			}
			if col.HasFilterTypeahead() {
				filter["data"] = map[string]any{"text": col.FilterValueToText(anyToString(filter["value"]))}
			}
			if col.Type == "filval" {
				filter["filtered"] = col.RelatedFieldID
			}
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(f.AsJSON()))
}

var reCurdate = regexp.MustCompile(`^(\s*([0-9]+)\s*([+])\s*)?CURDATE(\s*([-+])\s*([0-9]+)\s*)?$`)

// ParseDateFilter parses CURDATE expressions (GADS::Filter->parse_date_filter).
func ParseDateFilter(now time.Time, value string) (time.Time, bool) {
	m := reCurdate.FindStringSubmatch(value)
	if m == nil {
		return time.Time{}, false
	}
	t := now
	v1, op1, op2, v2 := m[2], m[3], m[5], m[6]
	if op1 == "+" && v1 != "" && v1 != "0" {
		n, _ := strconv.Atoi(v1)
		t = t.Add(time.Duration(n) * time.Second)
	}
	if op2 == "+" && v2 != "" && v2 != "0" {
		n, _ := strconv.Atoi(v2)
		t = t.Add(time.Duration(n) * time.Second)
	}
	if op2 == "-" && v2 != "" && v2 != "0" {
		n, _ := strconv.Atoi(v2)
		t = t.Add(-time.Duration(n) * time.Second)
	}
	return t, true
}

// compareIgnoringColumnID compares two filter structures ignoring the
// temporary column_id keys (Data::Compare with ignore_hash_keys).
func compareIgnoringColumnID(a, b any) bool {
	return reflect.DeepEqual(stripColumnID(a), stripColumnID(b))
}

func stripColumnID(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, vv := range x {
			if k == "column_id" {
				continue
			}
			out[k] = stripColumnID(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = stripColumnID(vv)
		}
		return out
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int:
		return strconv.Itoa(x)
	}
	return v
}

// truthy is Perl truthiness for decoded JSON values.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != "" && x != "0"
	case int64:
		return x != 0
	case int:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return true
	case map[string]any:
		return true
	}
	return true
}

func anyToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', 15, 64)
	case bool:
		if x {
			return "1"
		}
		return ""
	}
	return EncodeJSON(v)
}

func anyToInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return i, err == nil
	}
	return 0, false
}
