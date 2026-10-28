package gads

import (
	"encoding/base64"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/cldr"
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/perl"
)

// ColumnTypes lists all valid column types (GADS::Column::types).
var ColumnTypes = []string{"date", "daterange", "string", "intgr", "person", "tree", "enum", "file", "rag",
	"calc", "curval", "autocur", "id", "createddate", "createdby", "serial", "deletedby", "filval"}

// Column is a field of a table (GADS::Column and its sub-classes).
type Column struct {
	e      *Engine
	layout *Layout // layout that built this column (weak ref in Perl)

	ID               int64
	setID            int64
	Name             string
	NameShort        string
	Type             string
	InstanceID       int64
	Internal         bool
	Optional         bool
	Remember         bool
	IsUnique         bool
	setCanChild      *bool
	Multivalue       bool
	Position         *int64
	Helptext         string
	Description      string
	Width            int64
	TopicID          *int64
	Aggregate        string
	setGroupDisplay  string
	LookupEndpoint   string
	LookupGroup      string
	optionsFromDB    bool // Options were those of the database row (Perl has_options)
	DisplayCondition string
	LinkParentID     *int64
	RelatedFieldID   *int64
	ParentID         int64 // parent curval of a clone (Column->clone(parent_id => ...))
	Notes            string
	Ordering         string
	EndNodeOnly      bool
	Textbox          bool
	ForceRegex       string
	setFilterJSON    *string
	Options          map[string]any
	setDisplayFields []map[string]any
	hasDisplaySet    bool

	// Code fields
	Code          string
	codeLoaded    bool
	ReturnType    string
	DecimalPlaces *int64
	codeInStorage bool

	// File
	Filesize *int64

	// Curval etc
	refersToInstanceID *int64
	curvalFieldIDs     []int64
	curvalFieldsLoaded bool

	// Write state
	setPermissions map[int64][]string
	hasSetPerms    bool
	writeCache     bool
	filter         *Filter
	displayFields  *Filter

	// caches
	enumvals        []map[string]any
	enumvalsLoaded  bool
	enumvalsIndex   map[int64]map[string]any
	tree            *treeData
	dependsOn       []int64
	dependsLoaded   bool
	permissions     map[int64][]string
	layoutParent    *Layout
	people          []*User
	peopleLoaded    bool
	view            *View
	viewBuilt       bool
	subvalsRequired []*Column
	subvalsBuilt    bool
	autocursCache   []int64
	autocursLoaded  bool
	allValuesCache  []map[string]any
	hasValuesIndex  bool
	linkParent      *Column
	oldMultivalue   bool
}

// Layout returns the layout that built this column.
func (c *Column) Layout() *Layout { return c.layout }

// ---------------------------------------------------------------------
// Type metadata
// ---------------------------------------------------------------------

// IsCurcommon reports curval/autocur/filval.
func (c *Column) IsCurcommon() bool {
	return c.Type == "curval" || c.Type == "autocur" || c.Type == "filval"
}

// IsCode reports calc/rag.
func (c *Column) IsCode() bool { return c.Type == "calc" || c.Type == "rag" }

// Userinput reports whether the field is input by users.
var _ = 0

func (c *Column) userinputDefault() bool {
	switch c.Type {
	case "id", "serial", "createddate", "createdby", "deletedby", "calc", "rag", "autocur", "filval":
		return false
	}
	return true
}

// Userinput is the "userinput" attribute.
func (c *Column) UserinputFn() bool { return c.userinputDefault() }

// Userinput is exposed as a field for convenience (set on load).
// (Kept in sync by setTypeDefaults.)
var _ = 0

// CanMultivalue reports whether the type supports multiple values.
func (c *Column) CanMultivalue() bool {
	switch c.Type {
	case "string", "intgr", "date", "daterange", "enum", "tree", "person", "file", "calc", "curval", "autocur", "filval":
		return true
	}
	return false
}

// HasMultivaluePlus is used for width calculations.
func (c *Column) HasMultivaluePlus() bool {
	switch c.Type {
	case "string", "intgr", "date", "daterange":
		return true
	}
	return false
}

// Fixedvals reports whether the column has a fixed set of values.
func (c *Column) Fixedvals() bool {
	switch c.Type {
	case "enum", "tree", "person", "rag", "curval", "autocur", "filval", "createdby", "deletedby":
		return true
	}
	return false
}

// HasCache reports code fields.
func (c *Column) HasCache() bool { return c.IsCode() }

// Numeric reports numeric columns.
func (c *Column) Numeric() bool {
	switch c.Type {
	case "intgr", "id", "serial":
		return true
	case "calc":
		rt := c.ReturnTypeFn()
		return rt == "integer" || rt == "numeric"
	}
	return false
}

// Addable reports types that can have arithmetic applied.
func (c *Column) Addable() bool {
	switch c.Type {
	case "intgr", "id", "serial", "date", "createddate", "daterange":
		return true
	}
	return false
}

// UseIDInFilter for curcommon.
func (c *Column) UseIDInFilter() bool { return c.IsCurcommon() }

// HasFilterTypeahead reports whether filter values have typeahead.
func (c *Column) HasFilterTypeahead() bool {
	switch c.Type {
	case "enum", "tree", "person", "curval", "autocur", "filval", "createdby", "deletedby":
		return true
	case "calc":
		return c.ValueField() == "value_text"
	}
	return false
}

