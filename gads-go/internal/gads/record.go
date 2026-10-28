package gads

import (
	"crypto/rand"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
)

// Record is a port of GADS::Record: a single record (version) of a table.
type Record struct {
	e      *Engine
	layout *Layout
	user   *User

	currentID      int64
	recordID       int64
	recordIDOld    int64
	newEntrySet    bool
	newEntry       bool
	serial         *int64
	serialLoaded   bool
	parentID       int64
	parentLoaded   bool
	parent         *Record
	linkedID       int64
	linkedLoaded   bool
	linkedRecordID int64
	isDraft        int64
	isDraftLoaded  bool
	childRecords   []int64
	childIDs       []int64
	childLoaded    bool

	raw         map[int64][]rawValue
	hasRaw      bool
	fields      map[int64]Datum
	fieldsBuilt bool

	columnsRetrievedNo []*Column
	columnsRetrievedDo []*Column
	columnsSelected    []*Column
	columnsRender      []*Column
	chronology         []ChronologyEntry
	columnsOption      []int64
	hasColumnsOption   bool

	Rewind           *time.Time
	setDeleted       any
	setDeletedby     map[string]any
	setRecordCreated any
	createdRaw       any
	hasCreatedRaw    bool
	createdbyFetched bool
	// valuesRewind is the time of the values of a record retrieved by
	// Records with a rewind (for curval values not retrieved with it)
	valuesRewind *time.Time // the version createdby was retrieved with the record (Records results)

	editedTime  *DateDatum
	editedUser  *PersonDatum
	createdTime *DateDatum
	createdUser *PersonDatum
	now         *time.Time

	approvalID       int64
	approvalFlag     bool
	approvalRecordID int64
	DoingApproval    bool
	IncludeApproval  bool
	InitNoValue      bool
	Changed          bool

	needRec, needApp, needSet bool
	recordsToWriteAfter       []*Record
	submissionToken           string
	selectorID                string
	AlreadySubmittedError     bool

	isGroup            bool
	groupCols          map[int64]bool
	idCount            any
	groupRow           db.Row
	records            *Records // the Records this (grouped) record belongs to
	columnsRecalcExtra []*Column
}

func (e *Engine) newRecordObject(layout *Layout, user *User) *Record {
	r := &Record{e: e, layout: layout, user: user, InitNoValue: true}
	return r
}

// NewRecord creates a new record object for a layout and user.
func (e *Engine) NewRecord(layout *Layout, user *User) *Record {
	r := e.newRecordObject(layout, user)
	layout.SetRecord(r)
	return r
}

// Layout returns the record layout.
func (r *Record) Layout() *Layout { return r.layout }

// User returns the record user.
func (r *Record) User() *User { return r.user }

// SetUser sets the record user.
func (r *Record) SetUser(u *User) { r.user = u }

// CurrentID returns the current ID (0 if new).
func (r *Record) CurrentID() int64 { return r.currentID }

// RecordID returns the record (version) ID.
func (r *Record) RecordID() int64 { return r.recordID }

// NewEntry reports a record not yet written.
func (r *Record) NewEntry() bool {
	if !r.newEntrySet {
		r.newEntry = r.currentID == 0
		r.newEntrySet = true
	}
	return r.newEntry
}

func (r *Record) clearNewEntry() { r.newEntrySet = false }

// SetColumns sets the columns to retrieve when finding.
func (r *Record) SetColumns(ids []int64) {
	r.columnsOption = ids
	r.hasColumnsOption = true
}

// ParentID returns the parent current ID (child records).
func (r *Record) ParentID() int64 {
	if !r.parentLoaded {
		r.parentLoaded = true
		if r.currentID != 0 {
			if v, ok := r.e.DB.QueryInt64(`SELECT "parent_id" FROM "current" WHERE "id" = ?`, r.currentID); ok {
				r.parentID = v
			}
		}
	}
	return r.parentID
}

// SetParentID sets the parent.
func (r *Record) SetParentID(id int64) {
	r.parentID = id
	r.parentLoaded = true
	r.parent = nil
}

