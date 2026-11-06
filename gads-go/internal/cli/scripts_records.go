package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/textcsv"
)

func init() {
	Commands["delete-record-version.pl"] = DeleteRecordVersion
	Commands["delete-file.pl"] = DeleteFile
	Commands["import-user.pl"] = ImportUser
	Commands["fixtimes.pl"] = FixTimes
}

// dtString is a DateTime of the database stringified (ISO 8601)
func dtString(v any) string {
	if t, ok := db.AsTime(v); ok {
		return t.Format("2006-01-02T15:04:05")
	}
	s, _ := db.AsString(v)
	return s
}

func str(v any) string {
	s, _ := db.AsString(v)
	return s
}

// The tables of the values of a record version
var valueTables = []string{"calcval", "curval", "daterange", "date", "enum", "file", "intgr", "person", "ragval", "string"}

// DeleteRecordVersion is bin/delete-record-version.pl: purges one version
// of a record
func DeleteRecordVersion(env *Env) {
	var recordID string
	if !env.GetOptions(Option{Name: "record-id", Value: true, String: &recordID}) {
		env.Exit(0)
	}
	if recordID == "" {
		gads.Fail("Please provide the record version ID with --record-id")
	}
	d := env.E.DB
	record := d.QueryRow(`SELECT * FROM "record" WHERE "id" = ?`, recordID)
	if record == nil {
		gads.Fail("Record ID not found")
	}
	user := d.QueryRow(`SELECT * FROM "user" WHERE "id" = ?`, record["createdby"])
	if user == nil {
		Die(`Can't call method "value" on an undefined value`)
	}
	env.Say("This will purge the version %s from record %s created by %s at %s. Press any key to continue.",
		recordID, str(record["current_id"]), str(user["value"]), dtString(record["created"]))
	env.ReadLine()
	d.Txn(func() {
		for _, t := range valueTables {
			d.Exec(`DELETE FROM "`+t+`" WHERE "record_id" = ?`, record["id"])
		}
		d.Exec(`DELETE FROM "user_lastrecord" WHERE "record_id" = ?`, record["id"])
		// The version may be the current version of its record (which
		// fails the foreign key): reported as the DBI exception of the
		// DBIx::Class delete
		if _, err := d.TryExec(`DELETE FROM "record" WHERE "id" = ?`, record["id"]); err != nil {
			var de *db.Error
			if errors.As(err, &de) && d.Dialect == db.SQLite {
				gads.Panic(`DBI Exception: DBD::SQLite::st execute failed: %s [for Statement "DELETE FROM "record" WHERE ( "id" = ? )" with ParamValues: 1=%s]`,
					de.Err.Error(), str(record["id"]))
			}
			// DBD::Pg quotes the value
			if de != nil && d.Dialect == db.Postgres {
				if msg := de.Error(); msg != "" {
					gads.Panic(`%s [for Statement "DELETE FROM "record" WHERE ( "id" = ? )" with ParamValues: 1='%s']`,
						strings.TrimSuffix(msg, ` [for Statement "`+de.Query+`"]`), str(record["id"]))
				}
			}
			panic(err)
		}
	})
}

// DeleteFile is bin/delete-file.pl: purges the files of fields of a record
func DeleteFile(env *Env) {
	if len(env.Args) <= 1 {
		env.Say("Usage: ./delete-file.pl <current_id> <layout_id> [<layout id>...]")
		env.Exit(1)
	}
	d := env.E.DB
	currentID := env.Args[0]
	if d.QueryRow(`SELECT "id" FROM "current" WHERE "id" = ?`, currentID) == nil {
		Die(`Can't call method "records" on an undefined value`)
	}
	records := d.Query(`SELECT "id" FROM "record" WHERE "current_id" = ? ORDER BY "id"`, currentID)
	var values []db.Row
	for _, layout := range env.Args[1:] {
		for _, r := range records {
			values = append(values, d.Query(`SELECT * FROM "file" WHERE "record_id" = ? AND "layout_id" = ? ORDER BY "id"`, r["id"], layout)...)
		}
	}
	fileval := func(f db.Row) db.Row {
		if f["value"] == nil {
			return nil
		}
		return d.QueryRow(`SELECT "id", "name" FROM "fileval" WHERE "id" = ?`, f["value"])
	}
	env.Say("Found %d files to purge.", len(values))
	env.Say("These are...")
	for _, f := range values {
		if v := fileval(f); v != nil {
			env.Say("%s", str(v["name"]))
		}
	}
	io.WriteString(env.Stdout, "Do you wish to continue? [y/N] ")
	response := strings.TrimRight(env.ReadLine(), "\n")
	if !regexp.MustCompile(`(?i)^y`).MatchString(response) {
		env.Exit(0)
	}
	for _, f := range values {
		d.Txn(func() {
			if v := fileval(f); v != nil {
				// The content of files is no longer a column (they are
				// stored in the uploads directory), so DBIC refuses the update
				gads.Panic(`bless( {'msg' => 'DBIx::Class::Row::get_column(): No such column \'content\' on GADS::Schema::Result::Fileval'}, 'DBIx::Class::Exception' )`)
			}
		})
		env.Say("Deleted %s\n", str(f["id"]))
	}
	env.Exit(0)
}