// StringStorage reports whether values are stored as strings.
func (c *Column) StringStorage() bool {
	switch c.Type {
	case "string", "file":
		return true
	case "daterange":
		return c.ReturnTypeFn() == "string"
	case "calc":
		return c.ValueField() == "value_text"
	}
	return false
}

// ReturnTypeFn returns the return type of the column.
func (c *Column) ReturnTypeFn() string {
	switch c.Type {
	case "intgr", "id", "serial":
		return "integer"
	case "date", "createddate":
		return "date"
	case "daterange":
		return "daterange"
	case "calc":
		c.loadCode()
		if c.ReturnType == "" {
			return "string"
		}
		return c.ReturnType
	}
	return "string"
}

// HasTime reports whether date values include a time.
func (c *Column) HasTime() bool {
	switch c.Type {
	case "createddate":
		return true
	case "calc":
		rt := c.ReturnTypeFn()
		return rt == "daterange" || rt == "datetime"
	}
	return false
}

// DateFormat returns the CLDR date format for the column.
func (c *Column) DateFormat() string {
	f := c.e.Config.DateFormat
	if f == "" {
		f = "yyyy-MM-dd"
	}
	if c.HasTime() {
		f += " HH:mm:ss"
	}
	return f
}

// Table returns the value table name.
func (c *Column) Table() string {
	switch c.Type {
	case "string":
		return "string"
	case "intgr":
		return "intgr"
	case "date":
		return "date"
	case "daterange":
		return "daterange"
	case "enum", "tree":
		return "enum"
	case "person":
		return "person"
	case "file":
		return "file"
	case "curval", "filval", "autocur":
		return "curval"
	case "calc":
		return "calcval"
	case "rag":
		return "ragval"
	case "id", "serial", "deletedby":
		return "current"
	case "createddate":
		return "record"
	case "createdby":
		return "createdby"
	}
	return c.Type
}

// ValueField returns the column in the value table holding the value.
func (c *Column) ValueField() string {
	switch c.Type {
	case "id":
		return "id"
	case "serial":
		return "serial"
	case "createddate":
		return "created"
	case "deletedby":
		return "deletedby"
	case "file":
		return "name"
	case "autocur":
		return "id"
	case "calc":
		return calcFormatToField(c.ReturnTypeFn())
	}
	return "value"
}

func calcFormatToField(rt string) string {
	switch rt {
	case "datetime":
		return "value_datetime"
	case "date":
		return "value_date"
	case "daterange":
		return "value_text"
	case "integer":
		return "value_int"
	case "numeric":
		return "value_numeric"
	}
	return "value_text"
}

// ValueFieldAsIndex is the field used when searching by index (e.g. enum id).
func (c *Column) ValueFieldAsIndex(value any) string {
	switch c.Type {
	case "enum", "tree":
		return "id"
	case "person", "createdby", "deletedby":
		vals := toAnySlice(value)
		typ := ""
		for _, v := range vals {
			if typ != "" && typ != "id" {
				break
			}
			s := anyToString(v)
			if s == "" || reDigits.MatchString(s) {
				typ = "id"
			} else {
				typ = c.ValueField()
			}
		}
		return typ
	case "file":
		s := anyToString(value)
		if s == "" || reDigits.MatchString(s) {
			return "id"
		}
		return c.ValueField()
	}
	return c.ValueField()
}

var reDigits = regexp.MustCompile(`^[0-9]+$`)

// SortField returns the field used for sorting.
func (c *Column) SortField() string {
	switch c.Type {
	case "daterange":
		return "from"
	case "calc":
		if c.ReturnTypeFn() == "daterange" {
			return "value_date_from"
		}
	}
	return c.ValueField()
}

// FromField / ToField for daterange-like columns.
func (c *Column) FromField() string {
	if c.Type == "calc" {
		return "value_date_from"
	}
	return "from"
}

// ToField returns the "to" field.
func (c *Column) ToField() string {
	if c.Type == "calc" {
		return "value_date_to"
	}
	return "to"
}

// RetrieveFields returns the fields retrieved for the value.
func (c *Column) RetrieveFields() []string {
	switch c.Type {
	case "daterange":
		return []string{"from", "to"}
	case "enum":
		return []string{"id", "value", "deleted"}
	case "tree":
		return []string{"id", "value"}
	case "person":
		return userColumns
	case "createdby":
		if c.NameShort == "_created_user" {
			return nil
		}
		return userColumns
	case "deletedby":
		return userColumns
	case "file":
		return []string{"name", "mimetype", "id"}
	}
	return []string{c.ValueField()}
}

// Sprefix is the join prefix used in searches.
func (c *Column) Sprefix() string {
	switch c.Type {
	case "id", "serial", "deletedby", "autocur":
		return "current"
	case "createddate":
		return "record"
	case "createdby":
		return "createdby"
	case "enum", "tree", "person", "file":
		return "value"
	}
	return c.Field()
}

// Field returns the name of the join for the column ("field<id>").
func (c *Column) Field() string { return "field" + itoa(c.ID) }

// FetchWithRecord reports whether values are fetched with the main record.
func (c *Column) FetchWithRecord() bool {
	if c.IsCurcommon() {
		if c.Multivalue || c.Type == "autocur" || c.Type == "filval" {
			return false
		}
		lp := c.LayoutParent()
		if lp == nil || lp.User() == nil {
			return true
		}
		if _, ok := c.e.DB.QueryInt64(`SELECT "me"."id" FROM "view_limit" "me" JOIN "view" "view" ON "view"."id" = "me"."view_id" WHERE "me"."user_id" = ? AND "view"."instance_id" = ? LIMIT 1`, lp.User().ID, lp.InstanceID); ok {
			return false
		}
		return true
	}
	return !c.MultivalueFn()
}

