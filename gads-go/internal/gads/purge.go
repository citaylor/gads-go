package gads

import "github.com/ctrlo/gads-go/internal/db"

// historicPurgeTables are the value tables purged by historic_purge, in
// the order Perl collects their values.
var historicPurgeTables = []string{"calcval", "daterange", "date", "enum", "file", "intgr", "person", "ragval", "string"}

// purgeValueFields are the value fields blanked when a value is purged
// (GADS::Role::Purgable value_fields).
func purgeValueFields(table string) []string {
	if table == "daterange" {
		return []string{"from", "to", "value"}
	}
	return []string{"value"}
}

// HistoricPurge blanks the values of columns in all versions of a record,
// recording who purged them (Result::Current->historic_purge).
func (e *Engine) HistoricPurge(currentID int64, user *User, layoutIDs []int64) {
	recordIDs := e.DB.QueryInt64s(`SELECT "id" FROM "record" WHERE "current_id" = ?`, currentID)
	type value struct {
		table string
		row   db.Row
	}
	var values []value
	for _, lid := range layoutIDs {
		for _, t := range historicPurgeTables {
			for _, rid := range recordIDs {
				for _, row := range e.DB.Query(`SELECT * FROM `+q(t)+` "me" WHERE "me"."record_id" = ? AND "layout_id" = ?`, rid, lid) {
					values = append(values, value{t, row})
				}
			}
		}
	}
	for _, v := range values {
		e.purgeValue(v.table, v.row, user)
	}
}

// purgeValue purges one value (GADS::Role::Purgable->purge).
func (e *Engine) purgeValue(table string, row db.Row, user *User) {
	id, _ := db.AsInt64(row["id"])
	e.DB.Txn(func() {
		if row["purged_on"] != nil && row["purged_by"] != nil {
			return
		}
		var by any
		if user != nil {
			by = user.ID
		}
		e.DB.UpdateMap(table, map[string]any{"purged_by": by, "purged_on": e.Now()}, map[string]any{"id": id})
		for _, f := range purgeValueFields(table) {
			if _, ok := row[f]; !ok {
				Fail("No such column '%s' on GADS::Schema::Result::%s", f, perlResultName(table))
			}
			e.DB.UpdateMap(table, map[string]any{f: nil}, map[string]any{"id": id})
		}
	})
	// Perl's File purge then calls delete_related('value'), but as the
	// value has already been set to undef this deletes nothing: the file
	// content (fileval) is kept
}

func perlResultName(table string) string {
	if table == "" {
		return ""
	}
	return string(table[0]-'a'+'A') + table[1:]
}
