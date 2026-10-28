package cli

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/perl"
)

// Scripts added in GADS-dev
func init() {
	Commands["add-extension.pl"] = AddExtension
	Commands["bulk_layout_permissions.pl"] = BulkLayoutPermissions
}

// AddExtension is bin/add-extension.pl: it adds a MIME type and extension
// to the file types allowed for a file field (its override_types), after
// confirmation.
func AddExtension(env *Env) {
	var mimetype, extension, instance, layoutID string
	// GetOptions without "or exit": wrong options are only warned about
	env.GetOptions(
		Option{Name: "mimetype", Value: true, String: &mimetype},
		Option{Name: "extension", Value: true, String: &extension},
		Option{Name: "instance", Value: true, Int: true, String: &instance},
		Option{Name: "layout", Value: true, Int: true, String: &layoutID},
	)
	if !perl.True(mimetype) || !perl.True(extension) || !perl.True(instance) || !perl.True(layoutID) {
		gads.Fail("Usage bin/add-extension.pl --mimetype=<mimetype> --extension=<extension> --layout=<layout_id> --instance=<instance_id>")
	}
	// Already allowed types and extensions are an error
	if gads.CheckType(mimetype) {
		gads.Fail("Mimetype %s already allowed", mimetype)
	}
	if gads.CheckExtension(extension) {
		gads.Fail("Extension %s already allowed", extension)
	}
	e := env.E
	// Server-side as admin, without permissions
	e.IgnorePermissions = true
	iid, _ := strconv.ParseInt(instance, 10, 64)
	layout := e.NewLayout(iid, nil)
	cid, _ := strconv.ParseInt(layoutID, 10, 64)
	column := layout.Column(cid)
	if column == nil {
		Die(`Can't call method "type" on an undefined value`)
	}
	if column.Type != "file" {
		gads.Fail("Invalid column for properties to be set on")
	}
	io.WriteString(env.Stdout, "Do you want to add mime type "+mimetype+" with extension "+extension+" to "+column.Name+" (y/n): ")
	answer := strings.TrimSuffix(env.ReadLine(), "\n")
	if !regexp.MustCompile(`(?i)^y`).MatchString(answer) {
		e.Notice("Addition of extension cancelled")
		return
	}
	var types []any
	exists := false
	for _, t := range column.OverrideTypes() {
		if t.Extension == extension && t.Name == mimetype {
			exists = true
		}
		types = append(types, map[string]any{"name": t.Name, "extension": t.Extension})
	}
	if !exists {
		types = append(types, map[string]any{"name": mimetype, "extension": extension})
	}
	column.SetOption("override_types", types)
	column.Write(gads.ColumnWriteOptions{})
	e.Notice("Extension and mime type added")
}

var (
	reDigits         = regexp.MustCompile(`^\d+$`)
	rePermissionName = regexp.MustCompile(`^(?:read|write_new|write_existing|write_existing_no_approval|write_new_no_approval)$`)
	reGroupList      = regexp.MustCompile(`^\s*\d+(?:\s*,\s*\d+)*\s*$`)
	reListSplit      = regexp.MustCompile(`\s*,\s*`)
	reSQLiteInteger  = regexp.MustCompile(`^[+-]?[0-9]+(?:\.0*)?$`)
)

// BulkLayoutPermissions is bin/bulk_layout_permissions.pl: the import of
// group permissions of fields from a CSV file, or their export for groups
// to one.
func BulkLayoutPermissions(env *Env) {
	var action, file string
	if !env.GetOptions(
		Option{Name: "action", Value: true, String: &action},
		Option{Name: "file", Value: true, String: &file},
	) {
		env.Exit(0)
	}
	if !perl.True(action) {
		gads.Fail("Please state if you want to import/export group permissions with --action")
	}
	if !perl.True(file) {
		gads.Fail("Please provide the file you want to import/export to/from with --file")
	}
	switch action {
	case "import":
		importPermissions(env, file)
	case "export":
		exportPermissions(env, file)
	default:
		gads.Fail("You must state if you want to import or export with --action")
	}
}

