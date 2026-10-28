package gads

import (
	"regexp"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/perl"
)

// WriteOptions are the options of Record::write.
type WriteOptions struct {
	Draft               bool
	DryRun              bool
	NoAlerts            bool
	UpdateOnly          bool
	ForceMandatory      bool
	NoChangeUnlessBlank bool
	AllowUpdate         []int64
	VersionDatetime     *time.Time
	VersionUserID       int64
	MissingNotFatal     bool
	SubmittedFields     []*Column
	SubmissionToken     string
	ReEvaluateOnly      bool
	NoDraftDelete       bool
	NoWriteValues       bool
	Approval            bool
	Old                 bool
	// ForceReadonlyNew allows values of fields the user cannot write to a
	// new record (force_readonly_new)
	ForceReadonlyNew bool
}

// FindUnique finds a record with a unique value in a column.
func (r *Record) FindUnique(col *Column, value string, ignoreCurrentID int64) *Record {
	return r.findUnique(col, value, ignoreCurrentID, nil)
}

// FindUniqueColumns is FindUnique retrieving the given columns
// (retrieve_columns).
func (r *Record) FindUniqueColumns(col *Column, value string, retrieve []int64) *Record {
	return r.findUnique(col, value, 0, retrieve)
}

func (r *Record) findUnique(col *Column, value string, ignoreCurrentID int64, retrieve []int64) *Record {
	if col.ID == r.layout.ColumnID().ID {
		id, _ := anyToInt64(value)
		n := r.e.newRecordObject(r.layout, r.user)
		return n.FindCurrentID(id, FindOptions{})
	}
	if serial := r.layout.ColumnByNameShort("_serial"); col.ID == serial.ID {
		id, _ := anyToInt64(value)
		n := r.e.newRecordObject(r.layout, r.user)
		return n.FindSerialID(id)
	}
	recs := r.e.NewRecords(r.layout, nil)
	var rec *Record
	func() {
		old := r.e.IgnorePermissionsSearch
		r.e.IgnorePermissionsSearch = true
		defer func() { r.e.IgnorePermissionsSearch = old }()
		rec = recs.FindUnique(col, value, ignoreCurrentID, retrieve).Single()
	}()
	r.layout.SetRecord(r)
	if rec != nil {
		rec.user = r.user
	}
	return rec
}

// FindUnique returns a records object searching for a unique value.
func (rs *Records) FindUnique(col *Column, value string, ignoreCurrentID int64, retrieve []int64) *Records {
	rules := []any{map[string]any{
		"field":       col.ID,
		"id":          col.ID,
		"type":        col.Type,
		"value":       value,
		"value_field": col.ValueFieldAsIndex(value),
		"operator":    "equal",
	}}
	if ignoreCurrentID != 0 {
		cid := rs.Layout.ColumnID().ID
		rules = append(rules, map[string]any{
			"field":    cid,
			"id":       cid,
			"type":     "string",
			"value":    ignoreCurrentID,
			"operator": "not_equal",
		})
	}
	view := rs.e.NewTempView(rs.Layout, NewFilterHash(map[string]any{"rules": rules, "condition": "AND"}, nil))
	if len(retrieve) == 0 {
		retrieve = []int64{col.ID}
	}
	n := rs.e.NewRecords(rs.Layout, rs.User)
	n.Rows = 1
	n.View = view
	n.SetColumns(retrieve)
	return n
}

// Exists reports whether the search matches any records (used for unique
// checks with a cached query).
func (rs *Records) Exists(value string) bool {
	// Replace the value of the first rule and run a count
	if rs.View != nil {
		if rules, ok := rs.View.Filter().AsHash()["rules"].([]any); ok && len(rules) > 0 {
			if m, ok := rules[0].(map[string]any); ok {
				m["value"] = value
			}
		}
	}
	rs.Clear()
	return rs.Count() > 0
}

// SubmissionToken returns a submission token for the record form.
func (r *Record) SubmissionToken() string {
	if r.submissionToken == "" {
		for i := 0; i < 10; i++ {
			token := r.e.NewToken(32)
			if _, err := r.e.DB.TryInsertMap("submission", map[string]any{"created": r.e.Now(), "token": token}); err == nil {
				r.submissionToken = token
				break
			}
		}
	}
	return r.submissionToken
}

// SetBlankDependents blanks values of fields hidden by display conditions.
func (r *Record) SetBlankDependents(cols []*Column, submissionToken string) {
	for _, c := range cols {
		d := r.GetFieldValue(c)
		if DependentNotShown(d, false, submissionToken) && (c.CanChild() || r.ParentID() == 0) {
			d.SetValue("", SetValueOptions{})
		}
	}
}

func (r *Record) approverCanActionColumn(c *Column) bool {
	aon := r.approvalOfNew()
	return (aon && c.UserCan("approve_new")) || (!aon && c.UserCan("approve_existing"))
}

func (r *Record) approvalOfNew() bool {
	rid := r.approvalID
	if rid == 0 {
		rid = r.recordID
	}
	row := r.e.DB.QueryRow(`SELECT "record_id" FROM "record" WHERE "id" = ?`, rid)
	if row != nil && row["record_id"] != nil {
		rid, _ = db.AsInt64(row["record_id"])
	}
	n, _ := r.e.DB.QueryInt64(`SELECT COUNT(*) FROM "record" "me" LEFT JOIN "record" "record_previous" ON "record_previous"."current_id" = "me"."current_id" AND "record_previous"."id" < "me"."id" WHERE "me"."id" = ? AND "record_previous"."id" IS NULL`, rid)
	return n > 0
}

