package gads

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
)

// Port of GADS::Import (CSV import of records). The import always runs in
// process, as Perl does with GADS_NO_FORK.

// ImportOptions are the options of an import.
type ImportOptions struct {
	TakeFirstEnum       bool
	DryRun              bool
	IgnoreStringZeros   bool
	ForceMandatory      bool
	SplitMultiple       bool
	ShortNames          bool
	Append              []int64
	NoChangeUnlessBlank string
	BlankInvalidEnum    bool
	RoundIntegers       bool
	NoReportChanges     bool
	UpdateOnly          bool
	UpdateUnique        int64
	SkipExistingUnique  int64
}

// Import is an import of CSV data into a table.
type Import struct {
	e        *Engine
	layout   *Layout
	user     *User
	o        ImportOptions
	data     []byte
	fields   []*Column
	selects  map[int64]map[string][]int64
	reverse  map[int64]map[int64]string
	importID int64
	appendIx map[int64]bool
}

// NewImport creates an import of the CSV content.
func (e *Engine) NewImport(layout *Layout, user *User, content []byte, o ImportOptions) *Import {
	if o.NoChangeUnlessBlank != "" && !regexp.MustCompile(`(skip_new|bork)`).MatchString(o.NoChangeUnlessBlank) {
		Fail("Invalid option %s for no_change_unless_blank", o.NoChangeUnlessBlank)
	}
	content = []byte(strings.TrimPrefix(string(content), "\xef\xbb\xbf"))
	im := &Import{e: e, layout: layout, user: user, o: o, data: content,
		selects: map[int64]map[string][]int64{}, appendIx: map[int64]bool{}}
	for _, id := range o.Append {
		im.appendIx[id] = true
	}
	return im
}

// ImportID returns the ID of the import status row.
func (im *Import) ImportID() int64 { return im.importID }

func (im *Import) reader() *csv.Reader {
	r := csv.NewReader(strings.NewReader(string(im.data)))
	r.FieldsPerRecord = -1
	return r
}

func importTrim(s string) string {
	isH := func(r rune) bool { return r == ' ' || r == '\t' || r == ' ' }
	return strings.TrimRightFunc(strings.TrimLeftFunc(s, isH), isH)
}

// Process runs the import, returning the import status ID.
func (im *Import) Process() int64 {
	if im.o.UpdateUnique != 0 && im.o.SkipExistingUnique != 0 {
		Fail("skip_existing_unique and update_unique are mutually exclusive")
	}
	old := im.e.IgnorePermissions
	im.e.IgnorePermissions = true
	defer func() { im.e.IgnorePermissions = old }()
	im.buildFields()
	im.statusID()
	im.importRows()
	return im.importID
}

func (im *Import) statusID() int64 {
	if im.importID == 0 {
		m := map[string]any{
			"user_id":     im.user.ID,
			"type":        "data",
			"started":     im.e.Now(),
			"instance_id": im.layout.InstanceID,
		}
		if im.e.SiteID != 0 {
			m["site_id"] = im.e.SiteID
		}
		im.importID = im.e.DB.InsertMap("import", m)
	}
	return im.importID
}

func (im *Import) addSelect(colID int64, text string, id int64, takeFirst bool) {
	if im.selects[colID] == nil {
		im.selects[colID] = map[string][]int64{}
	}
	if ex, ok := im.selects[colID][text]; ok {
		if takeFirst {
			return
		}
		im.selects[colID][text] = append(ex, id)
		return
	}
	im.selects[colID][text] = []int64{id}
}

