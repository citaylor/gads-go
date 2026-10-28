package web

import (
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// ttLazy is a Perl hash built by a presentation method: its values are
// computed when a template first reads them (missing keys are undefined)
type ttLazy struct {
	keys  []string
	fns   map[string]func() any
	cache map[string]any
	set   *tt.Hash
}

func newLazy() *ttLazy {
	return &ttLazy{fns: map[string]func() any{}, cache: map[string]any{}, set: tt.NewHash()}
}

// def defines a key
func (l *ttLazy) def(k string, f func() any) *ttLazy {
	if _, ok := l.fns[k]; !ok {
		l.keys = append(l.keys, k)
	}
	l.fns[k] = f
	delete(l.cache, k)
	return l
}

// val defines a key with a value
func (l *ttLazy) val(k string, v any) *ttLazy { return l.def(k, func() any { return v }) }

func (l *ttLazy) get(k string) (any, bool) {
	if v, ok := l.set.Get(k); ok {
		return v, true
	}
	if v, ok := l.cache[k]; ok {
		return v, true
	}
	f, ok := l.fns[k]
	if !ok {
		return nil, false
	}
	v := f()
	l.cache[k] = v
	return v, true
}

// TTHash implements tt.HashLike (values assigned by templates)
func (l *ttLazy) TTHash() *tt.Hash { return l.set }

func (l *ttLazy) TTMethod(name string, args []any) ([]any, bool, error) {
	if v, ok := l.get(name); ok {
		return []any{v}, true, nil
	}
	return nil, false, nil
}

// unsupported defines a key whose value is not yet supported
func (l *ttLazy) unsupported(keys ...string) *ttLazy {
	for _, k := range keys {
		k := k
		l.def(k, func() any {
			gads.Panic("Presentation value %s is not yet supported", k)
			return nil
		})
	}
	return l
}

type recordPresentOpts struct {
	edit, new, child bool
	approval         bool
	bulk             string
	modal            int64
	curvalFields     []*gads.Column
}

// recordPresentation is GADS::Role::Presentation::Record presentation
func (c *Ctx) recordPresentation(r *gads.Record, o recordPresentOpts) *ttLazy {
	layout := r.Layout()
	var columns []*gads.Column
	switch {
	case o.edit:
		columns = r.EditColumns(gads.EditColumnsOptions{New: o.new, Approval: o.approval, Child: o.child, Modal: o.modal != 0, Bulk: o.bulk})
	case o.curvalFields != nil:
		columns = o.curvalFields
	default:
		gads.Panic("Presentation of the rendered columns of a record is not yet supported")
	}

	// The indentation of each field: indented if it has a display
	// condition of an immediately-previous field
	indent := map[int64]int64{}
	thisIndent := map[int64]int64{}
	var previous *gads.Column
	for _, col := range columns {
		if col.HasDisplayField() {
			for _, dfID := range col.DisplayFieldColIDs() {
				var seen int64
				// grep over the keys of the hash: any matching key
				if _, ok := thisIndent[dfID]; ok {
					seen = dfID
				}
				if seen != 0 || (previous != nil && dfID == previous.ID) {
					if seen != 0 && indent[seen] != 0 {
						indent[col.ID] = indent[seen] + 1
					} else {
						indent[col.ID] = 1
					}
					thisIndent[col.ID] = indent[col.ID]
					break
				}
				indent[col.ID] = 0
				thisIndent = map[int64]int64{col.ID: 0}
			}
		} else {
			indent[col.ID] = 0
			thisIndent = map[int64]int64{col.ID: 0}
		}
		previous = col
	}

	// presentation_map_columns
	cols := tt.NewList()
	var presCols []*ttPresentation
	for _, col := range columns {
		dp := c.datumPresentation(r.GetFieldValue(col))
		p := c.columnPresentation(col, presentOpts{new: o.new, edit: o.edit, record: r, datum: r.GetFieldValue(col), datumPres: dp, modal: o.modal})
		// Perl builds these eagerly, and they generate tokens (selector IDs
		// and the submission token), so build them in the same order
		if col.IsCurcommon() {
			dp.get("value")
			dp.get("id_hash")
			p.get("filtered_values")
		}
		presCols = append(presCols, p)
		cols.Items = append(cols.Items, p)
	}

	l := newLazy()
	l.val("parent_id", nilIfZero(r.ParentID()))
	l.val("linked_id", nilIfZero(r.LinkedID()))
	l.val("current_id", nilIfZero(r.CurrentID()))
	l.val("record_id", nilIfZero(r.RecordID()))
	l.val("instance_id", layout.InstanceID)
	l.val("columns", cols)
	l.def("topics", func() any { return c.recordTopics(presCols) })
	ih := tt.NewHash()
	for _, col := range columns {
		ih.Set(fmtInt(col.ID), indent[col.ID])
	}
	l.val("indent", ih)
	l.def("deleted", func() any { return ttTime(r.Deleted()) })
	l.def("deletedby", func() any {
		if d := r.Deletedby(); d != nil && !d.Blank() {
			return c.datumPresentation(d)
		}
		return nil
	})
	l.def("user_can_delete", func() any { return ttBoolEmpty(r.UserCanDelete()) })
	l.def("user_can_edit", func() any {
		return ttBoolEmpty(layout.UserCan("write_existing") || layout.UserCan("write_new"))
	})
	l.def("id_count", func() any { return ttValue(nilIfZero(r.IDCount())) })
	hasRag := ""
	for _, col := range columns {
		if col.Type == "rag" {
			hasRag = "1"
		}
	}
	l.val("has_rag_column", ttBoolEmpty(hasRag != ""))
	l.def("new_entry", func() any { return ttBoolEmpty(r.NewEntry()) })
	l.def("is_draft", func() any { return ttValue(nilIfZero(r.IsDraft())) })
	if o.edit {
		l.def("versions", func() any {
			format := (&ttConfig{s: c.s}).dateformat() + " HH:mm:ss"
			out := tt.NewList()
			for _, v := range r.Versions() {
				created, _ := db.AsTime(v["created"])
				var by any
				if uid, ok := toInt64(v["createdby"]); ok && v["createdby"] != nil {
					if u := c.E.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, uid); u != nil {
						by = newRow("user", u)
					}
				}
				out.Items = append(out.Items, tt.HashOf(
					"id", ttValue(v["id"]),
					"created", gads.FormatDate(created, format),
					"createdby", by,
				))
			}
			return out
		})
	}
	if !r.NewEntry() && o.edit {
		internal := func(short string, d gads.Datum) func() any {
			return func() any {
				col := layout.ColumnByNameShort(short)
				return c.columnPresentation(col, presentOpts{edit: true, new: o.new, record: r, datum: d, datumPres: c.datumPresentation(d), modal: o.modal})
			}
		}
		l.def("version_user", internal("_version_user", r.EditedUser()))
		l.def("version_datetime", internal("_version_datetime", r.EditedTime()))
		l.def("created_user", internal("_created_user", r.CreatedUser()))
		l.def("created_datetime", internal("_created", r.CreatedTime()))
	}
	return l
}

