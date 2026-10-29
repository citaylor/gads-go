package cli

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

func init() {
	Commands["onboard.pl"] = Onboard
}

// onboardField is a column of the CSV of bin/onboard.pl: a field, a
// special name (__version_datetime or __version_userid), or nothing (the
// ID, and the columns after the headings)
type onboardField struct {
	column  *gads.Column
	special string
}

// selectValue is the ID of an enumerated value or person, or several IDs
// if more than one has its text
type selectValue struct {
	ids []int64
}

var (
	reHTrim         = regexp.MustCompile(`^[\t ]+|[\t ]+$`)
	reDMY           = regexp.MustCompile(`^([0-9]{1,2})-([0-9]{1,2})-([0-9]{4})$`)
	reYYYYMMDD      = regexp.MustCompile(`^([0-9]{4})([0-9]{2})([0-9]{2})$`)
	reRangeText     = regexp.MustCompile(`^([0-9]{4}-[0-9]{1,2}-[0-9]{1,2})[\t ]*(-|to)[\t ]*([0-9]{4}-[0-9]{1,2}-[0-9]{1,2})$`)
	reIntgrInput    = regexp.MustCompile(`^[.0-9]+$`)
	reYMD           = regexp.MustCompile(`^([0-9]{4})-([0-9]{1,2})-([0-9]{1,2})$`)
	reLeadingSpace  = regexp.MustCompile(`^\s+`)
	reTrailingSpace = regexp.MustCompile(`\s+$`)
)

func hTrim(s string) string { return reHTrim.ReplaceAllString(s, "") }

// dumperCount is Data::Dumper of the counts (with its keys sorted)
func dumperCount(in, written, errors int) string {
	return fmt.Sprintf("$VAR1 = {\n          'errors' => %d,\n          'in' => %d,\n          'written' => %d\n        };\n", errors, in, written)
}