func importPermissions(env *Env, file string) {
	if st, err := os.Stat(file); err != nil || !st.Mode().IsRegular() {
		// The message is not translated, so its placeholder remains
		gads.Fail("File '{file}' does not exist")
	}
	content, err := os.ReadFile(file)
	if err != nil {
		permissionsFault(env, "Unable to open file {file}", err)
	}
	d := env.E.DB
	d.Txn(func() {
		r := csv.NewReader(strings.NewReader(string(content)))
		r.FieldsPerRecord = -1
		headers, _ := r.Read()
		sorted := append([]string{}, headers...)
		sort.Strings(sorted)
		if headers == nil || strings.Join(sorted, ",") != "group_id,layout_id,permission" {
			gads.Fail("Invalid CSV header/s. Import must consist of 3 columns: layout_id, group_id and permission")
		}
		recordNumber := 1
		for {
			fields, err := r.Read()
			if err != nil {
				break
			}
			recordNumber++
			row := map[string]string{}
			for i, h := range headers {
				if i < len(fields) {
					row[h] = fields[i]
				}
			}
			layoutID, groupID, permission := row["layout_id"], row["group_id"], row["permission"]
			if !reDigits.MatchString(layoutID) {
				gads.Fail("ERROR: Invalid Layout ID '%s' on row %d.", layoutID, recordNumber)
			}
			if !reDigits.MatchString(groupID) {
				gads.Fail("ERROR: Invalid Group ID '%s' on row %d.", groupID, recordNumber)
			}
			if !rePermissionName.MatchString(permission) {
				gads.Fail("Invalid Permission '%s' on row %d. Value must be read, write_new, write_existing, or write_existing_no_approval.", permission, recordNumber)
			}
			if d.QueryRow(`SELECT "id" FROM "layout" WHERE "id" = ?`, layoutID) == nil {
				gads.Fail("Layout ID '%s' does not exist\n", layoutID)
			}
			if d.QueryRow(`SELECT "id" FROM "group" WHERE "id" = ?`, groupID) == nil {
				gads.Fail("Group ID '%s' does not exist\n", groupID)
			}
			if d.QueryRow(`SELECT "id" FROM "layout_group" WHERE "group_id" = ? AND "layout_id" = ? AND "permission" = ?`, groupID, layoutID, permission) != nil {
				// (info: the permission already exists)
				continue
			}
			d.InsertMap("layout_group", map[string]any{"layout_id": layoutID, "group_id": groupID, "permission": permission})
		}
	})
}

func exportPermissions(env *Env, file string) {
	if _, err := os.Stat(file); err == nil {
		gads.Fail("%s already exists, unable to overwrite file!", file)
	}
	env.Say("Enter the group IDs you're looking to export (e.g. 1,2,3): ")
	input := strings.TrimSuffix(env.ReadLine(), "\n")
	if !perl.True(input) || !reGroupList.MatchString(input) {
		gads.Fail("You need to enter a number or a comma-separated list of numbers.\n")
	}
	groups := reListSplit.Split(input, -1)
	f, err := os.Create(file)
	if err != nil {
		permissionsFault(env, file, err)
	}
	defer f.Close()
	d := env.E.DB
	args := make([]any, len(groups))
	for i, g := range groups {
		args[i] = g
	}
	// The bind of a non-integer group ID (with spaces) for the integer
	// column is warned about by DBIC on SQLite
	if d.Dialect != db.Postgres {
		for _, g := range groups {
			if !reSQLiteInteger.MatchString(g) {
				env.E.PerlWarn("DBIx::Class::Storage::DBI::SQLite::_dbi_attrs_for_bind(): Non-integer value supplied for column 'me.group_id' despite the integer datatype at bin/bulk_layout_permissions.pl line 1\n")
				break
			}
		}
	}
	rows := d.Query(`SELECT "me"."layout_id", "me"."group_id", "me"."permission", "layout"."instance_id", "layout"."name" FROM "layout_group" "me" JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE "me"."group_id" IN (?`+strings.Repeat(", ?", len(args)-1)+`)`, args...)
	io.WriteString(f, gads.CSVFormatLine([]string{"instance_id", "layout_name", "layout_id", "group_id", "permission"}))
	for _, r := range rows {
		io.WriteString(f, gads.CSVFormatLine([]string{str(r["instance_id"]), str(r["name"]), str(r["layout_id"]), str(r["group_id"]), str(r["permission"])}))
	}
	// (info: successfully exported)
}

// permissionsFault is Log::Report FAULT: the message with the system error,
// exiting with its errno
func permissionsFault(env *Env, text string, err error) {
	msg, code := err.Error(), 1
	var pe *os.PathError
	if errors.As(err, &pe) {
		msg = pe.Err.Error()
		if errno, ok := pe.Err.(interface{ Is(error) bool }); ok && errno.Is(os.ErrNotExist) {
			msg, code = "No such file or directory", 2
		}
	}
	env.SayErr("fault: %s; %s", text, msg)
	env.Exit(code)
}