// Parent returns the parent record.
func (r *Record) Parent() *Record {
	if r.parent != nil {
		return r.parent
	}
	if r.ParentID() == 0 {
		return nil
	}
	p := r.e.newRecordObject(r.layout, r.user)
	p.FindCurrentID(r.ParentID(), FindOptions{})
	r.layout.SetRecord(r)
	r.parent = p
	return p
}

// LinkedID returns the linked current ID.
func (r *Record) LinkedID() int64 {
	if !r.linkedLoaded {
		r.linkedLoaded = true
		if r.currentID != 0 {
			if v, ok := r.e.DB.QueryInt64(`SELECT "linked_id" FROM "current" WHERE "id" = ?`, r.currentID); ok {
				r.linkedID = v
			}
		}
	}
	return r.linkedID
}

// SetLinkedID sets the linked ID.
func (r *Record) SetLinkedID(id int64) {
	r.linkedID = id
	r.linkedLoaded = true
}

// ChildRecordIDs returns IDs of active child records.
// ColumnsRender returns the columns to render, as set by the Records the
// record was retrieved from.
func (r *Record) ColumnsRender() []*Column { return r.columnsRender }

func (r *Record) ChildRecordIDs() []int64 {
	if !r.childLoaded {
		r.childLoaded = true
		r.childIDs = nil
		if r.ParentID() == 0 && r.currentID != 0 {
			r.childIDs = r.e.DB.QueryInt64s(`SELECT "id" FROM "current" "me" WHERE "deleted" IS NULL AND "draftuser_id" IS NULL AND "parent_id" = ? ORDER BY "id"`, r.currentID)
		}
	}
	return r.childIDs
}

// Serial returns the record serial.
func (r *Record) Serial() *int64 {
	if !r.serialLoaded {
		r.serialLoaded = true
		if r.currentID != 0 {
			row := r.e.DB.QueryRow(`SELECT "serial" FROM "current" WHERE "id" = ?`, r.currentID)
			if row != nil {
				r.serial = optInt(row["serial"])
			}
		}
	}
	return r.serial
}

// IsDraft returns the draft user ID if a draft.
func (r *Record) IsDraft() int64 {
	if !r.isDraftLoaded {
		r.isDraftLoaded = true
		if r.NewEntry() {
			r.isDraft = 0
		} else if v, ok := r.e.DB.QueryInt64(`SELECT "draftuser_id" FROM "current" WHERE "id" = ?`, r.currentID); ok {
			r.isDraft = v
		}
	}
	return r.isDraft
}

// Deleted returns the deletion time.
func (r *Record) Deleted() *time.Time {
	if r.NewEntry() {
		return nil
	}
	if !r.hasRaw {
		row := r.e.DB.QueryRow(`SELECT "deleted" FROM "current" WHERE "id" = ?`, r.currentID)
		if row == nil {
			return nil
		}
		if t, ok := db.AsTime(row["deleted"]); ok && row["deleted"] != nil {
			return &t
		}
		return nil
	}
	if r.setDeleted == nil {
		return nil
	}
	if t, ok := db.AsTime(r.setDeleted); ok {
		return &t
	}
	return nil
}

func (r *Record) nowTime() time.Time {
	if r.now == nil {
		t := r.e.Now()
		r.now = &t
	}
	return *r.now
}

func (r *Record) dateDatum(v any, col *Column) *DateDatum {
	d := &DateDatum{}
	d.record, d.recordID, d.currentID, d.column = r, r.recordID, r.currentID, col
	d.initValue = []rawValue{{Value: v}}
	d.hasInitValue = true
	return d
}

func (r *Record) personDatum(v any, col *Column) *PersonDatum {
	d := &PersonDatum{}
	d.record, d.recordID, d.currentID, d.column = r, r.recordID, r.currentID, col
	switch x := v.(type) {
	case map[string]any:
		d.initValue = []rawValue{{User: x}}
		d.hasInitValue = true
	case nil:
	default:
		id, _ := anyToInt64(x)
		if id != 0 {
			d.ids = []int64{id}
			d.idsSet = true
		}
	}
	return d
}

