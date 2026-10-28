package gads

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
)

// UploadUsersOptions are the options of a user upload
type UploadUsersOptions struct {
	ViewLimits, Groups, Permissions []any
	CurrentUser                     *User
	// WelcomeText makes the welcome email of a user (GADS::welcome_text)
	WelcomeText func(email, code string) Email
}

// UploadUsers is ResultSet::User upload: users are created from a CSV file
// with headings of the user fields; any error fails the whole upload.
// The welcome emails are sent when all the users have been created.
func (e *Engine) UploadUsers(content []byte, haveFile bool, o UploadUsersOptions) int {
	if !haveFile {
		Fail("Please select a file to upload")
	}
	// open_bom: a byte order mark is removed
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	var count int
	var created []*User
	e.DB.Txn(func() {
		r := csv.NewReader(bytes.NewReader(content))
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		site := e.Site()
		heading, _ := r.Read()
		userFields := map[string]bool{}
		for _, f := range site.UserFields() {
			userFields[strings.ToLower(f.Description)] = true
		}
		mapping := map[string]int{}
		var invalid []string
		for i, h := range heading {
			if userFields[strings.ToLower(h)] {
				mapping[strings.ToLower(h)] = i
			} else {
				invalid = append(invalid, `"`+h+`"`)
			}
		}
		if len(invalid) > 0 {
			var valid []string
			for f := range userFields {
				valid = append(valid, f)
			}
			Fail("The following column headings were found which are invalid: %s. Only the following fields can be used: %s",
				strings.Join(invalid, ", "), strings.Join(valid, ", "))
		}
		if _, ok := mapping["email address"]; !ok {
			Fail("There must be an email address column in the uploaded CSV")
		}
		freetext1 := strings.ToLower(site.str("register_freetext1_name"))
		freetext2 := strings.ToLower(site.str("register_freetext2_name"))
		lists := map[string]map[string]any{}
		for _, t := range []string{"title", "organisation", "department", "team"} {
			lists[t] = map[string]any{}
			for _, row := range e.OrderedList(t) {
				lists[t][strings.ToLower(anyToString(row["name"]))] = row["id"]
			}
		}
		type rowError struct{ row, err string }
		var errs []rowError
		for {
			row, err := r.Read()
			if err != nil {
				break
			}
			get := func(name string) (string, bool) {
				if i, ok := mapping[name]; ok && i < len(row) {
					return row[i], true
				}
				_, ok := mapping[name]
				return "", ok
			}
			line := strings.Join(row, ",")
			lookup := func(heading, list, label string) any {
				name, ok := get(heading)
				if !ok {
					return nil
				}
				id := lists[list][strings.ToLower(name)]
				if id == nil {
					errs = append(errs, rowError{line, fmt.Sprintf(`%s "%s" not found`, label, name)})
				}
				return id
			}
			org := lookup(strings.ToLower(site.OrganisationName()), "organisation", strings.ToLower(site.OrganisationName()))
			dep := lookup(strings.ToLower(site.DepartmentName()), "department", strings.ToLower(site.DepartmentName()))
			team := lookup(strings.ToLower(site.TeamName()), "team", strings.ToLower(site.TeamName()))
			title := lookup("title", "title", "Title")
			value := func(name string) string { v, _ := get(name); return v }
			values := map[string]any{
				"firstname":        value("forename"),
				"surname":          value("surname"),
				"email":            value("email address"),
				"freetext1":        value(freetext1),
				"freetext2":        value(freetext2),
				"title":            title,
				"organisation":     org,
				"department_id":    dep,
				"team_id":          team,
				"view_limits":      o.ViewLimits,
				"groups":           o.Groups,
				"permissions":      o.Permissions,
				"current_user":     o.CurrentUser,
				"no_welcome_email": int64(1),
			}
			values["value"] = userValue(anyToString(values["firstname"]), anyToString(values["surname"]))
			var u *User
			if err := Try(func() { u = e.CreateUser(values) }); err != nil {
				errs = append(errs, rowError{line, ExceptionString(err)})
			} else {
				count++
			}
			if len(errs) == 0 {
				created = append(created, u)
			}
		}
		if len(errs) > 0 {
			var parts []string
			for _, x := range errs {
				parts = append(parts, x.row+" ("+x.err+")")
			}
			Fail("The upload failed with errors on the following lines: %s", strings.Join(parts, "; "))
		}
	})
	if o.WelcomeText != nil {
		for _, u := range created {
			code := anyToString(u.Row["resetpw"])
			e.SendEmail(o.WelcomeText(u.Email, code))
		}
	}
	return count
}

// ExceptionString is how a Log::Report exception (wasFatal) stringifies:
// "reason: message\n". Errors that are not GADS errors are panics.
func ExceptionString(err error) string {
	if ge, ok := err.(*Error); ok {
		lvl := ge.Level
		if lvl == "" {
			lvl = LevelError
		}
		return strings.ToLower(string(lvl)) + ": " + ge.Msg + "\n"
	}
	return "panic: " + err.Error() + "\n"
}