// Write writes the record (Record::write).
func (r *Record) Write(o WriteOptions) {
	token := o.SubmissionToken
	o.SubmissionToken = ""
	if token != "" && !o.DryRun {
		row := r.e.DB.QueryRow(`SELECT "token" FROM "submission" WHERE "token" = ? LIMIT 1`, token)
		if row != nil {
			if _, err := r.e.DB.TryInsertMap("submission", map[string]any{"token": token, "created": r.e.Now(), "submitted": 1}); err != nil {
				r.AlreadySubmittedError = true
				Fail("This form has already been submitted and is currently being processed")
			}
		}
	}
	if r.layout.ForgetHistory() {
		o.UpdateOnly = true
	}
	r.e.DB.Txn(func() {
		r.write(o, token)
	})
	if r.IsDraft() == 0 {
		for _, c := range r.layout.AllPeopleNotify() {
			if pd, ok := r.GetFieldValue(c).(*PersonDatum); ok {
				r.sendNotify(pd)
			}
		}
	}
}

func (r *Record) write(o WriteOptions, token string) {
	if o.Draft && !r.NewEntry() {
		Fail("Cannot save draft of existing record")
	}
	if r.NewEntry() && !r.layout.UserCan("write_new") {
		Fail("No permissions to add a new entry")
	}
	if r.ParentID() != 0 {
		if n, _ := r.e.DB.QueryInt64(`SELECT COUNT(*) FROM "current" WHERE "id" = ? AND "parent_id" IS NOT NULL`, r.ParentID()); n > 0 {
			Fail("Cannot create a child record for an existing child record")
		}
	}
	if r.Rewind != nil {
		Fail("Unable to edit record that has been retrieved with rewind")
	}
	cols := o.SubmittedFields
	if cols == nil {
		cols = r.layout.All(AllOptions{Userinput: BoolP(true)})
	}
	if !o.NoChangeUnlessBlank {
		r.SetBlankDependents(cols, token)
	}
	allowUpdate := map[int64]bool{}
	for _, id := range o.AllowUpdate {
		allowUpdate[id] = true
	}
	needApp, needRec, childUnique := false, false, false
	if r.Changed {
		needRec = true
	}
	noWriteTopics := map[int64][]*Column{}
	topicOrder := []int64{}
	for _, c := range cols {
		if o.ReEvaluateOnly {
			continue
		}
		d, ok := r.Fields()[c.ID]
		if !ok || d == nil {
			continue
		}
		b := d.base()
		if (r.ParentID() == 0 || c.CanChild()) && r.LinkedID() == 0 && !c.Optional && d.Blank() && !o.ForceMandatory && !o.Draft && c.UserCan("write") {
			if !DependentNotShown(d, false, token) {
				if tid, prevent := r.e.topicPreventEdit(c); prevent {
					if _, ok := noWriteTopics[tid]; !ok {
						topicOrder = append(topicOrder, tid)
					}
					noWriteTopics[tid] = append(noWriteTopics[tid], c)
				} else {
					if !r.NewEntry() && !b.changed && (b.oldvalue == nil || !DependentNotShown(b.oldvalue, true, "")) {
						r.e.Mistake("'%s' is no longer optional, but was previously blank for this record.", c.Name)
					} else {
						msg := "'" + c.Name + "' is not optional. Please enter a value."
						if !o.MissingNotFatal {
							Fail("%s", msg)
						}
						r.e.ReportError("%s", msg)
					}
				}
			}
		}
		if r.DoingApproval && r.approvalOfNew() {
			if b.changed && !c.UserCan("approve_new") {
				Fail("You do not have permission to approve new values of new records")
			}
		} else if r.DoingApproval {
			if b.changed && !c.UserCan("approve_existing") {
				Fail("You do not have permission to approve edits of existing records")
			}
		} else if r.NewEntry() {
			if !d.Blank() && !c.UserCan("write_new") && !o.ForceReadonlyNew {
				Fail("You do not have permission to add data to field %s", c.Name)
			}
		} else if b.changed && !c.UserCan("write_existing") {
			if d.Blank() && r.ParentID() != 0 {
				needRec = true
			} else {
				Fail("You do not have permission to edit field %s", c.Name)
			}
		}
		if o.NoChangeUnlessBlank && !r.NewEntry() && b.changed && b.oldvalue != nil && !b.oldvalue.Blank() {
			if !allowUpdate[c.ID] && strings.ToLower(b.oldvalue.AsString()) != strings.ToLower(d.AsString()) && b.oldvalue.AsString() != "" {
				Fail(`Attempt to change %s from "%s" to "%s" but no changes are allowed to existing data`, c.Name, b.oldvalue.AsString(), d.AsString())
			}
		}
		if c.IsUnique && !d.Blank() && (r.NewEntry() || b.changed) && !(r.ParentID() != 0 && !c.CanChild()) {
			for _, val := range d.SearchValuesUnique() {
				if found := r.FindUnique(c, val, 0); found != nil {
					if r.NewEntry() || r.currentID != found.currentID {
						Fail(`Field "%s" must be unique but value "%s" already exists in record %d`, c.Name, d.AsString(), found.currentID)
					}
				}
			}
		}
		if r.ParentID() != 0 && !c.CanChild() && c.Userinput() {
			dp := r.Parent().Fields()[c.ID]
			d.SetValue(dp.SetValuesForChild(), SetValueOptions{IsParentValue: true})
		}
		if r.DoingApproval {
			if r.approverCanActionColumn(c) {
				needRec = true
			}
		} else if r.NewEntry() {
			if c.UserCan("write_new_no_approval") || o.Draft {
				needRec = true
			} else if c.UserCan("write_new") {
				needApp = true
				b.isAwaitingApproval = true
			}
		} else if b.changed {
			if c.UserCan("write_existing_no_approval") {
				needRec = true
			} else if c.UserCan("write_existing") {
				needApp = true
				b.isAwaitingApproval = true
			}
		}
		if c.CanChild() {
			childUnique = true
		}
	}
	createdDate := r.e.Now()
	if o.VersionDatetime != nil {
		createdDate = *o.VersionDatetime
	}
	var userID int64
	if r.user != nil {
		userID = r.user.ID
	}
	createdby := userID
	if o.VersionUserID != 0 {
		createdby = o.VersionUserID
	}
	if !o.UpdateOnly || r.layout.ForgetHistory() {
		if et := r.EditedTime(); et != nil {
			et.SetValue(createdDate, SetValueOptions{IsParentValue: true})
		}
		vcol := r.layout.ColumnByNameShort("_version_user")
		if eu := r.EditedUser(); eu != nil {
			eu.SetValue(nullOrID(createdby), SetValueOptions{NoValidation: true, IsParentValue: true})
		} else {
			r.editedUser = r.personDatum(nullOrID(createdby), vcol)
		}
	}
	if r.NewEntry() {
		r.createdUser = r.personDatum(nullOrID(createdby), r.layout.ColumnByNameShort("_created_user"))
		r.createdTime = r.dateDatum(createdDate, r.layout.ColumnByNameShort("_created"))
	}
	// Unique calc values
	for _, c := range r.layout.All(AllOptions{}) {
		if !c.HasCache() || !c.IsUnique {
			continue
		}
		d := r.Fields()[c.ID]
		if d == nil {
			continue
		}
		anyChild := false
		for _, pc := range c.ParamColumns(false) {
			if pc.CanChild() {
				anyChild = true
			}
		}
		if !d.Blank() && (r.NewEntry() || d.base().changed) && (r.ParentID() == 0 || anyChild) {
			d.(CodeDatum).ReEvaluate(ReEvalOptions{})
			for _, val := range d.SearchValuesUnique() {
				if found := r.FindUnique(c, val, 0); found != nil {
					Fail(`Field "%s" must be unique but value "%s" already exists in record %d`, c.Name, d.AsString(), found.currentID)
				}
			}
		}
	}
	for _, tid := range topicOrder {
		for _, col := range r.e.topicFields(r.layout, tid) {
			if f := r.Fields()[col.ID]; f != nil && !f.Blank() {
				var names []string
				for _, c := range noWriteTopics[tid] {
					names = append(names, c.Name)
				}
				Fail("You cannot write to %s until the following fields have been completed: %s", col.Name, strings.Join(names, ", "))
			}
		}
	}
	if r.ParentID() != 0 && !childUnique && r.NewEntry() {
		Fail("There are no child fields defined to be able to create a child record")
	}
	if !(needApp || needRec || o.UpdateOnly) || o.DryRun {
		return
	}
	if r.NewEntry() {
		if !o.NoDraftDelete {
			r.DeleteUserDrafts()
		}
		iid := r.layout.InstanceID
		var draftUser any
		if o.Draft && userID != 0 {
			draftUser = userID
		}
		cur := r.e.DB.InsertMap("current", map[string]any{
			"parent_id":    nullOrID(r.ParentID()),
			"linked_id":    nullOrID(r.LinkedID()),
			"instance_id":  iid,
			"draftuser_id": draftUser,
		})
		for !o.Draft {
			max, _ := r.e.DB.QueryInt64(`SELECT MAX( "serial" ) FROM "current" "me" WHERE "instance_id" = ?`, iid)
			max++
			svp := r.e.DB.SavepointBegin()
			if _, err := r.e.DB.TryExec(`UPDATE "current" SET "serial" = ? WHERE "id" = ?`, max, cur); err != nil {
				r.e.DB.SavepointRollback(svp)
				r.e.DB.SavepointRelease(svp)
				continue
			}
			r.e.DB.SavepointRelease(svp)
			break
		}
		r.currentID = cur
		r.serialLoaded = false
		r.isDraftLoaded = false
	}
	if needRec && !o.UpdateOnly {
		id := r.e.DB.InsertMap("record", map[string]any{
			"current_id": r.currentID,
			"created":    createdDate,
			"createdby":  nullOrID(createdby),
		})
		if r.recordID != 0 {
			r.recordIDOld = r.recordID
		}
		r.recordID = id
		// The link of the current to its latest version
		if _, ok := r.e.DB.QueryInt64(`SELECT "id" FROM "current" WHERE "id" = ?`, r.currentID); !ok {
			Fail(`Can't call method "update" on an undefined value`)
		}
		r.e.DB.Exec(`UPDATE "current" SET "current_version_id" = ? WHERE "id" = ?`, id, r.currentID)
	} else if r.layout.ForgetHistory() {
		r.e.DB.Exec(`UPDATE "record" SET "created" = ?, "createdby" = ? WHERE "id" = ?`, createdDate, nullOrID(createdby), r.recordID)
	}
	idd := r.Fields()[r.layout.ColumnID().ID].(*IDDatum)
	idd.currentID = r.currentID
	if needApp {
		r.approvalID = r.e.DB.InsertMap("record", map[string]any{
			"current_id": r.currentID,
			"created":    r.e.Now(),
			"record_id":  nullOrID(r.recordID),
			"approval":   1,
			"createdby":  nullOrID(userID),
		})
	}
	if r.NewEntry() && userID != 0 && !o.Draft {
		id := r.approvalID
		if id == 0 {
			id = r.recordID
		}
		if lr, ok := r.e.DB.QueryInt64(`SELECT "id" FROM "user_lastrecord" WHERE "user_id" = ? AND "instance_id" = ?`, userID, r.layout.InstanceID); ok {
			r.e.DB.Exec(`UPDATE "user_lastrecord" SET "record_id" = ? WHERE "id" = ?`, id, lr)
		} else {
			r.e.DB.InsertMap("user_lastrecord", map[string]any{"user_id": userID, "instance_id": r.layout.InstanceID, "record_id": id})
		}
	}
	r.needRec, r.needApp, r.needSet = needRec, needApp, true
	HooksInstance.RunHook("record.write.before_write_values", r, r.layout)
	if !o.NoWriteValues {
		r.WriteValues(o, token)
	}
	if token != "" {
		if sid, ok := r.e.DB.QueryInt64(`SELECT "id" FROM "submission" WHERE "token" = ? ORDER BY "id" LIMIT 1`, token); ok {
			r.e.DB.Exec(`DELETE FROM "filtered_value" WHERE "submission_id" = ?`, sid)
		}
	}
}