func (r *Record) firstRecordRow() db.Row {
	return r.e.DB.QueryRow(`SELECT * FROM "record" WHERE "current_id" = ? ORDER BY "created" ASC LIMIT 1`, r.currentID)
}

// EditedTime returns the _version_datetime datum.
func (r *Record) EditedTime() *DateDatum {
	if r.editedTime == nil {
		col := r.layout.ColumnByNameShort("_version_datetime")
		if r.NewEntry() {
			r.editedTime = r.dateDatum(r.nowTime(), col)
		} else {
			var v any
			if r.hasCreatedRaw {
				v = r.createdRaw
			} else {
				row := r.e.DB.QueryRow(`SELECT "created" FROM "record" WHERE "id" = ?`, r.recordID)
				if row != nil {
					v = row["created"]
				}
			}
			r.editedTime = r.dateDatum(v, col)
		}
	}
	return r.editedTime
}

// EditedUser returns the _version_user datum.
func (r *Record) EditedUser() *PersonDatum {
	if r.editedUser == nil {
		col := r.layout.ColumnByNameShort("_version_user")
		if r.NewEntry() {
			// Default to current user drafting record if new record
			r.editedUser = r.personDatum(r.currentUserID(), col)
			return r.editedUser
		}
		if r.hasRaw && r.createdbyFetched {
			// Retrieved as part of the query: the value is only there if
			// the column was prefetched
			var v any
			if vals := r.raw[col.ID]; len(vals) > 0 && vals[0].User != nil {
				v = vals[0].User
			}
			r.editedUser = r.personDatum(v, col)
		} else {
			row := r.e.DB.QueryRow(`SELECT "createdby" FROM "record" WHERE "id" = ?`, r.recordID)
			var v any
			if row != nil {
				v = row["createdby"]
			}
			r.editedUser = r.personDatum(v, col)
		}
	}
	return r.editedUser
}

// currentUserID is $self->user->id.
func (r *Record) currentUserID() int64 {
	if r.user == nil {
		Fail(`Can't call method "id" on an undefined value`)
	}
	return r.user.ID
}

// CreatedTime returns the _created datum.
func (r *Record) CreatedTime() *DateDatum {
	if r.createdTime == nil {
		col := r.layout.ColumnByNameShort("_created")
		if r.NewEntry() {
			r.createdTime = r.dateDatum(r.nowTime(), col)
		} else {
			v := r.setRecordCreated
			if v == nil {
				if row := r.firstRecordRow(); row != nil {
					v = row["created"]
				}
			}
			r.createdTime = r.dateDatum(v, col)
		}
	}
	return r.createdTime
}

// CreatedUser returns the _created_user datum.
func (r *Record) CreatedUser() *PersonDatum {
	if r.createdUser == nil {
		col := r.layout.ColumnByNameShort("_created_user")
		if r.NewEntry() {
			// Default to current user drafting record if new record
			r.createdUser = r.personDatum(r.currentUserID(), col)
			return r.createdUser
		}
		var v any
		if row := r.firstRecordRow(); row != nil {
			v = row["createdby"]
		}
		r.createdUser = r.personDatum(v, col)
	}
	return r.createdUser
}

// Deletedby returns the _deleted_by datum.
func (r *Record) Deletedby() *PersonDatum {
	if r.NewEntry() {
		return nil
	}
	col := r.layout.ColumnByNameShort("_deleted_by")
	if !r.hasRaw {
		row := r.e.DB.QueryRow(`SELECT "deletedby" FROM "current" WHERE "id" = ?`, r.currentID)
		if row == nil || row["deletedby"] == nil {
			return nil
		}
		return r.personDatum(row["deletedby"], col)
	}
	if r.setDeletedby == nil {
		return nil
	}
	return r.personDatum(r.setDeletedby, col)
}

// ---------------------------------------------------------------------
// Fields
// ---------------------------------------------------------------------

// Fields returns the datums of the record by column ID.
func (r *Record) Fields() map[int64]Datum {
	if !r.fieldsBuilt {
		r.fieldsBuilt = true
		r.fields = r.transformValues()
	}
	return r.fields
}

