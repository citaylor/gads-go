package gads

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// Alert is a user's alert on a view (GADS::Alert).
type Alert struct {
	e      *Engine
	layout *Layout
	user   *User
	ID     int64
	ViewID int64
	// Frequency is 0 (immediate) or 24 (daily). nil removes the alert.
	Frequency *int64

	view       *View
	viewLoaded bool
}

// NewAlert creates an alert object for a user's alert on a view. As the
// Perl view_id trigger, it fails if the layout user cannot access the view.
func (e *Engine) NewAlert(layout *Layout, user *User, viewID int64) *Alert {
	a := &Alert{e: e, layout: layout, user: user}
	if viewID != 0 {
		a.ViewID = viewID
		e.LoadView(viewID, layout)
	}
	return a
}

// SetFrequency sets the alert frequency from a submitted value: 0, 24 or
// empty (no alert).
func (a *Alert) SetFrequency(v any) {
	if v == nil {
		a.Frequency = nil
		return
	}
	s := anyToString(v)
	// Will be empty string from form submission
	if s == "" {
		a.Frequency = nil
		return
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		Fail("Frequency value of %s is invalid", s)
	}
	if n != 0 && n != 24 {
		Fail("Frequency value of %s is invalid", s)
	}
	f := int64(n)
	a.Frequency = &f
}

// View returns the alert's view, retrieved without user permissions (which
// are managed when sending the alerts).
func (a *Alert) View() *View {
	if !a.viewLoaded {
		a.viewLoaded = true
		a.view = a.e.viewForAlerts(a.ViewID, a.layout, false)
	}
	return a.view
}

func (a *Alert) clearView() { a.viewLoaded = false; a.view = nil }

// viewForAlerts is GADS::Views->view: the view if it exists, else nil.
func (e *Engine) viewForAlerts(id int64, layout *Layout, override bool) *View {
	v := &View{e: e, layout: layout, ID: id, InstanceID: layout.InstanceID, UserPermissionOverride: override}
	v.load()
	if !v.Exists() {
		return nil
	}
	return v
}

