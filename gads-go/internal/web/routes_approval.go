package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// ttRawRecord is a GADS::Record itself (not its presentation), as given to
// the edit template of an approval: only its attributes and methods are
// there
type ttRawRecord struct {
	c *Ctx
	r *gads.Record
}

func (o *ttRawRecord) TTMethod(name string, args []any) ([]any, bool, error) {
	r := o.r
	one := func(v any) ([]any, bool, error) { return []any{v}, true, nil }
	switch name {
	case "current_id":
		return one(nilIfZero(r.CurrentID()))
	case "record_id":
		return one(nilIfZero(r.RecordID()))
	case "parent_id":
		return one(nilIfZero(r.ParentID()))
	case "linked_id":
		return one(nilIfZero(r.LinkedID()))
	case "new_entry":
		return one(ttBool01(r.NewEntry()))
	case "deleted":
		return one(ttTime(r.Deleted()))
	case "is_draft":
		return one(ttValue(nilIfZero(r.IsDraft())))
	case "id_count":
		return one(ttValue(nilIfZero(r.IDCount())))
	case "child_record_ids":
		return one(ttValue(r.ChildRecordIDs()))
	case "user_can_delete":
		return one(ttBool01(r.CurrentID() != 0 && r.UserCanDelete()))
	case "created_user":
		// A Datum::Person, which has no data
		return one(tt.NewHash())
	case "versions":
		l := tt.NewList()
		for _, v := range r.Versions() {
			row := newRow("record", v)
			uid := v["createdby"]
			row.set("createdby", func([]any) any {
				if uid == nil {
					return nil
				}
				if u := o.c.E.DB.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, uid); u != nil {
					return newRow("user", u)
				}
				return nil
			})
			l.Items = append(l.Items, row)
		}
		return one(l)
	}
	return nil, false, nil
}

// any ['get','post'] => /:layout_name/approval/?:id? (GADS.pm:3886)
func (s *Server) hApproval(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	idS, _ := c.Param("id")
	id, _ := toInt64(idS)
	user := c.LoggedInUser()

	// If viewing or approving an individual record, first see if it is a
	// new record or an edit of an existing one, which affects permissions
	approvalOfNew := false
	if truthy(idS) {
		approvalOfNew = s.E.ApprovalOfNew(layout, user, id)
	}

	if truthy(c.ParamS("submit")) {
		// The latest record for this approval
		record := s.E.NewRecord(layout, user)
		record.SetApprovalID(id)
		record.DoingApproval = true
		// For an approval of a new record, the record does not exist as a
		// normal entry, so a new one is made with the current ID
		currentID, _ := toInt64(c.ParamS("current_id"))
		if err := gads.Try(func() { record.FindCurrentID(currentID, gads.FindOptions{}) }); err != nil {
			record.SetCurrentID(currentID)
			record.Initialise(0)
		}
		failed := false
		for _, col := range record.EditColumns(gads.EditColumnsOptions{New: approvalOfNew, Approval: true}) {
			newv, ok := c.Param(col.Field())
			if col.Userinput() && ok {
				failed = !c.Process(func() { record.Fields()[col.ID].SetValue(newv, gads.SetValueOptions{}) }) || failed
			}
		}
		if !failed && c.Process(func() { record.Write(gads.WriteOptions{}) }) {
			c.ForwardHome("success", "Record has been successfully approved", layout.Identifier()+"/approval")
		}
	}

	params := tt.HashOf("page", "approval")
	page := "approval"
	if truthy(idS) {
		// The record of values needing approval
		record := s.E.NewRecord(layout, user)
		record.InitNoValue = false
		record.IncludeApproval = true
		record.FindRecordID(id, gads.FindOptions{})
		params.Set("record", &ttRawRecord{c: c, r: record})
		params.Set("record_presentation", c.recordPresentation(record, recordPresentOpts{edit: true, new: approvalOfNew, approval: true}))
		// The existing values for comparison
		if !approvalOfNew {
			existing := s.E.NewRecord(layout, user)
			existing.FindCurrentID(record.CurrentID(), gads.FindOptions{})
			params.Set("existing", &ttRawRecord{c: c, r: existing})
		}
		page = "edit"
	} else {
		records := tt.NewList()
		for _, a := range s.E.ApprovalRecords(layout, user) {
			records.Items = append(records.Items, tt.HashOf(
				"record_id", a.RecordID,
				"current_id", a.CurrentID,
				"createdby", tt.HashOf("html", a.CreatedbyHTML),
			))
		}
		params.Set("records", records)
	}
	return c.Template(page, params, nil)
}