// MultivalueFn returns the effective multivalue setting.
func (c *Column) MultivalueFn() bool {
	if c.Type == "autocur" || c.Type == "filval" {
		return true
	}
	return c.Multivalue
}

// CanChild reports whether the column can have values unique to child
// records.
func (c *Column) CanChild() bool {
	if !c.Userinput() {
		// Code fields that depend on child columns are child fields too
		if _, ok := c.e.DB.QueryInt64(`SELECT "me"."layout_id" FROM "layout_depend" "me" JOIN "layout" "depend_on" ON "depend_on"."id" = "me"."depends_on" WHERE "me"."layout_id" = ? AND "depend_on"."can_child" = 1 LIMIT 1`, c.ID); ok {
			return true
		}
	}
	return c.setCanChild != nil && *c.setCanChild
}

// SetCanChild sets the can_child attribute.
func (c *Column) SetCanChild(v bool) { c.setCanChild = &v }

// Userinput reports whether values are input by users.
func (c *Column) Userinput() bool { return c.userinputDefault() }

// NoValueToWrite reports autocur columns.
func (c *Column) NoValueToWrite() bool { return c.Type == "autocur" }

// Hidden reports hidden columns.
func (c *Column) Hidden() bool { return c.Type == "deletedby" }

// SetGroupDisplay sets the group display setting.
func (c *Column) SetGroupDisplay(s string) { c.setGroupDisplay = s }

// GroupDisplay returns the group display setting.
func (c *Column) GroupDisplay() string {
	if c.Numeric() {
		return "sum"
	}
	return c.setGroupDisplay
}

// ---------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------

func (c *Column) opt(name string) any {
	if c.Options == nil {
		return nil
	}
	return c.Options[name]
}

func (c *Column) optBool(name string, def bool) bool {
	if c.Options == nil {
		return def
	}
	v, ok := c.Options[name]
	if !ok {
		return false
	}
	return truthy(v)
}

func (c *Column) setOpt(name string, v any) {
	if c.Options == nil {
		c.Options = map[string]any{}
	}
	c.Options[name] = v
}

// OptionNames returns the option names for the column type.
func (c *Column) OptionNames() []string {
	switch c.Type {
	case "intgr":
		return []string{"show_calculator"}
	case "date":
		return []string{"show_datepicker", "default_today"}
	case "daterange":
		return []string{"show_datepicker"}
	case "person":
		return []string{"default_to_login", "notify_on_selection", "notify_on_selection_message", "notify_on_selection_subject"}
	case "calc":
		return []string{"show_in_edit"}
	case "string":
		return []string{"max_length"}
	case "file":
		return []string{"override_types"}
	case "curval":
		return []string{"override_permissions", "value_selector", "show_add", "delete_not_used", "limit_rows", "show_view_all"}
	case "autocur", "filval":
		return []string{"override_permissions", "limit_rows"}
	}
	return nil
}

// UserOptions are the options that users can configure (user_options:
// those of option_names with user_configurable)
func (c *Column) UserOptions() []string {
	var out []string
	for _, n := range c.OptionNames() {
		if n != "override_types" {
			out = append(out, n)
		}
	}
	return out
}

var reMaxLength = regexp.MustCompile(`^\d+$`)

// coerceMaxLength is the coercion of String max_length: a whole number, or
// undef
func coerceMaxLength(v any) any {
	if v == nil {
		return nil
	}
	s := anyToString(v)
	if s != "" && reMaxLength.MatchString(s) {
		return v
	}
	return nil
}

// MaxLength is the maximum length of the values of a string column (nil if
// none)
func (c *Column) MaxLength() *int64 {
	if c.Type != "string" {
		return nil
	}
	v := coerceMaxLength(c.opt("max_length"))
	if v == nil {
		return nil
	}
	n, _ := anyToInt64(v)
	return &n
}

// OverrideTypes are the extra file types allowed for a file column
func (c *Column) OverrideTypes() []FileType {
	out := []FileType{}
	list, _ := c.opt("override_types").([]any)
	for _, t := range list {
		if m, ok := t.(map[string]any); ok {
			out = append(out, FileType{Name: anyToString(m["name"]), Extension: anyToString(m["extension"])})
		}
	}
	return out
}

// ShowViewAll for curval
func (c *Column) ShowViewAll() bool {
	if c.Type != "curval" {
		return false
	}
	return c.optBool("show_view_all", false)
}