// recordTopics is Record->get_topics
func (c *Ctx) recordTopics(cols []*ttPresentation) *tt.List {
	type topic struct {
		order    int
		h        *tt.Hash
		columns  *tt.List
		editable bool
	}
	topics := map[int64]*topic{}
	order := 0
	for _, p := range cols {
		var tid int64
		if t := p.col.TopicID; t != nil {
			tid = *t
		}
		tp := topics[tid]
		if tp == nil {
			order++
			tp = &topic{order: order, columns: tt.NewList()}
			topicObj, _ := p.get("topic")
			tp.h = tt.HashOf("order", int64(order), "topic", topicObj, "columns", tp.columns, "topic_id", tid)
			topics[tid] = tp
		}
		if ro, _ := p.get("readonly"); !tt.True(ro) {
			tp.editable = true
		}
		tp.columns.Items = append(tp.columns.Items, p)
	}
	var list []*topic
	for _, tp := range topics {
		list = append(list, tp)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].order < list[j].order })
	out := tt.NewList()
	for _, tp := range list {
		if tp.editable {
			tp.h.Set("has_editable", int64(1))
		} else {
			tp.h.Set("has_editable", nil)
		}
		out.Items = append(out.Items, tp.h)
	}
	return out
}

// datumPresentation is GADS::Role::Presentation::Datum presentation
func (c *Ctx) datumPresentation(d gads.Datum) *ttLazy {
	col := d.Column()
	l := newLazy()
	typ := col.Type
	if _, ok := d.(*gads.CountDatum); ok {
		typ = "count"
	}
	l.val("type", typ)
	l.def("value", func() any { return d.AsString() })
	l.def("has_value", func() any { return ttBoolEmpty(d.HasValue()) })
	l.def("filter_value", func() any { return d.FilterValue() })
	l.def("blank", func() any { return ttBool01(d.Blank()) })
	l.def("dependent_not_shown", func() any { return ttBool01(gads.DependentNotShown(d, false, "")) })
	l.def("html_form", func() any { return ttValue(d.HTMLForm()) })
	l.def("record_id", func() any {
		if r := gads.DatumRecord(d); r != nil {
			return nilIfZero(r.RecordID())
		}
		return nil
	})
	l.val("column_id", col.ID)
	l.val("column_name", col.Name)

	switch x := d.(type) {
	case *gads.EnumDatum:
		l.def("id_hash", func() any {
			h := tt.NewHash()
			for _, id := range x.IDs() {
				h.Set(fmtInt(id), int64(1))
			}
			return h
		})
		l.def("ids", func() any { return ttValue(x.IDs()) })
		l.def("deleted_values", func() any { return ttValue(x.DeletedValues()) })
	case *gads.FileDatum:
		l.def("files", func() any { return ttValue(x.Files()) })
	case *gads.PersonDatum:
		l.def("text", func() any { return x.AsString() })
		l.def("value", func() any { return x.AsString() })
		l.def("details", func() any {
			out := tt.NewList()
			site := c.E.Site()
			for _, v := range x.ValueHash() {
				out.Items = append(out.Items, personDetails(v, site))
			}
			return out
		})
		l.def("ids", func() any { return ttValue(x.IDs()) })
		l.def("deleted_values", func() any { return ttValue(x.DeletedValues()) })
	case *gads.RagDatum:
		delete(l.fns, "value")
		l.def("grade", func() any { return x.AsGrade() })
	case *gads.StringDatum, *gads.CalcDatum:
		l.def("raw", func() any { return d.AsString() })
		l.def("html", func() any {
			out := tt.NewList()
			for _, v := range d.HTMLForm() {
				out.Items = append(out.Items, text2html(tt.Str(v)))
			}
			return out
		})
	case *gads.TreeDatum:
		l.def("ids", func() any { return ttValue(x.IDs()) })
		l.def("ids_as_params", func() any {
			var parts []string
			for _, id := range x.IDs() {
				parts = append(parts, "ids="+fmtInt(id))
			}
			return strings.Join(parts, "&")
		})
	case *gads.CurcommonDatum:
		l.def("text", func() any { return d.AsString() })
		l.def("id_hash", func() any {
			h := tt.NewHash()
			for _, v := range x.Values() {
				h.Set(v.Record.SelectorID(), int64(1))
			}
			return h
		})
		l.def("links", func() any {
			var links []curvalLink
			for _, v := range x.Values() {
				links = append(links, curvalLink{value: v})
			}
			return c.curvalLinks(col, links)
		})
		if col.ValueSelector() == "typeahead" && !col.MultivalueFn() {
			l.unsupported("autocomplete_value")
		}
		l.def("selected_values", func() any {
			out := tt.NewList()
			for _, v := range x.Values() {
				out.Items = append(out.Items, c.curvalRowHash(col, v.Record))
			}
			return out
		})
		l.def("parent_layout_identifier", func() any { return col.LayoutParent().Identifier() })
		// has_more: the values may be limited by limit_rows
		l.def("has_more", func() any { return ttBool01(col.LimitRows() != 0) })
	}
	return l
}