// Write creates, updates or (with no frequency) removes the alert.
func (a *Alert) Write() {
	if a.View().IsGroup() {
		Fail("It is not possible to configure an alert on a view containing groups")
	}
	// Clear the view so that it is rebuilt once the alert is written,
	// otherwise UpdateCache would think the view has no alerts
	a.clearView()
	existing, ok := a.e.DB.QueryInt64(`SELECT "me"."id" FROM "alert" "me" WHERE ( ( "user_id" = ? AND "view_id" = ? ) )`, a.user.ID, a.ViewID)
	if ok {
		if a.Frequency != nil {
			a.e.DB.Exec(`UPDATE "alert" SET "frequency" = ? WHERE ( "id" = ? )`, *a.Frequency, existing)
		} else {
			a.e.DB.Txn(func() {
				// Any other alerts using the same view? Delete cache if not
				if n, _ := a.e.DB.QueryInt64(`SELECT COUNT( * ) FROM "alert" "me" WHERE ( "view_id" = ? )`, a.ViewID); n <= 1 {
					a.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( "view_id" = ? )`, a.ViewID)
				}
				// Delete any queued alerts
				a.e.DB.Exec(`DELETE FROM "alert_send" WHERE ( "alert_id" = ? )`, existing)
				a.e.DB.Exec(`DELETE FROM "alert" WHERE ( "id" = ? )`, existing)
			})
		}
		a.ID = existing
		return
	}
	if a.Frequency == nil {
		return
	}
	// No need for another cache if the view already has alerts, unless the
	// view contains CURUSER
	exists, _ := a.e.DB.QueryInt64(`SELECT COUNT( * ) FROM "alert" "me" WHERE ( "view_id" = ? )`, a.ViewID)
	a.ID = a.e.DB.InsertMap("alert", map[string]any{"view_id": a.ViewID, "user_id": a.user.ID, "frequency": *a.Frequency})
	if exists == 0 || a.View().HasCuruser() {
		a.UpdateCache(false)
	}
}

// UpdateCache rebuilds the alert cache of the view: one row for each
// record and column of the view (per user if the view has CURUSER).
func (a *Alert) UpdateCache(allUsers bool) {
	a.e.DB.Txn(func() { a.updateCache(allUsers) })
}

func (a *Alert) updateCache(allUsers bool) {
	view := a.View()
	if !view.HasAlerts() {
		a.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( "view_id" = ? )`, view.ID)
		return
	}
	curuser := view.HasCuruser()
	// If the view contains a CURUSER filter or the user of the alert has a
	// limited view, separate alert caches are needed for each user that has
	// this alert (all of the view's alert users with allUsers)
	usersSearch := []*User{a.user}
	if allUsers {
		usersSearch = nil
		for _, id := range a.e.DB.QueryInt64s(`SELECT "user"."id" FROM "alert" "me" JOIN "user" "user" ON "user"."id" = "me"."user_id" WHERE ( "view_id" = ? )`, view.ID) {
			usersSearch = append(usersSearch, a.e.FindUser(id))
		}
	}
	users := []*User{nil}
	if curuser {
		users = usersSearch
	} else {
		for _, u := range usersSearch {
			if u != nil && u.HasViewLimit(a.layout.InstanceID) {
				users = usersSearch
				break
			}
		}
	}
	for _, u := range users {
		// Generally the cache is generated without user permissions limiting
		// it, except with CURUSER or a view-limited user
		recs := a.e.NewRecords(a.layout, u)
		recs.View = view
		var userID any
		if curuser {
			userID = u.ID
		}
		exists := map[int64]bool{}
		for {
			rec := recs.Single()
			if rec == nil {
				break
			}
			for _, col := range view.Columns() {
				id, ok := a.alertCacheID(col, view.ID, rec.CurrentID(), userID)
				if !ok {
					id = a.e.DB.InsertMap("alert_cache", map[string]any{"layout_id": col, "view_id": view.ID, "current_id": rec.CurrentID(), "user_id": userID})
				}
				exists[id] = true
			}
		}
		// Delete any that should no longer exist
		var ids []int64
		if userID == nil {
			ids = a.e.DB.QueryInt64s(`SELECT "me"."id" FROM "alert_cache" "me" WHERE ( ( "user_id" IS NULL AND "view_id" = ? ) )`, view.ID)
		} else {
			ids = a.e.DB.QueryInt64s(`SELECT "me"."id" FROM "alert_cache" "me" WHERE ( ( "user_id" = ? AND "view_id" = ? ) )`, userID, view.ID)
		}
		for _, id := range ids {
			if !exists[id] {
				a.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( "id" = ? )`, id)
			}
		}
	}
	if curuser {
		// Possibly just changed to curuser: clean up rows without a user
		a.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( ( "user_id" IS NULL AND "view_id" = ? ) )`, view.ID)
		if allUsers && len(users) > 0 {
			// As the Perl search user_id => [ '-and', [ {'!=' => id}, ... ] ],
			// which SQL::Abstract renders as an OR of the inequalities
			var terms []string
			args := []any{}
			for _, u := range users {
				terms = append(terms, `"user_id" != ?`)
				args = append(args, u.ID)
			}
			args = append(args, view.ID)
			a.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( ( ( `+strings.Join(terms, " OR ")+` ) AND "view_id" = ? ) )`, args...)
		}
	} else {
		// Clean up user specific rows for a (now) non-curuser alert
		a.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( ( "user_id" IS NOT NULL AND "view_id" = ? ) )`, view.ID)
	}
}

func (a *Alert) alertCacheID(layoutID, viewID, currentID int64, userID any) (int64, bool) {
	if userID == nil {
		return a.e.DB.QueryInt64(`SELECT "me"."id" FROM "alert_cache" "me" WHERE ( ( "current_id" = ? AND "layout_id" = ? AND "user_id" IS NULL AND "view_id" = ? ) )`, currentID, layoutID, viewID)
	}
	return a.e.DB.QueryInt64(`SELECT "me"."id" FROM "alert_cache" "me" WHERE ( ( "current_id" = ? AND "layout_id" = ? AND "user_id" = ? AND "view_id" = ? ) )`, currentID, layoutID, userID, viewID)
}

// ---------------------------------------------------------------------
// Searching records in views
// ---------------------------------------------------------------------

// viewMatch is a record found in a view (for a user if the view has
// CURUSER).
type viewMatch struct {
	view   *View
	id     int64
	userID int64
}

// SearchView returns which of the current IDs are in the view
// (Records->search_view). Deleted records are not excluded.
func (r *Records) SearchView(currentIDs []int64, view *View, user *User) []viewMatch {
	if view == nil || len(currentIDs) == 0 {
		return nil
	}
	// The view must be set, otherwise general searches miss its conditions
	r.View = view
	// No columns, otherwise the joins would include all columns
	r.SetColumns([]int64{})
	var found []viewMatch
	var userID int64
	if user != nil {
		userID = user.ID
	}
	// The user's view limits are those of the user of the Records
	// (GADS-dev): the user argument is only that of the search construct
	{
		decoded := view.Filter().AsHash()
		if len(decoded) == 0 {
			// No filter, definitely in view
			for _, id := range currentIDs {
				found = append(found, viewMatch{view: view, id: id, userID: userID})
			}
			return found
		}
		so := searchOpts{joinOpts: joinOpts{currentVersionOnly: true}, ignorePerms: true, user: user}
		searches := []cond{condEq("me.instance_id", r.Layout.InstanceID)}
		// Search construct twice, so that value joins are consistently numbered
		r.searchConstruct(decoded, so)
		if c := r.searchConstruct(decoded, so); c != nil {
			searches = append(searches, c)
		}
		searches = append(searches, r.viewLimitsSearch(searchOpts{joinOpts: joinOpts{currentVersionOnly: true}})...)
		for i := 0; i < len(currentIDs); i += 500 {
			max := i + 500
			if max > len(currentIDs) {
				max = len(currentIDs)
			}
			ids := make([]any, 0, max-i)
			for _, id := range currentIDs[i:max] {
				ids = append(ids, id)
			}
			where := []cond{condAnd(searches), condCmp{field: "me.id", op: "=", value: ids}}
			jr := newJoinRenderer(r.e, formatRewind(r.Rewind))
			jr.layout = r.Layout
			st := r.store()
			if linked := r.linkedJoins(joinOpts{search: true, hasLinked: true, currentVersionOnly: true}); linked != nil {
				jr.render("current", "me", []*joinSpec{linked})
			}
			jr.render("current", "me", []*joinSpec{r.versionJoin(true, st.jpfetch(joinOpts{search: true, hasLinked: true, currentVersionOnly: true}))})
			args := append([]any{}, jr.args...)
			var wargs []any
			w := renderCond(condAnd(where), &wargs)
			args = append(args, wargs...)
			sql := `SELECT "me"."id" FROM "current" "me"` + jr.sb.String() + " WHERE " + w
			for _, id := range r.e.DB.QueryInt64s(sql, args...) {
				found = append(found, viewMatch{view: view, id: id, userID: userID})
			}
		}
	}
	return found
}

// ---------------------------------------------------------------------
// Sending alerts
// ---------------------------------------------------------------------

// AlertSend works out the alerts caused by changes to records
// (GADS::AlertSend).
type AlertSend struct {
	e          *Engine
	layout     *Layout
	CurrentIDs []int64
	// Columns are the IDs of the columns that changed
	Columns []int64
	// CurrentNew is whether this is a brand new record
	CurrentNew bool

	description *AlertDescription
}

// NewAlertSend creates an alert sender for changed records.
func (e *Engine) NewAlertSend(layout *Layout, currentIDs, columns []int64, currentNew bool) *AlertSend {
	return &AlertSend{e: e, layout: layout, CurrentIDs: currentIDs, Columns: columns, CurrentNew: currentNew}
}

// ProcessAlerts is the engine's default alert sender.
func ProcessAlerts(e *Engine, layout *Layout, currentIDs []int64, columns []int64, isNew bool) {
	e.NewAlertSend(layout, currentIDs, columns, isNew).Process()
}

func int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// inList renders "field IN ( ?, ... )" as SQL::Abstract does (0=1 if
// empty).
func inList(field string, n int) string {
	if n == 0 {
		return "0=1"
	}
	return field + " IN ( " + db.Placeholders(n) + " )"
}

// Process queues or sends the alerts.
func (s *AlertSend) Process() {
	// First the direct layout
	s.processInstance(s.layout, s.CurrentIDs)

	old := s.e.IgnorePermissions
	s.e.IgnorePermissions = true
	defer func() { s.e.IgnorePermissions = old }()

	// Then views in other layouts containing these fields in their filters
	args := append([]any{s.layout.InstanceID}, int64Args(s.Columns)...)
	instanceIDs := s.e.DB.QueryInt64s(`SELECT "me"."instance_id" FROM "view" "me" LEFT JOIN "alert" "alerts" ON "alerts"."view_id" = "me"."id" LEFT JOIN "filter" "filters" ON "filters"."view_id" = "me"."id" WHERE ( ( "alerts"."id" IS NOT NULL AND "instance_id" != ? AND `+inList(`"filters"."layout_id"`, len(s.Columns))+` ) ) GROUP BY "me"."instance_id"`, args...)
	for _, instanceID := range instanceIDs {
		layout := s.e.NewLayout(instanceID, nil)
		// Any current IDs that may have been affected, including historical
		// versions (in case a filter uses previous values)
		args := append([]any{instanceID}, int64Args(s.CurrentIDs)...)
		cids := s.e.DB.QueryInt64s(`SELECT "record"."current_id" FROM "curval" "me" JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" JOIN "record" "record" ON "record"."id" = "me"."record_id" WHERE ( ( "layout"."instance_id" = ? AND `+inList(`"value"`, len(s.CurrentIDs))+` ) ) GROUP BY "record"."current_id"`, args...)
		s.processInstance(layout, cids)
	}
}

type alertItem struct {
	view      *View
	currentID int64
	userID    int64
}

func (s *AlertSend) processInstance(layout *Layout, currentIDs []int64) {
	// The views that may have been affected: those with a filter on the
	// changed columns, or all views for a brand new record
	where := `"alerts"."id" IS NOT NULL AND "instance_id" = ?`
	args := []any{layout.InstanceID}
	if !s.CurrentNew {
		where = `"alerts"."id" IS NOT NULL AND ` + inList(`"filters"."layout_id"`, len(s.Columns)) + ` AND "instance_id" = ?`
		args = append(int64Args(s.Columns), layout.InstanceID)
	}
	viewIDs := s.e.DB.QueryInt64s(`SELECT "me"."id" FROM "view" "me" LEFT JOIN "alert" "alerts" ON "alerts"."view_id" = "me"."id" LEFT JOIN "filter" "filters" ON "filters"."view_id" = "me"."id" WHERE ( ( `+where+` ) ) GROUP BY "me"."id"`, args...)

	countRecs := s.e.NewRecords(layout, nil)
	countRecs.SetColumns([]int64{})
	totalRecords := countRecs.Count()
	allIDs := len(currentIDs) == totalRecords

	var toAdd []map[string]any
	var views []*View
	for _, id := range viewIDs {
		if v := s.e.viewForAlerts(id, layout, true); v != nil {
			views = append(views, v)
		}
	}
	if len(views) > 0 {
		// The views the records are now in
		var foundIn []viewMatch
		for _, view := range views {
			// User-specific view searches are needed if the view has a
			// CURUSER filter or an alert's user has a restricted view
			hasUnrestricted := false
			var users []*User
			for _, uid := range s.e.DB.QueryInt64s(`SELECT "me"."user_id" FROM "alert" "me" WHERE ( "me"."view_id" = ? )`, view.ID) {
				u := s.e.FindUser(uid)
				if u.HasViewLimit(layout.InstanceID) || view.HasCuruser() {
					users = append(users, u)
				} else {
					// Alerts should not be affected by the current user
					hasUnrestricted = true
				}
			}
			if hasUnrestricted {
				users = append(users, nil)
			}
			for _, u := range users {
				recs := s.e.NewRecords(layout, u)
				recs.SetColumns([]int64{})
				foundIn = append(foundIn, recs.SearchView(currentIDs, view, nil)...)
			}
		}
		nowIn := map[string]bool{}
		type pending struct {
			key string
			m   viewMatch
		}
		var arrivedOrder []pending
		nowIn2 := map[string]bool{}
		for _, m := range foundIn {
			key := alertKey(m.view.ID, m.userID, m.id)
			nowIn[key] = true
			if !nowIn2[key] {
				arrivedOrder = append(arrivedOrder, pending{key, m})
			}
			nowIn2[key] = true
		}
		// Compare with the cache, to find the views the records were in
		type original struct{ currentID, userID, viewID int64 }
		var originals []original
		viewArgs := int64Args(viewIDs)
		for i := 0; i < len(currentIDs) || (i == 0 && allIDs); i += 500 {
			w := inList(`"view"."id"`, len(viewIDs))
			a := append([]any{}, viewArgs...)
			if !allIDs {
				max := i + 500
				if max > len(currentIDs) {
					max = len(currentIDs)
				}
				w = inList(`"me"."current_id"`, max-i) + ` AND ` + w
				a = append(int64Args(currentIDs[i:max]), a...)
			}
			for _, row := range s.e.DB.Query(`SELECT MAX( "me"."current_id" ) AS "current_id", MAX( "me"."user_id" ) AS "user_id", MAX( "me"."view_id" ) AS "view_id" FROM "alert_cache" "me" JOIN "view" "view" ON "view"."id" = "me"."view_id" WHERE ( ( `+w+` ) ) GROUP BY "me"."view_id", "me"."user_id", "me"."current_id" ORDER BY "me"."view_id", "me"."user_id", "me"."current_id"`, a...) {
				cid, _ := anyToInt64(row["current_id"])
				uid, _ := anyToInt64(row["user_id"])
				vid, _ := anyToInt64(row["view_id"])
				originals = append(originals, original{cid, uid, vid})
			}
			if allIDs {
				break
			}
		}
		// Work out whether the records have disappeared from any views
		var gone []alertItem
		for _, o := range originals {
			key := alertKey(o.viewID, o.userID, o.currentID)
			if !nowIn[key] {
				gone = append(gone, alertItem{view: s.e.viewForAlerts(o.viewID, layout, true), currentID: o.currentID, userID: o.userID})
				if o.userID == 0 {
					s.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( ( "current_id" = ? AND "user_id" IS NULL AND "view_id" = ? ) )`, o.currentID, o.viewID)
				} else {
					s.e.DB.Exec(`DELETE FROM "alert_cache" WHERE ( ( "current_id" = ? AND "user_id" = ? AND "view_id" = ? ) )`, o.currentID, o.userID, o.viewID)
				}
			} else {
				// No change. Anything left is new in the view
				delete(nowIn2, key)
			}
		}
		// The views the records are new in
		var arrived []alertItem
		for _, p := range arrivedOrder {
			if !nowIn2[p.key] {
				continue
			}
			var userID any
			if p.m.userID != 0 {
				userID = p.m.userID
			}
			for _, col := range p.m.view.Columns() {
				toAdd = append(toAdd, map[string]any{"user_id": userID, "view_id": p.m.view.ID, "layout_id": col, "current_id": p.m.id})
			}
			arrived = append(arrived, alertItem{view: p.m.view, currentID: p.m.id, userID: p.m.userID})
		}
		s.goneArrived("gone", gone)
		s.goneArrived("arrived", arrived)
	}

	// Now find which values have changed in each view, from the changed
	// columns and records in the cache
	type cacheRow struct {
		viewID, layoutID, currentID, userID int64
	}
	var viewOrder []int64
	caches := map[int64][]cacheRow{}
	for i := 0; i < len(currentIDs) || (i == 0 && allIDs); i += 500 {
		w := inList(`"alert_caches"."layout_id"`, len(s.Columns)) + ` AND "me"."instance_id" = ?`
		a := append(int64Args(s.Columns), layout.InstanceID)
		if !allIDs {
			max := i + 500
			if max > len(currentIDs) {
				max = len(currentIDs)
			}
			w = inList(`"alert_caches"."current_id"`, max-i) + ` AND ` + w
			a = append(int64Args(currentIDs[i:max]), a...)
		}
		for _, row := range s.e.DB.Query(`SELECT "me"."id" AS "view_id", "alert_caches"."layout_id" AS "layout_id", "alert_caches"."current_id" AS "current_id", "alert_caches"."user_id" AS "user_id" FROM "view" "me" LEFT JOIN "alert_cache" "alert_caches" ON "alert_caches"."view_id" = "me"."id" WHERE ( ( `+w+` ) ) ORDER BY "me"."id", "alert_caches"."id"`, a...) {
			vid, _ := anyToInt64(row["view_id"])
			lid, _ := anyToInt64(row["layout_id"])
			cid, _ := anyToInt64(row["current_id"])
			uid, _ := anyToInt64(row["user_id"])
			if _, ok := caches[vid]; !ok {
				viewOrder = append(viewOrder, vid)
			}
			caches[vid] = append(caches[vid], cacheRow{vid, lid, cid, uid})
		}
		if allIDs {
			break
		}
	}
	for _, vid := range viewOrder {
		type sendNow struct {
			user   *User
			cids   []int64
			colIDs []int64
		}
		var sendOrder []int64
		send := map[int64]*sendNow{}
		for _, ac := range caches[vid] {
			type alertRow struct{ id, userID, frequency int64 }
			var alerts []alertRow
			var rows []db.Row
			if ac.userID != 0 {
				rows = s.e.DB.Query(`SELECT "me"."id", "me"."user_id", "me"."frequency" FROM "alert" "me" WHERE ( ( "user_id" = ? AND "view_id" = ? ) )`, ac.userID, ac.viewID)
			} else {
				rows = s.e.DB.Query(`SELECT "me"."id", "me"."user_id", "me"."frequency" FROM "alert" "me" WHERE ( "me"."view_id" = ? )`, ac.viewID)
			}
			for _, row := range rows {
				id, _ := anyToInt64(row["id"])
				uid, _ := anyToInt64(row["user_id"])
				freq, _ := anyToInt64(row["frequency"])
				alerts = append(alerts, alertRow{id, uid, freq})
			}
			for _, al := range alerts {
				// Check each user of this alert can read the field
				col := layout.Column(ac.layoutID)
				if col == nil || !col.UserIDCan(al.userID, "read") {
					continue
				}
				if al.frequency != 0 {
					// Send later. Unique constraint, so ignore failures
					s.e.DB.TryInsertMap("alert_send", map[string]any{"alert_id": al.id, "layout_id": ac.layoutID, "current_id": ac.currentID, "status": "changed"})
				} else {
					if send[al.userID] == nil {
						send[al.userID] = &sendNow{user: s.e.FindUser(al.userID)}
						sendOrder = append(sendOrder, al.userID)
					}
					send[al.userID].colIDs = append(send[al.userID].colIDs, ac.layoutID)
					send[al.userID].cids = append(send[al.userID].cids, ac.currentID)
				}
			}
		}
		if len(sendOrder) > 0 {
			view := s.e.viewForAlerts(vid, layout, true)
			for _, uid := range sendOrder {
				a := send[uid]
				var names []string
				for _, c := range a.colIDs {
					names = append(names, layout.Column(c).Name)
				}
				s.sendAlert("changed", a.cids, view, []*User{a.user}, names)
			}
		}
	}

	// Finally update the alert cache. Not done earlier, otherwise a new
	// record would be flagged as a change
	s.e.DB.Populate("alert_cache", toAdd)
}

func alertKey(viewID, userID, currentID int64) string {
	u := ""
	if userID != 0 {
		u = strconv.FormatInt(userID, 10)
	}
	return fmt.Sprintf("%d_%s_%d", viewID, u, currentID)
}

func (s *AlertSend) goneArrived(action string, items []alertItem) {
	for _, item := range items {
		var users []*User
		for _, row := range s.e.DB.Query(`SELECT "me"."id", "me"."user_id", "me"."frequency" FROM "alert" "me" WHERE ( "me"."view_id" = ? )`, item.view.ID) {
			id, _ := anyToInt64(row["id"])
			uid, _ := anyToInt64(row["user_id"])
			freq, _ := anyToInt64(row["frequency"])
			if freq != 0 {
				// Send later. Unique constraint, so ignore failures
				s.e.DB.TryInsertMap("alert_send", map[string]any{"alert_id": id, "current_id": item.currentID, "status": action})
			} else {
				users = append(users, s.e.FindUser(uid))
			}
		}
		if len(users) > 0 {
			s.sendAlert(action, []int64{item.currentID}, item.view, users, nil)
		}
	}
}

func uniqInt64s(ids []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func uniqStrings(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func (s *AlertSend) alertDescription() *AlertDescription {
	if s.description == nil {
		s.description = s.e.NewAlertDescription()
	}
	return s.description
}

func (s *AlertSend) sendAlert(action string, currentIDs []int64, view *View, users []*User, columns []string) {
	base := s.e.Config.URL
	if base == "" {
		Panic("URL not configured in application config")
	}
	for _, user := range users {
		viewName := view.Name
		cids := uniqInt64s(currentIDs)
		ad := s.alertDescription()
		description := ad.Description(s.layout.InstanceID, cids, user)
		link := ad.Link(s.layout.InstanceID, cids, user)
		var text, html string
		switch action {
		case "changed":
			cnames := strings.Join(uniqStrings(columns), ", ")
			text = "The following items were changed for " + description + ": " + cnames + "\n\n"
			text += "Links to the records are as follows:\n"
			html = "<p>The following items were changed for " + link + ": " + cnames + "</p>"
		case "arrived":
			text = `New items as follows have appeared in the view "` + viewName + `": ` + description + "\n\n"
			text += "Links to the new items are as follows:\n"
			html = "<p>New items as follows have appeared in the view &quot;" + viewName + "&quot;: " + link + "</p>"
		case "gone":
			text = `Items as follows have disappeared from the view "` + viewName + `": ` + description + "\n\n"
			text += "Links to the removed items are as follows:\n"
			html = "<p>Items as follows have disappeared from the view &quot;" + viewName + "&quot;: " + link + "</p>"
		}
		for _, cid := range cids {
			text += base + "record/" + strconv.FormatInt(cid, 10) + "\n"
		}
		s.e.SendEmail(Email{Subject: `Changes in view "` + viewName + `"`, To: []string{user.Email}, Text: text, HTML: html})
	}
}

// ---------------------------------------------------------------------
// Alert descriptions
// ---------------------------------------------------------------------

// AlertDescription provides descriptions of records for email alerts
// (GADS::AlertDescription).
type AlertDescription struct {
	e            *Engine
	cache        map[int64]map[int64]string
	columnsCache map[int64][]*Column
}

// NewAlertDescription creates an alert description builder.
func (e *Engine) NewAlertDescription() *AlertDescription {
	return &AlertDescription{e: e, cache: map[int64]map[int64]string{}, columnsCache: map[int64][]*Column{}}
}

// AlertColumns returns the columns used to describe records of an
// instance in alerts.
func (d *AlertDescription) AlertColumns(instanceID int64) []*Column {
	if cols, ok := d.columnsCache[instanceID]; ok {
		return cols
	}
	old := d.e.IgnorePermissions
	d.e.IgnorePermissions = true
	defer func() { d.e.IgnorePermissions = old }()
	cols := d.e.NewLayout(instanceID, nil).AlertColumns()
	d.columnsCache[instanceID] = cols
	return cols
}

// OnlyID reports whether records are described only by their ID.
func (d *AlertDescription) OnlyID(instanceID int64) bool {
	cols := d.AlertColumns(instanceID)
	return len(cols) == 1 && cols[0].NameShort == "_id"
}

// Descriptions returns the description of each record.
func (d *AlertDescription) Descriptions(instanceID int64, currentIDs []int64, user *User) []string {
	var out []string
	for _, cid := range currentIDs {
		if d.cache[user.ID] == nil {
			d.cache[user.ID] = map[int64]string{}
		}
		if c, ok := d.cache[user.ID][cid]; ok && c != "" && c != "0" {
			out = append(out, c)
			continue
		}
		cols := d.AlertColumns(instanceID)
		var desc string
		if d.OnlyID(instanceID) {
			desc = strconv.FormatInt(cid, 10)
		} else {
			var ids []int64
			for _, c := range cols {
				ids = append(ids, c.ID)
			}
			// Has the record since been deleted?
			row := d.e.DB.QueryRow(`SELECT "me"."deleted", "me"."instance_id" FROM "current" "me" WHERE "me"."id" = ?`, cid)
			if row["deleted"] != nil {
				desc = "[record deleted]"
			} else {
				inst, _ := anyToInt64(row["instance_id"])
				rec := d.e.NewRecord(d.e.NewLayout(inst, user), user)
				rec.SetColumns(ids)
				rec.FindCurrentID(cid, FindOptions{})
				var parts []string
				for _, id := range ids {
					if rec.Layout().ColumnPerm(id, "read") == nil {
						continue
					}
					if s := rec.Fields()[id].AsString(); s != "" && s != "0" {
						parts = append(parts, s)
					}
				}
				desc = strings.Join(parts, ", ")
			}
		}
		d.cache[user.ID][cid] = desc
		out = append(out, desc)
	}
	return out
}

func (d *AlertDescription) joinDelim(instanceID int64) string {
	if d.OnlyID(instanceID) {
		return ", "
	}
	return "; "
}

// Description describes the records as text.
func (d *AlertDescription) Description(instanceID int64, currentIDs []int64, user *User) string {
	descs := d.Descriptions(instanceID, currentIDs, user)
	desc := strings.Join(descs, d.joinDelim(instanceID))
	if d.OnlyID(instanceID) {
		if len(descs) == 1 {
			desc = "record ID " + desc
		} else {
			desc = "record IDs " + desc
		}
	}
	return desc
}

// Link describes the records as HTML links.
func (d *AlertDescription) Link(instanceID int64, currentIDs []int64, user *User) string {
	descs := d.Descriptions(instanceID, currentIDs, user)
	url := d.e.Config.URL
	prefix := ""
	if d.OnlyID(instanceID) {
		descs = nil
		for _, cid := range currentIDs {
			descs = append(descs, "ID "+strconv.FormatInt(cid, 10))
		}
		if len(currentIDs) == 1 {
			prefix = "record "
		} else {
			prefix = "records "
		}
	}
	var links []string
	for i, cid := range currentIDs {
		desc := ""
		if i < len(descs) {
			desc = descs[i]
		}
		links = append(links, `<a href="`+url+`/record/`+strconv.FormatInt(cid, 10)+`">`+desc+`</a>`)
	}
	return prefix + strings.Join(links, d.joinDelim(instanceID))
}

// ---------------------------------------------------------------------
// Layout alert columns
// ---------------------------------------------------------------------

// AlertColumns returns the columns describing records in alerts, or the ID
// column if none are set.
func (l *Layout) AlertColumns() []*Column {
	var cols []*Column
	for _, id := range l.e.DB.QueryInt64s(`SELECT "me"."layout_id" FROM "alert_column" "me" WHERE "me"."instance_id" = ?`, l.InstanceID) {
		if c := l.Column(id); c != nil {
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		cols = append(cols, l.ColumnByNameShort("_id"))
	}
	return cols
}

// SetAlertColumns sets the columns describing records in alerts.
func (l *Layout) SetAlertColumns(ids []int64) {
	l.e.DB.Exec(`DELETE FROM "alert_column" WHERE ( "instance_id" = ? )`, l.InstanceID)
	for _, id := range ids {
		l.e.DB.InsertMap("alert_column", map[string]any{"layout_id": id, "instance_id": l.InstanceID})
	}
}

// UserAlerts is GADS::Alert->all: the alerts of a user keyed by view ID
// (nil if the user has no alerts)
func (e *Engine) UserAlerts(user *User) map[int64]db.Row {
	var out map[int64]db.Row
	for _, r := range e.DB.Query(`SELECT "me".* FROM "alert" "me" WHERE "me"."user_id" = ?`, user.ID) {
		if out == nil {
			out = map[int64]db.Row{}
		}
		vid, _ := db.AsInt64(r["view_id"])
		out[vid] = db.Row{"id": r["id"], "view_id": r["view_id"], "frequency": r["frequency"]}
	}
	return out
}

// LoadViewOverride is GADS::Views->view with user_permission_override: the
// view without checks of the user's access (nil if it does not exist)
func (e *Engine) LoadViewOverride(id int64, layout *Layout) *View {
	return e.viewForAlerts(id, layout, true)
}