// optionsForWrite builds the options hash written to the database.
func (c *Column) optionsForWrite() map[string]any {
	out := map[string]any{}
	for _, n := range c.OptionNames() {
		switch n {
		case "max_length":
			out[n] = coerceMaxLength(c.opt(n))
		case "override_types":
			if v, ok := c.opt(n).([]any); ok {
				out[n] = v
			} else {
				out[n] = []any{}
			}
		case "show_calculator", "default_today", "default_to_login", "notify_on_selection", "override_permissions", "show_add", "delete_not_used", "show_view_all":
			out[n] = boolInt(c.optBool(n, false))
		case "show_datepicker":
			if c.Type == "date" {
				out[n] = boolInt(c.ShowDatepicker())
			} else {
				out[n] = boolInt(c.optBool(n, false))
			}
		case "show_in_edit":
			v := c.opt(n)
			if sv, ok := v.(string); ok {
				// Int without coercion: a value of a form is kept as its
				// string
				out[n] = sv
			} else if !truthy(v) {
				out[n] = int64(0)
			} else {
				i, _ := anyToInt64(v)
				out[n] = i
			}
		case "value_selector":
			out[n] = c.ValueSelector()
		case "limit_rows":
			if v := c.LimitRows(); v > 0 {
				out[n] = v
			} else {
				out[n] = nil
			}
		case "notify_on_selection_message", "notify_on_selection_subject":
			v := c.opt(n)
			if v == nil {
				out[n] = ""
			} else {
				out[n] = v
			}
		}
	}
	return out
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// ShowDatepicker for date columns (defaults to true when no options).
func (c *Column) ShowDatepicker() bool {
	v, ok := c.Options["show_datepicker"]
	if !ok && !c.optionsFromDB {
		// Perl's builder: a date column without options shows the picker
		return c.Type == "date"
	}
	return truthy(v)
}

// DefaultToday for date columns.
func (c *Column) DefaultToday() bool { return c.optBool("default_today", false) }

// ShowInEdit for calc columns.
func (c *Column) ShowInEdit() int64 {
	if c.Type != "calc" {
		// Column->show_in_edit is userinput
		if c.UserinputFn() {
			return 1
		}
		return 0
	}
	i, _ := anyToInt64(c.opt("show_in_edit"))
	return i
}

// OverridePermissions for curcommon.
func (c *Column) OverridePermissions() bool { return c.optBool("override_permissions", false) }

// LimitRows for curcommon.
func (c *Column) LimitRows() int64 {
	v := c.opt("limit_rows")
	if !truthy(v) {
		return 0
	}
	i, _ := anyToInt64(v)
	return i
}

// ValueSelector for curval (dropdown/typeahead/noshow).
func (c *Column) ValueSelector() string {
	if c.Type != "curval" {
		return ""
	}
	if c.Options == nil {
		return "dropdown"
	}
	v, ok := c.Options["value_selector"]
	if !ok {
		return "dropdown"
	}
	s := anyToString(v)
	if s == "" {
		return "dropdown"
	}
	return s
}

// ShowAdd for curval.
func (c *Column) ShowAdd() bool {
	if c.Type != "curval" {
		return false
	}
	return c.optBool("show_add", false)
}

// DeleteNotUsed for curval.
func (c *Column) DeleteNotUsed() bool {
	if c.Type != "curval" {
		return false
	}
	return c.optBool("delete_not_used", false)
}

// NotifyOnSelection for person.
func (c *Column) NotifyOnSelection() bool { return c.optBool("notify_on_selection", false) }

// DefaultToLogin for person.
func (c *Column) DefaultToLogin() bool { return c.optBool("default_to_login", false) }

// ---------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------

func (e *Engine) columnFromRow(layout *Layout, row db.Row) *Column {
	c := &Column{e: e, layout: layout, writeCache: true}
	c.ID, _ = db.AsInt64(row["id"])
	c.Name, _ = db.AsString(row["name"])
	c.NameShort, _ = db.AsString(row["name_short"])
	c.Type, _ = db.AsString(row["type"])
	c.InstanceID, _ = db.AsInt64(row["instance_id"])
	c.Internal = rowBool(row["internal"])
	c.Optional = rowBool(row["optional"])
	c.Remember = rowBool(row["remember"])
	c.IsUnique = rowBool(row["isunique"])
	cc := rowBool(row["can_child"])
	c.setCanChild = &cc
	if c.CanMultivalue() {
		c.Multivalue = rowBool(row["multivalue"])
	}
	c.Position = optInt(row["position"])
	c.Helptext, _ = db.AsString(row["helptext"])
	c.Description, _ = db.AsString(row["description"])
	c.Width, _ = db.AsInt64(row["width"])
	c.TopicID = optInt(row["topic_id"])
	c.Aggregate, _ = db.AsString(row["aggregate"])
	c.setGroupDisplay, _ = db.AsString(row["group_display"])
	c.LookupEndpoint, _ = db.AsString(row["lookup_endpoint"])
	c.LookupGroup, _ = db.AsString(row["lookup_group"])
	dc, _ := db.AsString(row["display_condition"])
	c.DisplayCondition = strings.TrimRight(dc, " \t")
	c.LinkParentID = optInt(row["link_parent"])
	c.RelatedFieldID = optInt(row["related_field"])
	c.Notes, _ = db.AsString(row["notes"])
	c.Ordering, _ = db.AsString(row["ordering"])
	c.EndNodeOnly = rowBool(row["end_node_only"])
	c.Textbox = rowBool(row["textbox"])
	c.ForceRegex, _ = db.AsString(row["force_regex"])
	if f, ok := db.AsString(row["filter"]); ok {
		c.setFilterJSON = &f
	}
	c.optionsFromDB = true
	if opts, ok := db.AsString(row["options"]); ok && opts != "" {
		if v, err := DecodeJSON(opts); err == nil {
			if m, ok := v.(map[string]any); ok {
				c.Options = m
			}
		}
	} else {
		c.Options = map[string]any{}
	}
	if c.Type == "curval" && c.ShowAdd() && c.ValueSelector() == "noshow" {
		c.Multivalue = true
	}
	return c
}

func rowBool(v any) bool {
	i, ok := db.AsInt64(v)
	return ok && i != 0
}

func (c *Column) loadCode() {
	if c.codeLoaded || c.ID == 0 {
		return
	}
	c.codeLoaded = true
	switch c.Type {
	case "calc":
		row := c.e.DB.QueryRow(`SELECT * FROM "calc" WHERE "layout_id" = ? ORDER BY "id" LIMIT 1`, c.ID)
		if row != nil {
			c.Code, _ = db.AsString(row["code"])
			c.ReturnType, _ = db.AsString(row["return_format"])
			c.DecimalPlaces = optInt(row["decimal_places"])
			c.codeInStorage = true
		}
	case "rag":
		row := c.e.DB.QueryRow(`SELECT * FROM "rag" WHERE "layout_id" = ? ORDER BY "id" LIMIT 1`, c.ID)
		if row != nil {
			c.Code, _ = db.AsString(row["code"])
			c.codeInStorage = true
		}
	}
}

// SetCode sets the code of a code column, overriding any stored value.
func (c *Column) SetCode(code string) {
	c.loadCode()
	c.Code = code
}

var reCalcReturnType = regexp.MustCompile(`^(string|date|datetime|integer|numeric|globe|error|daterange)$`)

// SetReturnType sets the return type of a calc column (an invalid type is
// an error of the isa of return_type).
func (c *Column) SetReturnType(rt string) {
	c.loadCode()
	if rt != "" && rt != "0" && !reCalcReturnType.MatchString(rt) {
		Fail("Bad return type %s", rt)
	}
	c.ReturnType = rt
}

// SetDecimalPlaces sets the decimal places of a calc column.
func (c *Column) SetDecimalPlaces(d *int64) {
	c.loadCode()
	c.DecimalPlaces = d
}

// CodeFn returns the code for a code column.
func (c *Column) CodeFn() string {
	c.loadCode()
	return c.Code
}

// ---------------------------------------------------------------------
// Relationships
// ---------------------------------------------------------------------

// LinkParent returns the linked parent column.
func (c *Column) LinkParent() *Column {
	if c.LinkParentID == nil || *c.LinkParentID == 0 {
		return nil
	}
	if c.linkParent != nil {
		return c.linkParent
	}
	if c.layout != nil {
		c.linkParent = c.layout.Column(*c.LinkParentID)
	}
	return c.linkParent
}

// RelatedField returns the related field (autocur/filval).
func (c *Column) RelatedField() *Column {
	if c.RelatedFieldID == nil {
		return nil
	}
	return c.layout.Column(*c.RelatedFieldID)
}

// DependsOn returns the columns a code field depends on.
func (c *Column) DependsOn() []int64 {
	if c.Userinput() {
		return nil
	}
	if !c.dependsLoaded {
		c.dependsOn = c.e.DB.QueryInt64s(`SELECT "depends_on" FROM "layout_depend" WHERE "layout_id" = ? ORDER BY "id"`, c.ID)
		c.dependsLoaded = true
	}
	return c.dependsOn
}

// ClearDependsOn clears the cache.
func (c *Column) ClearDependsOn() { c.dependsLoaded = false; c.dependsOn = nil }

// DependedBy returns the columns depending on this one.
func (c *Column) DependedBy() []int64 {
	return c.e.DB.QueryInt64s(`SELECT "layout_id" FROM "layout_depend" WHERE "depends_on" = ? ORDER BY "id"`, c.ID)
}

// Permissions returns all group permissions for the column.
func (c *Column) Permissions() map[int64][]string {
	if c.permissions != nil {
		return c.permissions
	}
	c.permissions = map[int64][]string{}
	for _, r := range c.e.DB.Query(`SELECT "group_id", "permission" FROM "layout_group" WHERE "layout_id" = ? ORDER BY "id"`, c.ID) {
		g, _ := db.AsInt64(r["group_id"])
		p, _ := db.AsString(r["permission"])
		c.permissions[g] = append(c.permissions[g], p)
	}
	return c.permissions
}

// UserCan reports whether the layout user has a permission on the column.
func (c *Column) UserCan(permission string) bool {
	if c.e.IgnorePermissions {
		return true
	}
	if c.Internal && permission == "read" {
		return true
	}
	if !c.Userinput() && permission != "read" {
		return false
	}
	if c.layout.CurrentUserCanColumn(c.ID, permission) {
		return true
	}
	if permission == "write" {
		if c.layout.CurrentUserCanColumn(c.ID, "write_new") || c.layout.CurrentUserCanColumn(c.ID, "write_existing") {
			return true
		}
	}
	return false
}

// UserIDCan reports whether a user has a permission on the column.
func (c *Column) UserIDCan(userID int64, permission string) bool {
	return c.layout.UserCanColumn(userID, c.ID, permission)
}

// ---------------------------------------------------------------------
// Filters, display fields
// ---------------------------------------------------------------------

// FilterFn returns the column's filter (curval and person columns).
func (c *Column) FilterFn() *Filter {
	if c.filter != nil {
		return c.filter
	}
	js := ""
	if c.setFilterJSON != nil {
		js = *c.setFilterJSON
	}
	switch c.Type {
	case "curval":
		c.filter = NewFilterJSON(js, c.LayoutParent())
	case "person":
		c.filter = NewFilterJSON(js, c.layout)
		c.filter.IsPeopleFilter = true
	default:
		c.filter = NewFilter(nil)
	}
	return c.filter
}

// SetFilter sets the column filter. As Perl keeps the filter object set
// when the column's caches are cleared, it is also kept as the column's
// filter JSON, from which the filter is rebuilt.
func (c *Column) SetFilter(f *Filter) {
	c.filter = f
	if f != nil {
		js := f.AsJSON()
		c.setFilterJSON = &js
	}
}

// ClearFilter clears the filter cache.
func (c *Column) ClearFilter() { c.filter = nil }

// DisplayFields returns the display condition filter.
func (c *Column) DisplayFields() *Filter {
	if c.displayFields != nil {
		return c.displayFields
	}
	var rules []any
	if c.hasDisplaySet {
		for _, df := range c.setDisplayFields {
			rules = append(rules, map[string]any{
				"id":       df["display_field_id"],
				"operator": df["operator"],
				"value":    df["regex"],
			})
		}
	} else if c.ID != 0 {
		for _, r := range c.e.DB.Query(`SELECT "display_field_id", "operator", "regex" FROM "display_field" WHERE "layout_id" = ? ORDER BY "id"`, c.ID) {
			id, _ := db.AsInt64(r["display_field_id"])
			rules = append(rules, map[string]any{
				"id":       id,
				"operator": nullStr(r["operator"]),
				"value":    nullStr(r["regex"]),
			})
		}
	}
	h := map[string]any{}
	if len(rules) > 0 {
		cond := c.DisplayCondition
		if cond == "" {
			cond = "AND"
		}
		h = map[string]any{"condition": cond, "rules": rules}
	}
	c.displayFields = NewFilterHash(h, c.layout)
	return c.displayFields
}

// SetDisplayFields sets the display fields filter.
func (c *Column) SetDisplayFields(f *Filter) { c.displayFields = f }

func nullStr(v any) any {
	if v == nil {
		return nil
	}
	s, _ := db.AsString(v)
	return s
}

// HasDisplayField reports whether display conditions exist.
func (c *Column) HasDisplayField() bool { return len(c.DisplayFields().Filters()) > 0 }

// DisplayFieldColIDs returns the columns in display conditions.
func (c *Column) DisplayFieldColIDs() []int64 {
	var out []int64
	for _, f := range c.DisplayFields().Filters() {
		i, _ := anyToInt64(f["column_id"])
		out = append(out, i)
	}
	return out
}

// ---------------------------------------------------------------------
// Dates
// ---------------------------------------------------------------------

// ParseDate parses a user date value (CURDATE or CLDR format).
func (c *Column) ParseDate(value string) (time.Time, bool) {
	if dt, ok := ParseDateFilter(c.e.Now(), value); ok {
		return dt, true
	}
	if value == "" {
		return time.Time{}, false
	}
	return c.e.ParseDateTime(value)
}

// ParseDateTime is GADS::DateTime::parse_datetime.
func (e *Engine) ParseDateTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	format := e.Config.DateFormat
	if format == "" {
		format = "yyyy-MM-dd"
	}
	if strings.Contains(value, " ") {
		format += " HH:mm:ss"
	}
	return cldr.Parse(value, format)
}

