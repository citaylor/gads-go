package gads

import "github.com/ctrlo/gads-go/internal/db"

// ChronologyEntry is one version of a record with the changes made in it
// that the user can see.
type ChronologyEntry struct {
	Editor   *PersonDatum
	Datetime *DateDatum
	Changed  []Datum
	// Previous is the datum of the version before for each change (nil
	// in the first version)
	Previous []Datum
}

// FindChronologyID loads a record together with the changes made in each of
// its versions (Record::find_chronology_id). The record holds the values of
// the latest version.
func (r *Record) FindChronologyID(currentID int64) *Record {
	if currentID == 0 {
		return nil
	}
	if r.e.DB.QueryRow(`SELECT "instance_id" FROM "current" WHERE "id" = ?`, currentID) == nil {
		Fail("Record ID %d not found", currentID)
	}
	r.FindCurrentID(currentID, FindOptions{})
	args := []any{currentID}
	extra := ""
	if r.Rewind != nil {
		extra = ` AND "me"."created" <= ?`
		args = append(args, db.FormatDateTime(*r.Rewind))
	}
	var chronology []ChronologyEntry
	var last *Record
	var lastID int64
	for _, rid := range r.e.DB.QueryInt64s(`SELECT "me"."id" FROM "record" "me" WHERE "me"."current_id" = ? AND "me"."approval" = 0`+extra+` ORDER BY "me"."created", "me"."id"`, args...) {
		// Only the versions the user can see in accordance with any view
		// limits (GADS-dev)
		v := r.e.newRecordObject(r.layout, r.user)
		if v.FindRecordID(rid, FindOptions{NoErrors: true}) == nil {
			continue
		}
		lastID = rid
		var changed, previous []Datum
		for _, c := range r.columnsRender {
			if c.Internal {
				continue
			}
			d := v.Fields()[c.ID]
			if d == nil {
				continue
			}
			if last == nil && !d.Blank() || last != nil && last.Fields()[c.ID].AsString() != d.AsString() {
				changed = append(changed, d)
				if last != nil {
					previous = append(previous, last.Fields()[c.ID])
				} else {
					previous = append(previous, nil)
				}
			}
		}
		// There may have been changes, but not ones the user has access to
		if len(changed) > 0 {
			chronology = append(chronology, ChronologyEntry{Editor: v.EditedUser(), Datetime: v.EditedTime(), Changed: changed, Previous: previous})
		}
		last = v
	}
	// The latest version visible may be an old one, in which case the user
	// cannot see the record at all
	cvid, _ := r.e.DB.QueryInt64(`SELECT "current_version_id" FROM "current" WHERE "id" = ?`, currentID)
	if cvid != lastID {
		Fail("Requested record not found")
	}
	r.chronology = chronology
	return r
}

// Chronology returns the versions loaded by FindChronologyID.
func (r *Record) Chronology() []ChronologyEntry { return r.chronology }
