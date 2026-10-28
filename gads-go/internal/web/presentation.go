package web

import (
	"encoding/base64"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// presentOpts are the options of Column->presentation
type presentOpts struct {
	sort      *gads.SortSpec
	new       bool
	edit      bool
	modal     int64
	record    *gads.Record
	datum     gads.Datum // the datum of datum_presentation
	datumPres *ttLazy    // datum_presentation
}

// ttPresentation is the hash returned by GADS::Role::Presentation::Column
// presentation. Its values are computed when a template first reads them;
// those not yet supported panic so that a template needing them is noticed.
type ttPresentation struct {
	c     *Ctx
	col   *gads.Column
	o     presentOpts
	cache map[string]any
	set   *tt.Hash // values assigned by templates
}

func (c *Ctx) columnPresentation(col *gads.Column, o presentOpts) *ttPresentation {
	return &ttPresentation{c: c, col: col, o: o, cache: map[string]any{}, set: tt.NewHash()}
}

// TTHash implements tt.HashLike
func (p *ttPresentation) TTHash() *tt.Hash { return p.set }

func (p *ttPresentation) TTMethod(name string, args []any) ([]any, bool, error) {
	if v, ok := p.set.Get(name); ok {
		return []any{v}, true, nil
	}
	if v, ok := p.cache[name]; ok {
		return []any{v}, true, nil
	}
	v, ok := p.value(name)
	if !ok {
		return nil, false, nil
	}
	p.cache[name] = v
	return []any{v}, true, nil
}

// sortHash is the sort of the presentation
func (p *ttPresentation) sortHash() any {
	s := p.o.sort
	if s == nil {
		return nil
	}
	id := fmtInt(p.col.ID)
	if s.ID == p.col.ID {
		if s.Type == "asc" {
			return tt.HashOf("symbol", "&udarr;", "type", s.Type, "text", "descending",
				"current", "&darr;", "link", id+"desc", "aria", "ascending")
		}
		return tt.HashOf("symbol", "&udarr;", "type", s.Type, "text", "ascending",
			"current", "&uarr;", "link", id+"asc", "aria", "descending")
	}
	return tt.HashOf("symbol", "&darr;", "type", "none", "text", "ascending", "link", id+"asc")
}

func (p *ttPresentation) value(name string) (any, bool) {
	col := p.col
	switch name {
	case "id":
		return col.ID, true
	case "type":
		return col.Type, true
	case "name":
		return col.Name, true
	case "name_short":
		return emptyNil(col.NameShort), true
	case "full_id":
		return col.FullID(), true
	case "show_view_all":
		// has_options && options->{show_view_all}
		if col.Type != "curval" {
			return "", true
		}
		return ttBool01(col.ShowViewAll()), true
	case "description":
		return emptyNil(col.Description), true
	case "is_id":
		return ttBoolEmpty(col.NameShort == "_id"), true
	case "multivalue":
		return ttBool01(col.MultivalueFn()), true
	case "has_multivalue_plus":
		return ttBool01(col.HasMultivaluePlus()), true
	case "helptext":
		return emptyNil(col.Helptext), true
	case "readonly":
		if p.o.new {
			return ttBoolEmpty(!col.UserCan("write_new")), true
		}
		return ttBoolEmpty(!col.UserCan("write_existing")), true
	case "data":
		if p.o.datumPres != nil {
			return p.o.datumPres, true
		}
		if p.o.datum != nil {
			gads.Panic("Datum presentation is not yet supported")
		}
		return nil, true
	case "topic":
		if col.TopicID == nil {
			return nil, true
		}
		if r := p.c.E.DB.QueryRow(`SELECT * FROM "topic" WHERE "id" = ?`, *col.TopicID); r != nil {
			return newRow("topic", r), true
		}
		return nil, true
	case "topic_id":
		if col.TopicID == nil {
			return nil, true
		}
		return *col.TopicID, true
	case "fixedvals":
		return ttBool01(col.Fixedvals()), true
	case "widthcols":
		return ttValue(col.Widthcols()), true
	case "optional":
		return ttBool01(col.Optional), true
	case "userinput":
		return ttBool01(col.UserinputFn()), true
	case "lookup_endpoint":
		return emptyNil(col.LookupEndpoint), true
	case "depends_on_b64":
		return col.DependsOnB64(), true
	case "addable":
		return ttBool01(col.Addable()), true
	case "return_type":
		return emptyNil(col.ReturnTypeFn()), true
	case "show_in_edit":
		return col.ShowInEdit(), true
	case "has_typeahead":
		return ttBool01(col.HasFilterTypeahead()), true
	case "has_display_field":
		return ttBoolEmpty(col.HasDisplayField()), true
	case "display_for_edit":
		if !p.o.edit {
			return nil, true
		}
		show := col.UserinputFn() || col.HasBrowserCode()
		if r := p.o.record; r != nil {
			if r.ParentID() != 0 && !col.CanChild() && !r.Layout().UserCan("create_child") {
				show = false
			}
			if r.LinkedID() != 0 && col.LinkParent() != nil {
				show = false
			}
		}
		return ttBool01(show), true
	case "sort":
		return p.sortHash(), true
	}
	// Values of after_presentation of the column types
	switch {
	case col.IsCurcommon():
		switch name {
		case "layout_parent":
			return p.c.ttLayout(col.LayoutParent()), true
		case "value_selector":
			return emptyNil(col.ValueSelector()), true
		case "show_add":
			return ttBool01(col.ShowAdd()), true
		case "typeahead_use_id":
			return int64(1), true
		case "limit_rows":
			return ttValue(col.LimitRows()), true
		case "modal_field_ids":
			ids := col.CurvalFieldIDs()
			if ids == nil {
				ids = []int64{}
			}
			return gads.EncodeJSON(ids), true
		case "filtered_values":
			if !p.o.edit || hasSubvals(col) {
				return nil, true
			}
			out := tt.NewList()
			for _, v := range col.FilteredValues(p.o.record.SubmissionToken()) {
				out.Items = append(out.Items, p.c.curvalRowHash(col, v.Record))
			}
			return out, true
		case "has_subvals":
			return ttBool01(hasSubvals(col)), true
		case "data_filter_fields":
			if col.Type != "curval" {
				return "", true
			}
			var fields []string
			for _, f := range col.SubvalsInputRequired() {
				fields = append(fields, `"`+f.Field()+`"`)
			}
			return "[" + strings.Join(fields, ", ") + "]", true
		}
	case col.Type == "date" || col.Type == "daterange":
		if name == "show_datepicker" {
			return ttBool01(col.ShowDatepicker()), true
		}
	case col.Type == "enum":
		if name == "select_values" {
			return ttValue(col.Enumvals()), true
		}
	case col.Type == "person":
		switch name {
		case "default_to_login":
			return ttBool01(col.DefaultToLogin()), true
		case "select_values":
			l := tt.NewList()
			for _, u := range col.People() {
				l.Items = append(l.Items, p.c.ttUser(u))
			}
			return l, true
		}
	case col.Type == "intgr":
		if name == "show_calculator" {
			return ttBool01(col.ShowCalculator()), true
		}
	case col.Type == "string":
		switch name {
		case "textbox":
			return ttBool01(col.Textbox), true
		case "max_length":
			if m := col.MaxLength(); m != nil {
				return *m, true
			}
			return nil, true
		}
	case col.Type == "tree":
		if name == "end_node_only" {
			return ttBool01(col.EndNodeOnly), true
		}
	}
	switch name {
	case "helptext_html":
		if col.Helptext == "" {
			return "", true
		}
		gads.Panic("Column presentation %s is not yet supported", name)
	case "lookup_fields":
		l := tt.NewList()
		for _, f := range p.lookupFields() {
			l.Items = append(l.Items, f)
		}
		return l, true
	case "lookup_fields_json":
		f := p.lookupFields()
		if f == nil {
			f = []string{}
		}
		return gads.EncodeJSON(f), true
	case "display_fields_b64":
		if !col.HasDisplayField() {
			return nil, true
		}
		return col.DisplayFields().Base64(), true
	case "code_b64":
		if !col.HasBrowserCode() {
			return nil, true
		}
		return base64.StdEncoding.EncodeToString([]byte(col.CodeFn())), true
	case "params_b64":
		if !col.HasBrowserCode() {
			return nil, true
		}
		params := col.Params()
		if params == nil {
			params = []string{}
		}
		return base64.StdEncoding.EncodeToString([]byte(gads.EncodeJSON(params))), true
	}
	return nil, false
}

// get returns a value of the presentation
func (p *ttPresentation) get(name string) (any, bool) {
	r, ok, _ := p.TTMethod(name, nil)
	if !ok || len(r) == 0 {
		return nil, false
	}
	return r[0], true
}

// hasSubvals is Column->has_subvals (only a curval has filtered values
// substituted from the record)
func hasSubvals(col *gads.Column) bool {
	if col.Type != "curval" {
		return false
	}
	return len(col.FilterFn().ColumnsInSubs()) > 0
}

// lookupFields is Column->lookup_fields: the short names of the fields of
// the lookup group
func (p *ttPresentation) lookupFields() []string {
	col := p.col
	if col.LookupEndpoint == "" {
		return nil
	}
	if col.LookupGroup == "" {
		if col.NameShort == "" {
			gads.Fail("Lookup field %s requires a short name", col.Name)
		}
		return []string{col.NameShort}
	}
	var names []string
	for _, r := range p.c.E.DB.Query(`SELECT * FROM "layout" "me" WHERE "me"."instance_id" = ? AND "me"."lookup_group" = ?`, col.InstanceID, col.LookupGroup) {
		ns, _ := db.AsString(r["name_short"])
		if ns == "" {
			n, _ := db.AsString(r["name"])
			gads.Fail("Lookup field %s requires a short name", n)
		}
		names = append(names, ns)
	}
	return names
}
