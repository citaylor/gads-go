package gads

import "github.com/ctrlo/gads-go/internal/db"

// Views lists the views of a layout available to its user (GADS::Views).
type Views struct {
	e      *Engine
	layout *Layout
	// OtherUserID shows another user's views (only if the layout user has
	// the layout permission)
	OtherUserID *int64
}

// NewViews creates a views list for a layout.
func (e *Engine) NewViews(layout *Layout) *Views {
	return &Views{e: e, layout: layout}
}

// ViewSummary is a view as listed (the view row).
type ViewSummary struct {
	ID      int64
	Name    string
	Global  bool
	IsAdmin bool
}

// UserViewsAll returns the shared, then personal, then admin views of the
// user.
func (vs *Views) UserViewsAll() []ViewSummary {
	u := vs.UserViews()
	return append(append(u.Shared, u.Personal...), u.Admin...)
}

// UserViewList is GADS::Views->user_views
type UserViewList struct {
	Shared, Personal, Admin []ViewSummary
}

// UserViews returns the views of the user by type (shared, personal and
// admin)
func (vs *Views) UserViews() UserViewList {
	hasLayout := vs.layout.UserCan("layout")
	var userID any
	if hasLayout && vs.OtherUserID != nil {
		userID = *vs.OtherUserID
	} else if u := vs.layout.User(); u != nil {
		userID = u.ID
	}
	ownCond := `"me"."user_id" = ?`
	groupCond := `"user_groups"."user_id" = ?`
	var args []any
	if userID == nil {
		ownCond = `"me"."user_id" IS NULL`
		groupCond = `"user_groups"."user_id" IS NULL`
	} else {
		args = append(args, userID, userID)
	}
	or := ownCond + ` OR ( "global" = ? AND ( "me"."group_id" IS NULL OR ` + groupCond + ` ) )`
	if userID == nil {
		args = append(args, 1)
	} else {
		args = []any{userID, 1, userID}
	}
	if hasLayout {
		or += ` OR "is_admin" = ?`
		args = append(args, 1)
	}
	args = append(args, vs.layout.InstanceID)
	rows := vs.e.DB.Query(`SELECT "me"."id", "me"."name", "me"."global", "me"."is_admin" FROM "view" "me" LEFT JOIN "group" "group" ON "group"."id" = "me"."group_id" LEFT JOIN "user_group" "user_groups" ON "user_groups"."group_id" = "group"."id" WHERE ( ( ( `+or+` ) AND "instance_id" = ? ) ) ORDER BY "me"."global", "me"."is_admin", "me"."name"`, args...)
	seen := map[int64]bool{}
	var shared, personal, admin []ViewSummary
	for _, r := range rows {
		id, _ := db.AsInt64(r["id"])
		if seen[id] {
			continue
		}
		seen[id] = true
		name, _ := db.AsString(r["name"])
		v := ViewSummary{ID: id, Name: name, Global: rowBool(r["global"]), IsAdmin: rowBool(r["is_admin"])}
		switch {
		case v.Global:
			shared = append(shared, v)
		case v.IsAdmin:
			admin = append(admin, v)
		default:
			personal = append(personal, v)
		}
	}
	return UserViewList{Shared: shared, Personal: personal, Admin: admin}
}

// LimitExtra is GADS::Views->views_limit_extra: the views that can be
// selected as an extra limit of the records, ordered by name
// LimitOverride is views_limit_override: the view limit override views of
// the table (whatever the user's permissions)
func (vs *Views) LimitOverride() []db.Row {
	return vs.e.DB.Query(`SELECT "me".* FROM "view" "me" WHERE "me"."instance_id" = ? AND "me"."is_limit_override" = ? ORDER BY "me"."name"`, vs.layout.InstanceID, 1)
}

func (vs *Views) LimitExtra() []db.Row {
	if !vs.layout.UserCan("view_limit_extra") {
		return nil
	}
	return vs.e.DB.Query(`SELECT "me".* FROM "view" "me" WHERE "me"."instance_id" = ? AND "me"."is_limit_extra" = ? ORDER BY "me"."name"`, vs.layout.InstanceID, 1)
}
