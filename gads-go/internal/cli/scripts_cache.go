package cli

import (
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/cldr"
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

func init() {
	Commands["update-cached.pl"] = UpdateCached
	Commands["update-alertcache.pl"] = UpdateAlertcache
	Commands["send-alerts.pl"] = SendAlerts
	Commands["date-format.pl"] = DateFormat
	Commands["fix-curval-times.pl"] = FixCurvalTimes
}

// siteIDs are the sites, in the order of the database
func siteIDs(e *gads.Engine) []int64 {
	return e.DB.QueryInt64s(`SELECT "id" FROM "site"`)
}

// overrideLayouts are the layouts of the instances of the current site, as
// GADS::Instances->all with no user and user_permission_override. That
// option does not reach user_can_anything, so without IGNORE_PERMISSIONS
// (as update-alertcache.pl and date-format.pl) there are none.
func overrideLayouts(e *gads.Engine) []*gads.Layout {
	return e.NewInstances(nil).All()
}

// overrideViews are GADS::Views->all with user_permission_override: every
// view of the instance
func overrideViews(e *gads.Engine, layout *gads.Layout) []*gads.View {
	var out []*gads.View
	for _, id := range e.DB.QueryInt64s(`SELECT "id" FROM "view" WHERE "instance_id" = ?`, layout.InstanceID) {
		if v := e.LoadViewOverride(id, layout); v != nil {
			out = append(out, v)
		}
	}
	return out
}

type cacheDatum interface {
	ReEvaluate(o gads.ReEvalOptions)
	WriteValue()
	Changed() bool
}

// UpdateCached is bin/update-cached.pl: re-evaluates the cached (code)
// values of all records and sends the alerts of the changes
func UpdateCached(env *Env) {
	var fromRecordID string
	if !env.GetOptions(Option{Name: "from-record-id", Value: true, String: &fromRecordID}) {
		env.Exit(0)
	}
	e := env.E
	old := e.IgnorePermissions
	e.IgnorePermissions = true
	defer func() { e.IgnorePermissions = old }()
	for _, site := range siteIDs(e) {
		e.SiteID = site
		for _, layout := range overrideLayouts(e) {
			if truthy(layout.Row()["no_overnight_update"]) {
				continue
			}
			cols := layout.ColIDsForCacheUpdate()
			if len(cols) == 0 {
				continue
			}
			records := e.NewRecords(layout, nil)
			if fromRecordID != "" && fromRecordID != "0" {
				filter := gads.NewFilterHash(map[string]any{
					"rules": []any{map[string]any{
						"id":       layout.ColumnID().ID,
						"type":     "integer",
						"value":    fromRecordID,
						"operator": "greater",
					}},
				}, nil)
				view := e.NewTempView(layout, filter)
				view.Name = "Test view"
				view.SetColumns(cols)
				records.View = view
			} else {
				records.SetColumns(cols)
			}
			records.SetIncludeChildren(true)
			changed := map[int64][]int64{}
			var order []int64
			for {
				rec := records.Single()
				if rec == nil {
					break
				}
				for _, col := range layout.All(gads.AllOptions{OrderDependencies: true, HasCache: true}) {
					d, ok := rec.Fields()[col.ID].(cacheDatum)
					if !ok {
						continue
					}
					d.ReEvaluate(gads.ReEvalOptions{NoErrors: true})
					d.WriteValue()
					if _, seen := changed[col.ID]; !seen {
						changed[col.ID] = []int64{}
						order = append(order, col.ID)
					}
					if d.Changed() {
						changed[col.ID] = append(changed[col.ID], rec.CurrentID())
					}
				}
				layout.ClearCachedRecords()
			}
			for _, colID := range order {
				e.NewAlertSend(layout, changed[colID], []int64{colID}, false).Process()
			}
		}
	}
}

// UpdateAlertcache is bin/update-alertcache.pl: rebuilds the alert caches
// of all views with alerts, and removes those of views without
func UpdateAlertcache(env *Env) {
	e := env.E
	for _, site := range siteIDs(e) {
		e.SiteID = site
		for _, layout := range overrideLayouts(e) {
			for _, view := range overrideViews(e, layout) {
				if view.HasAlerts() {
					a := e.NewAlert(layout, nil, view.ID)
					a.UpdateCache(true)
				} else {
					e.DB.Exec(`DELETE FROM "alert_cache" WHERE "view_id" = ?`, view.ID)
				}
			}
		}
	}
}

type notification struct{ text, html string }

// SendAlerts is bin/send-alerts.pl: emails each user the changes of the
// views they are alerted to, and removes the sent changes
func SendAlerts(env *Env) {
	e := env.E
	desc := e.NewAlertDescription()
	send := func(email string, ns []notification) {
		if len(ns) == 0 {
			return
		}
		text := "This email contains details of any changes in views that you have asked to be alerted to.\n\n"
		html := "<p>This email contains details of any changes in views that you have asked to be alerted to.</p>"
		var ts, hs []string
		for _, n := range ns {
			ts = append(ts, n.text)
			hs = append(hs, n.html)
		}
		text += strings.Join(ts, "\n")
		html += strings.Join(hs, "\n")
		e.SendEmail(gads.Email{To: []string{email}, Subject: "Changes in your views", Text: text, HTML: html})
	}
	type current struct{ id, instanceID int64 }
	doColumns := func(cur *current, user *gads.User, columns []string) []notification {
		if len(columns) == 0 {
			return nil
		}
		cols := strings.Join(columns, ", ")
		d := desc.Description(cur.instanceID, []int64{cur.id}, user)
		l := desc.Link(cur.instanceID, []int64{cur.id}, user)
		return []notification{{
			text: "* The following columns have changed in " + d + ": " + cols,
			html: "<li>The following columns have changed in " + l + ": " + cols + "</li>",
		}}
	}
	rows := e.DB.Query(`SELECT "me".*, "alert"."user_id" AS "alert_user_id", "alert"."view_id" AS "alert_view_id" FROM "alert_send" "me" JOIN "alert" "alert" ON "alert"."id" = "me"."alert_id" JOIN "user" "user" ON "user"."id" = "alert"."user_id" ORDER BY "user"."id", "alert_id", "current_id"`)
	var lastCurrent *current
	var lastAlertID *int64
	var lastUser *gads.User
	var notifications []notification
	var columns []string
	for _, row := range rows {
		alertID, _ := db.AsInt64(row["alert_id"])
		userID, _ := db.AsInt64(row["alert_user_id"])
		currentID, _ := db.AsInt64(row["current_id"])
		if lastUser != nil && lastUser.ID != userID {
			notifications = append(notifications, doColumns(lastCurrent, lastUser, columns)...)
			columns = nil
			send(lastUser.Email, notifications)
			notifications = nil
		}
		if lastAlertID == nil || *lastAlertID != alertID {
			if lastCurrent != nil {
				notifications = append(notifications, doColumns(lastCurrent, lastUser, columns)...)
			}
			columns = nil
			if len(notifications) > 0 {
				notifications = append(notifications, notification{"", "</ul><p></p>"})
			}
			viewName := queryStr(e, `SELECT "name" FROM "view" WHERE "id" = ?`, row["alert_view_id"])
			notifications = append(notifications, notification{
				`The following are changes in the view "` + viewName + `":`,
				`<p>The following are changes in the view "` + viewName + `":</p><ul>`,
			})
		}
		if lastCurrent != nil && lastCurrent.id != currentID && len(columns) > 0 {
			notifications = append(notifications, doColumns(lastCurrent, lastUser, columns)...)
			columns = nil
		}
		instanceID, _ := e.DB.QueryInt64(`SELECT "instance_id" FROM "current" WHERE "id" = ?`, currentID)
		cur := &current{id: currentID, instanceID: instanceID}
		user := e.FindUser(userID)
		d := desc.Description(instanceID, []int64{currentID}, user)
		l := desc.Link(instanceID, []int64{currentID}, user)
		status := str(row["status"])
		switch {
		case status == "changed":
			columns = append(columns, queryStr(e, `SELECT "name" FROM "layout" WHERE "id" = ?`, row["layout_id"]))
		case strings.HasPrefix(status, "gone"):
			notifications = append(notifications, notification{
				"* " + d + " has now disappeared from the view",
				"<li>" + l + " has disappeared from the view</li>",
			})
		case status == "arrived":
			notifications = append(notifications, notification{
				"* " + d + " is now in the view",
				"<li>" + l + " is now in the view</li>",
			})
		default:
			gads.Panic("I don't know what to do with alert status of '%s'", status)
		}
		lastAlertID = &alertID
		lastUser = user
		lastCurrent = cur
		e.DB.Exec(`DELETE FROM "alert_send" WHERE "id" = ?`, row["id"])
	}
	if lastCurrent != nil {
		notifications = append(notifications, doColumns(lastCurrent, lastUser, columns)...)
	}
	if len(notifications) > 0 {
		notifications = append(notifications, notification{"", "</ul><p></p>"})
	}
	if lastUser != nil {
		send(lastUser.Email, notifications)
	}
}

// DateFormat is bin/date-format.pl: rewrites the dates of the view filters
// in a new date format
func DateFormat(env *Env) {
	var newFormat string
	if !env.GetOptions(Option{Name: "to", Value: true, String: &newFormat}) {
		env.Exit(0)
	}
	if newFormat == "" {
		gads.Fail("Please provide new date format with --to")
	}
	e := env.E
	for _, site := range siteIDs(e) {
		e.SiteID = site
		for _, layout := range overrideLayouts(e) {
			for _, view := range overrideViews(e, layout) {
				for _, filter := range view.Filter().Filters() {
					cid, _ := db.AsInt64(filter["column_id"])
					column := layout.Column(cid)
					if column == nil {
						gads.Fail("Invalid column_id %s for view %d", str(filter["column_id"]), view.ID)
					}
					rt := column.ReturnTypeFn()
					if rt != "date" && rt != "daterange" {
						continue
					}
					if rt == "daterange" {
						gads.Fail("Unable to process daterange columns")
					}
					var vals []any
					if l, ok := filter["value"].([]any); ok {
						vals = l
					} else {
						vals = []any{filter["value"]}
					}
					if len(vals) == 0 {
						continue
					}
					if len(vals) != 1 {
						gads.Fail("Unexpected number of values for filter")
					}
					t, ok := e.ParseDateTime(str(vals[0]))
					if !ok {
						continue
					}
					filter["value"] = cldr.Format(t, newFormat)
				}
				view.Write(false, true)
			}
		}
	}
}

// sqlTime is the DateTime::Format::SQLite format of a time
func sqlTime(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

// FixCurvalTimes is bin/fix-curval-times.pl: reports curval records edited
// at the time of an edit of their parent record
func FixCurvalTimes(env *Env) {
	e := env.E
	d := e.DB
	for _, curval := range d.Query(`SELECT * FROM "curval"`) {
		parent := d.QueryRow(`SELECT * FROM "record" WHERE "id" = ?`, curval["record_id"])
		if curval["value"] == nil || d.QueryRow(`SELECT "id" FROM "current" WHERE "id" = ?`, curval["value"]) == nil {
			continue
		}
		if parent["createdby"] == nil {
			continue
		}
		createdby := d.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, parent["createdby"])
		if createdby == nil {
			continue
		}
		created, _ := db.AsTime(parent["created"])
		pcid := str(parent["current_id"])
		for _, cr := range d.Query(`SELECT * FROM "record" "me" WHERE "me"."current_id" = ? AND ( "me"."created" > ? AND "me"."created" < ? ) AND "me"."createdby" = ?`,
			curval["value"], sqlTime(created), sqlTime(created.Add(60*time.Second)), createdby["id"]) {
			audit := d.QueryRow(`SELECT * FROM "audit" "me" WHERE "datetime" > ? AND "datetime" <= ? AND "method" = ? AND ( "url" = ? OR "url" LIKE ? OR "url" = ? OR "url" LIKE ? ) AND "user_id" = ? LIMIT 1`,
				sqlTime(created.Add(-60*time.Second)), sqlTime(created), "POST", "/record/"+pcid, "%/record/%", "/edit/"+pcid, "%/edit/", createdby["id"])
			if audit == nil {
				env.SayErr("Cannot find edit of %s edited at %s by %s", pcid, dtString(parent["created"]), str(createdby["value"]))
				Die(`Can't call method "id" on an undefined value`)
			}
			ccid := str(cr["current_id"])
			if d.QueryRow(`SELECT "id" FROM "audit" "me" WHERE "datetime" < ? AND "datetime" >= ? AND "id" != ? AND "method" = ? AND ( "url" = ? OR "url" = ? ) AND "user_id" = ? LIMIT 1`,
				sqlTime(created.Add(60*time.Second)), sqlTime(created), audit["id"], "POST", "/record/"+ccid, "/edit/"+ccid, createdby["id"]) != nil {
				env.SayErr("DIRECT EDIT!")
			}
			crBy := d.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, cr["createdby"])
			env.SayErr("Parent %s edited %s by %s has nearby curval record edited at %s by %s with ID %s",
				pcid, dtString(parent["created"]), str(createdby["value"]), dtString(cr["created"]), str(crBy["value"]), ccid)
		}
	}
}

