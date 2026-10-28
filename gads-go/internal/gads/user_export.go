package gads

import (
	"regexp"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// ExportUsers is GADS::Users csv: an export of the users is created for the
// user and produced (inline: Perl forks a process for it)
func (e *Engine) ExportUsers(user *User) {
	id := e.DB.InsertMap("export", map[string]any{"user_id": user.ID, "type": "users", "started": e.Now()})
	var content string
	if err := Try(func() { content = e.usersCSV(user) }); err != nil {
		msg := err.Error()
		if ge, ok := err.(*Error); ok {
			msg = ge.Msg
		}
		e.DB.UpdateMap("export", map[string]any{"result": "Failed", "result_internal": msg, "completed": e.Now()}, map[string]any{"id": id})
		return
	}
	e.DB.UpdateMap("export", map[string]any{
		"completed": e.Now(),
		"result":    "Success",
		"mimetype":  "text/csv",
		"content":   content,
	}, map[string]any{"id": id})
}

var reCSVInjection = regexp.MustCompile(`^[-+=@]+`)

// usersCSV is GADS::Users _produce_csv
func (e *Engine) usersCSV(user *User) string {
	site := e.Site()
	layouts := e.NewInstances(user).All()
	cols := []string{"ID", "Surname", "Forename", "Email", "Lastlogin", "Created"}
	if site.flag("register_show_title") {
		cols = append(cols, "Title")
	}
	if site.flag("register_show_organisation") {
		cols = append(cols, "Organisation")
	}
	if site.flag("register_show_department") {
		cols = append(cols, site.DepartmentName())
	}
	if site.flag("register_show_team") {
		cols = append(cols, site.TeamName())
	}
	ft1, ft2 := site.str("register_freetext1_name"), site.str("register_freetext2_name")
	if ft1 != "" {
		cols = append(cols, ft1)
	}
	if ft2 != "" {
		cols = append(cols, ft2)
	}
	cols = append(cols, "Notes", "Permissions", "Groups", "Page hits last month")
	for _, l := range layouts {
		cols = append(cols, `Last hit to table "`+l.Name()+`"`)
	}
	out := csvLine(cols)

	month := e.Now().AddDate(0, -1, 0).Format("2006-01-02")
	rows := e.DB.Query(`SELECT "me".*, "title"."name" AS "title_name", "organisation"."name" AS "organisation_name", "department"."name" AS "department_name", "team"."name" AS "team_name" FROM "user" "me" LEFT JOIN "organisation" "organisation" ON "organisation"."id" = "me"."organisation" LEFT JOIN "department" "department" ON "department"."id" = "me"."department_id" LEFT JOIN "team" "team" ON "team"."id" = "me"."team_id" LEFT JOIN "title" "title" ON "title"."id" = "me"."title" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL` + e.siteCond("me") + ` ORDER BY "me"."surname", "me"."id"`)
	str := func(v any) string { s, _ := db.AsString(v); return s }
	for _, r := range rows {
		id := r["id"]
		fields := []string{str(id), str(r["surname"]), str(r["firstname"]), str(r["email"]), str(r["lastlogin"]), str(r["created"])}
		if site.flag("register_show_title") {
			fields = append(fields, str(r["title_name"]))
		}
		if site.flag("register_show_organisation") {
			fields = append(fields, str(r["organisation_name"]))
		}
		if site.flag("register_show_department") {
			fields = append(fields, str(r["department_name"]))
		}
		if site.flag("register_show_team") {
			fields = append(fields, str(r["team_name"]))
		}
		if ft1 != "" {
			fields = append(fields, str(r["freetext1"]))
		}
		if ft2 != "" {
			fields = append(fields, str(r["freetext2"]))
		}
		fields = append(fields, str(r["account_request_notes"]))
		var perms, groups []string
		for _, p := range e.DB.Query(`SELECT "permission"."description" FROM "user_permission" "me" JOIN "permission" "permission" ON "permission"."id" = "me"."permission_id" WHERE "me"."user_id" = ? ORDER BY "me"."id"`, id) {
			perms = append(perms, str(p["description"]))
		}
		for _, g := range e.DB.Query(`SELECT "group"."name" FROM "user_group" "me" JOIN "group" "group" ON "group"."id" = "me"."group_id" WHERE "me"."user_id" = ? ORDER BY "me"."id"`, id) {
			groups = append(groups, str(g["name"]))
		}
		fields = append(fields, strings.Join(perms, "; "), strings.Join(groups, "; "))
		hits, _ := e.DB.QueryInt64(`SELECT COUNT("id") FROM "audit" WHERE "user_id" = ? AND "datetime" > ?`, id, month)
		fields = append(fields, itoa(hits))
		for _, l := range layouts {
			row := e.DB.QueryRow(`SELECT MAX("datetime") AS "m" FROM "audit" WHERE "user_id" = ? AND "instance_id" = ?`, id, l.InstanceID)
			fields = append(fields, str(row["m"]))
		}
		// Remove CSV vulnerabilities for Excel
		for i, f := range fields {
			if f == "" || f == "0" {
				fields[i] = ""
			} else {
				fields[i] = reCSVInjection.ReplaceAllString(f, "")
			}
		}
		out += csvLine(fields)
	}
	return out
}
