package gads

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// AllReports is Layout->reports(all => 1) for a user who can administer
// the table: all the reports of the table that are not deleted
func (l *Layout) AllReports() []db.Row {
	if !l.UserCan("layout") {
		var out []db.Row
		for _, r := range l.Reports() {
			out = append(out, r)
		}
		return out
	}
	return l.e.DB.Query(`SELECT "me".* FROM "report" "me" WHERE "me"."deleted" IS NULL AND "me"."instance_id" = ? ORDER BY "me"."id"`, l.InstanceID)
}

// SecurityMarking is Layout security_marking: that of the table, the site
// or the configuration (Instance read_security_marking)
func (l *Layout) SecurityMarking() string {
	if m, _ := db.AsString(l.Row()["security_marking"]); m != "" {
		return m
	}
	if m, _ := db.AsString(l.e.Site().Row()["security_marking"]); m != "" {
		return m
	}
	return l.e.Config.Header
}

// SetMarking is Layout set_marking
func (l *Layout) SetMarking(marking any) {
	l.e.DB.UpdateMap("instance", map[string]any{"security_marking": marking}, map[string]any{"id": l.InstanceID})
}

// ReportInput is the values of a report as edited
type ReportInput struct {
	Name, Title, Description, SecurityMarking any
	Layouts, Groups                           []string
}

// validateReport is Result::Report validate
func validateReport(name, title any) {
	if !truthy(name) {
		Fail("No name given")
	}
	if !truthy(title) {
		Fail("No title given")
	}
}

// CreateReport is ResultSet::Report create_report
func (e *Engine) CreateReport(user *User, instanceID int64, in ReportInput) int64 {
	var id int64
	e.DB.Txn(func() {
		validateReport(in.Name, in.Title)
		id = e.DB.InsertMap("report", map[string]any{
			"user_id": user.ID, "name": in.Name, "title": in.Title, "description": in.Description,
			"instance_id": instanceID, "createdby": user.ID, "created": e.Now(), "security_marking": in.SecurityMarking,
		})
		for _, l := range in.Layouts {
			e.DB.InsertMap("report_layout", map[string]any{"report_id": id, "layout_id": l})
		}
		for _, g := range in.Groups {
			e.DB.InsertMap("report_group", map[string]any{"report_id": id, "group_id": g})
		}
	})
	return id
}

// LoadReportForEdit is ResultSet::Report load_for_edit: a report that is
// not deleted
func (e *Engine) LoadReportForEdit(id any) db.Row {
	var row db.Row
	if n, ok := db.AsInt64(id); ok {
		row = e.DB.QueryRow(`SELECT "me".* FROM "report" "me" WHERE "me"."deleted" IS NULL AND "me"."id" = ?`, n)
	}
	if row == nil {
		Fail("No report found for id %s", anyToString(id))
	}
	return row
}

// ReportLayoutIDs are the fields of a report (report_layouts)
func (e *Engine) ReportLayoutIDs(reportID any) []int64 {
	return e.DB.QueryInt64s(`SELECT "layout_id" FROM "report_layout" WHERE "report_id" = ? ORDER BY "id"`, reportID)
}

// ReportGroupIDs is Result::Report group_ids
func (e *Engine) ReportGroupIDs(reportID any) []int64 {
	return e.DB.QueryInt64s(`SELECT "group_id" FROM "report_group" WHERE "report_id" = ? ORDER BY "id"`, reportID)
}

// UpdateReport is Result::Report update_report: the fields and groups not
// given are removed
func (e *Engine) UpdateReport(report db.Row, in ReportInput) {
	e.DB.Txn(func() {
		validateReport(in.Name, in.Title)
		e.DB.UpdateMap("report", map[string]any{
			"name": in.Name, "title": in.Title, "description": in.Description, "security_marking": in.SecurityMarking,
		}, map[string]any{"id": report["id"]})
		sync := func(table, column string, ids []string) {
			for _, v := range ids {
				if _, ok := e.DB.QueryInt64(`SELECT "id" FROM `+q(table)+` WHERE "report_id" = ? AND `+q(column)+` = ?`, report["id"], v); !ok {
					e.DB.InsertMap(table, map[string]any{"report_id": report["id"], column: v})
				}
			}
			where := ""
			args := []any{report["id"]}
			if len(ids) > 0 {
				var conds []string
				for _, v := range ids {
					conds = append(conds, q(column)+" != ?")
					args = append(args, v)
				}
				where = " AND ( " + strings.Join(conds, " AND ") + " )"
			}
			e.DB.Exec(`DELETE FROM `+q(table)+` WHERE "report_id" = ?`+where, args...)
		}
		sync("report_layout", "layout_id", in.Layouts)
		sync("report_group", "group_id", in.Groups)
	})
}

// RemoveReport is Result::Report remove: the report is marked deleted (the
// update validates the report)
func (e *Engine) RemoveReport(report db.Row) {
	if report["deleted"] != nil {
		return
	}
	validateReport(report["name"], report["title"])
	e.DB.UpdateMap("report", map[string]any{"deleted": e.Now()}, map[string]any{"id": report["id"]})
}

// GroupsViewableRows is groups_viewable as group rows
func (u *User) GroupsViewableRows() []db.Row {
	if u.Permission()["superadmin"] {
		return u.e.DB.Query(`SELECT * FROM "group" ORDER BY "id"`)
	}
	// Perl returns these in hash order: the order here is that of
	// GroupsViewable
	var out []db.Row
	seen := map[int64]bool{}
	ids := u.e.DB.QueryInt64s(`SELECT "me"."group_id" FROM "layout_group" "me" JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE "layout"."instance_id" IN (SELECT "me"."instance_id" FROM "instance_group" "me" JOIN "group" "group" ON "group"."id" = "me"."group_id" LEFT JOIN "user_group" "user_groups" ON "user_groups"."group_id" = "group"."id" WHERE "me"."permission" = ? AND "user_groups"."user_id" = ?)`, "layout", u.ID)
	ids = append(ids, u.e.DB.QueryInt64s(`SELECT "group_id" FROM "user_group" WHERE "user_id" = ?`, u.ID)...)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if row := u.e.DB.QueryRow(`SELECT * FROM "group" WHERE "id" = ?`, id); row != nil {
			out = append(out, row)
		}
	}
	return out
}
