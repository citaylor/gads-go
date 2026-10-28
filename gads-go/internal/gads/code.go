package gads

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/lua"
	"github.com/ctrlo/gads-go/internal/perl"
)

var reCode = regexp.MustCompile(`(?s)^\s*function\s+evaluate\s*\(([A-Za-z0-9_,\s]+)\)(.*?)end\s*$`)
var reParamSplit = regexp.MustCompile(`[,\s]+`)

type parsedCode struct {
	code   string
	params []string
}

func parseCode(code string) parsedCode {
	if code == "" {
		return parsedCode{}
	}
	m := reCode.FindStringSubmatch(code)
	if m == nil {
		Fail("Invalid code definition: must contain function evaluate(...)")
	}
	var params []string
	if m[1] != "" {
		for _, p := range reParamSplit.Split(m[1], -1) {
			if p != "" {
				params = append(params, p)
			}
		}
		// Perl split drops leading empty field only if it is produced by a
		// leading separator; it keeps the rest
		if strings.TrimLeft(m[1], ", \t\r\n") != m[1] {
			params = append([]string{""}, params...)
		}
	}
	return parsedCode{code: m[2], params: params}
}

// Params returns the short names of the parameters of the code.
func (c *Column) Params() []string {
	return parseCode(c.CodeFn()).params
}

// ParamColumns returns the columns used as parameters.
func (c *Column) ParamColumns(fatal bool) []*Column {
	var out []*Column
	for _, p := range c.Params() {
		col := c.layout.ColumnByNameShort(p)
		if col == nil {
			if fatal {
				Fail(`Unknown short column name "%s" in calculation`, p)
			}
			continue
		}
		if col.InstanceID != c.InstanceID {
			if fatal {
				Fail(`It is only possible to use fields from the same table (%s). "%s" is from %s.`, c.layout.Name(), p, col.layout.Name()) // As GADS: column layout is the one it was retrieved from
			}
		}
		out = append(out, col)
	}
	return out
}

// codeResult is the result of Column::Code::eval.
type codeResult struct {
	ret      any // nil, string, []any, map[string]any(daterange)
	err      string
	hasError bool
	code     string
}

// HasError reports whether the evaluation failed.
func (c codeResult) HasError() bool { return c.hasError }

// Error returns the evaluation error.
func (c codeResult) Error() string { return c.err }

// Return returns the flattened return value.
func (c codeResult) Return() any { return c.ret }

// flatten implements Column::Code::_flatten (Perl stringification).
func (c *Column) flatten(v any) any {
	if v == nil {
		return nil
	}
	if c.ReturnTypeFn() == "daterange" {
		m, _ := v.(map[string]any)
		return map[string]any{"from": perlStringify(m["from"]), "to": perlStringify(m["to"])}
	}
	return perlStringify(v)
}

// perlStringify stringifies a value returned from Inline::Lua.
func perlStringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return perl.NumString(x)
	case lua.Bool:
		if x {
			return "1"
		}
		return "0"
	case []any:
		return "ARRAY(0x0)"
	case map[string]any:
		return "HASH(0x0)"
	}
	return anyToString(v)
}

// Eval evaluates code with variables (Column::Code::eval).
func (c *Column) Eval(code string, vars map[string]any) codeResult {
	runCode := parseCode(code).code
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var mapping strings.Builder
	for _, k := range keys {
		mapping.WriteString(k + ` = vars["` + k + `"]` + "\n")
	}
	runCode = mapping.String() + runCode
	res, err := c.e.Lua.Run(runCode, vars, lua.Func(c.e.workingDaysDiffLua), lua.Func(c.e.workingDaysAddLua))
	if err != nil {
		msg := err.Error()
		if _, ok := err.(*lua.RuntimeError); ok {
			msg = strings.TrimRight(msg, "\n")
		}
		return codeResult{err: msg, hasError: true, code: runCode}
	}
	out := codeResult{code: runCode}
	ret := res.Return
	if c.MultivalueFn() {
		if arr, ok := ret.([]any); ok {
			var fl []any
			for _, v := range arr {
				fl = append(fl, c.flatten(v))
			}
			out.ret = fl
		} else if ret != nil {
			out.ret = c.flatten(ret)
		}
	} else if ret != nil {
		out.ret = c.flatten(ret)
	}
	if res.Error != nil && perlStringify(res.Error) != "" {
		out.err = perlStringify(res.Error)
		out.hasError = true
	}
	return out
}