func nullOrID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// WriteValues writes all values of the record (Record::write_values).
func (r *Record) WriteValues(o WriteOptions, token string) {
	if !r.needSet {
		Panic("Called out of order - need_app and need_rec not set")
	}
	var columnsChanged []*Column
	updateAutocurs := map[int64][]int64{}
	var updateOrder []int64
	r.e.DB.Txn(func() {
		for _, c := range r.layout.All(AllOptions{OrderDependencies: true}) {
			if c == nil {
				continue
			}
			if o.ReEvaluateOnly && c.Userinput() {
				continue
			}
			if o.Draft && !c.Userinput() {
				continue
			}
			if c.NameShort == "_serial" {
				continue
			}
			d := r.GetFieldValue(c)
			if r.LinkedID() != 0 && c.LinkParent() != nil {
				continue
			}
			if c.Internal {
				if d != nil && d.base().changed {
					columnsChanged = append(columnsChanged, c)
				}
				continue
			}
			if r.needRec || o.UpdateOnly {
				if r.DoingApproval {
					r.fieldWrite(c, d, o, token)
					if !r.approverCanActionColumn(c) {
						continue
					}
					r.e.DB.Exec(`DELETE FROM `+q(c.Table())+` WHERE "record_id" = ? AND "layout_id" = ?`, r.approvalID, c.ID)
				} else {
					if (r.NewEntry() && c.UserCan("write_new_no_approval")) || (!r.NewEntry() && c.UserCan("write_existing_no_approval")) || !c.Userinput() {
						r.fieldWrite(c, d, o, token)
						if d.base().changed {
							columnsChanged = append(columnsChanged, c)
						}
					} else if r.NewEntry() {
						if !c.Userinput() {
							r.fieldWrite(c, d, WriteOptions{}, token)
						}
					} else if c.UserCan("write") {
						if o.UpdateOnly {
							Panic("update_only set but attempt to hold write for approval")
						}
						oo := WriteOptions{Old: true}
						r.fieldWrite(c, d, oo, token)
					} else {
						r.fieldWrite(c, d, o, token)
					}
				}
			}
			if r.needApp {
				if !d.base().isAwaitingApproval {
					continue
				}
				if (r.NewEntry() && !d.Blank()) || (!r.NewEntry() && d.base().changed) {
					r.fieldWrite(c, d, WriteOptions{Approval: true}, token)
				}
			}
		}
		// Autocur updates
		for _, c := range r.layout.All(AllOptions{Type: "curval"}) {
			if r.ParentID() != 0 && !c.CanChild() {
				continue
			}
			for _, autocur := range c.Autocurs() {
				d := r.Fields()[c.ID].(*CurcommonDatum)
				deleted := map[int64]bool{}
				for _, id := range d.idsDeleted {
					deleted[id] = true
				}
				affected := map[int64]bool{}
				for _, id := range d.IDsAffected() {
					if !deleted[id] {
						affected[id] = true
					}
				}
				for _, ld := range r.e.DB.Query(`SELECT "layout_id" FROM "layout_depend" WHERE "depends_on" = ?`, autocur.ID) {
					lid, _ := db.AsInt64(ld["layout_id"])
					dependsOn := autocur
					if rf := dependsOn.RelatedField(); rf == nil || rf.InstanceID != r.layout.InstanceID {
						continue
					}
					code := ""
					if lc := r.layout.Column(lid); lc != nil {
						code = lc.CodeFn()
					}
					match := false
					for _, cc := range columnsChanged {
						if cc.NameShort == "" {
							continue
						}
						re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(cc.NameShort) + `(?:[^_a-z0-9]|$)`)
						if re.MatchString(code) {
							match = true
						}
					}
					if !match {
						continue
					}
					for id := range affected {
						if _, ok := updateAutocurs[id]; !ok {
							updateAutocurs[id] = []int64{}
							updateOrder = append(updateOrder, id)
						}
					}
				}
				if !d.base().changed {
					continue
				}
				for _, cid := range d.IDsChanged() {
					if deleted[cid] {
						continue
					}
					if _, ok := updateAutocurs[cid]; !ok {
						updateOrder = append(updateOrder, cid)
					}
					updateAutocurs[cid] = append(updateAutocurs[cid], autocur.ID)
				}
			}
		}
		if r.DoingApproval {
			remaining := false
			for _, t := range []string{"string", "intgr", "person", "date", "daterange", "file", "enum"} {
				if n, _ := r.e.DB.QueryInt64(`SELECT COUNT(*) FROM `+q(t)+` WHERE "record_id" = ?`, r.approvalID); n > 0 {
					remaining = true
					break
				}
			}
			if !remaining {
				r.e.DB.Exec(`UPDATE "user_lastrecord" SET "record_id" = ? WHERE "record_id" = ?`, r.recordID, r.approvalID)
				r.e.DB.Exec(`DELETE FROM "record" WHERE "id" = ?`, r.approvalID)
			}
		}
		if !o.Draft {
			for _, childID := range r.ChildRecordIDs() {
				child := r.e.newRecordObject(r.layout, r.user)
				child.FindCurrentID(childID, FindOptions{})
				for _, c := range r.layout.All(AllOptions{OrderDependencies: true, ExcludeInternal: true}) {
					if c == nil {
						continue
					}
					dc := child.Fields()[c.ID]
					if c.Userinput() && !c.CanChild() {
						dp := r.Fields()[c.ID]
						dc.SetValue(dp.SetValuesForChild(), SetValueOptions{IsParentValue: true})
					}
				}
				co := o
				co.UpdateOnly = true
				child.Write(co)
				r.layout.SetRecord(r)
			}
			var update []int64
			for _, cid := range updateOrder {
				skip := false
				for _, w := range r.recordsToWriteAfter {
					if w.currentID == cid {
						skip = true
					}
				}
				if !skip {
					update = append(update, cid)
				}
			}
			byInstance := map[int64][]int64{}
			var instOrder []int64
			for _, cid := range update {
				iid, ok := r.e.DB.QueryInt64(`SELECT "instance_id" FROM "current" WHERE "id" = ?`, cid)
				if !ok {
					continue
				}
				if _, ok := byInstance[iid]; !ok {
					instOrder = append(instOrder, iid)
				}
				byInstance[iid] = append(byInstance[iid], cid)
			}
			for _, iid := range instOrder {
				func() {
					old := r.e.IgnorePermissions
					r.e.IgnorePermissions = true
					defer func() { r.e.IgnorePermissions = old }()
					layout := r.layout.Clone(iid)
					recs := r.e.NewRecords(layout, nil)
					recs.SetLimitCurrentIDs(byInstance[iid])
					for {
						rec := recs.Single()
						if rec == nil {
							break
						}
						for _, aid := range updateAutocurs[rec.currentID] {
							if d := rec.Fields()[aid]; d != nil {
								d.base().changed = true
							}
						}
						ro := o
						ro.UpdateOnly = true
						ro.ReEvaluateOnly = true
						rec.Write(ro)
					}
				}()
			}
		}
		for _, w := range r.recordsToWriteAfter {
			w.WriteValues(o, "")
		}
		r.recordsToWriteAfter = nil
	})
	if !o.NoAlerts && !o.Draft && r.e.AlertSender != nil {
		var ids []int64
		for _, c := range columnsChanged {
			ids = append(ids, c.ID)
		}
		r.e.AlertSender(r.e, r.layout, []int64{r.currentID}, ids, r.NewEntry())
		if r.e.NoFork {
			return
		}
	}
	r.clearNewEntry()
	r.newEntrySet = true
	r.newEntry = false
	r.needSet = false
}

