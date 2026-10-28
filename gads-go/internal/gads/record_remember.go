package gads

// LoadRememberedValues fills a new record with the user's draft, or with
// the values of remembered columns from the last record the user wrote
// (Record::load_remembered_values).
func (r *Record) LoadRememberedValues(instanceID int64) {
	if instanceID != 0 && instanceID != r.layout.InstanceID {
		r.layout = r.layout.Clone(instanceID)
	}
	// First see if there's a draft. If so, use that instead
	if r.user.HasDraft(r.layout.InstanceID) {
		if r.FindDraftuserID(r.user.ID, 0) != nil {
			// Set created date to latest time rather than time draft was
			// saved, in case used in any calculated values
			created := r.layout.ColumnByNameShort("_created")
			r.Fields()[created.ID] = r.InitialiseField(created.ID, nil)
			r.RemoveID()
			return
		}
		r.Initialise(0)
	}
	t := true
	var remember []int64
	for _, c := range r.layout.All(AllOptions{Remember: &t}) {
		remember = append(remember, c.ID)
	}
	if len(remember) == 0 {
		return
	}
	lastRecord, ok := r.e.DB.QueryInt64(`SELECT "me"."record_id" FROM "user_lastrecord" "me" LEFT JOIN "record" "record" ON "record"."id" = "me"."record_id" LEFT JOIN "current" "current" ON "current"."id" = "record"."current_id" WHERE "current"."deleted" IS NULL AND "me"."instance_id" = ? AND "user_id" = ? LIMIT 1`, r.layout.InstanceID, r.user.ID)
	if !ok {
		return
	}
	// When loading the previous record, don't use the permissions of the
	// current user. The record may no longer be available to the user due
	// to view limits. The user won't actually be able to see the record,
	// they will just see values that they previously entered themselves.
	saved := r.e.IgnorePermissions
	r.e.IgnorePermissions = true
	defer func() { r.e.IgnorePermissions = saved }()
	previous := r.e.NewRecord(r.layout, nil)
	previous.SetColumns(remember)
	previous.IncludeApproval = true
	previous.FindRecordID(lastRecord, FindOptions{})
	fields := r.Fields()
	pf := previous.Fields()
	for _, c := range previous.columnsRetrievedDo {
		fields[c.ID] = pf[c.ID].Clone(r, false)
	}
	// The layout's record will have been updated to previous, so set it
	// back to the correct record
	r.layout.SetRecord(r)
	if previous.approvalFlag && previous.approvalRecordID != 0 {
		// The last edited record was one for approval: fill missing values
		// from its associated main record. As Perl, the ID used is that of
		// this record, not the previous one
		child := r.e.NewRecord(r.layout, r.user)
		child.IncludeApproval = true
		if r.approvalRecordID == 0 {
			Fail("Record version ID  not found")
		}
		child.FindRecordID(r.approvalRecordID, FindOptions{})
		cf := child.Fields()
		for _, col := range r.layout.All(AllOptions{UserCanWriteNew: true, Userinput: &t}) {
			if !fields[col.ID].HasValue() && col.Remember {
				fields[col.ID] = cf[col.ID].Clone(r, false)
			}
		}
	}
}

// Clone copies the record as a new record (Record::clone): the values of
// writable columns are copied (without record or current IDs), others are
// initialised empty.
func (r *Record) Clone() *Record {
	cloned := r.e.NewRecord(r.layout, r.user)
	fields := map[int64]Datum{}
	cloned.SetFields(fields)
	for id, d := range r.Fields() {
		if r.layout.Column(id).UserCan("write") {
			n := d.Clone(cloned, true)
			n.base().recordID = 0
			n.base().currentID = 0
			fields[id] = n
		} else {
			fields[id] = r.InitialiseField(id, cloned)
		}
	}
	return cloned
}