func truthy(v any) bool {
	s, _ := db.AsString(v)
	return v != nil && s != "" && s != "0"
}

func queryStr(e *gads.Engine, q string, args ...any) string {
	if r := e.DB.QueryRow(q, args...); r != nil {
		for _, v := range r {
			return str(v)
		}
	}
	return ""
}

func init() { Commands["update-snapshot.pl"] = UpdateSnapshot }

// UpdateSnapshot is bin/update-snapshot.pl: rewrites the values of the
// filtered value (filval) fields of every record from the values of their
// curval fields. As in GADS, the tables are those of no user without
// IGNORE_PERMISSIONS, which are none. (GADS also evaluates the filter as
// at the time of each record, which is not done here.)
func UpdateSnapshot(env *Env) {
	e := env.E
	d := e.DB
	for _, site := range siteIDs(e) {
		e.SiteID = site
		for _, layout := range overrideLayouts(e) {
			var filvals []*gads.Column
			for _, c := range layout.All(gads.AllOptions{}) {
				if c.Type == "filval" {
					filvals = append(filvals, c)
				}
			}
			if len(filvals) == 0 {
				continue
			}
			env.SayErr("doing table %s", layout.Name())
			records := e.NewRecords(layout, nil)
			records.SetIncludeChildren(true)
			for {
				rec := records.Single()
				if rec == nil {
					break
				}
				env.SayErr("Doing %d", rec.CurrentID())
				d.Txn(func() {
					for _, fv := range filvals {
						d.Exec(`DELETE FROM "curval" WHERE "record_id" = ? AND "layout_id" = ?`, rec.RecordID(), fv.ID)
						layout.SetRecord(rec)
						related := fv.RelatedField()
						related.ClearCurval()
						for _, v := range related.FilteredValues("") {
							d.InsertMap("curval", map[string]any{"record_id": rec.RecordID(), "layout_id": fv.ID, "value": v.ID})
						}
					}
				})
			}
		}
	}
}