func (im *Import) buildFields() {
	if im.fields != nil {
		return
	}
	header, err := im.reader().Read()
	if err != nil && err != io.EOF {
		header = nil
	}
	columnID := im.layout.ColumnID()
	fields := []*Column{}
	idName := "ID"
	if im.o.ShortNames {
		idName = "_id"
	}
	for i, field := range header {
		switch {
		case ((im.o.UpdateUnique != 0 && im.o.UpdateUnique == columnID.ID) ||
			(im.o.SkipExistingUnique != 0 && im.o.SkipExistingUnique == columnID.ID)) && field == idName:
			fields = append(fields, im.layout.Column(columnID.ID))
		case im.o.ShortNames && (field == "_version_datetime" || field == "_version_user"):
			fields = append(fields, im.layout.Column(im.layout.ColumnByNameShort(field).ID))
		case field == "Last edited time" || field == "Last edited by":
			fields = append(fields, im.layout.Column(im.layout.ColumnByName(field).ID))
		default:
			key := "name"
			if im.o.ShortNames {
				key = "name_short"
			}
			rows := im.e.DB.Query(`SELECT "id", "type" FROM "layout" WHERE "instance_id" = ? AND "`+key+`" = ?`, im.layout.InstanceID, field)
			if reAllSpace.MatchString(field) {
				Fail("Empty field name in import headings (Column %d)", i+1)
			}
			if len(rows) > 1 {
				Fail("Layout has more than one field named %s", field)
			}
			if len(rows) == 0 {
				Fail("Field '%s' in import headings not found in table", field)
			}
			id, _ := db.AsInt64(rows[0]["id"])
			typ, _ := db.AsString(rows[0]["type"])
			column := im.layout.Column(id)
			if !column.Userinput() && (im.o.UpdateUnique == 0 || column.ID != im.o.UpdateUnique) {
				Fail("Field '%s' is not a user-input field", field)
			}
			fields = append(fields, column)
			switch typ {
			case "enum", "tree":
				for _, v := range column.Enumvals() {
					if truthy(v["deleted"]) {
						continue
					}
					vid, _ := anyToInt64(v["id"])
					im.addSelect(id, importTrim(strings.ToLower(graphStr(v["value"]))), vid, im.o.TakeFirstEnum)
				}
			case "person":
				for _, p := range column.People() {
					im.addSelect(id, strings.ToLower(p.Value), p.ID, false)
				}
			}
		}
	}
	im.fields = fields
}

func (im *Import) selectsReverse(colID int64, id any) string {
	if im.reverse == nil {
		im.reverse = map[int64]map[int64]string{}
		for cid, m := range im.selects {
			im.reverse[cid] = map[int64]string{}
			for text, ids := range m {
				if len(ids) == 1 {
					im.reverse[cid][ids[0]] = text
				}
			}
		}
	}
	i, _ := anyToInt64(id)
	return im.reverse[colID][i]
}

