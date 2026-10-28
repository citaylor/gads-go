package web

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// any ['get', 'post'] => '/api/users' (API.pm:1331): the users of the
// user administration table (DataTables server-side processing)
func (s *Server) hAPIUsers(c *Ctx) string {
	// Parameters may be passed by URL query or in the body, flattened into
	// one object (Hash::MultiValue get: the last value)
	params := map[string]string{}
	for _, src := range []*multiValue{c.query, c.body} {
		if src == nil {
			continue
		}
		for _, k := range src.keys {
			vs := src.getAll(k)
			if len(vs) > 0 {
				params[k] = vs[len(vs)-1]
			}
		}
	}
	get := func(k string) (string, bool) { v, ok := params[k]; return v, ok }
	site := s.E.Site()
	row := site.Row()
	flag := func(k string) bool { return truthy(row[k]) }
	str := func(k string) string { v, _ := db.AsString(row[k]); return v }

	if v, _ := get("cols"); truthy(v) {
		// Columns shown in the users table summary
		cols := []string{"surname", "firstname"}
		if flag("register_show_title") {
			cols = append(cols, "title")
		}
		cols = append(cols, "email")
		if flag("register_show_organisation") {
			cols = append(cols, "organisation")
		}
		if flag("register_show_department") {
			cols = append(cols, "department")
		}
		if flag("register_show_team") {
			cols = append(cols, "team")
		}
		if str("register_freetext1_name") != "" {
			cols = append(cols, "freetext1")
		}
		cols = append(cols, "created", "lastlogin", "created_by")
		out := []any{}
		for _, col := range cols {
			out = append(out, map[string]any{"name": col, "data": col})
		}
		c.ContentType("application/json; charset=UTF-8")
		return gads.EncodeJSON(out)
	}

	start, _ := get("start")
	if !truthy(start) {
		start = "0"
	}
	length, _ := get("length")
	if !truthy(length) {
		length = "10"
	}

	from := ` FROM "user" "me" LEFT JOIN "organisation" "organisation" ON "organisation"."id" = "me"."organisation" LEFT JOIN "department" "department" ON "department"."id" = "me"."department_id" LEFT JOIN "team" "team" ON "team"."id" = "me"."team_id" LEFT JOIN "title" "title" ON "title"."id" = "me"."title"`
	where := []string{`"me"."account_request" = 0`, `"me"."deleted" IS NULL`}
	if s.E.SiteID != 0 {
		where = append(where, `"me"."site_id" = `+fmtInt(s.E.SiteID))
	}
	total, _ := s.E.DB.QueryInt64(`SELECT COUNT( * )` + from + ` WHERE ` + strings.Join(where, " AND "))

	colOrder, hasOrder := get("order[0][column]")
	var sortBy string
	if hasOrder {
		sortBy, _ = get("columns[" + colOrder + "][name]")
	}
	dir, _ := get("order[0][dir]")
	search, _ := get("search[value]")

	var field *gads.UserField
	if sortBy != "" {
		for _, f := range site.UserFields() {
			if f.Description == sortBy {
				f := f
				field = &f
				break
			}
		}
	}
	switch {
	case field != nil:
		switch field.Name {
		case "surname":
			sortBy = `"me"."surname"`
		case "firstname":
			sortBy = `"me"."firstname"`
		case "title":
			sortBy = `"title"."name"`
		case "email":
			sortBy = `"me"."email"`
		case "organisation":
			sortBy = `"organisation"."name"`
		case "department_id":
			sortBy = `"department"."name"`
		case "team_id":
			sortBy = `"team"."name"`
		default:
			sortBy = `"me"."id"`
		}
	case sortBy == "Created":
		sortBy = `"me"."created"`
	case sortBy == "Created by":
		sortBy = `"me"."created_by_id"`
	case sortBy == "ID":
		sortBy = `"me"."id"`
	case sortBy == "Last login":
		sortBy = `"me"."lastlogin"`
	default:
		sortBy = `"me"."surname"`
	}

	var args []any
	for _, w := range perlSplit(`\s+`, search) {
		if !truthy(w) {
			continue
		}
		// Escape special like char (SQLite has no escape character, as Perl)
		w = strings.ReplaceAll(w, "_", `\_`)
		like := "%" + w + "%"
		conds := []string{}
		for _, col := range []string{`"department"."name"`, `"me"."email"`, `"me"."freetext1"`, `"me"."freetext2"`} {
			conds = append(conds, col+" LIKE ?")
			args = append(args, like)
		}
		if isDigits(w) {
			conds = append(conds, `"me"."id" = ?`)
			args = append(args, w)
		} else {
			conds = append(conds, `"me"."id" IS NULL`)
		}
		for _, col := range []string{`"me"."value"`, `"organisation"."name"`, `"team"."name"`, `"title"."name"`} {
			conds = append(conds, col+" LIKE ?")
			args = append(args, like)
		}
		where = append(where, "( "+strings.Join(conds, " OR ")+" )")
	}
	cond := ` WHERE ` + strings.Join(where, " AND ")
	filtered, _ := s.E.DB.QueryInt64(`SELECT COUNT( * )`+from+cond, args...)
	order := "DESC"
	if dir == "asc" {
		order = "ASC"
	}
	rows := s.E.DB.Query(`SELECT "me"."id", "me"."surname", "me"."firstname", "title"."name" AS "title_name", "me"."email", "organisation"."name" AS "organisation_name", "department"."name" AS "department_name", "team"."name" AS "team_name", "me"."created", "me"."freetext1", "me"."freetext2", "me"."lastlogin", "me"."value", "created_by"."firstname" AS "created_by_firstname", "created_by"."surname" AS "created_by_surname", "created_by"."id" AS "created_by_id"`+
		from+` LEFT JOIN "user" "created_by" ON "created_by"."id" = "me"."created_by_id"`+cond+` ORDER BY `+sortBy+` `+order+` LIMIT ? OFFSET ?`, append(args, length, start)...)

	data := []any{}
	for _, r := range rows {
		data = append(data, userForDataTable(r, site))
	}
	var draw any
	if v, ok := get("draw"); ok {
		draw = v
	}
	c.ContentType("application/json; charset=UTF-8")
	return gads.EncodeJSON(map[string]any{
		"draw":            draw,
		"recordsTotal":    total,
		"recordsFiltered": filtered,
		"data":            data,
	})
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// userForDataTable is Result::User->for_data_table
func userForDataTable(r db.Row, site *gads.Site) map[string]any {
	field := func(name string, v any) map[string]any {
		return map[string]any{"type": "string", "name": name, "values": []any{v}}
	}
	ymd := func(k, none string) any {
		t, ok := db.AsTime(r[k])
		if !ok || r[k] == nil {
			return none
		}
		return t.Format("2006-01-02")
	}
	id, _ := db.AsInt64(r["id"])
	out := map[string]any{
		"_id":        id,
		"ID":         map[string]any{"type": "id", "name": "ID", "values": []any{id}},
		"Surname":    field("Surname", r["surname"]),
		"Forename":   field("Forename", r["firstname"]),
		"Email":      field("Email", r["email"]),
		"Created":    field("Created", ymd("created", "Unknown")),
		"Last login": field("Last login (GMT)", ymd("lastlogin", "Never logged in")),
		"Created by": field("Created By", "Unknown"),
	}
	// created_by->as_string
	if r["created_by_id"] != nil {
		fn, _ := db.AsString(r["created_by_firstname"])
		sn, _ := db.AsString(r["created_by_surname"])
		out["Created by"] = field("Created By", sn+", "+fn)
	}
	row := site.Row()
	if truthy(row["register_show_title"]) {
		out["Title"] = field("Title", r["title_name"])
	}
	if truthy(row["register_show_organisation"]) {
		out[site.OrganisationName()] = field(site.OrganisationName(), r["organisation_name"])
	}
	if truthy(row["register_show_department"]) {
		out[site.DepartmentName()] = field(site.DepartmentName(), r["department_name"])
	}
	if truthy(row["register_show_team"]) {
		out[site.TeamName()] = field(site.TeamName(), r["team_name"])
	}
	if n := site.FreetextName(1); n != "" {
		out[n] = field(n, r["freetext1"])
	}
	return out
}

// post '/:layout_name/api/field/values/:id' (API.pm:423): the filtered
// values of a curval field, given the values of the fields its filter uses
func (s *Server) hAPIFieldValues(c *Ctx) string {
	user := c.LoggedInUser()
	if user == nil {
		gads.Fail("User session has timed out - please log in and try again")
	}
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	colID, _ := c.RouteParam("id")
	token, _ := c.QueryParam("submission-token")
	if !truthy(token) {
		gads.Panic("Submission ID missing")
	}
	cid, _ := toInt64(colID)
	curval := layout.Column(cid)
	if curval == nil {
		gads.Panic(`Can't call method "subvals_input_required" on an undefined value`)
	}

	record := s.E.NewRecord(layout, user)
	record.Initialise(0)

	// Missing values are reported as non-fatal errors: all messages of the
	// block are captured, and the errors reported to the caller
	before := len(s.E.Reports)
	err := gads.Try(func() {
		for _, col := range curval.SubvalsInputRequired() {
			vals := []any{}
			for _, v := range c.query.getAll(col.Field()) {
				vals = append(vals, v)
			}
			record.GetFieldValue(col).SetValue(vals, gads.SetValueOptions{})
		}
		record.Write(gads.WriteOptions{
			DryRun:          true,
			MissingNotFatal: true,
			// A read-only value of the initiating record should still
			// affect the filtered values
			ForceReadonlyNew: true,
			SubmittedFields:  curval.SubvalsInputRequired(),
			SubmissionToken:  token,
		})
	})
	reports := append([]gads.Report(nil), s.E.Reports[before:]...)
	s.E.Reports = s.E.Reports[:before]
	var msgs []string
	for _, r := range reports {
		if r.Level == gads.LevelError {
			msgs = append(msgs, r.Msg)
		}
	}
	if err != nil {
		// See whether any unexpected exceptions first, and if so throw them
		if e, ok := err.(*gads.Error); !ok || e.Level != gads.LevelError {
			panic(err)
		}
		msgs = append(msgs, err.(*gads.Error).Msg)
	}
	if len(msgs) > 0 {
		return gads.EncodeJSON(map[string]any{"error": int64(1), "message": strings.Join(msgs, ", ")})
	}
	records := []any{}
	for _, v := range curval.FilteredValues(token) {
		records = append(records, map[string]any{"id": v.ID, "label": v.Value, "html": v.HTML})
	}
	return gads.EncodeJSON(map[string]any{"error": int64(0), "records": records})
}