// fieldWrite writes the value of a single field (Record::_field_write).
func (r *Record) fieldWrite(c *Column, d Datum, o WriteOptions, token string) {
	if c.NoValueToWrite() {
		return
	}
	if !c.Userinput() {
		datumSetRecordID(d, r.recordID)
		cd := d.(CodeDatum)
		cd.ReEvaluate(ReEvalOptions{SubmissionToken: token, NewEntry: r.NewEntry()})
		if c.ReturnTypeFn() == "error" && d.AsString() != "" {
			Fail("%s", d.AsString())
		}
		if fv, ok := d.(*CurcommonDatum); ok && c.Type == "filval" {
			fv.writeFilval(token)
			return
		}
		cd.WriteValue()
		return
	}
	write := d
	if o.Old {
		write = d.base().oldvalue
	}
	table := c.Table()
	childUnique := 0
	if d != nil && c.CanChild() {
		childUnique = 1
	}
	recordID := r.recordID
	if o.Approval {
		recordID = r.approvalID
	}
	type entry map[string]any
	base := func() entry { return entry{"child_unique": childUnique, "layout_id": c.ID, "record_id": recordID} }
	var entries []entry
	if write != nil {
		switch c.Type {
		case "daterange":
			dd := write.(*DaterangeDatum)
			if len(dd.Values()) == 0 {
				e := base()
				e["from"], e["to"], e["value"] = nil, nil, nil
				entries = append(entries, e)
			}
			texts := dd.TextAll()
			for i, v := range dd.Values() {
				e := base()
				// Date columns, stored without a time as DBIC does
				e["from"] = db.Date(v.Start)
				e["to"] = db.Date(v.End)
				if i < len(texts) {
					e["value"] = texts[i]
				} else {
					e["value"] = nil
				}
				entries = append(entries, e)
			}
		case "file", "enum", "tree", "person", "curval", "filval":
			if c.Type == "curval" && c.ShowAdd() {
				cd := write.(*CurcommonDatum)
				for _, rec := range cd.ValuesAsQueryRecords() {
					created := r.GetFieldValue(r.layout.ColumnByNameShort("_version_datetime")).(*DateDatum).Values()
					var vt *time.Time
					if len(created) > 0 {
						vt = &created[0]
					}
					ro := o
					ro.NoDraftDelete = true
					ro.NoWriteValues = true
					ro.SubmissionToken = ""
					ro.VersionDatetime = vt
					rec.Write(ro)
					if rec.IsEdited() {
						r.recordsToWriteAfter = append(r.recordsToWriteAfter, rec)
					}
					e := base()
					e["value"] = rec.currentID
					entries = append(entries, e)
				}
			}
			ids := datumIDs(write)
			if c.Type == "curval" && !r.NewEntry() && write.Column().MultivalueFn() {
				cd := write.(*CurcommonDatum)
				var oldIDs []int64
				if cd.base().changed && cd.base().oldvalue != nil {
					oldIDs = cd.base().oldvalue.(*CurcommonDatum).IDs()
				} else {
					oldIDs = append(oldIDs, cd.IDs()...)
					for _, qr := range cd.ValuesAsQueryRecords() {
						oldIDs = append(oldIDs, qr.currentID)
					}
				}
				args := []any{c.ID, r.recordIDOld}
				sql := `SELECT "value" FROM "curval" WHERE "layout_id" = ? AND "record_id" = ?`
				excl := append(append([]int64{}, oldIDs...), ids...)
				if len(oldIDs) > 0 {
					var parts []string
					for _, id := range excl {
						parts = append(parts, `"value" != ?`)
						args = append(args, id)
					}
					sql += " AND ( " + strings.Join(parts, " AND ") + " )"
				}
				ids = append(ids, r.e.DB.QueryInt64s(sql, args...)...)
			}
			for _, id := range ids {
				e := base()
				e["value"] = id
				entries = append(entries, e)
			}
			if c.Type == "curval" && c.DeleteNotUsed() {
				cd := d.(*CurcommonDatum)
				var deleted []int64
				for _, idDeleted := range cd.IDsRemoved() {
					used := false
					for _, refers := range c.LayoutParent().ReferredBy() {
						rl := r.e.NewLayout(refers.InstanceID, r.layout.User())
						view := r.e.NewTempView(rl, NewFilterHash(map[string]any{"rules": []any{map[string]any{
							"id": refers.ID, "type": "string", "value": idDeleted, "operator": "equal",
						}}}, nil))
						func() {
							old := r.e.IgnorePermissions
							r.e.IgnorePermissions = true
							defer func() { r.e.IgnorePermissions = old }()
							rr := r.e.NewRecords(rl, nil)
							rr.View = view
							rr.SetColumns([]int64{})
							used = rr.Count() > 0
						}()
						if used {
							break
						}
					}
					if !used {
						rec := r.e.newRecordObject(c.LayoutParent(), nil)
						rec.FindCurrentID(idDeleted, FindOptions{})
						var by int64
						if r.user != nil {
							by = r.user.ID
						}
						rec.DeleteCurrent(true, by)
						deleted = append(deleted, idDeleted)
					}
				}
				cd.idsDeleted = deleted
			}
			if len(entries) == 0 {
				e := base()
				e["value"] = nil
				entries = append(entries, e)
			}
		case "string":
			sd := write.(*StringDatum)
			if len(sd.Values()) == 0 {
				e := base()
				e["value"], e["value_index"] = nil, nil
				entries = append(entries, e)
			}
			for _, v := range sd.Values() {
				e := base()
				if v.ok {
					e["value"] = v.s
					e["value_index"] = perl.Lc(perl.Substr(v.s, 128))
				} else {
					e["value"] = nil
				}
				entries = append(entries, e)
			}
		case "date":
			dd := write.(*DateDatum)
			if len(dd.Values()) == 0 {
				e := base()
				e["value"] = nil
				entries = append(entries, e)
			}
			for _, v := range dd.Values() {
				e := base()
				e["value"] = db.Date(v)
				entries = append(entries, e)
			}
		case "intgr":
			id := write.(*IntegerDatum)
			if len(id.Values()) == 0 {
				e := base()
				e["value"] = nil
				entries = append(entries, e)
			}
			for _, v := range id.Values() {
				e := base()
				e["value"] = intDBValue(v)
				entries = append(entries, e)
			}
		default:
			Panic("Unknown column type: %s", c.Type)
		}
	}
	if o.UpdateOnly {
		rows := r.e.DB.QueryInt64s(`SELECT "id" FROM `+q(table)+` WHERE "record_id" = ? AND "layout_id" = ? ORDER BY "id"`, recordID, c.ID)
		for _, rid := range rows {
			if len(entries) > 0 {
				e := entries[len(entries)-1]
				entries = entries[:len(entries)-1]
				r.e.DB.UpdateMap(table, map[string]any(e), map[string]any{"id": rid})
			} else {
				r.e.DB.Exec(`DELETE FROM `+q(table)+` WHERE "id" = ?`, rid)
			}
		}
	}
	for _, e := range entries {
		r.e.DB.InsertMap(table, map[string]any(e))
	}
}