// ImportUser is bin/import-user.pl: creates users from a CSV file of
// firstname, surname, email, freetext1, freetext2, title, organisation and
// group
func ImportUser(env *Env) {
	if len(env.Args) == 0 || env.Args[0] == "" {
		Die("Usage: %s filename", "bin/import-user.pl")
	}
	file := env.Args[0]
	content, err := os.ReadFile(env.Path(file)) // #nosec G304 -- a file named on the script's command line
	if err != nil {
		Die("%s: %s", file, perlIOError(err))
	}
	d := env.E.DB
	// rset(...)->all: the rows of the site
	site, siteArgs := "", []any(nil)
	if env.E.SiteID != 0 {
		site, siteArgs = ` WHERE "site_id" = ?`, []any{env.E.SiteID}
	}
	ids := func(table string) map[string]int64 {
		m := map[string]int64{}
		for _, r := range d.Query(`SELECT "id", "name" FROM "`+table+`"`+site+` ORDER BY "id"`, siteArgs...) {
			id, _ := db.AsInt64(r["id"])
			m[str(r["name"])] = id
		}
		return m
	}
	titles, organisations, groups := ids("title"), ids("organisation"), ids("group")
	reader := textcsv.NewReader(bytes.NewReader(content))
	d.Txn(func() {
		for {
			// (while (my $row = $csv->getline($fh)): an error ends the
			// rows as their end does)
			row, err := reader.Read()
			if err != nil {
				break
			}
			// The fields a short row lacks are undef in Perl, which warns
			// when they are used
			n := len(row)
			for len(row) < 8 {
				row = append(row, "")
			}
			undef := func(i int, name, op string) {
				if i >= n {
					fmt.Fprintf(env.Stderr, "Use of uninitialized value $%s in %s at bin/import-user.pl line 1.\n", name, op)
				}
			}
			find := func(m map[string]int64, i int, name, label string) int64 {
				undef(i, name, "hash element")
				id, ok := m[row[i]]
				if !ok || id == 0 {
					undef(i, name, "concatenation (.) or string")
					Die(`%s "%s" not found`, label, row[i])
				}
				return id
			}
			firstname, surname, email, freetext1, freetext2 := row[0], row[1], row[2], row[3], row[4]
			titleID := find(titles, 5, "title", "Title")
			orgID := find(organisations, 6, "organisation", "Organisation")
			groupID := find(groups, 7, "group", "Group")
			userID := env.E.InsertSite("user", map[string]any{
				"firstname":    firstname,
				"surname":      surname,
				"email":        email,
				"username":     email,
				"freetext1":    freetext1,
				"freetext2":    freetext2,
				"title":        titleID,
				"organisation": orgID,
			})
			d.InsertMap("user_group", map[string]any{"user_id": userID, "group_id": groupID})
		}
	})
	env.SayErr("Finished")
}

// perlIOError is the text of $! of a failed open
func perlIOError(err error) string {
	if os.IsNotExist(err) {
		return "No such file or directory"
	}
	if os.IsPermission(err) {
		return "Permission denied"
	}
	return err.Error()
}

// FixTimes is bin/fixtimes.pl: moves back by an hour the creation of the
// first versions of records created since the clocks changed on 29th March
// 2020
func FixTimes(env *Env) {
	d := env.E.DB
	rows := d.Query(`SELECT "me"."id", "me"."created" FROM "record" "me"
		LEFT JOIN "record" "record_earlier_id" ON "record_earlier_id"."current_id" = "me"."current_id" AND "record_earlier_id"."id" < "me"."id"
		WHERE "me"."created" >= ? AND "record_earlier_id"."id" IS NULL`, "2020-03-29 02:00")
	for _, r := range rows {
		t, ok := db.AsTime(r["created"])
		if !ok {
			continue
		}
		d.Exec(`UPDATE "record" SET "created" = ? WHERE "id" = ?`, t.Add(-3600e9), r["id"])
	}
}
