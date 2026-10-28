package web

import (
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// editAttr are the attributes of a column used by the field edit pages.
// Attributes of other column types are not methods of the column, so are
// undef (an attribute that is not there).
func (o *ttColumnObj) editAttr(name string, args []any) ([]any, bool) {
	col := o.col
	t := col.Type
	one := func(v any) ([]any, bool) { return []any{v}, true }
	isCurcommon := t == "curval" || t == "autocur" || t == "filval"
	switch name {
	case "topic":
		if col.TopicID == nil {
			return one(nil)
		}
		if r := o.c.s.E.DB.QueryRow(`SELECT * FROM "topic" WHERE "id" = ?`, *col.TopicID); r != nil {
			return one(newRow("topic", r))
		}
		return one(nil)
	case "display_fields_as_text":
		return one(displayFieldsAsText(o.c.s.E, col))
	case "related_field_id":
		return one(ttValue(col.RelatedFieldID))
	case "link_parent_id":
		return one(ttValue(col.LinkParentID))
	case "link_parent":
		return one(o.c.ttColumn(col.LinkParent()))
	case "group_has":
		gid, _ := toInt64(argAt(args, 0))
		perm := tt.Str(argAt(args, 1))
		for _, p := range col.Permissions()[gid] {
			if p == perm {
				return one(int64(1))
			}
		}
		return one(int64(0))
	case "permissions":
		h := tt.NewHash()
		perms := col.Permissions()
		var gids []int64
		for g := range perms {
			gids = append(gids, g)
		}
		sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
		for _, g := range gids {
			l := tt.NewList()
			for _, p := range perms[g] {
				l.Items = append(l.Items, p)
			}
			h.Set(fmtInt(g), l)
		}
		return one(h)
	case "filter":
		return one(&ttFilterObj{f: col.FilterFn()})
	case "display_fields":
		return one(&ttFilterObj{f: col.DisplayFields()})
	case "show_datepicker":
		if t != "date" && t != "daterange" {
			return one(nil)
		}
		return one(ttBool01(col.ShowDatepicker()))
	case "default_today":
		if t != "date" {
			return one(nil)
		}
		return one(ttBool01(col.DefaultToday()))
	case "show_calculator":
		if t != "intgr" {
			return one(nil)
		}
		return one(ttBool01(col.ShowCalculator()))
	case "enumvals":
		if t != "enum" && t != "tree" {
			return one(nil)
		}
		return one(ttValue(col.Enumvals()))
	case "code":
		if !col.IsCode() {
			return one(nil)
		}
		return one(emptyNil(col.CodeFn()))
	case "override_permissions":
		if !isCurcommon {
			return one(nil)
		}
		return one(ttBool01(col.OverridePermissions()))
	case "limit_rows":
		if !isCurcommon {
			return one(nil)
		}
		return one(nilIfZero(col.LimitRows()))
	case "delete_not_used":
		if t != "curval" {
			return one(nil)
		}
		return one(ttBool01(col.DeleteNotUsed()))
	case "filesize":
		if t != "file" {
			return one(nil)
		}
		return one(ttValue(col.Filesize))
	case "default_to_login":
		if t != "person" {
			return one(nil)
		}
		return one(ttBool01(col.DefaultToLogin()))
	case "notify_on_selection":
		if t != "person" {
			return one(nil)
		}
		return one(ttBool01(col.NotifyOnSelection()))
	case "notify_on_selection_subject", "notify_on_selection_message":
		if t != "person" {
			return one(nil)
		}
		if col.Options == nil {
			return one("")
		}
		return one(ttValue(col.Options[name]))
	case "has_subvals":
		if t != "curval" {
			return one(int64(0))
		}
		return one(ttBoolEmpty(len(col.FilterFn().ColumnsInSubs()) > 0))
	}
	return nil, false
}

// displayFieldsAsText is Column->display_fields_as_text: a summary of the
// conditions under which a field is displayed
func displayFieldsAsText(e *gads.Engine, col *gads.Column) string {
	rows := e.DB.Query(`SELECT "me"."operator", "me"."regex", "display_field"."name" FROM "display_field" "me" JOIN "layout" "display_field" ON "display_field"."id" = "me"."display_field_id" WHERE "me"."layout_id" = ? ORDER BY "me"."id"`, col.ID)
	if len(rows) == 0 {
		return ""
	}
	var conds []string
	for _, r := range rows {
		conds = append(conds, tt.Str(ttValue(r["name"]))+" "+tt.Str(ttValue(r["operator"]))+" "+tt.Str(ttValue(r["regex"])))
	}
	typ := "Only display when the following is true"
	switch col.DisplayCondition {
	case "AND":
		typ = "Only displayed when all the following are true"
	case "OR":
		typ = "Only displayed when any of the following are true"
	}
	return typ + ": " + strings.Join(conds, "; ")
}