// ---------------------------------------------------------------------
// Working days
// ---------------------------------------------------------------------

func gbHolidaysExist(year int, region string) bool {
	prefix := itoa(int64(year)) + "-"
	for d, regions := range gbHolidays {
		if strings.HasPrefix(d, prefix) {
			if _, ok := regions[region]; ok {
				return true
			}
		}
	}
	return false
}

func isGBHoliday(t time.Time, region string) bool {
	r, ok := gbHolidays[t.Format("2006-01-02")]
	if !ok {
		return false
	}
	_, ok = r[region]
	return ok
}

func fromEpoch(v any) time.Time {
	f := perl.Numify(anyToString(v))
	if x, ok := v.(float64); ok {
		f = x
	}
	sec := int64(f)
	return time.Unix(sec, 0).UTC()
}

func (e *Engine) workingDaysDiffLua(args []any) ([]any, error) {
	var res int64
	err := Try(func() {
		if len(args) != 4 {
			Fail("Parameters for working_days_diff need to be: start, end, country, region")
		}
		if anyToString(args[2]) != "GB" {
			Fail("Only country GB is currently supported")
		}
		if !truthy(args[0]) {
			Fail("Start date missing for working_days_diff")
		}
		if !truthy(args[1]) {
			Fail("End date missing for working_days_diff")
		}
		region := anyToString(args[3])
		start, end := fromEpoch(args[0]), fromEpoch(args[1])
		min, max := start.Year(), end.Year()
		if !start.Before(end) {
			min = end.Year()
		}
		if !end.After(start) {
			max = start.Year()
		}
		if start.Before(end) {
			min = start.Year()
		}
		if end.After(start) {
			max = end.Year()
		}
		for y := min; y <= max; y++ {
			if !gbHolidaysExist(y, region) {
				Fail("No bank holiday information available for year %d", y)
			}
		}
		days := int64(0)
		if end.After(start) {
			m := start.AddDate(0, 0, 1)
			for !m.After(end) {
				if !isGBHoliday(m, region) && m.Weekday() != time.Saturday && m.Weekday() != time.Sunday {
					days++
				}
				m = m.AddDate(0, 0, 1)
			}
		} else {
			m := start.AddDate(0, 0, -1)
			for !m.Before(end) {
				if !isGBHoliday(m, region) && m.Weekday() != time.Saturday && m.Weekday() != time.Sunday {
					days--
				}
				m = m.AddDate(0, 0, -1)
			}
		}
		res = days
	})
	if err != nil {
		return nil, err
	}
	return []any{lua.Int(res)}, nil
}

func (e *Engine) workingDaysAddLua(args []any) ([]any, error) {
	var res int64
	err := Try(func() {
		if len(args) != 4 {
			Fail("Parameters for working_days_add need to be: start, end, country, region")
		}
		if anyToString(args[2]) != "GB" {
			Fail("Only country GB is currently supported")
		}
		if !truthy(args[0]) {
			Fail("Date missing for working_days_add")
		}
		region := anyToString(args[3])
		start := fromEpoch(args[0])
		if !gbHolidaysExist(start.Year(), region) {
			Fail("No bank holiday information available for year %d", start.Year())
		}
		days := int64(perl.Numify(anyToString(args[1])))
		if f, ok := args[1].(float64); ok {
			days = int64(f)
		}
		for days != 0 {
			start = start.AddDate(0, 0, 1)
			if !isGBHoliday(start, region) && start.Weekday() != time.Saturday && start.Weekday() != time.Sunday {
				days--
			}
		}
		res = start.Unix()
	})
	if err != nil {
		return nil, err
	}
	return []any{lua.Int(res)}, nil
}

// ---------------------------------------------------------------------
// Code datum (calc and rag)
// ---------------------------------------------------------------------

type codeDatum struct {
	datumBase
	values      []any // calc: string/float64/int64/time.Time/DateSpan; rag: string
	valuesBuilt bool
	hasValueSet bool
}