// FormatDate formats a date in Europe/London with the CLDR pattern.
func FormatDate(t time.Time, format string) string {
	return cldr.Format(t.In(London()), format)
}

var londonLoc *time.Location

// London returns the Europe/London time zone.
func London() *time.Location {
	if londonLoc == nil {
		l, err := time.LoadLocation("Europe/London")
		if err != nil {
			Panic("Unable to load Europe/London timezone: %s", err)
		}
		londonLoc = l
	}
	return londonLoc
}

// ---------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------

// Validate validates a single user value. If fatal, an error is raised.
func (c *Column) Validate(value any, fatal bool) bool {
	switch c.Type {
	case "intgr", "id":
		for _, v := range toAnySlice(value) {
			s := anyToString(v)
			if truthyStr(s) && !reInteger.MatchString(s) {
				if !fatal {
					return false
				}
				Fail("'%s' is not a valid integer for '%s'", s, c.Name)
			}
		}
		return true
	case "date", "createddate":
		s := anyToString(value)
		if !truthyStr(s) {
			return true
		}
		return c.ValidateDate(s, fatal)
	case "daterange":
		return c.validateDaterange(value, fatal)
	case "enum":
		s := anyToString(value)
		if !truthyStr(s) {
			return true
		}
		id, _ := anyToInt64(s)
		if c.Enumval(id) == nil {
			if !fatal {
				return false
			}
			Fail("'%s' is not a valid enum ID for '%s'", s, c.Name)
		}
		return true
	case "tree":
		s := anyToString(value)
		if !truthyStr(s) {
			return true
		}
		id, _ := anyToInt64(s)
		n := c.Node(id)
		if n == nil {
			if !fatal {
				return false
			}
			Fail("'%s' is not a valid tree node ID for '%s'", s, c.Name)
		}
		if n.deleted {
			if !fatal {
				return false
			}
			Fail("Node '%s' has been deleted and can therefore not be used", s)
		}
		return true
	case "file":
		s := anyToString(value)
		if !truthyStr(s) {
			return true
		}
		ok := reDigits.MatchString(s)
		if ok {
			_, ok = c.e.DB.QueryInt64(`SELECT "id" FROM "fileval" WHERE "id" = ?`, s)
		}
		if !ok {
			if !fatal {
				return false
			}
			Fail("'%s' is not a valid file ID for '%s'", s, c.Name)
		}
		// Check that the file type is allowed here as well
		row := c.e.DB.QueryRow(`SELECT "id", "name" FROM "fileval" WHERE "id" = ?`, s)
		id, _ := db.AsInt64(row["id"])
		name, _ := db.AsString(row["name"])
		c.e.CheckFileval(id, name, CheckOptions{ExtraTypes: c.OverrideTypes(), CheckName: false})
		return true
	case "curval":
		s := anyToString(value)
		if !truthyStr(s) {
			return true
		}
		if !reDigits.MatchString(s) {
			if !fatal {
				return false
			}
			Fail("Value for %s must be an integer", c.Name)
		}
		rid := c.RefersToInstanceID()
		var ok bool
		if rid != nil {
			_, ok = c.e.DB.QueryInt64(`SELECT "id" FROM "current" "me" WHERE "instance_id" = ? AND "id" = ?`, *rid, s)
		}
		if !ok {
			if !fatal {
				return false
			}
			Fail("%s is not a valid record ID for %s", s, c.Name)
		}
		return true
	case "calc":
		s := anyToString(value)
		switch c.ReturnTypeFn() {
		case "date", "datetime":
			_, ok := c.ParseDate(s)
			return ok
		case "daterange":
			return c.validateDaterange(value, false)
		case "integer":
			if s == "[CURUSER.ID]" {
				return true
			}
			return reInteger.MatchString(s)
		case "numeric":
			return perl.LooksLikeNumber(s)
		}
		return true
	}
	return true
}

