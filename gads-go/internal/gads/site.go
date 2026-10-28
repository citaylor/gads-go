package gads

import (
	"strings"
	"unicode"

	"github.com/ctrlo/gads-go/internal/db"
)

// Site is the site of the engine (GADS::Schema::Result::Site), the first
// row of the site table as limited by the engine's SiteID.
type Site struct {
	row db.Row
}

// UserField is a field of a user that the site uses (Site->user_fields).
type UserField struct {
	Name        string
	Description string
	Editable    bool
	// EditableRaw is the editable value as configured (1 by default)
	EditableRaw any
	// The table and field of the value (for dropdowns)
	Table, Field string
	Type         string
	Placeholder  string
	IsRequired   bool
	UserHidden   bool
}

// Site loads the site.
func (e *Engine) Site() *Site {
	q := `SELECT * FROM "site" "me"`
	if e.SiteID != 0 {
		q += ` WHERE "me"."id" = ` + itoa(e.SiteID)
	}
	row := e.DB.QueryRow(q + ` LIMIT 1`)
	if row == nil {
		row = db.Row{}
	}
	return &Site{row: row}
}

func (s *Site) str(c string) string {
	v, _ := db.AsString(s.row[c])
	return v
}

func (s *Site) flag(c string) bool { return rowBool(s.row[c]) }

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// OrganisationName is the name used for organisations.
func (s *Site) OrganisationName() string {
	return orDefault(s.str("register_organisation_name"), "Organisation")
}

// DepartmentName is the name used for departments.
func (s *Site) DepartmentName() string {
	return orDefault(s.str("register_department_name"), "Department")
}

// TeamName is the name used for teams.
func (s *Site) TeamName() string { return orDefault(s.str("register_team_name"), "Team") }

// UserFields returns the user fields of the site, each editable unless
// configured otherwise.
func (s *Site) UserFields() []UserField {
	fields := []UserField{
		{Name: "firstname", Description: "Forename", Type: "freetext", Placeholder: "Forename"},
		{Name: "surname", Description: "Surname", Type: "freetext", Placeholder: "Surname"},
		{Name: "email", Description: "Email address", Type: "freetext", Placeholder: "name@example.com", IsRequired: true},
		{Name: "account_request_notes", Description: orDefault(s.str("account_request_notes_name"), "Notes"), Type: "textarea",
			Placeholder: orDefault(s.str("account_request_notes_placeholder"), "Notes"), UserHidden: true},
	}
	if s.flag("register_show_title") {
		fields = append(fields, UserField{Name: "title", Description: "Title", Type: "dropdown", Placeholder: "Select title", Table: "title", Field: "name"})
	}
	if s.flag("register_show_organisation") {
		fields = append(fields, UserField{Name: "organisation", Description: s.OrganisationName(), Type: "dropdown", Placeholder: "Select " + s.OrganisationName(), Table: "organisation", Field: "name"})
	}
	if s.flag("register_show_department") {
		fields = append(fields, UserField{Name: "department_id", Description: s.DepartmentName(), Type: "dropdown", Placeholder: "Select " + s.DepartmentName(), Table: "department", Field: "name"})
	}
	if s.flag("register_show_team") {
		fields = append(fields, UserField{Name: "team_id", Description: s.TeamName(), Type: "dropdown", Placeholder: "Select " + s.TeamName(), Table: "team", Field: "name"})
	}
	if n := s.str("register_freetext1_name"); n != "" {
		fields = append(fields, UserField{Name: "freetext1", Description: n, Type: "freetext", Placeholder: orDefault(s.str("register_freetext1_placeholder"), n)})
	}
	if n := s.str("register_freetext2_name"); n != "" {
		fields = append(fields, UserField{Name: "freetext2", Description: n, Type: "freetext", Placeholder: orDefault(s.str("register_freetext2_placeholder"), n)})
	}
	editable := map[string]any{}
	if js := s.str("user_editable_fields"); js != "" {
		if v, err := DecodeJSON(js); err == nil {
			editable, _ = v.(map[string]any)
		}
	}
	for i := range fields {
		fields[i].Editable = true // Default to editable
		fields[i].EditableRaw = int64(1)
		if v, ok := editable[fields[i].Name]; ok && v != nil {
			fields[i].Editable = truthy(v)
			fields[i].EditableRaw = v
		}
	}
	return fields
}

// UserFieldIsEditable reports whether a user field is used and editable.
func (s *Site) UserFieldIsEditable(name string) bool {
	for _, f := range s.UserFields() {
		if f.Name == name && f.Editable {
			return true
		}
	}
	return false
}

// Row returns the site's database row
func (s *Site) Row() db.Row { return s.row }

// ID returns the site ID
func (s *Site) ID() int64 {
	id, _ := db.AsInt64(s.row["id"])
	return id
}

// UserFieldsAsString lists the descriptions of the user fields
func (s *Site) UserFieldsAsString() string {
	var d []string
	for _, f := range s.UserFields() {
		d = append(d, f.Description)
	}
	return strings.Join(d, ", ")
}

func notBlank(v string) bool { return strings.TrimSpace(v) != "" }

// HasMainHomepage reports whether the site has homepage text
func (s *Site) HasMainHomepage() bool {
	return notBlank(s.str("homepage_text")) || notBlank(s.str("homepage_text2"))
}

// HasTableHomepage reports whether any table of the site has homepage text
func (s *Site) HasTableHomepage(e *Engine) bool {
	for _, r := range e.DB.Query(`SELECT "homepage_text", "homepage_text2" FROM "instance" WHERE "site_id" = ? ORDER BY "id"`, s.ID()) {
		a, _ := db.AsString(r["homepage_text"])
		b, _ := db.AsString(r["homepage_text2"])
		if notBlank(a) || notBlank(b) {
			return true
		}
	}
	return false
}

// Plural pluralises an English noun (Lingua::EN::Inflect PL, for the
// regular forms)
func Plural(n string) string {
	l := strings.ToLower(n)
	if p, ok := irregularPlurals[l]; ok {
		// Keep the case of the first letter
		if n[0] != l[0] {
			return strings.ToUpper(p[:1]) + p[1:]
		}
		return p
	}
	switch {
	case n == "":
		return n
	case strings.HasSuffix(l, "s"), strings.HasSuffix(l, "x"), strings.HasSuffix(l, "z"),
		strings.HasSuffix(l, "ch"), strings.HasSuffix(l, "sh"):
		return n + "es"
	// A capitalised word is a proper noun (Mary -> Marys)
	case strings.HasSuffix(l, "y") && len(l) > 1 && !strings.ContainsRune("aeiou", rune(l[len(l)-2])) && !unicode.IsUpper(rune(n[0])):
		return n[:len(n)-1] + "ies"
	}
	return n + "s"
}

var irregularPlurals = map[string]string{
	"person": "people", "man": "men", "woman": "women", "child": "children",
	"mouse": "mice", "foot": "feet", "tooth": "teeth", "goose": "geese",
}

// FreetextName is register_freetext1_name or register_freetext2_name
func (s *Site) FreetextName(n int) string {
	return s.str("register_freetext" + itoa(int64(n)) + "_name")
}