func (d *codeDatum) vars() map[string]any {
	col := d.column
	code := col.CodeFn()
	needed := map[string]bool{}
	for _, n := range d.record.layout.AllShortNames() {
		if strings.Contains(code, n) {
			needed[n] = true
		}
	}
	seen := newSeenRoot()
	return d.record.ValuesByShortname(col.Params(), needed, seen)
}

// buildValue evaluates or loads the code value.
func (d *codeDatum) buildValue(convert func(codeResult) []any) []any {
	col := d.column
	if d.hasInitValue {
		var out []any
		rt := col.ReturnTypeFn()
		for _, r := range d.initValue {
			switch rt {
			case "datetime":
				if t, ok := dtFromDB(r.Value); ok {
					out = append(out, t)
				} else {
					out = append(out, nil)
				}
			case "date":
				if t, ok := dtFromDB(r.ValueDate); ok {
					out = append(out, t)
				} else if t, ok := dtFromDB(r.Value); ok && col.Type == "calc" {
					out = append(out, t)
				} else {
					out = append(out, nil)
				}
			case "daterange":
				f, ok1 := dtFromDB(r.From)
				t, ok2 := dtFromDB(r.To)
				if ok1 || ok2 {
					out = append(out, DateSpan{f, t})
				}
			default:
				v := r.Value
				if col.Type == "calc" {
					switch col.ValueField() {
					case "value_text":
						if r.ValueText != nil || r.Value != nil {
							v = firstNonNil(r.ValueText, r.Value)
						}
					case "value_int":
						v = firstNonNil(r.ValueInt, r.Value)
					case "value_numeric":
						v = firstNonNil(r.ValueNumeric, r.Value)
					}
				}
				out = append(out, v)
			}
		}
		return out
	}
	code := col.CodeFn()
	if code == "" {
		return nil
	}
	// Used during tests to check that stored values are being used
	if col.e.PanicOnCode {
		Panic("Entering calculation code")
	}
	vars := d.vars()
	ret := col.Eval(code, vars)
	if ret.hasError {
		col.e.Warn(`Failed to eval code for field "%s": %s`, col.Name, ret.err)
	}
	return convert(ret)
}

func firstNonNil(vs ...any) any {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}
	return nil
}

// CalcDatum is GADS::Datum::Calc.
type CalcDatum struct{ codeDatum }

// Values returns the calculated values.
func (d *CalcDatum) Values() []any {
	if !d.valuesBuilt {
		d.values = d.buildValue(d.convertValue)
		d.valuesBuilt = true
	}
	return d.values
}

func (d *CalcDatum) convertDate(v any) (time.Time, bool) {
	if v == nil {
		return time.Time{}, false
	}
	s := anyToString(v)
	if !perl.LooksLikeNumber(s) {
		return time.Time{}, false
	}
	f := perl.Numify(s)
	return time.Unix(int64(f), 0).UTC(), true
}

func (d *CalcDatum) convertValue(in codeResult) []any {
	col := d.column
	var values []any
	if col.MultivalueFn() {
		if arr, ok := in.ret.([]any); ok {
			values = arr
		} else {
			values = []any{in.ret}
		}
	} else {
		values = []any{in.ret}
	}
	if in.hasError {
		values = []any{`Unable to evaluate field "` + col.Name + `": ` + in.err}
	}
	var out []any
	for _, v := range values {
		switch col.ReturnTypeFn() {
		case "date":
			if t, ok := d.convertDate(v); ok {
				t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
				out = append(out, t)
			} else {
				out = append(out, nil)
			}
		case "datetime":
			if t, ok := d.convertDate(v); ok {
				out = append(out, t)
			} else {
				out = append(out, nil)
			}
		case "daterange":
			if !truthy(v) {
				continue
			}
			if m, ok := v.(map[string]any); ok && truthy(m["from"]) && truthy(m["to"]) {
				f, _ := d.convertDate(m["from"])
				t, _ := d.convertDate(m["to"])
				out = append(out, DateSpan{f, t})
			} else {
				col.e.Warn("Unexpected daterange return type")
			}
		case "numeric", "integer":
			if v == nil {
				continue
			}
			s := anyToString(v)
			if perl.LooksLikeNumber(s) {
				if col.ReturnTypeFn() == "integer" {
					out = append(out, perl.RoundHalfAway(perl.Numify(s)))
				} else {
					out = append(out, s)
				}
			}
		case "globe":
			if !truthy(v) {
				// Do nothing
			} else if checkCountry(anyToString(v)) {
				out = append(out, v)
			} else {
				col.e.Mistake("Failed to produce globe location: unknown country %s", anyToString(v))
			}
		default:
			if v != nil {
				out = append(out, v)
			}
		}
	}
	return out
}