// Onboard is bin/onboard.pl: imports the records of a CSV file into a
// table, matching existing records by a unique field if requested, and
// writes the rows that failed to another CSV. It writes as no user, so
// new records and changed values fail for want of permission, as in GADS.
func Onboard(env *Env) {
	var takeFirstEnum, ignoreIncompleteDateranges, dryRun, ignoreStringZeros bool
	var force, invalidCSV, instanceID, updateUnique, skipExistingUnique, noChangeUnlessBlank string
	var updateOnly, blankInvalidEnum, reportChanges bool
	var invalidReport, appendNames []string
	if !env.GetOptions(
		Option{Name: "take-first-enum", Bool: &takeFirstEnum},
		Option{Name: "ignore-incomplete-dateranges", Bool: &ignoreIncompleteDateranges},
		Option{Name: "dry-run", Bool: &dryRun},
		Option{Name: "ignore-string-zeros", Bool: &ignoreStringZeros},
		Option{Name: "force", Value: true, String: &force},
		Option{Name: "invalid-csv", Value: true, String: &invalidCSV},
		Option{Name: "invalid-report", Value: true, Multi: true, Ints: &invalidReport},
		Option{Name: "instance-id", Value: true, String: &instanceID},
		Option{Name: "update-unique", Value: true, String: &updateUnique},
		Option{Name: "skip-existing-unique", Value: true, String: &skipExistingUnique},
		Option{Name: "update-only", Bool: &updateOnly},
		Option{Name: "blank-invalid-enum", Bool: &blankInvalidEnum},
		Option{Name: "no-change-unless-blank", Value: true, String: &noChangeUnlessBlank},
		Option{Name: "report-changes", Bool: &reportChanges},
		Option{Name: "append", Value: true, Multi: true, Ints: &appendNames},
	) {
		env.Exit(0)
	}
	if !perlFalse(force) && force != "mandatory" {
		Die("Invalid option '%s' supplied to --force", force)
	}
	if !perlFalse(noChangeUnlessBlank) && noChangeUnlessBlank != "skip_new" && noChangeUnlessBlank != "bork" {
		Die("Invalid option '%s' supplied to --no-change-unless-blank", noChangeUnlessBlank)
	}
	if perlFalse(instanceID) {
		Die("Need --instance-id to be specified")
	}
	if !perlFalse(updateUnique) && !perlFalse(skipExistingUnique) {
		Die("--skip-existing-unique and --update-unique are mutually exclusive")
	}
	if len(env.Args) == 0 || perlFalse(env.Args[0]) {
		Die("Usage: %s [--take-first-enum] [--ignore-incomplete-dateranges]\n    [--ignore-string-zeros] [--dry-run] [--force=mandatory] [--invalid-csv=failed.csv] filename", "bin/onboard.pl")
	}
	file := env.Args[0]
	e := env.E
	d := e.DB
	// The first site
	if id, ok := d.QueryInt64(`SELECT "id" FROM "site" ORDER BY "id" LIMIT 1`); ok {
		e.SiteID = id
	}
	content, err := os.ReadFile(env.Path(file))
	if err != nil {
		Die("%s: %s", file, perlIOError(err))
	}
	reader := csv.NewReader(strings.NewReader(string(content)))
	reader.FieldsPerRecord = -1
	headings, err := reader.Read()
	if err != nil && err != io.EOF {
		Die("%s", err)
	}
	iid, _ := strconv.ParseInt(instanceID, 10, 64)
	layout := e.NewLayout(iid, nil)

	var fields []onboardField
	selects := map[int64]map[string]*selectValue{}
	selectsReverse := map[int64]map[int64]string{}
	multiColumns := map[int64]bool{}
	dr := false
	var updateUniqueCol, skipExistingUniqueCol *gads.Column
	addSelect := func(colID int64, text string, id int64, takeFirst bool) {
		if selects[colID] == nil {
			selects[colID] = map[string]*selectValue{}
			selectsReverse[colID] = map[int64]string{}
		}
		if sv, ok := selects[colID][text]; ok {
			if takeFirst {
				return
			}
			sv.ids = append(sv.ids, id)
		} else {
			selects[colID][text] = &selectValue{ids: []int64{id}}
		}
		selectsReverse[colID][id] = text
	}
	for _, field := range headings {
		if updateUnique == "ID" && field == "ID" {
			for _, c := range layout.All(gads.AllOptions{}) {
				if c.Name == "ID" {
					Die("ID is present in the layout but is a special name")
				}
			}
			fields = append(fields, onboardField{})
			continue
		}
		if field == "__version_datetime" || field == "__version_userid" {
			fields = append(fields, onboardField{special: field})
			continue
		}
		fid, ok := d.QueryInt64(`SELECT "id" FROM "layout" WHERE "name" = ? AND "instance_id" = ? ORDER BY "id" LIMIT 1`, field, layout.InstanceID)
		if !ok {
			Die("Field %s does not exist", field)
		}
		column := layout.Column(fid)
		for _, f := range fields {
			if f.column != nil && f.column.Name == field && column.Type != "daterange" {
				Die("Field %s exists twice", field)
			}
		}
		if dr && column.Type == "daterange" {
			multiColumns[column.ID] = true
		} else {
			fields = append(fields, onboardField{column: column})
		}
		if !perlFalse(updateUnique) && updateUnique != "ID" && updateUnique == column.Name {
			updateUniqueCol = column
		}
		if !perlFalse(skipExistingUnique) && skipExistingUnique == column.Name {
			skipExistingUniqueCol = column
		}
		switch column.Type {
		case "enum", "tree":
			for _, v := range d.Query(`SELECT "id", "value" FROM "enumval" WHERE "layout_id" = ? AND "deleted" = 0 ORDER BY "id"`, column.ID) {
				id, _ := db.AsInt64(v["id"])
				addSelect(column.ID, hTrim(strings.ToLower(str(v["value"]))), id, takeFirstEnum)
			}
		case "person":
			for _, v := range d.Query(`SELECT "id", "value" FROM "user" "me" WHERE "deleted" IS NULL AND "account_request" = 0` + siteAnd(e, "me") + ` ORDER BY "id"`) {
				id, _ := db.AsInt64(v["id"])
				addSelect(column.ID, strings.ToLower(str(v["value"])), id, false)
			}
		case "daterange":
			// A second daterange column is expected immediately after
			dr = !dr
		}
	}
	if !perlFalse(updateUnique) && updateUnique != "ID" && updateUniqueCol == nil {
		gads.Fail("field %s not found for update-unique", updateUnique)
	}
	if !perlFalse(skipExistingUnique) && skipExistingUniqueCol == nil {
		gads.Fail("field %s not found for skip-existing-unique", skipExistingUnique)
	}
	fieldName := func(f onboardField) string {
		if f.column != nil {
			return f.column.Name
		}
		return f.special
	}
	var invalid strings.Builder
	var reportIndexes []int
	if !perlFalse(invalidCSV) {
		var names []string
		for _, f := range fields {
			names = append(names, fieldName(f))
		}
		heads := names
		if len(invalidReport) > 0 {
			heads = invalidReport
		}
		invalid.WriteString(csvLine(append(append([]string{"Status"}, heads...), "Errors")))
		for _, name := range invalidReport {
			re, err := regexp.Compile("^(?:" + name + ")$")
			index := -1
			for i, n := range names {
				if err == nil && re.MatchString(n) {
					index = i
					break
				}
			}
			if index == -1 {
				gads.Fail("Field %s invalid for invalid-report", name)
			}
			reportIndexes = append(reportIndexes, index)
		}
	}
	appendCols := map[int64]bool{}
	var appendIDs []int64
	for _, name := range appendNames {
		var col *gads.Column
		for _, f := range fields {
			if f.column != nil && f.column.Name == name {
				col = f.column
				break
			}
		}
		if col == nil {
			gads.Fail("Field %s invalid for append", name)
		}
		if !appendCols[col.ID] {
			appendIDs = append(appendIDs, col.ID)
		}
		appendCols[col.ID] = true
	}
	var allIDs []int64
	for _, c := range layout.All(gads.AllOptions{}) {
		allIDs = append(allIDs, c.ID)
	}
	countIn, countWritten, countErrors := 0, 0, 0
	for {
		orig, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		row := append([]string{}, orig...)
		if strings.Join(row, " ") == "" {
			continue
		}
		countIn++
		colCount := 0
		input := map[int64]any{}
		var inputID string
		var bad, badEnum []string
		drf := false
		var versionDatetime *time.Time
		var versionUserID int64
		for i := range row {
			row[i] = hTrim(row[i])
			col := row[i]
			var f onboardField
			if colCount < len(fields) {
				f = fields[colCount]
			}
			if f.column == nil && f.special == "" {
				inputID = col
				colCount++
				continue
			}
			if f.special == "__version_datetime" {
				if t, ok := parseYMD(col); ok {
					versionDatetime = &t
				} else {
					versionDatetime = nil
					bad = append(bad, fmt.Sprintf(`Invalid version_datetime "%s"`, col))
				}
			} else if f.special == "__version_userid" {
				versionUserID, _ = strconv.ParseInt(col, 10, 64)
			} else {
				c := f.column
				switch c.Type {
				case "enum", "tree", "person":
					if col == "" {
						input[c.ID] = col
					} else if sv, ok := selects[c.ID][strings.ToLower(col)]; ok && len(sv.ids) > 1 {
						bad = append(bad, fmt.Sprintf(`Multiple instances of enum value "%s" for "%s"`, col, c.Name))
					} else if ok {
						input[c.ID] = sv.ids[0]
					} else {
						badEnum = append(badEnum, fmt.Sprintf(`Invalid enum value "%s" for "%s"`, col, c.Name))
						if blankInvalidEnum {
							input[c.ID] = ""
						}
					}
				case "daterange":
					col = strings.ReplaceAll(col, "/", "-")
					col = reDMY.ReplaceAllString(col, "$3-$2-$1")
					col = reYYYYMMDD.ReplaceAllString(col, "$1-$2-$3")
					row[i] = col
					if multiColumns[c.ID] {
						if v, ok := input[c.ID]; ok {
							input[c.ID] = append(v.([]any), col)
							drf = false
						} else {
							input[c.ID] = []any{col}
							drf = true
						}
					} else if m := reRangeText.FindStringSubmatch(col); m != nil {
						input[c.ID] = []any{m[1], m[3]}
					} else {
						bad = append(bad, fmt.Sprintf(`Invalid daterange value "%s" for "%s"`, col, c.Name))
					}
				case "string":
					if ignoreStringZeros && col == "0" {
						input[c.ID] = ""
					} else {
						input[c.ID] = col
					}
				case "intgr":
					if reIntgrInput.MatchString(col) {
						if !perlFalse(col) {
							input[c.ID] = strconv.FormatFloat(pn(col), 'f', 0, 64)
						}
					} else if !perlFalse(col) {
						bad = append(bad, fmt.Sprintf(`Invalid item "%s" for integer field "%s"`, col, c.Name))
					}
				default:
					input[c.ID] = col
				}
			}
			if !drf {
				colCount++
			}
		}
		full := strings.ReplaceAll(strings.Join(row, " "), "\n", "")
		write := len(bad) == 0 && (len(badEnum) == 0 || blankInvalidEnum)
		if write {
			skip := false
			record := e.NewRecord(layout, nil)
			missing := func(name string) {
				record.Initialise(0)
				bad = append(bad, fmt.Sprintf(`Missing unique identifier for "%s". Data will be uploaded as new record. Full record follows: %s`, name, full))
			}
			if !perlFalse(updateUnique) {
				if updateUnique == "ID" {
					if !perlFalse(inputID) {
						id, _ := strconv.ParseInt(inputID, 10, 64)
						if err := gads.Try(func() { record.FindCurrentID(id, gads.FindOptions{}) }); err != nil {
							bad = append(bad, fmt.Sprintf("Failed to retrieve record ID %s (%s). Data will not be uploaded.", inputID, tryStopped(err)))
							skip = true
						}
					} else {
						missing(updateUnique)
					}
				} else if v := input[updateUniqueCol.ID]; v != nil && !perlFalse(fmt.Sprint(v)) {
					if existing := record.FindUniqueColumns(updateUniqueCol, fmt.Sprint(v), allIDs); existing != nil {
						record = existing
					} else {
						record.Initialise(0)
					}
				} else {
					missing(updateUnique)
				}
			} else if !perlFalse(skipExistingUnique) {
				if v, ok := input[skipExistingUniqueCol.ID]; ok && v != nil {
					if existing := record.FindUnique(skipExistingUniqueCol, fmt.Sprint(v), 0); existing != nil {
						bad = append(bad, fmt.Sprintf(`Skipping: unique identifier "%v" already exists for "%s"`, v, skipExistingUnique))
						skip = true
					} else {
						record.Initialise(0)
					}
				} else {
					// GADS names the update-unique field (not given here)
					missing(updateUnique)
				}
			} else {
				record.Initialise(0)
			}
			if !skip {
				var changes []string
				failed := onboardUpdateFields(fields, input, record, &changes, onboardOptions{
					ignoreIncompleteDateranges: ignoreIncompleteDateranges,
					appendCols:                 appendCols,
					skipNew:                    noChangeUnlessBlank == "skip_new",
					reportChanges:              reportChanges,
					selectsReverse:             selectsReverse,
				})
				if reportChanges && len(changes) > 0 {
					var ident string
					if updateUnique == "ID" {
						ident = strconv.FormatInt(record.CurrentID(), 10)
					} else {
						if updateUniqueCol == nil {
							Die(`Can't call method "id" on an undefined value`)
						}
						ident = record.Fields()[updateUniqueCol.ID].AsString()
					}
					env.Say("Changes for record %s are as follows:", ident)
					for _, c := range changes {
						env.Say("%s", c)
					}
					env.Say("\n")
				}
				if len(failed) == 0 {
					err := gads.Try(func() {
						record.Write(gads.WriteOptions{
							NoAlerts:            true,
							DryRun:              dryRun,
							ForceMandatory:      force == "mandatory",
							UpdateOnly:          updateOnly,
							NoChangeUnlessBlank: !perlFalse(noChangeUnlessBlank),
							AllowUpdate:         appendIDs,
							VersionDatetime:     versionDatetime,
							VersionUserID:       versionUserID,
						})
					})
					if err != nil {
						failed = append(failed, errMessage(err))
						write = false
					} else {
						countWritten++
					}
				} else {
					write = false
				}
				bad = append(bad, failed...)
			}
		}
		bad = append(bad, badEnum...)
		if len(bad) > 0 {
			countErrors++
			if !perlFalse(invalidCSV) {
				out := orig
				if len(reportIndexes) > 0 {
					out = nil
					for _, i := range reportIndexes {
						if i < len(orig) {
							out = append(out, orig[i])
						} else {
							out = append(out, "")
						}
					}
				}
				status := "Record not written"
				if write {
					status = "Record written"
				}
				invalid.WriteString(csvLine(append(append([]string{status}, out...), bad...)))
			}
		}
	}
	if !perlFalse(invalidCSV) {
		if err := os.WriteFile(env.Path(invalidCSV), []byte(invalid.String()), 0o666); err != nil { // #nosec G306 -- as Perl creates the file (0666, less the umask)
			Die("Failed to create %s: %s", invalidCSV, perlIOError(err))
		}
	}
	env.Say("%s", dumperCount(countIn, countWritten, countErrors))
}