// SetFields sets the datums.
func (r *Record) SetFields(f map[int64]Datum) {
	r.fields = f
	r.fieldsBuilt = true
}

func (r *Record) createDatum(col *Column, values []rawValue, hasValue bool) Datum {
	childUnique := false
	if len(values) > 0 {
		childUnique = values[0].ChildUnique
	}
	var d Datum
	if r.isGroup && !col.Numeric() && !r.groupCols[col.ID] {
		cd := &CountDatum{}
		if len(values) > 0 {
			cd.val = values[0].Value
		}
		d = cd
	} else {
		d = r.e.newDatum(col, r)
	}
	b := d.base()
	b.record = r
	b.recordID = r.recordID
	b.currentID = r.currentID
	b.childUnique = childUnique
	b.initNoValue = r.InitNoValue
	if !(r.isGroup && col.Aggregate == "recalc") && !(r.IsDraft() != 0 && !col.Userinput()) {
		b.initValue = values
		b.hasInitValue = true
		_ = hasValue
	}
	return d
}

// newDatum creates an empty datum of the column's class.
func (e *Engine) newDatum(col *Column, r *Record) Datum {
	var d Datum
	switch col.Type {
	case "id":
		d = &IDDatum{}
	case "serial":
		d = &SerialDatum{}
	case "createddate", "date":
		d = &DateDatum{}
	case "createdby", "deletedby", "person":
		d = &PersonDatum{}
	case "daterange":
		d = &DaterangeDatum{}
	case "string":
		d = &StringDatum{}
	case "intgr":
		d = &IntegerDatum{}
	case "tree":
		d = &TreeDatum{}
	case "enum":
		d = &EnumDatum{}
	case "file":
		d = &FileDatum{}
	case "rag":
		d = &RagDatum{}
	case "calc":
		d = &CalcDatum{}
	case "curval", "autocur", "filval":
		d = &CurcommonDatum{}
	default:
		Panic("Unknown column type %s", col.Type)
	}
	b := d.base()
	b.column = col
	b.record = r
	if r != nil {
		b.recordID = r.recordID
		b.currentID = r.currentID
	}
	return d
}

// IDCount returns the number of records in a group row.
func (r *Record) IDCount() int64 {
	n, _ := db.AsInt64(r.idCount)
	return n
}

func (r *Record) transformGroupValues() map[int64]Datum {
	fields := map[int64]Datum{}
	seen := map[int64]bool{}
	var cols []*Column
	add := func(c *Column) {
		if c != nil && !seen[c.ID] {
			seen[c.ID] = true
			cols = append(cols, c)
		}
	}
	for _, c := range r.columnsSelected {
		add(c)
	}
	for _, c := range r.columnsRecalcExtra {
		add(c)
	}
	for _, c := range r.columnsRetrievedNo {
		add(c)
	}
	for _, c := range cols {
		if c.Internal {
			continue
		}
		vals, ok := r.groupRawValues(c)
		fields[c.ID] = r.createDatum(c, vals, ok)
	}
	colID := r.layout.ColumnID()
	idd := &IDDatum{}
	idd.record, idd.column = r, colID
	fields[colID.ID] = idd
	return fields
}

func (r *Record) transformValues() map[int64]Datum {
	if r.groupRow != nil {
		return r.transformGroupValues()
	}
	if !r.hasRaw {
		Panic("Record data has not been set")
	}
	fields := map[int64]Datum{}
	cols := map[int64]*Column{}
	var order []*Column
	add := func(c *Column) {
		if c == nil {
			return
		}
		if _, ok := cols[c.ID]; !ok {
			order = append(order, c)
		}
		cols[c.ID] = c
	}
	for _, c := range r.columnsRender {
		add(c)
	}
	for _, c := range r.columnsRetrievedNo {
		add(c)
	}
	for _, c := range order {
		if c.Internal {
			continue
		}
		key := c.ID
		if r.LinkedID() != 0 && c.LinkParent() != nil {
			key = c.LinkParent().ID
		}
		vals, ok := r.raw[key]
		fields[c.ID] = r.createDatum(c, vals, ok)
	}
	colID := r.layout.ColumnID()
	idd := &IDDatum{}
	idd.record, idd.recordID, idd.currentID, idd.column = r, r.recordID, r.currentID, colID
	fields[colID.ID] = idd
	return fields
}