// AsStrings returns the values as strings.
func (d *CalcDatum) AsStrings() []string {
	col := d.column
	format := col.DateFormat()
	var out []string
	for _, v := range d.Values() {
		switch x := v.(type) {
		case nil:
			out = append(out, "")
		case time.Time:
			out = append(out, FormatDate(x, format))
		case DateSpan:
			out = append(out, DaterangeAsString(x, format))
		default:
			if col.ReturnTypeFn() == "numeric" {
				dc := int64(0)
				if col.DecimalPlaces != nil {
					dc = *col.DecimalPlaces
				}
				f := numValue(v)
				if dc != 0 {
					out = append(out, perl.SprintfFixed(f, int(dc)))
				} else {
					out = append(out, perl.NumString(f))
				}
			} else {
				out = append(out, scalarString(v))
			}
		}
	}
	return out
}

func numValue(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	}
	return perl.Numify(anyToString(v))
}

// scalarString stringifies a scalar as Perl would.
func scalarString(v any) string {
	switch x := v.(type) {
	case float64:
		return perl.NumString(x)
	case time.Time:
		return db.FormatDateTime(x)
	}
	return anyToString(v)
}

// AsString joins the values.
func (d *CalcDatum) AsString() string { return strings.Join(d.AsStrings(), ", ") }

// TextAll returns the values.
func (d *CalcDatum) TextAll() []string { return d.AsStrings() }

// Blank reports an empty string.
func (d *CalcDatum) Blank() bool { return d.AsString() == "" }

// HTML escapes the string.
func (d *CalcDatum) HTML() string { return htmlEscape(d.AsString()) }

// SetValue is called on code fields only by bulk operations.
func (d *CalcDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	d.hasValueSet = true
}

// HTMLForm returns the values.
func (d *CalcDatum) HTMLForm() []any {
	if len(d.Values()) == 0 {
		return []any{""}
	}
	var out []any
	for _, s := range d.AsStrings() {
		out = append(out, s)
	}
	return out
}

