package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/perljson"
)

func init() {
	Commands["export.pl"] = Export
}

func inList(n int) string {
	return "(" + strings.TrimSuffix(strings.Repeat("?, ", n), ", ") + ")"
}

func int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// Export is bin/export.pl: writes the configuration of a site (and with
// --include-data its users and records) to the directory _export, as a
// JSON file for each item
func Export(env *Env) {
	var siteID string
	var instanceIDs []string
	var includeData bool
	if !env.GetOptions(
		Option{Name: "site-id", Value: true, String: &siteID},
		Option{Name: "instance-id", Value: true, Multi: true, Ints: &instanceIDs},
		Option{Name: "include-data", Bool: &includeData},
	) {
		env.Exit(0)
	}
	if perlFalse(siteID) {
		gads.Fail("Please provide site ID with --site-id")
	}
	mkdir := func(dir, what string) {
		if err := os.Mkdir(env.Path(dir), 0o777); err != nil {
			gads.Fail("Unable to create %s directory: %s", what, perlErr(err))
		}
	}
	write := func(file string, v any) {
		if err := os.WriteFile(env.Path(file), []byte(perljson.Pretty(v)), 0o666); err != nil {
			gads.Fail("Error opening %s for write: %s", file, perlErr(err))
		}
	}
	if err := os.Mkdir(env.Path("_export"), 0o777); err != nil {
		gads.Fail("Unable to create export directory: %s", perlErr(err))
	}
	e := env.E
	d := e.DB
	e.SiteID, _ = strconv.ParseInt(siteID, 10, 64)
	old := e.IgnorePermissions
	e.IgnorePermissions = true
	defer func() { e.IgnorePermissions = old }()
	instances := e.NewInstances(nil)
	var layouts []*gads.Layout
	if len(instanceIDs) > 0 {
		for _, id := range instanceIDs {
			iid, _ := strconv.ParseInt(id, 10, 64)
			l := instances.Layout(iid)
			if l == nil {
				env.SayErr("notice: Layout for instance ID %s not found", id)
				Die(`Can't call method "instance_id" on an undefined value`)
			}
			layouts = append(layouts, l)
		}
	} else {
		layouts = instances.All()
	}
	var ids []int64
	for _, l := range layouts {
		ids = append(ids, l.InstanceID)
	}
	mkdir("_export/groups", "groups")
	if len(ids) > 0 {
		in := inList(len(ids))
		args := append(append([]any{}, int64Args(ids)...), int64Args(ids)...)
		for _, g := range d.Query(`SELECT DISTINCT "me"."id", "me"."name" FROM "group" "me" LEFT JOIN "layout_group" "layout_groups" ON "layout_groups"."group_id" = "me"."id" LEFT JOIN "layout" "layout" ON "layout"."id" = "layout_groups"."layout_id" LEFT JOIN "instance_group" "instance_groups" ON "instance_groups"."group_id" = "me"."id" WHERE ( "layout"."instance_id" IN `+in+` OR "instance_groups"."instance_id" IN `+in+` )`+siteAnd(e, "me")+` ORDER BY "me"."id"`, args...) {
			write("_export/groups/"+str(g["id"]), map[string]any{"id": g["id"], "name": g["name"]})
		}
	}
	dumpAll := func(path string, items []any, id func(any) string, hash func(any) map[string]any) {
		for i, it := range items {
			write(fmt.Sprintf("%s%05d_%s", path, i+1, id(it)), hash(it))
		}
	}
	rowID := func(v any) string { return str(v.(db.Row)["id"]) }
	if includeData {
		mkdir("_export/users", "users")
		var users []db.Row
		if len(instanceIDs) > 0 {
			in := inList(len(ids))
			args := append(append([]any{}, int64Args(ids)...), int64Args(ids)...)
			users = d.Query(`SELECT DISTINCT "me".* FROM "user" "me" LEFT JOIN "record" "record_createdbies" ON "record_createdbies"."createdby" = "me"."id" LEFT JOIN "current" "current" ON "current"."id" = "record_createdbies"."current_id" LEFT JOIN "current" "current_deletedbies" ON "current_deletedbies"."deletedby" = "me"."id" WHERE ( "current"."instance_id" IN `+in+` OR "current_deletedbies"."instance_id" IN `+in+` )`+siteAnd(e, "me")+` ORDER BY "me"."id"`, args...)
		} else {
			users = d.Query(`SELECT * FROM "user" "me" WHERE 1 = 1` + siteAnd(e, "me") + ` ORDER BY "me"."id"`)
		}
		var items []any
		for _, u := range users {
			items = append(items, u)
		}
		dumpAll("_export/users/", items, rowID, func(v any) map[string]any { return e.UserExportHash(v.(db.Row)) })
	}
	for _, layout := range layouts {
		iid := layout.InstanceID
		dir := fmt.Sprintf("_export/instance%d", iid)
		mkdir(dir, "instance")
		write(dir+"/instance", layout.Export())
		mkdir(dir+"/topics", "topics")
		for _, t := range d.Query(`SELECT * FROM "topic" WHERE "instance_id" = ? ORDER BY "id"`, iid) {
			write(dir+"/topics/"+str(t["id"]), map[string]any{
				"id":            t["id"],
				"name":          t["name"],
				"description":   t["description"],
				"initial_state": t["initial_state"],
				"click_to_edit": t["click_to_edit"],
				"instance_id":   t["instance_id"],
			})
		}
		mkdir(dir+"/layout", "layout")
		var cols []any
		for _, c := range layout.All(gads.AllOptions{OrderDependencies: true}) {
			cols = append(cols, c)
		}
		dumpAll(dir+"/layout/", cols, func(v any) string { return strconv.FormatInt(v.(*gads.Column).ID, 10) },
			func(v any) map[string]any { return v.(*gads.Column).ExportHash() })
		mkdir(dir+"/metrics", "metrics")
		var metrics []any
		for _, m := range d.Query(`SELECT * FROM "metric_group" "me" WHERE "instance_id" = ? ORDER BY "me"."name"`, iid) {
			metrics = append(metrics, m)
		}
		dumpAll(dir+"/metrics/", metrics, rowID, func(v any) map[string]any { return e.MetricGroupExportHash(v.(db.Row)) })
		mkdir(dir+"/graphs", "graphs")
		var graphs []any
		for _, g := range d.Query(`SELECT * FROM "graph" WHERE "instance_id" = ? ORDER BY "id"`, iid) {
			// Graphs of users are not included without include_data
			if !includeData && g["user_id"] != nil {
				continue
			}
			graphs = append(graphs, g)
		}
		dumpAll(dir+"/graphs/", graphs, rowID, func(v any) map[string]any { return gads.GraphExportHash(v.(db.Row)) })
		mkdir(dir+"/views", "views")
		var views []any
		for _, cond := range []string{`"global" = 1`, `"is_admin" = 1`} {
			for _, id := range d.QueryInt64s(`SELECT "id" FROM "view" WHERE `+cond+` AND "instance_id" = ? ORDER BY "id"`, iid) {
				if v := e.LoadViewOverride(id, layout); v != nil {
					views = append(views, v)
				}
			}
		}
		dumpAll(dir+"/views/", views, func(v any) string { return strconv.FormatInt(v.(*gads.View).ID, 10) },
			func(v any) map[string]any { return v.(*gads.View).ExportHash() })
		if includeData {
			mkdir(dir+"/records", "records")
			var currents []any
			for _, c := range d.Query(`SELECT * FROM "current" WHERE "draftuser_id" IS NULL AND "instance_id" = ? ORDER BY "id"`, iid) {
				currents = append(currents, c)
			}
			dumpAll(dir+"/records/", currents, rowID, func(v any) map[string]any { return e.CurrentExportHash(v.(db.Row)) })
		}
	}
}

// siteAnd is the condition of the site of a table with a site_id
func siteAnd(e *gads.Engine, alias string) string {
	if e.SiteID == 0 {
		return ""
	}
	return fmt.Sprintf(` AND "%s"."site_id" = %d`, alias, e.SiteID)
}