var reInteger = regexp.MustCompile(`^-?[0-9]+$`)

func truthyStr(s string) bool { return s != "" && s != "0" }

// ValidateSearch validates a value used in a search.
func (c *Column) ValidateSearch(value any, fatal bool, opts map[string]bool) bool {
	switch c.Type {
	case "date", "createddate":
		if c.validateSearchDate(anyToString(value), fatal) {
			return true
		}
		return c.Validate(value, fatal)
	case "daterange":
		return c.validateDaterangeSearch(anyToString(value), fatal, opts)
	case "enum", "tree", "file":
		return true
	case "serial":
		s := anyToString(value)
		if !reDigits.MatchString(s) {
			Fail("Invalid serial value: %s", s)
		}
		return true
	case "curval", "autocur", "filval":
		s := anyToString(value)
		if !truthyStr(s) {
			if !fatal {
				return false
			}
			Fail("Search value cannot be blank for %s.", c.Name)
		}
		if !reDigits.MatchString(s) {
			if !fatal {
				return false
			}
			Fail("Search value must be an ID number for %s.", c.Name)
		}
		return true
	case "calc":
		if c.ReturnTypeFn() == "daterange" {
			return c.validateDaterangeSearch(anyToString(value), false, nil)
		}
		return c.Validate(value, false)
	}
	return c.Validate(value, fatal)
}

