package gads

import (
	"fmt"

	"github.com/dlclark/regexp2"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/lua"
)

// rawValue is a value as retrieved from a value table (the equivalent of
// a HashRefInflator row used as a datum init_value).
type rawValue struct {
	ID          int64
	Value       any // string, int64, float64, time.Time or nil
	From, To    any // daterange / calc daterange
	Text        string
	HasText     bool
	Deleted     bool
	Mimetype    string
	User        map[string]any
	Record      *Record
	ChildUnique bool
	// NullJoin is a value fetched by a join that found no value (Perl
	// prefetches it as a hash of undefined values)
	NullJoin bool
	// Calc value fields
	ValueText    any
	ValueInt     any
	ValueDate    any
	ValueNumeric any
	subRecordID  int64
}

// SetValueOptions are options to Datum.SetValue.
type SetValueOptions struct {
	Draft           bool
	IsParentValue   bool
	Bulk            bool
	NoValidation    bool
	AllowSetAutocur bool
	// SubtractDaysEnd is subtracted from the end of each date range (the
	// end of a range of the timeline is midnight of the following day)
	SubtractDaysEnd int
}

// ReEvalOptions are options to code re-evaluation.
type ReEvalOptions struct {
	NoErrors        bool
	Force           bool
	SubmissionToken string
	NewEntry        bool
}

// ForCodeContext carries recursion information for code values.
type ForCodeContext struct {
	Fields      map[string]bool
	AlreadySeen *seenNode
}

// Datum is the value of a column for a record (GADS::Datum).
type Datum interface {
	base() *datumBase
	Column() *Column
	SetValue(value any, opts SetValueOptions)
	AsString() string
	TextAll() []string
	Blank() bool
	HTMLForm() []any
	SetValuesForChild() []any
	SearchValuesUnique() []string
	ForCode(ctx ForCodeContext) any
	ValueRegexTest(submissionToken string) []string
	FilterValue() string
	Clone(record *Record, fresh bool) Datum
	HasValue() bool
	RawValues() []any
	HTML() string
}

// CodeDatum is implemented by calc/rag datums.
type CodeDatum interface {
	Datum
	ReEvaluate(o ReEvalOptions)
	WriteValue()
}

type datumBase struct {
	record             *Record
	recordID           int64
	currentID          int64
	column             *Column
	changed            bool
	childUnique        bool
	childUniqueOld     *bool
	initValue          []rawValue
	hasInitValue       bool
	initNoValue        bool
	oldvalue           Datum
	isAwaitingApproval bool
	forCodeCache       any
	forCodeBuilt       bool
}

func (b *datumBase) base() *datumBase { return b }

// Column returns the datum's column.
func (b *datumBase) Column() *Column { return b.column }

// Record returns the datum's record.
func (b *datumBase) Record() *Record { return b.record }

// Changed reports whether the value has changed.
func (b *datumBase) Changed() bool { return b.changed }

// SetChanged sets the changed flag.
func (b *datumBase) SetChanged(v bool) { b.changed = v }

// OldValue returns the value before the last set_value.
func (b *datumBase) OldValue() Datum { return b.oldvalue }

// IsAwaitingApproval reports approval state.
func (b *datumBase) IsAwaitingApproval() bool { return b.isAwaitingApproval }

// SetChildUnique sets child_unique, flagging a change if different.
func (b *datumBase) SetChildUnique(v bool) {
	if b.childUniqueOld != nil && v != *b.childUniqueOld {
		b.changed = true
	}
	b.childUnique = v
	b.childUniqueOld = &v
}

// checkParent raises the parent value error (Datum::set_value).
func (b *datumBase) checkParent(opts SetValueOptions) {
	if !opts.IsParentValue && !b.column.CanChild() && b.record != nil && b.record.ParentID() != 0 {
		Fail("Cannot set this value as it is a parent value")
	}
	b.forCodeBuilt = false
	b.forCodeCache = nil
}

func (b *datumBase) cloneBase(record *Record) datumBase {
	nb := datumBase{
		record:    b.record,
		recordID:  b.recordID,
		currentID: b.currentID,
		column:    b.column,
	}
	if record != nil {
		nb.record = record
	}
	return nb
}

// DatumBase helpers usable via interface
func datumChanged(d Datum) bool          { return d.base().changed }
func datumChildUnique(d Datum) bool      { return d.base().childUnique }
func datumSetRecordID(d Datum, id int64) { d.base().recordID = id }

// DatumChanged is the exported accessor.
func DatumChanged(d Datum) bool { return d.base().changed }