// GetFieldValue returns the datum for a column, retrieving it if needed.
func (r *Record) GetFieldValue(col *Column) Datum {
	switch col.NameShort {
	case "_version_datetime":
		return r.EditedTime()
	case "_version_user":
		if u := r.EditedUser(); u != nil {
			return u
		}
		return nil
	case "_created":
		return r.CreatedTime()
	case "_created_user":
		if u := r.CreatedUser(); u != nil {
			return u
		}
		return nil
	}
	if d, ok := r.Fields()[col.ID]; ok && d != nil {
		return d
	}
	recordID := r.recordIDOld
	if recordID == 0 {
		recordID = r.recordID
	}
	var raw []rawValue
	if col.Type == "autocur" {
		for _, v := range col.FetchMultivalues([]int64{recordID}, fetchMultiOptions{alreadySeen: newSeenRoot()}) {
			raw = append(raw, v.value)
		}
	} else if col.NameShort == "_serial" {
		if s := r.Serial(); s != nil {
			raw = []rawValue{{Value: *s}}
		}
	} else if col.Internal {
		raw = nil
	} else {
		raw = r.e.rawValuesFromTable(col, []int64{recordID})[recordID]
	}
	d := r.createDatum(col, raw, true)
	r.Fields()[col.ID] = d
	return d
}

// rawValuesFromTable fetches values of a column for record IDs directly
// from its value table (as HashRefInflator rows).
func (e *Engine) rawValuesFromTable(col *Column, recordIDs []int64) map[int64][]rawValue {
	out := map[int64][]rawValue{}
	if len(recordIDs) == 0 {
		return out
	}
	args := []any{col.ID}
	for _, id := range recordIDs {
		args = append(args, id)
	}
	rows := e.DB.Query(`SELECT * FROM `+q(col.Table())+` WHERE "layout_id" = ? AND "record_id" IN (`+db.Placeholders(len(recordIDs))+`) ORDER BY "id"`, args...)
	for _, row := range rows {
		rid, _ := db.AsInt64(row["record_id"])
		out[rid] = append(out[rid], rawFromTableRow(col, row))
	}
	return out
}

func rawFromTableRow(col *Column, row db.Row) rawValue {
	rv := rawValue{ChildUnique: rowBool(row["child_unique"])}
	rv.ID, _ = db.AsInt64(row["id"])
	switch col.Type {
	case "daterange":
		rv.From, rv.To, rv.Value = row["from"], row["to"], row["value"]
	case "calc":
		rv.ValueText, rv.ValueInt, rv.ValueDate, rv.ValueNumeric = row["value_text"], row["value_int"], row["value_date"], row["value_numeric"]
		rv.From, rv.To = row["value_date_from"], row["value_date_to"]
		rv.Value = row[col.ValueField()]
	default:
		rv.Value = row["value"]
	}
	return rv
}

// ---------------------------------------------------------------------
// Finding records
// ---------------------------------------------------------------------

// FindOptions are options to the find functions.
type FindOptions struct {
	InstanceID   int64
	Deleted      bool
	IncludeDraft bool
	DraftuserID  int64
	NoErrors     bool
}

// FindCurrentID loads the latest version of a record.
func (r *Record) FindCurrentID(currentID int64, o FindOptions) *Record {
	if currentID == 0 {
		return nil
	}
	row := r.e.DB.QueryRow(`SELECT "instance_id" FROM "current" WHERE "id" = ?`, currentID)
	if row == nil {
		Fail("Record ID %d not found", currentID)
	}
	iid, _ := db.AsInt64(row["instance_id"])
	if o.InstanceID != 0 && o.InstanceID != iid {
		Fail("Record ID %d invalid for table %d", currentID, o.InstanceID)
	}
	r.freshLayout(iid)
	return r.find(findSpec{currentID: currentID, opts: o})
}

