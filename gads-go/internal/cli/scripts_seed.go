package cli

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

func init() {
	Commands["seed-database.pl"] = SeedDatabase
}

// permissionFixtures are the permissions fixture set of GADS
// (share/fixtures/<version>/permissions)
var permissionFixtures = []map[string]any{
	{"id": int64(3), "name": "useradmin", "description": "User can manage other user accounts", "order": int64(3)},
	{"id": int64(10), "name": "audit", "description": "User can access audit data", "order": int64(11)},
	{"id": int64(12), "name": "superadmin", "description": "Super-administrator", "order": nil},
}

// SeedDatabase is bin/seed-database.pl: creates the tables if needed (GADS
// installs them with DBIx::Class::Migration, here the schema of the Go
// port is deployed), the permissions, and the first site, user (with all
// permissions) and table. With TEST set in the environment, a basic user
// in a group is also created.
func SeedDatabase(env *Env) {
	var initialUsername, instanceName, host string
	if !env.GetOptions(
		Option{Name: "initial_username", Value: true, String: &initialUsername},
		Option{Name: "instance_name", Value: true, String: &instanceName},
		Option{Name: "site", Value: true, String: &host},
	) {
		env.Exit(0)
	}
	prompt := func(v *string, text string) {
		if *v == "" {
			env.Say("%s", text)
			*v = strings.TrimSuffix(env.ReadLine(), "\n")
		}
	}
	prompt(&instanceName, "Please enter the name of the first datasheet")
	prompt(&initialUsername, "Please enter the email address of the first user")
	prompt(&host, "Please enter the hostname that will be used to access this site")
	d := env.E.DB
	env.Say("Installing schema and fixtures if needed...")
	if gads.Try(func() { d.QueryInt64(`SELECT COUNT(*) FROM "site"`) }) != nil {
		if err := d.Deploy(); err != nil {
			Die("%s", err)
		}
	}
	if n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "permission"`); n == 0 {
		for _, p := range permissionFixtures {
			d.InsertMap("permission", p)
		}
	}
	if n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "permission"`); n == 0 {
		Die("No permissions populated. Do the fixtures exist?")
	}
	d.Txn(func() {
		env.Say(`Creating site "%s"...`, host)
		siteID := d.InsertMap("site", map[string]any{"host": host, "register_organisation_name": "Organisation"})
		env.Say(`Creating initial username "%s"...`, initialUsername)
		userID := d.InsertMap("user", map[string]any{"username": initialUsername, "email": initialUsername, "site_id": siteID})
		env.Say("Adding all permissions to initial username...")
		for _, p := range d.Query(`SELECT "id" FROM "permission" ORDER BY "id"`) {
			id, _ := db.AsInt64(p["id"])
			d.InsertMap("user_permission", map[string]any{"user_id": userID, "permission_id": id})
		}
		if env.Getenv("TEST") != "" {
			env.Say(`Creating basic user "basic@example.com" with no permissions...`)
			basic := d.InsertMap("user", map[string]any{"username": "basic@example.com", "email": "basic@exmaple.com", "site_id": siteID})
			env.Say("Creating group for basic user")
			group := d.InsertMap("group", map[string]any{"name": "basic", "site_id": siteID})
			env.Say("Adding basic user to basic group")
			d.InsertMap("user_group", map[string]any{"user_id": basic, "group_id": group})
		}
		env.Say("Creating initial datasheet...")
		d.InsertMap("instance", map[string]any{"name": instanceName, "site_id": siteID})
	})
}