// DependentNotShown reports whether the datum's field is hidden by its
// display conditions (Datum::dependent_not_shown).
func DependentNotShown(d Datum, oldvalue bool, submissionToken string) bool {
	b := d.base()
	col := b.column
	if col == nil {
		return false
	}
	filters := col.DisplayFields().Filters()
	if len(filters) == 0 {
		return false
	}
	cond := col.DisplayCondition
	shown := false
	fields := b.record.Fields()
	for _, filter := range filters {
		dfid, _ := anyToInt64(filter["column_id"])
		regex := anyToString(filter["value"])
		vd, ok := fields[dfid]
		if !ok || vd == nil {
			shown = true
			continue
		}
		op := anyToString(filter["operator"])
		if strings.Contains(op, "equal") {
			regex = "^" + regex + "$"
		}
		if oldvalue && vd.base().changed && vd.base().oldvalue != nil {
			vd = vd.base().oldvalue
		}
		values := vd.ValueRegexTest(submissionToken)
		formCol := vd.Column()
		if formCol.Type == "filval" {
			formCol = formCol.RelatedField()
		}
		if formCol != nil && !formCol.UserCan("read") {
			values = []string{""}
		}
		thisNotShown := true
		if strings.Contains(op, "not") {
			thisNotShown = false
		}
		if len(values) == 0 {
			values = []string{""}
		}
		re := perlRegex("(?i)" + regex)
		for _, v := range values {
			if strings.Contains(op, "not") {
				if re != nil && re.MatchString(v) {
					thisNotShown = true
				}
			} else {
				if re != nil && re.MatchString(v) {
					thisNotShown = false
				}
			}
		}
		if !thisNotShown {
			shown = true
		}
		if cond != "" {
			if cond == "OR" {
				if shown {
					break
				}
			} else {
				if thisNotShown {
					shown = false
				}
				if !shown {
					break
				}
			}
		}
	}
	return !shown
}

var regexCache = map[string]*regexp2.Regexp{}

// perlMatcher matches with Perl-compatible (backtracking) semantics.
type perlMatcher struct{ re *regexp2.Regexp }

// MatchString reports whether s matches.
func (m perlMatcher) MatchString(s string) bool {
	if m.re == nil {
		return false
	}
	ok, _ := m.re.MatchString(s)
	return ok
}

// perlRegex compiles a (user supplied) Perl regex, returning nil if it
// cannot be compiled.
func perlRegex(re string) *perlMatcher {
	r, ok := regexCache[re]
	if !ok {
		var err error
		r, err = regexp2.Compile(re, regexp2.None)
		if err != nil {
			r = nil
		}
		regexCache[re] = r
	}
	if r == nil {
		return nil
	}
	return &perlMatcher{r}
}

// dateForCode converts a date to the hash passed to Lua (Datum::date_for_code).
func dateForCode(t *time.Time) any {
	if t == nil {
		return nil
	}
	v := t.In(London())
	return map[string]any{
		"year":   lua.Int(v.Year()),
		"month":  lua.Int(int(v.Month())),
		"day":    lua.Int(v.Day()),
		"hour":   lua.Int(v.Hour()),
		"minute": lua.Int(v.Minute()),
		"second": lua.Int(v.Second()),
		"yday":   lua.Int(v.YearDay()),
		"epoch":  lua.Int(t.Unix()),
		"ymd":    v.Format("2006-01-02"),
	}
}

// htmlEscape implements HTML::Entities::encode_entities with its default
// unsafe characters: control characters, high-bit characters and <&>'".
// Characters with a named entity use the name, others the hex form.
func htmlEscape(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '<':
			sb.WriteString("&lt;")
		case r == '>':
			sb.WriteString("&gt;")
		case r == '&':
			sb.WriteString("&amp;")
		case r == '"':
			sb.WriteString("&quot;")
		case r == '\'':
			sb.WriteString("&#39;")
		case r > 126 || (r < 32 && r != '\n' && r != '\r' && r != '\t'):
			if name, ok := entityNames[r]; ok {
				sb.WriteString("&" + name + ";")
			} else {
				sb.WriteString(fmt.Sprintf("&#x%X;", r))
			}
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// sortedStrings returns a sorted copy (Perl sort).
func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// joinSpace joins as "@array" interpolation does.
func joinSpace(in []string) string { return strings.Join(in, " ") }

// valueList normalises a set_value input into a list.
func valueList(value any) []any {
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		return v
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	case []int64:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	}
	return []any{value}
}

// DatumOldvalue returns the value of a datum before it was changed.
func DatumOldvalue(d Datum) Datum { return d.base().oldvalue }

// SetDatumOldvalue sets the datum's oldvalue (as $datum->oldvalue($old)).
func SetDatumOldvalue(d, old Datum) { d.base().oldvalue = old }

// NewStandaloneDatum creates a datum for a column that is not part of a
// retrieved record, as GADS does for grouped values (with initial values) or
// for values that start as undefined (initNoValue).
func (e *Engine) NewStandaloneDatum(col *Column, init []any, hasInit, initNoValue bool) Datum {
	r := e.NewRecord(col.layout, nil)
	d := e.newDatum(col, r)
	b := d.base()
	b.initNoValue = initNoValue
	if hasInit {
		b.hasInitValue = true
		for _, v := range init {
			switch x := v.(type) {
			case map[string]any:
				b.initValue = append(b.initValue, rawValue{User: x})
			case []any:
				rv := rawValue{}
				if len(x) > 0 {
					rv.From = x[0]
				}
				if len(x) > 1 {
					rv.To = x[1]
				}
				b.initValue = append(b.initValue, rv)
			default:
				b.initValue = append(b.initValue, rawValue{Value: v})
			}
		}
	}
	return d
}

// DatumRecord returns the record of a datum (nil if none)
func DatumRecord(d Datum) *Record { return d.base().record }