// ValidateDate validates a date string.
func (c *Column) ValidateDate(value string, fatal bool) bool {
	if _, ok := c.ParseDate(value); !ok {
		if !fatal {
			return false
		}
		Fail("Invalid date '%s' for %s. Please enter as %s.", value, c.Name, c.DateFormat())
	}
	return true
}

func (c *Column) validateSearchDate(value string, fatal bool) bool {
	if !truthyStr(value) {
		if !fatal {
			return false
		}
		Fail("Date cannot be blank for %s.", c.Name)
	}
	if _, ok := ParseDateFilter(c.e.Now(), value); ok {
		return true
	}
	return c.ValidateDate(value, fatal)
}

func (c *Column) validateDaterange(value any, fatal bool) bool {
	var from, to string
	switch v := value.(type) {
	case nil:
		return true
	case map[string]any:
		from, to = anyToString(v["from"]), anyToString(v["to"])
	case [2]string:
		from, to = v[0], v[1]
	case []any:
		if len(v) > 0 {
			from = anyToString(v[0])
		}
		if len(v) > 1 {
			to = anyToString(v[1])
		}
	case string:
		if !truthyStr(v) {
			return true
		}
		// A plain string is a hashref in Perl only via split; treat as
		// invalid structure
		return false
	default:
		return true
	}
	if !truthyStr(from) && !truthyStr(to) {
		return true
	}
	if truthyStr(from) != truthyStr(to) {
		if !fatal {
			return false
		}
		Fail("Please enter 2 date values for '%s'", c.Name)
	}
	f, ok := c.ParseDate(from)
	if truthyStr(from) && !ok {
		if !fatal {
			return false
		}
		Fail("Invalid start date %s for %s. Please enter as %s.", from, c.Name, c.DateFormat())
	}
	t, ok := c.ParseDate(to)
	if truthyStr(to) && !ok {
		if !fatal {
			return false
		}
		Fail("Invalid end date %s for %s. Please enter as %s.", to, c.Name, c.DateFormat())
	}
	if f.After(t) {
		if !fatal {
			return false
		}
		Fail("Start date must be before the end date for '%s'", c.Name)
	}
	return true
}

var reDaterangeSplit = regexp.MustCompile(`^(.+) to (.+)$`)