// parseYMD is DateTime::Format::Strptime of %Y-%m-%d
func parseYMD(s string) (time.Time, bool) {
	m := reYMD.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	dd, _ := strconv.Atoi(m[3])
	t := time.Date(y, time.Month(mo), dd, 0, 0, 0, 0, time.UTC)
	if t.Month() != time.Month(mo) || t.Day() != dd {
		return time.Time{}, false
	}
	return t, true
}

// errMessage is the message of an exception
func errMessage(err error) string {
	if ge, ok := err.(*gads.Error); ok {
		return ge.Msg
	}
	return err.Error()
}

// tryStopped is a Log::Report try object as a string: the reason and
// message of its fatal exception
func tryStopped(err error) string {
	level := "error"
	if ge, ok := err.(*gads.Error); ok {
		level = strings.ToLower(string(ge.Level))
	}
	return fmt.Sprintf("try-block stopped with %s: %s: %s\n", strings.ToUpper(level), level, errMessage(err))
}

type onboardOptions struct {
	ignoreIncompleteDateranges bool
	appendCols                 map[int64]bool
	skipNew                    bool
	reportChanges              bool
	selectsReverse             map[int64]map[int64]string
}

// onboardUpdateFields is update_fields of bin/onboard.pl: sets the values
// of the input in the record, returning the errors
func onboardUpdateFields(fields []onboardField, input map[int64]any, record *gads.Record, changes *[]string, o onboardOptions) []string {
	var bad []string
	for _, f := range fields {
		col := f.column
		if col == nil || !col.Userinput() {
			continue
		}
		newv, defined := input[col.ID]
		if defined && newv == nil {
			defined = false
		}
		if record.CurrentID() != 0 && !defined {
			continue
		}
		if col.Type == "daterange" && o.ignoreIncompleteDateranges {
			l, _ := newv.([]any)
			var a, b string
			if len(l) > 0 {
				a = fmt.Sprint(l[0])
			}
			if len(l) > 1 {
				b = fmt.Sprint(l[1])
			}
			if perlFalse(a) || perlFalse(b) {
				newv = []any{"", ""}
			}
		}
		datum := record.Fields()[col.ID]
		oldValue := datum.AsString()
		wasBlank := datum.Blank()
		if o.appendCols[col.ID] {
			s := ps(newv)
			if perlFalse(oldValue) {
				s = reLeadingSpace.ReplaceAllString(s, "")
			}
			oldValue = reTrailingSpace.ReplaceAllString(oldValue, "")
			if !perlFalse(oldValue) {
				oldValue += "\n"
			}
			newv = oldValue + s
		}
		if o.skipNew && record.CurrentID() != 0 && !wasBlank && !o.appendCols[col.ID] {
			newValue := ps(newv)
			if col.Fixedvals() {
				id, _ := strconv.ParseInt(newValue, 10, 64)
				newValue = o.selectsReverse[col.ID][id]
			}
			if strings.ToLower(oldValue) != strings.ToLower(newValue) {
				*changes = append(*changes, fmt.Sprintf(`Not going to change value of "%s" from "%s" to "%s"`, col.Name, oldValue, newValue))
			} else if oldValue != newValue && !col.Fixedvals() {
				*changes = append(*changes, fmt.Sprintf(`Not going to change case of "%s" from "%s" to "%s"`, col.Name, oldValue, newValue))
			}
			continue
		}
		value := newv
		if n, ok := newv.(int64); ok {
			value = strconv.FormatInt(n, 10)
		}
		if err := gads.Try(func() { datum.SetValue(value, gads.SetValueOptions{}) }); err != nil {
			bad = append(bad, errMessage(err))
		} else if o.reportChanges && record.CurrentID() != 0 && gads.DatumChanged(datum) && !wasBlank && !o.appendCols[col.ID] {
			newValue := datum.AsString()
			if strings.ToLower(oldValue) != strings.ToLower(newValue) {
				*changes = append(*changes, fmt.Sprintf(`Change value of "%s" from "%s" to "%s"`, col.Name, oldValue, newValue))
			}
		}
	}
	return bad
}