var (
	reImportDaterange = regexp.MustCompile(`^(\S+)[ \t]*(-|to)[ \t]*(\S+)$`)
	reImportInt       = regexp.MustCompile(`^[0-9]+$`)
	reImportDecimal   = regexp.MustCompile(`^[.0-9]+$`)
	reImportVersionDT = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2}) (\d{1,2}):(\d{2})$`)
)

func parseImportVersionDT(s string) *time.Time {
	m := reImportVersionDT.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	if n(2) < 1 || n(2) > 12 || n(3) < 1 || n(3) > 31 || n(4) > 23 {
		return nil
	}
	t := time.Date(n(1), time.Month(n(2)), n(3), n(4), n(5), 0, 0, time.UTC)
	if t.Day() != n(3) {
		return nil
	}
	return &t
}

// errorMessage returns the message of a caught error, as Perl's
// $@->died->message.
func errorMessage(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Msg
	}
	return err.Error()
}

func (im *Import) importRows() {
	written, errors, skipped, in := 0, 0, 0, 0
	allColumnIDs := []int64{}
	for _, c := range im.layout.All(AllOptions{}) {
		allColumnIDs = append(allColumnIDs, c.ID)
	}
	importID := im.statusID()
	r := im.reader()
	if _, err := r.Read(); err != nil { // the header row
		im.finish(in, written, errors, skipped)
		return
	}
	columnID := im.layout.ColumnID()
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		if len(row) == 0 || strings.Join(row, " ") == "" {
			continue
		}
		in++
		var changes, bad, badEnum []string
		input := map[int64][]any{}
		var inputKeys []int64
		setInput := func(id int64, v []any) {
			if _, ok := input[id]; !ok {
				inputKeys = append(inputKeys, id)
			}
			input[id] = v
		}
		var versionDT *time.Time
		var versionUser string
		colCount := 0
		for _, cell := range row {
			var col *Column
			if colCount < len(im.fields) {
				col = im.fields[colCount]
			}
			if col == nil {
				bad = append(bad, fmt.Sprintf(`Extraneous value found on row: "%s"`, cell))
				continue
			}
			if col.ID == columnID.ID {
				setInput(col.ID, append(input[col.ID], cell))
				colCount++
				continue
			}
			setInput(col.ID, []any{})
			values := []string{cell}
			if im.o.SplitMultiple && col.Multivalue {
				values = strings.Split(cell, ",")
			}
			for _, value := range values {
				value = importTrim(value)
				vdtName, vuName := "Last edited time", "Last edited by"
				if im.o.ShortNames {
					vdtName, vuName = "_version_datetime", "_version_user"
				}
				switch {
				case col.Name == vdtName:
					versionDT = parseImportVersionDT(value)
					if versionDT == nil {
						bad = append(bad, fmt.Sprintf(`Invalid version_datetime "%s"`, value))
					}
				case col.Name == vuName:
					versionUser = value
				case col.Type == "enum" || col.Type == "tree":
					if value == "" {
						input[col.ID] = append(input[col.ID], value)
					} else if ids, ok := im.selects[col.ID][strings.ToLower(value)]; ok && len(ids) > 1 {
						bad = append(bad, fmt.Sprintf("Multiple instances of enum value '%s' for '%s'", value, col.Name))
					} else if ok {
						input[col.ID] = append(input[col.ID], ids[0])
					} else {
						badEnum = append(badEnum, fmt.Sprintf("Invalid enum value '%s' for '%s'", value, col.Name))
						if im.o.BlankInvalidEnum {
							input[col.ID] = append(input[col.ID], "")
						}
					}
				case col.Type == "daterange":
					if !perlTrue(value) {
						input[col.ID] = append(input[col.ID], []any{"", ""})
					} else if m := reImportDaterange.FindStringSubmatch(value); m != nil {
						input[col.ID] = append(input[col.ID], []any{m[1], m[3]})
					} else {
						bad = append(bad, fmt.Sprintf("Invalid daterange value '%s' for '%s'", value, col.Name))
					}
				case col.Type == "string":
					if im.o.IgnoreStringZeros && value == "0" {
						value = ""
					}
					input[col.ID] = append(input[col.ID], value)
				case col.Type == "intgr":
					if perlTrue(value) {
						re := reImportInt
						if im.o.RoundIntegers {
							re = reImportDecimal
						}
						if re.MatchString(value) {
							if im.o.RoundIntegers {
								f, _ := strconv.ParseFloat(value, 64)
								value = strconv.FormatFloat(math.RoundToEven(f), 'f', 0, 64)
							}
							input[col.ID] = append(input[col.ID], value)
						} else {
							bad = append(bad, fmt.Sprintf("Invalid value '%s' for integer field '%s'", value, col.Name))
						}
					}
				default:
					input[col.ID] = append(input[col.ID], value)
				}
			}
			colCount++
		}

		skip := false
		write := len(bad) == 0 && (len(badEnum) == 0 || im.o.BlankInvalidEnum)
		if write {
			record := im.e.NewRecord(im.layout, im.user)
			if uu := im.o.UpdateUnique; uu != 0 {
				values, has := input[uu]
				switch {
				case !has:
					bad = append(bad, "Specified unique field to update not found in import")
					skip = true
				case len(values) > 1:
					bad = append(bad, "Multiple values specified for unique field to update")
					skip = true
				case len(values) == 1:
					uv := graphStr(values[0])
					if uu == columnID.ID {
						if perlTrue(uv) {
							if err := Try(func() { record.FindCurrentIDString(uv, FindOptions{InstanceID: im.layout.InstanceID}) }); err != nil {
								bad = append(bad, fmt.Sprintf("Failed to retrieve record ID %s (try-block stopped with %s: %s: %s\n). Data will not be uploaded.",
									uv, errorReason(err), strings.ToLower(errorReason(err)), errorMessage(err)))
								skip = true
							}
						} else {
							changes = append(changes, "Unique identifier ID blank, data will be uploaded as new record.")
							record.Initialise(0)
						}
					} else if existing := record.FindUniqueColumns(im.layout.Column(uu), importUniqueValue(values[0]), allColumnIDs); existing != nil {
						record = existing
					} else {
						changes = append(changes, fmt.Sprintf("Unique identifier '%s' does not exist. Data will be uploaded as new record.", uv))
						record.Initialise(0)
					}
				default:
					record.Initialise(0)
					changes = append(changes, fmt.Sprintf("Missing unique identifier for '%s'. Data will be uploaded as new record.", im.layout.Column(uu).Name))
				}
			} else if uid := im.o.SkipExistingUnique; uid != 0 {
				values := input[uid]
				if len(values) > 1 {
					bad = append(bad, "Multiple values specified for unique field")
					skip = true
				} else if len(values) == 1 && values[0] != nil {
					uf := im.layout.Column(uid)
					if existing := record.FindUnique(uf, importUniqueValue(values[0]), 0); existing != nil {
						bad = append(bad, fmt.Sprintf("Skipping: unique identifier '%s' already exists for '%s'", graphStr(values[0]), uf.Name))
						skip = true
					} else {
						record.Initialise(0)
					}
				} else {
					record.Initialise(0)
					bad = append(bad, "Missing unique identifier. Data will be uploaded as new record.")
				}
			} else {
				record.Initialise(0)
			}

			if skip {
				skipped++
			} else {
				failed := im.updateFields(input, record, &changes)
				if im.o.NoReportChanges {
					changes = nil
				}
				if len(failed) == 0 {
					wo := WriteOptions{
						NoAlerts:            true,
						DryRun:              im.o.DryRun,
						ForceMandatory:      im.o.ForceMandatory,
						UpdateOnly:          im.o.UpdateOnly,
						NoChangeUnlessBlank: im.o.NoChangeUnlessBlank != "",
						AllowUpdate:         im.o.Append,
						VersionDatetime:     versionDT,
					}
					if versionUser != "" {
						wo.VersionUserID, _ = anyToInt64(versionUser)
					}
					if err := Try(func() { record.Write(wo) }); err != nil {
						failed = append(failed, errorMessage(err))
						write = false
					} else {
						written++
					}
				} else {
					write = false
				}
				bad = append(bad, failed...)
			}
		}
		bad = append(bad, badEnum...)
		status := "Record not written"
		if write {
			status = "Record written"
		}
		ir := map[string]any{
			"import_id": importID,
			"content":   strings.TrimSuffix(csvLine(row), "\n"),
			"status":    status,
		}
		if len(changes) > 0 && !skip {
			ir["changes"] = strings.Join(changes, ", ")
		}
		if len(bad) > 0 {
			if !skip {
				errors++
			}
			ir["errors"] = strings.Join(bad, ", ")
		}
		im.e.DB.InsertMap("import_row", ir)
		im.e.DB.Exec(`UPDATE "import" SET "row_count" = ? WHERE "id" = ?`, in, importID)
	}
	im.finish(in, written, errors, skipped)
}

// importUniqueValue is the string of a unique value (an enum ID or text).
func importUniqueValue(v any) string { return graphStr(v) }

func errorReason(err error) string {
	if e, ok := err.(*Error); ok && e.Level != "" {
		return string(e.Level)
	}
	return "ERROR"
}

func (im *Import) finish(in, written, errors, skipped int) {
	result := fmt.Sprintf("Rows in: %d, rows written: %d, errors: %d, skipped: %d", in, written, errors, skipped)
	im.e.DB.Exec(`UPDATE "import" SET "completed" = ?, "result" = ?, "written_count" = ?, "error_count" = ?, "skipped_count" = ? WHERE "id" = ?`,
		im.e.Now(), result, written, errors, skipped, im.importID)
}

func (im *Import) updateFields(input map[int64][]any, record *Record, changes *[]string) []string {
	var bad []string
	for _, col := range im.fields {
		if !col.Userinput() || col.Internal {
			continue
		}
		newv := input[col.ID]
		datum := record.Fields()[col.ID]
		oldValue := datum.AsString()
		wasBlank := datum.Blank()
		var setv any = newv
		if im.appendIx[col.ID] {
			if col.Multivalue {
				newv = append(newv, datum.SetValuesForChild()...)
				setv = newv
			} else {
				var nv string
				if len(newv) > 0 {
					nv = graphStr(newv[len(newv)-1])
				}
				if oldValue == "" {
					nv = strings.TrimLeft(nv, " \t\r\n\f\v")
				}
				ov := strings.TrimRight(oldValue, " \t\r\n\f\v")
				if ov != "" {
					ov += "\n"
				}
				setv = ov + nv
			}
		}
		if im.o.NoChangeUnlessBlank == "skip_new" && record.CurrentID() != 0 && !wasBlank && !im.appendIx[col.ID] {
			var parts []string
			for _, v := range newv {
				switch {
				case col.Fixedvals():
					parts = append(parts, im.selectsReverse(col.ID, v))
				case col.Type == "daterange":
					pair, _ := v.([]any)
					parts = append(parts, graphStr(pair[0])+" to "+graphStr(pair[1]))
				default:
					parts = append(parts, graphStr(v))
				}
			}
			newValue := strings.Join(parts, ", ")
			if strings.ToLower(oldValue) != strings.ToLower(newValue) {
				*changes = append(*changes, fmt.Sprintf(`Not going to change value of "%s" from "%s" to "%s"`, col.Name, oldValue, newValue))
			} else if oldValue != newValue && !col.Fixedvals() {
				*changes = append(*changes, fmt.Sprintf(`Not going to change case of "%s" from "%s" to "%s"`, col.Name, oldValue, newValue))
			}
			continue
		}
		if err := Try(func() { datum.SetValue(setv, SetValueOptions{}) }); err != nil {
			bad = append(bad, errorMessage(err))
		} else if !im.o.NoReportChanges && record.CurrentID() != 0 && datumChanged(datum) && !wasBlank && !im.appendIx[col.ID] {
			newValue := datum.AsString()
			if strings.ToLower(oldValue) != strings.ToLower(newValue) {
				*changes = append(*changes, fmt.Sprintf(`Change value of "%s" from "%s" to "%s"`, col.Name, oldValue, newValue))
			}
		}
	}
	return bad
}