// SplitDaterange splits "X to Y" (GADS::DateTime::split).
func (c *Column) SplitDaterange(value string) (string, string, bool) {
	m := reDaterangeSplit.FindStringSubmatch(value)
	if m == nil {
		return "", "", false
	}
	_, ok1 := c.ParseDate(m[1])
	_, ok2 := c.ParseDate(m[2])
	if !ok1 || !ok2 {
		return "", "", false
	}
	return m[1], m[2], true
}

func (c *Column) validateDaterangeSearch(value string, fatal bool, opts map[string]bool) bool {
	if !truthyStr(value) {
		return true
	}
	if opts["single_only"] {
		if _, ok := c.ParseDate(value); ok {
			return true
		}
		if !fatal {
			return false
		}
		Fail("Invalid single date format %s for %s", value, c.Name)
	}
	if opts["full_only"] {
		if f, t, ok := c.SplitDaterange(value); ok {
			return c.validateDaterange(map[string]any{"from": f, "to": t}, fatal)
		}
		if !fatal {
			return false
		}
		Fail("Invalid full date format %s for %s. Please enter as %s.", value, c.Name, c.e.Config.DateFormat)
	}
	if _, ok := c.ParseDate(value); ok {
		return true
	}
	if f, t, ok := c.SplitDaterange(value); ok && c.validateDaterange(map[string]any{"from": f, "to": t}, false) {
		return true
	}
	if !fatal {
		return false
	}
	Fail("Invalid format {value} for {name}")
	return false
}

// ---------------------------------------------------------------------
// Miscellaneous
// ---------------------------------------------------------------------

// FilterValueToText converts a filter value to display text.
func (c *Column) FilterValueToText(value string) string {
	if c.IsCurcommon() {
		var out string
		Try(func() {
			if !reDigits.MatchString(value) {
				return
			}
			id, _ := anyToInt64(value)
			rows := c.IDsToValues([]int64{id}, idsToValuesOptions{})
			if len(rows) > 0 {
				out = rows[0].Value
			}
		})
		return out
	}
	return value
}

// SortColumns returns the columns used to sort by this column.
func (c *Column) SortColumns() []*Column {
	if c.IsCurcommon() {
		var out []*Column
		for _, cc := range c.CurvalFields() {
			if cc == nil {
				continue
			}
			out = append(out, cc.SortColumns()...)
		}
		return out
	}
	return []*Column{c}
}

// SortParent returns the parent column for sort columns.
func (c *Column) SortParent() *Column {
	if c.IsCurcommon() {
		return c
	}
	return nil
}

// Tjoin describes the join needed to retrieve the column values. It
// returns nil for columns with no join (internal columns).
func (c *Column) Tjoin(opts tjoinOptions) *joinSpec {
	switch c.Type {
	case "id", "serial", "createddate":
		return nil
	case "createdby":
		if c.NameShort == "_created_user" {
			return nil
		}
		return joinLeaf("createdby")
	case "deletedby":
		return joinLeaf("createdby")
	case "enum", "tree", "person", "file":
		return joinHash(c.Field(), joinLeaf("value"))
	case "curval", "filval", "autocur":
		var kids []*joinSpec
		for _, cc := range c.CurvalFieldsRetrieve(curvalRetrieveOptions{}) {
			if cc.Internal {
				continue
			}
			if j := cc.Tjoin(tjoinOptions{}); j != nil {
				kids = append(kids, j)
			}
		}
		return c.makeJoinFrom(kids, opts.currentVersion)
	}
	return joinLeaf(c.Field())
}

// Clone returns a copy of the column with a parent (curval) column.
func (c *Column) Clone(parentID int64) *Column {
	cc := *c
	cc.ParentID = parentID
	return &cc
}

// FullID is the column ID prefixed with that of any parent.
func (c *Column) FullID() string {
	if c.ParentID == 0 {
		return itoa(c.ID)
	}
	return itoa(c.ParentID) + "_" + itoa(c.ID)
}

// CodeRegex returns the regex matching the column in old-style code.
func (c *Column) CodeRegex() *regexp.Regexp {
	return regexp.MustCompile(`(?i)\[\^?` + regexp.QuoteMeta(c.Name) + c.suffix() + `\]`)
}

func (c *Column) suffix() string {
	rt := c.ReturnTypeFn()
	if rt == "date" || rt == "daterange" {
		return `(\.from|\.to|\.value)?(\.year|\.month|\.day)?`
	}
	if c.Type == "tree" {
		return `(\.level[0-9]+)?`
	}
	return ""
}

// DependsOnB64 returns the depends_on list encoded for the front end.
func (c *Column) DependsOnB64() string {
	d := c.DependsOn()
	if len(d) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(EncodeJSON(d)))
}

// Widthcols returns the number of grid columns for the edit form.
func (c *Column) Widthcols() int {
	multiplus := c.Multivalue && c.HasMultivaluePlus()
	if c.Width == 50 {
		if multiplus {
			return 4
		}
		return 6
	}
	if multiplus {
		return 10
	}
	return 12
}

func toAnySlice(v any) []any {
	switch x := v.(type) {
	case nil:
		return []any{nil}
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case []int64:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	}
	return []any{v}
}

func itoa(i int64) string {
	return anyToString(i)
}

func sortInt64s(s []int64) {
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
}

// ShowCalculator is Intgr->show_calculator
func (c *Column) ShowCalculator() bool { return c.optBool("show_calculator", false) }