// FindCurrentIDString finds a record from a (possibly invalid) string ID.
func (r *Record) FindCurrentIDString(s string, o FindOptions) *Record {
	if s == "" || s == "0" {
		return nil
	}
	if !reDigitsOnly.MatchString(s) {
		Fail("Invalid record ID %s", s)
	}
	id, _ := anyToInt64(s)
	return r.FindCurrentID(id, o)
}

// FindRecordID loads a specific version of a record.
func (r *Record) FindRecordID(recordID int64, o FindOptions) *Record {
	row := r.e.DB.QueryRow(`SELECT "current"."instance_id", "me"."current_id" FROM "record" "me" JOIN "current" "current" ON "current"."id" = "me"."current_id" WHERE "me"."id" = ?`, recordID)
	if row == nil {
		Fail("Record version ID %d not found", recordID)
	}
	iid, _ := db.AsInt64(row["instance_id"])
	if o.InstanceID != 0 && o.InstanceID != iid {
		Fail("Record ID %d invalid for table %d", recordID, o.InstanceID)
	}
	r.freshLayout(iid)
	return r.find(findSpec{recordID: recordID, opts: o})
}

// FindSerialID loads a record by serial.
func (r *Record) FindSerialID(serial int64) *Record {
	id, ok := r.e.DB.QueryInt64(`SELECT "id" FROM "current" WHERE "serial" = ? AND "instance_id" = ?`, serial, r.layout.InstanceID)
	if !ok {
		Fail("Serial ID %d not found", serial)
	}
	return r.find(findSpec{currentID: id})
}

// FindDraftuserID loads the draft of a user.
func (r *Record) FindDraftuserID(userID int64, instanceID int64) *Record {
	if instanceID == 0 {
		instanceID = r.layout.InstanceID
	}
	// As _find clears the layout, rebuilt for the record's user
	r.freshLayout(instanceID)
	return r.find(findSpec{draftuserID: userID, opts: FindOptions{NoErrors: true}})
}

// FindDeletedCurrentID loads a deleted record.
func (r *Record) FindDeletedCurrentID(currentID int64) *Record {
	return r.FindCurrentID(currentID, FindOptions{Deleted: true})
}

type findSpec struct {
	currentID   int64
	recordID    int64
	draftuserID int64
	opts        FindOptions
}

// freshLayout replaces the record's layout with a new one for the
// instance, as Record::_find clears the layout which is then rebuilt from
// the instance ID (with empty caches) using the record's user.
func (r *Record) freshLayout(instanceID int64) {
	l := r.e.NewLayout(instanceID, r.user)
	l.SetRecord(r)
	r.layout = l
}

// Clear removes the loaded record data (Record::clear).
func (r *Record) Clear() { r.clearForFind() }

func (r *Record) clearForFind() {
	r.currentID = 0
	r.recordID = 0
	r.parentLoaded = false
	r.parent = nil
	r.linkedLoaded = false
	r.childLoaded = false
	r.fieldsBuilt = false
	r.fields = nil
	r.raw = nil
	r.hasRaw = false
	r.editedTime, r.editedUser, r.createdTime, r.createdUser = nil, nil, nil, nil
	r.newEntrySet = false
	r.serialLoaded = false
	r.isDraftLoaded = false
}