// SetValuesForChild returns the values.
func (d *CalcDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns string values.
func (d *CalcDatum) SearchValuesUnique() []string { return d.AsStrings() }

// HasValue reports a set value.
func (d *CalcDatum) HasValue() bool { return d.hasValueSet }

// RawValues returns the values.
func (d *CalcDatum) RawValues() []any { return d.Values() }

// ValueRegexTest returns the string.
func (d *CalcDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the string.
func (d *CalcDatum) FilterValue() string { return d.AsString() }

// ForCode returns the Lua value.
func (d *CalcDatum) ForCode(ctx ForCodeContext) any {
	col := d.column
	rt := col.ReturnTypeFn()
	var out []any
	for _, v := range d.Values() {
		switch rt {
		case "date", "datetime":
			if t, ok := v.(time.Time); ok {
				out = append(out, dateForCode(&t))
			} else {
				out = append(out, nil)
			}
		case "daterange":
			if s, ok := v.(DateSpan); ok {
				st, en := s.Start, s.End
				out = append(out, map[string]any{"from": dateForCode(&st), "to": dateForCode(&en), "value": DaterangeAsString(s, col.DateFormat())})
			}
		case "numeric":
			n, isInt := perl.NumifyTyped(d.AsString())
			if isInt {
				out = append(out, lua.Int(n.(int64)))
			} else {
				out = append(out, lua.Num(n.(float64)))
			}
		case "integer":
			if v == nil {
				out = append(out, nil)
			} else {
				out = append(out, lua.Int(int64(perl.Int(numValue(v)))))
			}
		default:
			if v == nil {
				out = append(out, nil)
			} else {
				out = append(out, scalarString(v))
			}
		}
	}
	if col.MultivalueFn() {
		if out == nil {
			out = []any{}
		}
		return out
	}
	if len(out) > 0 {
		return out[0]
	}
	return nil
}

// Clone copies the datum.
func (d *CalcDatum) Clone(record *Record, fresh bool) Datum {
	n := &CalcDatum{}
	n.datumBase = d.cloneBase(record)
	n.values = append([]any(nil), d.Values()...)
	n.valuesBuilt = true
	n.hasValueSet = d.hasValueSet
	return n
}

// equal compares two calc value lists (Datum::Calc::equal).
func (d *CalcDatum) equal(a, b []any) bool {
	col := d.column
	rt := col.ReturnTypeFn()
	conv := func(in []any) []any {
		out := append([]any(nil), in...)
		if rt == "daterange" {
			for i, v := range out {
				if s, ok := v.(DateSpan); ok {
					out[i] = DaterangeAsString(s, col.DateFormat())
				}
			}
		}
		if len(out) > 0 && out[0] != nil {
			sort.SliceStable(out, func(i, j int) bool { return scalarString(out[i]) < scalarString(out[j]) })
		}
		return out
	}
	a, b = conv(a), conv(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if (x == nil) != (y == nil) {
			return false
		}
		if x == nil && y == nil {
			continue
		}
		switch rt {
		case "numeric", "integer":
			if numValue(x) != numValue(y) {
				return false
			}
		case "date", "datetime":
			tx, ok1 := x.(time.Time)
			ty, ok2 := y.(time.Time)
			if !ok1 || !ok2 || !tx.Equal(ty) {
				return false
			}
		case "daterange":
			// As GADS: values compared after conversion to text are strings
			// so this check always fails for non-hash values
			return false
		default:
			if scalarString(x) != scalarString(y) {
				return false
			}
		}
	}
	return true
}

// ReEvaluate re-runs the calculation (Datum::Code::re_evaluate).
func (d *CalcDatum) ReEvaluate(o ReEvalOptions) {
	if o.NoErrors && d.column.ReturnTypeFn() == "error" {
		return
	}
	old := d.Values()
	original := d.Clone(nil, false)
	if !d.record.NewEntry() || o.Force {
		d.hasInitValue = false
		d.initValue = nil
		d.valuesBuilt = false
		d.values = nil
	}
	n := d.Values()
	if !d.equal(old, n) {
		d.changed = true
		d.oldvalue = original
	}
}

// WriteValue writes the value to the calcval table.
func (d *CalcDatum) WriteValue() { d.writeCache("calcval", d.equalScalar, d.calcRow) }

func (d *CalcDatum) equalScalar(a, b any) bool {
	return d.equal([]any{a}, []any{b})
}

func (d *CalcDatum) calcRow(v any) (map[string]any, string) {
	col := d.column
	vf := col.ValueField()
	if col.ReturnTypeFn() == "daterange" {
		if s, ok := v.(DateSpan); ok {
			text := DaterangeAsString(s, col.DateFormat())
			return map[string]any{"value_date_from": s.Start, "value_date_to": s.End, "value_text": text}, text
		}
		return map[string]any{}, ""
	}
	switch x := v.(type) {
	case time.Time:
		if vf == "value_datetime" {
			return map[string]any{vf: x}, db.FormatDate(x)
		}
		return map[string]any{vf: db.Date(x)}, db.FormatDate(x)
	case float64:
		if vf == "value_int" {
			return map[string]any{vf: int64(x)}, perl.NumString(x)
		}
		return map[string]any{vf: x}, perl.NumString(x)
	}
	if vf == "value_numeric" && v != nil {
		return map[string]any{vf: perl.Numify(anyToString(v))}, anyToString(v)
	}
	return map[string]any{vf: v}, anyToString(v)
}

// writeCache implements Datum::Code::write_cache.
func (d *codeDatum) writeCache(table string, equal func(a, b any) bool, toRow func(v any) (map[string]any, string)) {
	col := d.column
	var values []any
	vals := d.currentValues()
	if len(vals) > 0 && vals[0] != nil {
		values = append(values, vals...)
		sort.SliceStable(values, func(i, j int) bool { return scalarString(values[i]) < scalarString(values[j]) })
	}
	vfield := col.ValueField()
	if table == "ragval" {
		vfield = "value"
	}
	e := col.e
	e.DB.Txn(func() {
		rows := e.DB.Query(`SELECT * FROM `+q(table)+` "me" WHERE "record_id" = ? AND "layout_id" = ? ORDER BY `+q("me."+vfield), d.recordID, col.ID)
		uniqueTable := ""
		if col.Type == "calc" {
			uniqueTable = "calc_unique"
		}
		if len(values) != len(rows) {
			e.DB.Exec(`DELETE FROM `+q(table)+` WHERE "record_id" = ? AND "layout_id" = ?`, d.recordID, col.ID)
			if old := d.oldvalue; old != nil && uniqueTable != "" {
				for _, ov := range old.(*CalcDatum).Values() {
					if ov == nil {
						// A value that cannot be read with the current return
						// type (e.g. after a change of type) deletes the
						// unique values without one
						d.deleteUnique(uniqueTable, map[string]any{vfield: nil})
						continue
					}
					row, sv := d.uniqueKey(ov, vfield, toRow)
					if sv != "" && d.uniqueExists(sv) {
						continue
					}
					d.deleteUnique(uniqueTable, row)
				}
			}
			rows = nil
		}
		for _, v := range values {
			var row db.Row
			if len(rows) > 0 {
				row = rows[0]
				rows = rows[1:]
			}
			toWrite, _ := toRow(v)
			if col.ReturnTypeFn() == "daterange" && v == nil {
				toWrite = map[string]any{}
			}
			if row != nil {
				existing := d.rowValue(row, vfield)
				if !equal(existing, v) {
					blank := map[string]any{}
					if col.Type == "calc" {
						for _, f := range []string{"value_date", "value_datetime", "value_int", "value_numeric", "value_text", "value_date_from", "value_date_to"} {
							blank[f] = nil
						}
					} else {
						blank[vfield] = nil
					}
					oldRow := map[string]any{}
					for k := range blank {
						oldRow[k] = row[k]
					}
					for k, v := range toWrite {
						blank[k] = v
					}
					id, _ := db.AsInt64(row["id"])
					e.DB.UpdateMap(table, blank, map[string]any{"id": id})
					if uniqueTable != "" {
						sv := anyToString(row[vfield])
						if !(sv != "" && d.uniqueExists(sv)) {
							nn := map[string]any{}
							for k, v := range oldRow {
								if v != nil {
									nn[k] = v
								}
							}
							d.deleteUnique(uniqueTable, nn)
						}
						d.writeUnique(uniqueTable, toWrite)
					}
				}
			} else {
				m := map[string]any{"record_id": d.recordID, "layout_id": col.ID}
				for k, v := range toWrite {
					m[k] = v
				}
				e.DB.InsertMap(table, m)
				if uniqueTable != "" {
					d.writeUnique(uniqueTable, toWrite)
				}
			}
		}
		for _, row := range rows {
			id, _ := db.AsInt64(row["id"])
			e.DB.Exec(`DELETE FROM `+q(table)+` WHERE "id" = ?`, id)
		}
	})
}

func (d *codeDatum) currentValues() []any {
	return d.values
}

func (d *codeDatum) rowValue(row db.Row, vfield string) any {
	col := d.column
	v := row[vfield]
	if col.Type == "calc" {
		switch col.ReturnTypeFn() {
		case "date", "datetime":
			if t, ok := dtFromDB(v); ok {
				return t
			}
			return nil
		case "daterange":
			return v
		}
	}
	return v
}

func (d *codeDatum) uniqueKey(v any, vfield string, toRow func(v any) (map[string]any, string)) (map[string]any, string) {
	row, _ := toRow(v)
	sv := ""
	// As GADS, a datetime is also formatted as a date
	if t, ok := v.(time.Time); ok && (vfield == "value_date" || vfield == "value_datetime") {
		sv = db.FormatDate(t)
	} else {
		sv = scalarString(v)
	}
	m := map[string]any{vfield: row[vfield]}
	return m, sv
}

func (d *codeDatum) uniqueExists(value string) bool {
	col := d.column
	layout := col.layout.Clone(col.InstanceID)
	old := col.e.IgnorePermissionsSearch
	col.e.IgnorePermissionsSearch = true
	defer func() { col.e.IgnorePermissionsSearch = old }()
	recs := col.e.NewRecords(layout, nil)
	return recs.FindUnique(col, value, d.record.currentID, nil).Count() > 0
}

func (d *codeDatum) writeUnique(table string, values map[string]any) {
	e := d.column.e
	// The cache is not written for a value over 250 characters
	for _, v := range values {
		if v != nil && len([]rune(anyToString(v))) > 250 {
			return
		}
	}
	m := map[string]any{"layout_id": d.column.ID}
	for k, v := range values {
		m[k] = v
	}
	svp := e.DB.SavepointBegin()
	if _, err := e.DB.TryInsertMap(table, m); err != nil {
		e.DB.SavepointRollback(svp)
		e.DB.SavepointRelease(svp)
		if !db.IsUniqueViolation(err) {
			panic(err)
		}
		return
	}
	e.DB.SavepointRelease(svp)
}

func (d *codeDatum) deleteUnique(table string, values map[string]any) {
	m := map[string]any{"layout_id": d.column.ID}
	for k, v := range values {
		m[k] = v
	}
	d.column.e.DB.DeleteWhere(table, m)
}

// RagDatum is GADS::Datum::Rag.
type RagDatum struct{ codeDatum }

var ragMapping = map[string]string{
	"a_grey":      "undefined",
	"b_red":       "danger",
	"b_attention": "attention",
	"c_amber":     "warning",
	"c_yellow":    "advisory",
	"d_green":     "success",
	"d_blue":      "complete",
	"e_purple":    "unexpected",
}

// Values returns the RAG values.
func (d *RagDatum) Values() []any {
	if !d.valuesBuilt {
		d.values = d.buildValue(d.convertValue)
		d.valuesBuilt = true
	}
	return d.values
}

func (d *RagDatum) convertValue(in codeResult) []any {
	if in.hasError {
		return []any{"e_purple"}
	}
	v := in.ret
	s := perlStringify(v)
	if v == nil || !perl.True(s) {
		return []any{""}
	}
	switch strings.ToLower(s) {
	case "grey":
		return []any{"a_grey"}
	case "red":
		return []any{"b_red"}
	case "attention":
		return []any{"b_attention"}
	case "amber":
		return []any{"c_amber"}
	case "yellow":
		return []any{"c_yellow"}
	case "green":
		return []any{"d_green"}
	case "blue":
		return []any{"d_blue"}
	}
	return []any{"e_purple"}
}

func (d *RagDatum) valueSingle() any {
	v := d.Values()
	if len(v) == 0 {
		return nil
	}
	return v[len(v)-1]
}

// AsString returns the RAG code.
func (d *RagDatum) AsString() string {
	v := d.valueSingle()
	if v == nil {
		return ""
	}
	return anyToString(v)
}

// AsGrade returns the RAG grade.
func (d *RagDatum) AsGrade() string { return ragMapping[d.AsString()] }

// TextAll returns the value.
func (d *RagDatum) TextAll() []string { return []string{d.AsString()} }

// Blank is always false.
func (d *RagDatum) Blank() bool { return false }

// HTML returns the grade.
func (d *RagDatum) HTML() string {
	// As the rag.tt template (render_rag_datum, rendered without a layout so
	// without a description), including its whitespace
	grade := d.AsGrade()
	if _, ok := ragSymbols[grade]; !ok || grade == "" {
		grade = "blank"
	}
	return strings.Repeat("\n", 28) + "    \n    \n    \n    <span class=\"rag rag--" + grade +
		"\" title=\"\" aria-labelledby=\"rag_" + grade + "_meaning\">\n      <span>" + ragSymbols[grade] +
		"</span>\n    </span>\n\n"
}

// ragSymbols are the symbols of each RAG grade (snippets/rag_symbols.tt)
var ragSymbols = map[string]string{
	"danger":     "&#x2717;",
	"attention":  "&#x26a0;",
	"warning":    "&#x26a0;",
	"advisory":   "&#x26a0;",
	"success":    "&#x2713;",
	"complete":   "&#x2713;",
	"undefined":  "&#x3f;",
	"unexpected": "&#x21;",
	"blank":      "&#x5f;",
}

// SetValue is not applicable.
func (d *RagDatum) SetValue(value any, opts SetValueOptions) {
	d.checkParent(opts)
	d.hasValueSet = true
}

// HTMLForm returns the value.
func (d *RagDatum) HTMLForm() []any {
	if len(d.Values()) == 0 {
		return []any{""}
	}
	return d.Values()
}

// SetValuesForChild returns the value.
func (d *RagDatum) SetValuesForChild() []any { return d.HTMLForm() }

// SearchValuesUnique returns the value.
func (d *RagDatum) SearchValuesUnique() []string { return []string{d.AsString()} }

// HasValue reports set.
func (d *RagDatum) HasValue() bool { return d.hasValueSet }

// RawValues returns the values.
func (d *RagDatum) RawValues() []any { return d.Values() }

// ValueRegexTest returns the value.
func (d *RagDatum) ValueRegexTest(string) []string { return []string{d.AsString()} }

// FilterValue returns the value.
func (d *RagDatum) FilterValue() string { return d.AsString() }

// ForCode returns the value as a string.
func (d *RagDatum) ForCode(ctx ForCodeContext) any { return d.AsString() }

// Clone copies.
func (d *RagDatum) Clone(record *Record, fresh bool) Datum {
	n := &RagDatum{}
	n.datumBase = d.cloneBase(record)
	n.values = append([]any(nil), d.Values()...)
	n.valuesBuilt = true
	n.hasValueSet = d.hasValueSet
	return n
}

func (d *RagDatum) equal(a, b any) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil && b == nil {
		return true
	}
	return anyToString(a) == anyToString(b)
}

// ReEvaluate re-runs the code.
func (d *RagDatum) ReEvaluate(o ReEvalOptions) {
	old := d.valueSingle()
	original := d.Clone(nil, false)
	if !d.record.NewEntry() || o.Force {
		d.hasInitValue = false
		d.initValue = nil
		d.valuesBuilt = false
		d.values = nil
	}
	n := d.valueSingle()
	if !d.equal(old, n) {
		d.changed = true
		d.oldvalue = original
	}
}

// WriteValue writes to ragval.
func (d *RagDatum) WriteValue() {
	d.Values()
	d.writeCache("ragval", d.equal, func(v any) (map[string]any, string) {
		return map[string]any{"value": v}, anyToString(v)
	})
}

// UpdateCached recalculates the values of a code field for all records.
func (c *Column) UpdateCached(noAlertSend bool) {
	if !c.writeCache {
		return
	}
	c.codeLoaded = false
	layout := c.layout
	layout.Clear()
	old := c.e.IgnorePermissions
	c.e.IgnorePermissions = true
	defer func() { c.e.IgnorePermissions = old }()
	col := layout.Column(c.ID)
	if col == nil {
		return
	}
	recs := c.e.NewRecords(layout.Clone(col.InstanceID), nil)
	recs.SetColumns(append(append([]int64{}, col.DependsOn()...), col.ID))
	recs.IgnoreViewLimitExtra = true
	recs.SetIncludeChildren(true)
	var changed []int64
	for {
		rec := recs.Single()
		if rec == nil {
			break
		}
		d := rec.Fields()[col.ID].(CodeDatum)
		d.ReEvaluate(ReEvalOptions{NoErrors: true})
		d.WriteValue()
		if d.base().changed {
			changed = append(changed, rec.currentID)
		}
	}
	if noAlertSend || c.e.AlertSender == nil {
		return
	}
	c.e.AlertSender(c.e, layout, changed, []int64{col.ID}, false)
}

// checkCountry implements the plotly country regexes used for globe calcs.
func checkCountry(country string) bool {
	for _, re := range countryRegexes {
		if ok, _ := re.MatchString(country); ok {
			return true
		}
	}
	return false
}