func intDBValue(v any) any {
	if v == nil {
		return nil
	}
	s := anyToString(v)
	if i, ok := anyToInt64(s); ok {
		return i
	}
	if perl.LooksLikeNumber(s) {
		return perl.Numify(s)
	}
	return s
}

func datumIDs(d Datum) []int64 {
	switch x := d.(type) {
	case *EnumDatum:
		return x.IDs()
	case *TreeDatum:
		return x.IDs()
	case *PersonDatum:
		return x.IDs()
	case *FileDatum:
		return x.IDs()
	case *CurcommonDatum:
		return x.IDs()
	}
	return nil
}

func (e *Engine) topicPreventEdit(c *Column) (int64, bool) {
	if c.TopicID == nil {
		return 0, false
	}
	row := e.DB.QueryRow(`SELECT "prevent_edit_topic_id" FROM "topic" WHERE "id" = ?`, *c.TopicID)
	if row == nil || row["prevent_edit_topic_id"] == nil {
		return 0, false
	}
	id, _ := db.AsInt64(row["prevent_edit_topic_id"])
	return id, true
}

func (e *Engine) topicFields(layout *Layout, topicID int64) []*Column {
	var out []*Column
	for _, c := range layout.All(AllOptions{}) {
		if c.TopicID != nil && *c.TopicID == topicID {
			out = append(out, c)
		}
	}
	return out
}

