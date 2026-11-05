package conformance

import (
	"fmt"
	"os"
	"strings"
)

// With GADS_DECOY_SITE=1 (SPEC.md "Decoy site") the first sheet of each
// database is in site 1 unless the scenario gives a site, and once it is
// built a second site (99) gets a copy of the rows of every table that
// GADS restricts to a site, with the same names and email addresses. GADS
// searches these tables in the site of the schema only, so that the
// observations are unchanged by the copies unless a search misses the
// site, in Perl or in Go. The Perl runner runs the same SQL.
func decoySite() bool { return os.Getenv("GADS_DECOY_SITE") != "" }

// decoySiteSQL are the statements that make the decoy site; ? is the site
// of the first sheet
const decoySiteSQL = `INSERT INTO "site" ("id", "host", "name") VALUES (99, 'decoy.invalid', 'Decoy site');
INSERT INTO "organisation" ("id", "name", "site_id", "deleted") SELECT "id" + 900000, "name", 99, "deleted" FROM "organisation" WHERE "site_id" = ?;
INSERT INTO "department" ("id", "name", "site_id", "deleted") SELECT "id" + 900000, "name", 99, "deleted" FROM "department" WHERE "site_id" = ?;
INSERT INTO "team" ("id", "name", "site_id", "deleted") SELECT "id" + 900000, "name", 99, "deleted" FROM "team" WHERE "site_id" = ?;
INSERT INTO "title" ("id", "name", "site_id", "deleted") SELECT "id" + 900000, "name", 99, "deleted" FROM "title" WHERE "site_id" = ?;
INSERT INTO "group" ("id", "name", "site_id") SELECT "id" + 900000, "name", 99 FROM "group" WHERE "site_id" = ?;
INSERT INTO "user" ("id", "site_id", "firstname", "surname", "email", "username", "value", "password", "pwchanged", "deleted", "account_request") SELECT "id" + 900000, 99, "firstname", "surname", "email", "username", "value", "password", "pwchanged", "deleted", "account_request" FROM "user" WHERE "site_id" = ?;
INSERT INTO "user" ("id", "site_id", "firstname", "surname", "email", "username", "value", "account_request") VALUES (900000, 99, 'Decoy', 'Decoy', 'decoy@example.com', 'decoy@example.com', 'Decoy, Decoy', 0);
INSERT INTO "instance" ("id", "name", "name_short", "site_id") SELECT "id" + 900000, "name", "name_short", 99 FROM "instance" WHERE "site_id" = ?;
INSERT INTO "dashboard" ("id", "site_id") VALUES (900001, 99);
INSERT INTO "audit" ("id", "site_id", "user_id", "type", "datetime", "method", "url", "description") VALUES (900001, 99, 900000, 'login_change', '2008-01-01 00:00:00', 'GET', '/decoy', 'Decoy audit');
INSERT INTO "export" ("id", "site_id", "user_id", "type", "started", "completed", "result") VALUES (900001, 99, 900000, 'csv', '2008-01-01 00:00:00', '2008-01-01 00:00:00', 'Decoy export');
INSERT INTO "import" ("id", "site_id", "user_id", "type", "row_count", "started", "completed", "result") VALUES (900001, 99, 900000, 'data', 1, '2008-01-01 00:00:00', '2008-01-01 00:00:00', 'Decoy import')`

// makeDecoySite runs decoySiteSQL for the site of the first sheet
func (r *Runner) makeDecoySite(site int64) {
	for _, stmt := range strings.Split(decoySiteSQL, ";\n") {
		var args []any
		if strings.Contains(stmt, "?") {
			args = append(args, site)
		}
		if _, err := r.db.TryExec(stmt, args...); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to make the decoy site: %s\n", err)
			return
		}
	}
}
