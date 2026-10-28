package gads

import (
	"sort"

	"github.com/ctrlo/gads-go/internal/db"
)

// ApprovalRecord is a record awaiting approval (GADS::Approval records)
type ApprovalRecord struct {
	RecordID  int64
	CurrentID int64
	// CreatedbyHTML is the html of the person who submitted the values.
	// Perl passes the user to the constructor of a Datum::Person as
	// set_value, which is a method not an attribute, so it is always blank.
	CreatedbyHTML string
}

// The tables of values that may await approval
var approvalTables = []string{"string", "date", "daterange", "intgr", "enum", "curval", "file", "person"}

// ApprovalRecords is GADS::Approval->records: the records of the table with
// values awaiting the approval of the user. As Perl's are the values of a
// hash, the order is not defined (they are sorted by record here).
func (e *Engine) ApprovalRecords(layout *Layout, user *User) []ApprovalRecord {
	// First short-cut and see if it is worth continuing
	n, _ := e.DB.QueryInt64(`SELECT COUNT(*) FROM "record" "me" JOIN "current" "current" ON "current"."id" = "me"."current_id" WHERE "me"."approval" = 1 AND "current"."instance_id" = ?`, layout.InstanceID)
	if n == 0 {
		return nil
	}
	var rows []db.Row
	search := func(permission, previous string) {
		for _, t := range approvalTables {
			rows = append(rows, e.DB.Query(`SELECT MAX("record"."id") AS "record_id", MAX("record"."current_id") AS "current_id"
				FROM "`+t+`" "me"
				JOIN "record" "record" ON "record"."id" = "me"."record_id"
				JOIN "current" "current" ON "current"."id" = "record"."current_id"
				LEFT JOIN "record" "record_2" ON "record_2"."id" = "record"."record_id"
				LEFT JOIN "record" "record_previous" ON "record_previous"."current_id" = "record_2"."current_id" AND "record_previous"."id" < "record_2"."id"
				JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id"
				LEFT JOIN "layout_group" "layout_groups" ON "layout_groups"."layout_id" = "layout"."id"
				LEFT JOIN "group" "group" ON "group"."id" = "layout_groups"."group_id"
				LEFT JOIN "user_group" "user_groups" ON "user_groups"."group_id" = "group"."id"
				WHERE "current"."instance_id" = ? AND "record"."approval" = 1 AND "layout_groups"."permission" = ?
				AND "user_groups"."user_id" = ? AND "record_previous"."id" `+previous+`
				GROUP BY "record"."id"`, layout.InstanceID, permission, user.ID)...)
		}
	}
	if layout.UserCan("approve_new") {
		search("approve_new", "IS NULL")
	}
	if layout.UserCan("approve_existing") {
		search("approve_existing", "IS NOT NULL")
	}
	seen := map[int64]bool{}
	var out []ApprovalRecord
	for _, r := range rows {
		rid, _ := db.AsInt64(r["record_id"])
		if seen[rid] {
			continue
		}
		seen[rid] = true
		cid, _ := db.AsInt64(r["current_id"])
		out = append(out, ApprovalRecord{RecordID: rid, CurrentID: cid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordID < out[j].RecordID })
	return out
}

// SetApprovalID sets the ID of the approval record of the values being
// approved (Record approval_id)
func (r *Record) SetApprovalID(id int64) { r.approvalID = id }

// SetCurrentID sets the current ID of a record not found as a normal
// record (an approval of a new record)
func (r *Record) SetCurrentID(id int64) { r.currentID = id }

// ApprovalOfNew is Record->approval_of_new of a record (or approval
// record) ID: whether the approval is of a new record
func (e *Engine) ApprovalOfNew(layout *Layout, user *User, recordID int64) bool {
	if e.DB.QueryRow(`SELECT "id" FROM "record" WHERE "id" = ?`, recordID) == nil {
		Panic(`Can't call method "record" on an undefined value`)
	}
	r := e.newRecordObject(layout, user)
	r.recordID = recordID
	return r.approvalOfNew()
}