// DeleteUserDrafts deletes the user's drafts.
func (r *Record) DeleteUserDrafts() {
	if r.user == nil || !r.user.HasDraft(r.layout.InstanceID) {
		return
	}
	for {
		// Permissions are ignored for the whole of each iteration, as Perl
		// localises IGNORE_PERMISSIONS in the loop body
		found := func() bool {
			old := r.e.IgnorePermissions
			r.e.IgnorePermissions = true
			defer func() { r.e.IgnorePermissions = old }()
			draft := r.e.newRecordObject(r.layout, nil)
			draft = draft.FindDraftuserID(r.user.ID, r.layout.InstanceID)
			if draft == nil {
				return false
			}
			// Find and delete any draft subrecords associated with this draft
			var purge []*CurcommonDatum
			for _, c := range draft.layout.All(AllOptions{Type: "curval"}) {
				if d, ok := draft.Fields()[c.ID].(*CurcommonDatum); ok {
					purge = append(purge, d)
				}
			}
			draft.DeleteCurrent(true, 0)
			draft.PurgeCurrent()
			for _, p := range purge {
				p.PurgeDrafts()
			}
			return true
		}()
		if !found {
			break
		}
	}
	r.layout.SetRecord(r)
}

// DeleteCurrent marks the record as deleted.
func (r *Record) DeleteCurrent(override bool, deletedby int64) {
	if !override && r.user != nil && !r.UserCanDelete() {
		Fail("You do not have permission to delete records")
	}
	if _, ok := r.e.DB.QueryInt64(`SELECT "id" FROM "current" WHERE "id" = ? AND "instance_id" = ?`, r.currentID, r.layout.InstanceID); !ok {
		Fail("Unable to find current record to delete")
	}
	by := deletedby
	if by == 0 && r.user != nil {
		by = r.user.ID
	}
	r.e.DB.Exec(`UPDATE "current" SET "deleted" = ?, "deletedby" = ? WHERE "id" = ?`, r.e.Now(), nullOrID(by), r.currentID)
}