// personDetails is Datum::Person _presentation_details
func personDetails(v map[string]any, site *gads.Site) *tt.Hash {
	details := tt.NewList()
	if email := v["email"]; truthy(email) {
		details.Items = append(details.Items, tt.HashOf("value", ttValue(email), "type", "email"))
	}
	for _, ft := range [][2]any{
		{v["freetext1"], site.FreetextName(1)},
		{v["freetext2"], site.FreetextName(2)},
	} {
		if !truthy(ft[0]) {
			continue
		}
		details.Items = append(details.Items, tt.HashOf("definition", emptyNil(ft[1].(string)), "value", ttValue(ft[0]), "type", "text"))
	}
	return tt.HashOf("id", ttValue(v["id"]), "text", ttValue(v["value"]), "details", details)
}

// curvalRowHash is Curcommon->_format_row as a Perl hash
func (c *Ctx) curvalRowHash(col *gads.Column, rec *gads.Record) *tt.Hash {
	v := col.FormatRow(rec)
	vals := tt.NewList()
	for _, s := range v.Values {
		vals.Items = append(vals.Items, s)
	}
	valueID := nilIfZero(v.ID)
	if v.AsQuery != "" {
		valueID = v.AsQuery
	}
	return tt.HashOf(
		"id", nilIfZero(v.ID),
		"value_id", valueID,
		"selector_id", v.SelectorID,
		"record", rec,
		"value", v.Value,
		"values", vals,
		"html", v.HTML,
	)
}

// curvalLink is a value of a curval with its status in the chronology of a
// record (Added, Removed or Changed, and the version of the value)
type curvalLink struct {
	value     gads.CurcommonValue
	status    any
	versionID any
}

// curvalLinks is Presentation::Datum::Curcommon _presentation_details
func (c *Ctx) curvalLinks(col *gads.Column, links []curvalLink) *tt.List {
	out := tt.NewList()
	var rti any
	if r := col.RefersToInstanceID(); r != nil {
		rti = *r
	}
	for _, link := range links {
		v := link.value
		vals := tt.NewList()
		for _, s := range v.Values {
			vals.Items = append(vals.Items, s)
		}
		out.Items = append(out.Items, tt.HashOf(
			"id", nilIfZero(v.ID),
			"href", v.Value,
			"refers_to_instance_id", rti,
			"values", vals,
			"presentation", c.recordPresentation(v.Record, recordPresentOpts{curvalFields: col.CurvalFields()}),
			"status", link.status,
			"version_id", link.versionID,
		))
	}
	return out
}