func (r *Record) find(spec findSpec) *Record {
	r.clearForFind()
	if spec.opts.Deleted && !r.layout.UserCan("purge") && !r.e.IgnorePermissions {
		Fail("You do not have access to this deleted record")
	}
	// The columns of the (fresh) layout are built now, as by the records
	// of Record::_find, so the record keeps those of the time of the find
	r.layout.Columns()
	recs := r.e.NewRecords(r.layout, r.user)
	if r.hasColumnsOption {
		recs.SetColumns(r.columnsOption)
	}
	recs.Rewind = r.Rewind
	recs.IsDeleted = spec.opts.Deleted
	if spec.draftuserID != 0 {
		recs.IsDraft = spec.draftuserID
		recs.NoViewLimits = true
	} else if spec.opts.IncludeDraft {
		if r.user != nil {
			recs.IsDraft = r.user.ID
		} else {
			recs.IsDraft = -1
		}
	}
	recs.IncludeApproval = r.IncludeApproval
	recs.SetIncludeChildren(true)
	recs.IgnoreViewLimitExtra = true
	recs.findRecordID = spec.recordID
	recs.findDraftuserID = spec.draftuserID
	if spec.currentID != 0 {
		recs.SetLimitCurrentIDs([]int64{spec.currentID})
	} else if spec.recordID != 0 {
		cid, _ := r.e.DB.QueryInt64(`SELECT "current_id" FROM "record" WHERE "id" = ?`, spec.recordID)
		recs.SetLimitCurrentIDs([]int64{cid})
	}
	results := recs.findResults()
	if len(results) == 0 {
		if spec.opts.NoErrors {
			return nil
		}
		Fail("Requested record not found")
	}
	src := results[0]
	r.currentID = src.currentID
	r.recordID = src.recordID
	r.raw = src.raw
	r.hasRaw = true
	r.serial = src.serial
	r.serialLoaded = true
	r.parentID = src.parentID
	r.parentLoaded = true
	r.linkedID = src.linkedID
	r.linkedLoaded = true
	r.linkedRecordID = src.linkedRecordID
	r.isDraftLoaded = false
	r.columnsRetrievedNo = src.columnsRetrievedNo
	r.columnsRetrievedDo = src.columnsRetrievedDo
	r.columnsSelected = src.columnsSelected
	r.columnsRender = src.columnsRender
	r.setDeleted = src.setDeleted
	r.setDeletedby = src.setDeletedby
	r.setRecordCreated = src.setRecordCreated
	r.createdRaw = src.createdRaw
	r.hasCreatedRaw = true
	// Record::_find does not retrieve the createdby of the version
	r.createdbyFetched = false
	r.approvalFlag = src.approvalFlag
	r.approvalRecordID = src.approvalRecordID
	r.newEntrySet = false
	r.layout.SetRecord(r)
	return r
}

// Versions returns the versions of the record, latest first.
func (r *Record) Versions() []db.Row {
	args := []any{r.currentID}
	extra := ""
	if r.Rewind != nil {
		extra = ` AND "me"."created" < ?`
		args = append(args, db.FormatDateTime(*r.Rewind))
	}
	return r.e.DB.Query(`SELECT "me".* FROM "record" "me" WHERE "current_id" = ? AND "approval" = 0`+extra+` ORDER BY "me"."created" DESC, "me"."id" DESC`, args...)
}

// Initialise prepares a new empty record.
func (r *Record) Initialise(instanceID int64) {
	if instanceID != 0 && instanceID != r.layout.InstanceID {
		r.layout = r.layout.Clone(instanceID)
	}
	fields := map[int64]Datum{}
	for _, c := range r.layout.All(AllOptions{}) {
		fields[c.ID] = r.InitialiseField(c.ID, nil)
	}
	r.columnsRetrievedDo = r.layout.All(AllOptions{})
	r.SetFields(fields)
	r.hasRaw = true
	r.raw = map[int64][]rawValue{}
	r.layout.SetRecord(r)
}

// InitialiseField creates an empty datum for a column.
func (r *Record) InitialiseField(colID int64, record *Record) Datum {
	col := r.layout.Column(colID)
	if r.LinkedID() != 0 && col.LinkParent() != nil {
		lr := r.e.newRecordObject(r.layout, r.user)
		lr.FindCurrentID(r.LinkedID(), FindOptions{})
		return lr.Fields()[col.LinkParent().ID]
	}
	rec := r
	if record != nil {
		rec = record
	}
	return r.e.newDatum(col, rec)
}