// UserCanDelete reports delete permission.
func (r *Record) UserCanDelete() bool {
	if r.currentID == 0 {
		return false
	}
	return r.layout.UserCan("delete")
}

// UserCanPurge reports purge permission.
func (r *Record) UserCanPurge() bool {
	if r.currentID == 0 {
		return false
	}
	return r.layout.UserCan("purge")
}

// Restore undeletes the record.
func (r *Record) Restore() {
	if r.user != nil && !r.UserCanPurge() {
		Fail("You do not have permission to restore records")
	}
	r.e.DB.Exec(`UPDATE "current" SET "deleted" = NULL WHERE "id" = ?`, r.currentID)
}

// Purge deletes this version completely.
func (r *Record) Purge() {
	if r.user != nil && !r.UserCanPurge() {
		Fail("You do not have permission to purge records")
	}
	r.e.DB.Txn(func() {
		r.purgeRecordValues(r.recordID)
		cur := r.e.DB.QueryRow(`SELECT "current"."id", "current"."current_version_id" FROM "record" "me" JOIN "current" "current" ON "current"."id" = "me"."current_id" WHERE "me"."id" = ?`, r.recordID)
		if cur == nil {
			Fail(`Can't call method "current" on an undefined value`)
		}
		cvid, _ := db.AsInt64(cur["current_version_id"])
		if cur["current_version_id"] != nil && cvid == r.recordID {
			// The latest version: the current is linked to the previous one
			n, _ := r.e.DB.QueryInt64(`SELECT COUNT( * ) FROM "record" WHERE "current_id" = ?`, cur["id"])
			if n < 2 {
				Fail("Unable to delete this version as there are none others")
			}
			prev, _ := r.e.DB.QueryInt64(`SELECT "me"."id" FROM "record" "me" WHERE "me"."current_id" = ? AND "me"."id" != ? ORDER BY "me"."created" DESC, "me"."id" DESC LIMIT 1`, cur["id"], r.recordID)
			r.e.DB.Exec(`UPDATE "current" SET "current_version_id" = ? WHERE "id" = ?`, prev, cur["id"])
		}
		r.e.DB.Exec(`DELETE FROM "record" WHERE "id" = ?`, r.recordID)
	})
}

