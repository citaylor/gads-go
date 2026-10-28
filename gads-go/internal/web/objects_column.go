package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// ttColumnObj is a GADS::Column. Columns are hash-based Moo objects, so
// templates may set attributes on them (kept in extra).
type ttColumnObj struct {
	c     *Ctx
	col   *gads.Column
	extra *tt.Hash
}

func (c *Ctx) ttColumn(col *gads.Column) any {
	if col == nil {
		return nil
	}
	// The same object for a column throughout a request, as Perl's column
	// objects (values set on them by templates are kept)
	if o, ok := c.colObjs[col]; ok {
		return o
	}
	o := &ttColumnObj{c: c, col: col, extra: tt.NewHash()}
	if c.colObjs == nil {
		c.colObjs = map[*gads.Column]*ttColumnObj{}
	}
	c.colObjs[col] = o
	return o
}

func (c *Ctx) ttColumns(cols []*gads.Column) *tt.List {
	l := tt.NewList()
	for _, col := range cols {
		l.Items = append(l.Items, c.ttColumn(col))
	}
	return l
}

// TTHash implements tt.HashLike
func (o *ttColumnObj) TTHash() *tt.Hash { return o.extra }

func ttBool01(b bool) any {
	if b {
		return int64(1)
	}
	return int64(0)
}

func (o *ttColumnObj) TTMethod(name string, args []any) ([]any, bool, error) {
	col := o.col
	one := func(v any) ([]any, bool, error) { return []any{v}, true, nil }
	switch name {
	case "id":
		return one(col.ID)
	case "name":
		return one(col.Name)
	case "name_short":
		if col.NameShort == "" {
			return one(nil)
		}
		return one(col.NameShort)
	case "type":
		return one(col.Type)
	case "instance_id":
		return one(col.InstanceID)
	case "description":
		return one(emptyNil(col.Description))
	case "helptext":
		return one(emptyNil(col.Helptext))
	case "internal", "is_internal":
		return one(ttBool01(col.Internal))
	case "optional":
		return one(ttBool01(col.Optional))
	case "remember":
		return one(ttBool01(col.Remember))
	case "isunique":
		return one(ttBool01(col.IsUnique))
	case "multivalue":
		return one(ttBool01(col.MultivalueFn()))
	case "textbox":
		return one(ttBool01(col.Textbox))
	case "end_node_only":
		return one(ttBool01(col.EndNodeOnly))
	case "position":
		return one(ttValue(col.Position))
	case "topic_id":
		return one(ttValue(col.TopicID))
	case "width":
		return one(col.Width)
	case "widthcols":
		return one(ttValue(col.Widthcols()))
	case "aggregate":
		return one(emptyNil(col.Aggregate))
	case "group_display":
		return one(emptyNil(col.GroupDisplay()))
	case "lookup_endpoint":
		return one(emptyNil(col.LookupEndpoint))
	case "lookup_group":
		return one(emptyNil(col.LookupGroup))
	case "display_condition":
		return one(emptyNil(col.DisplayCondition))
	case "notes":
		return one(emptyNil(col.Notes))
	case "ordering":
		return one(emptyNil(col.Ordering))
	case "force_regex":
		return one(emptyNil(col.ForceRegex))
	case "return_type":
		return one(emptyNil(col.ReturnTypeFn()))
	case "userinput":
		return one(ttBool01(col.UserinputFn()))
	case "is_curcommon":
		return one(ttBool01(col.IsCurcommon()))
	case "can_child":
		return one(ttBool01(col.CanChild()))
	case "has_filter_typeahead":
		return one(ttBool01(col.HasFilterTypeahead()))
	case "use_id_in_filter":
		return one(ttBool01(col.UseIDInFilter()))
	case "filter_id":
		return one(fmtInt(col.ID))
	case "filter_name":
		return one(col.Name)
	case "has_display_field":
		return one(ttBool01(col.HasDisplayField()))
	case "has_multivalue_plus":
		return one(ttBool01(col.HasMultivaluePlus()))
	case "addable":
		return one(ttBool01(col.Addable()))
	case "numeric":
		return one(ttBool01(col.Numeric()))
	case "show_in_edit":
		return one(col.ShowInEdit())
	case "user_can":
		return one(ttBoolEmpty(col.UserCan(tt.Str(argAt(args, 0)))))
	case "refers_to_instance_id":
		return one(ttValue(col.RefersToInstanceID()))
	case "value_selector":
		return one(emptyNil(col.ValueSelector()))
	case "show_add":
		return one(ttBool01(col.ShowAdd()))
	case "has_curval_field":
		id, _ := toInt64(argAt(args, 0))
		for _, f := range col.CurvalFieldIDs() {
			if f == id {
				return one(int64(1))
			}
		}
		return one("")
	case "curval_field_ids":
		return one(ttValue(col.CurvalFieldIDs()))
	case "layout":
		return one(o.c.ttLayout(col.Layout()))
	}
	if v, ok := o.editAttr(name, args); ok {
		return v, true, nil
	}
	return nil, false, nil
}

func emptyNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// allOptions converts the named arguments of Layout->all from a template
func allOptions(args []any) gads.AllOptions {
	var o gads.AllOptions
	h, _ := argAt(args, len(args)-1).(*tt.Hash)
	if h == nil {
		// The options may be given as a list of keys and values
		if len(args) < 2 {
			return o
		}
		h = tt.NewHash()
		for i := 0; i+1 < len(args); i += 2 {
			h.Set(tt.Str(args[i]), args[i+1])
		}
	}
	b := func(k string) bool {
		v, _ := h.Get(k)
		return tt.True(v)
	}
	o.ExcludeInternal = b("exclude_internal")
	o.UserCanRead = b("user_can_read")
	o.UserCanWrite = b("user_can_write")
	o.UserCanWriteNew = b("user_can_write_new")
	o.UserCanWriteExisting = b("user_can_write_existing")
	o.UserCanReadwriteNew = b("user_can_readwrite_new")
	o.UserCanReadwriteExisting = b("user_can_readwrite_existing")
	o.UserCanApproveNew = b("user_can_approve_new")
	o.UserCanApproveExisting = b("user_can_approve_existing")
	o.SortByTopics = b("sort_by_topics")
	o.OnlyInternal = b("only_internal")
	o.IncludeHidden = b("include_hidden")
	o.CanChild = b("can_child")
	o.OnlyUnique = b("only_unique")
	if v, ok := h.Get("type"); ok && v != nil {
		o.Type = tt.Str(v)
	}
	return o
}