// ValuesByShortname returns the values passed to Lua code.
func (r *Record) ValuesByShortname(names []string, allPossible map[string]bool, alreadySeen *seenNode) map[string]any {
	out := map[string]any{}
	for _, n := range names {
		col := r.layout.ColumnByNameShort(n)
		if col == nil {
			Fail("Short name %s does not exist", n)
		}
		linked := r.LinkedID() != 0 && col.LinkParent() != nil
		d := r.GetFieldValue(col)
		if d == nil {
			Panic("Value for column %s missing. Possibly missing entry in layout_depend?", col.Name)
		}
		b := d.base()
		if b.isAwaitingApproval && b.oldvalue != nil {
			d = b.oldvalue
		} else if linked && b.oldvalue != nil {
			d = b.oldvalue
		}
		out[col.NameShort] = d.ForCode(ForCodeContext{Fields: allPossible, AlreadySeen: alreadySeen})
	}
	return out
}

// ForCode returns code values for a set of columns (curval values).
func (r *Record) ForCode(cols []*Column, fields map[string]bool, alreadySeen *seenNode) map[string]any {
	out := map[string]any{}
	for _, c := range cols {
		d := r.GetFieldValue(c)
		out[c.NameShort] = d.ForCode(ForCodeContext{Fields: fields, AlreadySeen: alreadySeen})
	}
	return out
}

// IsEdited reports whether any user-input field has changed.
func (r *Record) IsEdited() bool {
	for _, d := range r.Fields() {
		if d != nil && d.Column().Userinput() && d.base().changed {
			return true
		}
	}
	return false
}

// AsQuery returns the record as a URL query string.
func (r *Record) AsQuery(excludeCurcommon bool) string {
	var parts []string
	for _, c := range r.layout.All(AllOptions{Userinput: BoolP(true), UserCanRead: true}) {
		if excludeCurcommon && c.IsCurcommon() {
			continue
		}
		d := r.GetFieldValue(c)
		if d == nil {
			Panic("Field %d is missing from record", c.ID)
		}
		for _, v := range d.HTMLForm() {
			s := anyToString(v)
			if m, ok := v.(map[string]any); ok {
				s = anyToString(m["id"])
			}
			parts = append(parts, c.Field()+"="+uriEscape(s))
		}
	}
	return strings.Join(parts, "&")
}

func uriEscape(s string) string {
	// URI::Escape uri_escape_utf8: escapes all but unreserved
	var sb strings.Builder
	for _, b := range []byte(s) {
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '_' || b == '.' || b == '~' {
			sb.WriteByte(b)
		} else {
			sb.WriteString("%" + strings.ToUpper(hex2(b)))
		}
	}
	return sb.String()
}

func hex2(b byte) string {
	const h = "0123456789abcdef"
	return string([]byte{h[b>>4], h[b&15]})
}

var _ = url.QueryEscape

// SelectorID returns the selector id used in forms.
func (r *Record) SelectorID() string {
	if r.selectorID == "" {
		// As Perl, a token is generated whether or not it is used
		token := r.e.NewToken(32)
		if r.IsDraft() != 0 {
			r.selectorID = "query_" + itoa(r.currentID)
		} else if r.NewEntry() {
			r.selectorID = "new_" + token
		} else {
			r.selectorID = itoa(r.currentID)
		}
	}
	return r.selectorID
}

// NewToken generates a random token of n alphanumeric characters
// (Session::Token->new(length => n)->get), or uses the engine's
// TokenGenerator if set (tests use a predictable sequence).
func (e *Engine) NewToken(n int) string {
	if e.TokenGenerator != nil {
		return e.TokenGenerator(n)
	}
	return randomToken(n)
}

func randomToken(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

var reDigitsOnly = regexp.MustCompile(`^[0-9]+$`)

// DebugRaw returns the raw values (debugging aid).
func (r *Record) DebugRaw() map[int64][]rawValue { return r.raw }

// RemoveID removes the record's IDs so that it will be written as a new
// record (remove_id).
func (r *Record) RemoveID() {
	r.currentID = 0
	r.SetLinkedID(0)
	r.clearNewEntry()
	if d := r.CreatedTime(); d != nil {
		d.SetValue(r.e.Now(), SetValueOptions{})
	}
}

// NewRecordUnattached creates a record object without a layout, as
// GADS::Record->new(user => ...): finding a record sets its layout
func (e *Engine) NewRecordUnattached(user *User) *Record { return e.newRecordObject(nil, user) }