// PurgeCurrent deletes the record and all its versions.
func (r *Record) PurgeCurrent() {
	if r.user != nil && !r.UserCanPurge() {
		Fail("You do not have permission to purge records")
	}
	id := r.currentID
	if id == 0 {
		Panic("No current_id specified for purge")
	}
	row := r.e.DB.QueryRow(`SELECT "deleted" FROM "current" WHERE "id" = ? AND "instance_id" = ?`, id, r.layout.InstanceID)
	if row == nil {
		Fail("Invalid ID %d", id)
	}
	if row["deleted"] == nil {
		Fail("Cannot purge record that is not already deleted")
	}
	refs := r.e.DB.Query(`SELECT "current"."id" AS "cid", "layout"."name" AS "lname" FROM "curval" "c" JOIN "record" "record" ON "record"."id" = "c"."record_id" JOIN "current" "current" ON "current"."id" = "record"."current_id" JOIN "layout" "layout" ON "layout"."id" = "c"."layout_id" WHERE "c"."value" = ? ORDER BY "current"."id", "layout"."name"`, id)
	if len(refs) > 0 {
		var parts []string
		names := map[int64][]string{}
		var order []int64
		for _, ref := range refs {
			cid, _ := db.AsInt64(ref["cid"])
			n, _ := db.AsString(ref["lname"])
			if _, ok := names[cid]; !ok {
				order = append(order, cid)
			}
			dup := false
			for _, x := range names[cid] {
				if x == n {
					dup = true
				}
			}
			if !dup {
				names[cid] = append(names[cid], n)
			}
		}
		for _, cid := range order {
			parts = append(parts, itoa(cid)+" ("+strings.Join(names[cid], ", ")+")")
		}
		Fail("The following records refer to this record as a value (possibly in a historical version): %s", strings.Join(parts, ", "))
	}
	r.e.DB.Exec(`DELETE FROM "filtered_value" WHERE "current_id" = ?`, id)
	records := r.e.DB.QueryInt64s(`SELECT "id" FROM "record" WHERE "current_id" = ? ORDER BY "id"`, id)
	r.e.DB.Txn(func() {
		for _, child := range r.ChildRecordIDs() {
			cr := r.e.newRecordObject(r.layout, r.user)
			cr.FindCurrentID(child, FindOptions{})
			cr.PurgeCurrent()
		}
		for _, rid := range records {
			r.purgeRecordValues(rid)
		}
		r.e.DB.Exec(`UPDATE "record" SET "record_id" = NULL WHERE "current_id" = ?`, id)
		r.e.DB.Exec(`DELETE FROM "alert_cache" WHERE "current_id" = ?`, id)
		r.e.DB.Exec(`UPDATE "current" SET "current_version_id" = NULL WHERE "id" = ?`, id)
		r.e.DB.Exec(`DELETE FROM "record" WHERE "current_id" = ?`, id)
		r.e.DB.Exec(`DELETE FROM "alert_send" WHERE "current_id" = ?`, id)
		r.e.DB.Exec(`DELETE FROM "current" WHERE "id" = ?`, id)
	})
}

func (r *Record) purgeRecordValues(rid int64) {
	for _, t := range []string{"ragval", "calcval", "enum", "string", "intgr", "daterange", "date", "person", "file", "curval"} {
		r.e.DB.Exec(`DELETE FROM `+q(t)+` WHERE "record_id" = ?`, rid)
	}
	r.e.DB.Exec(`DELETE FROM "user_lastrecord" WHERE "record_id" = ?`, rid)
}

// WriteLinkedID links the record to another record.
func (r *Record) WriteLinkedID(linkedID int64) {
	if !r.layout.UserCan("link") {
		Fail("You do not have permission to link records")
	}
	r.e.DB.Txn(func() {
		if linkedID != 0 {
			for _, c := range r.layout.All(AllOptions{Linked: true}) {
				r.Fields()[c.ID].SetValue("", SetValueOptions{})
			}
			r.Write(WriteOptions{NoAlerts: true})
		}
		r.e.DB.Exec(`UPDATE "current" SET "linked_id" = ? WHERE "id" = ?`, nullOrID(linkedID), r.currentID)
		r.SetLinkedID(linkedID)
	})
}

func (r *Record) sendNotify(d *PersonDatum) {
	if d.Blank() || !d.base().changed || r.e.Emailer == nil {
		return
	}
	col := d.Column()
	subject := anyToString(col.opt("notify_on_selection_subject"))
	text := anyToString(col.opt("notify_on_selection_message"))
	re := regexp.MustCompile(`(\$[a-z0-9_]+)\b`)
	replace := func(s string) string {
		return re.ReplaceAllStringFunc(s, func(v string) string {
			name := strings.TrimPrefix(v, "$")
			if name == "_link" {
				return v
			}
			c := r.layout.ColumnByNameShort(name)
			if c == nil {
				return v
			}
			return r.GetFieldValue(c).AsString()
		})
	}
	subject = replace(subject)
	text = replace(text)
	link := r.e.Config.URL + "/record/" + itoa(r.currentID)
	subject = strings.ReplaceAll(subject, "$_link", link)
	html := strings.ReplaceAll(htmlEscape(text), "$_link", `<a href="`+link+`">`+itoa(r.currentID)+`</a>`)
	text = strings.ReplaceAll(text, "$_link", link)
	for _, v := range d.ValueHash() {
		r.e.SendEmail(Email{Subject: subject, To: []string{anyToString(v["email"])}, Text: text, HTML: html})
	}
}
