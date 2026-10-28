package conformance

// Port of t/lib/Test/GADS/DataSheet.pm. The order of database operations
// matches the original exactly so that all generated IDs are identical.

import (
	"fmt"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// DataSheet is a test table with a standard set of columns.
type DataSheet struct {
	e   *gads.Engine
	run *Runner

	InstanceID        int64
	Data              []map[string]any
	dataSet           bool
	Curval            int64
	CurvalFieldIDs    []int64
	ColumnCount       map[string]int
	EnumvalsCount     int
	CalcCode          string
	calcCodeSet       bool
	CalcReturnType    string
	RagCode           string
	ragCodeSet        bool
	HasRag            bool
	Multivalue        bool
	MultivalueColumns map[string]bool
	Optional          bool
	NoGroups          bool
	UsersToCreate     []string
	SiteID            int64
	CurvalOffset      *int64

	layout       *gads.Layout
	columns      map[string]*gads.Column
	group        int64
	groupBuilt   bool
	users        map[string]*gads.User
	userLayout   *gads.User
	usersBuilt   bool
	organisation int64
	department   int64
	permissions  map[string]int64
}

// NewDataSheet creates a data sheet with default options.
func NewDataSheet(run *Runner) *DataSheet {
	return &DataSheet{
		e:              run.e,
		run:            run,
		InstanceID:     1,
		ColumnCount:    map[string]int{"enum": 1, "curval": 1, "tree": 1, "integer": 1},
		EnumvalsCount:  3,
		CalcReturnType: "integer",
		HasRag:         true,
		Optional:       true,
		MultivalueColumns: map[string]bool{
			"curval": true, "enum": true, "tree": true, "file": true,
			"date": true, "daterange": true, "string": true, "calc": true,
			"integer": true,
		},
		UsersToCreate: []string{"superadmin", "audit", "useradmin", "normal1", "normal2"},
	}
}

func (s *DataSheet) curvalOffset() int64 {
	if s.CurvalOffset != nil {
		return *s.CurvalOffset
	}
	if s.Curval != 0 {
		return 6
	}
	return 0
}

func (s *DataSheet) data() []map[string]any {
	if s.dataSet {
		return s.Data
	}
	off := s.curvalOffset()
	return []map[string]any{
		{"string1": "Foo", "integer1": 50, "date1": "2014-10-10", "enum1": 1 + off, "daterange1": []any{"2012-02-10", "2013-06-15"}},
		{"string1": "Bar", "integer1": 99, "date1": "2009-01-02", "enum1": 2 + off, "daterange1": []any{"2008-05-04", "2008-07-14"}},
	}
}

func (s *DataSheet) calcCode() string {
	if s.calcCodeSet {
		return s.CalcCode
	}
	i := s.InstanceID
	return fmt.Sprintf(`function evaluate (L%[1]ddaterange1)
        if type(L%[1]ddaterange1) == "table" and L%[1]ddaterange1[1] then
            dr1 = L%[1]ddaterange1[1]
        elseif type(L%[1]ddaterange1) == "table" and next(L%[1]ddaterange1) == nil then
            dr1 = nil
        else
            dr1 = L%[1]ddaterange1
        end
        if dr1 == null then return end
        return dr1.from.year
    end`, i)
}

func (s *DataSheet) ragCode() string {
	if s.ragCodeSet {
		return s.RagCode
	}
	i := s.InstanceID
	return fmt.Sprintf(`
        function evaluate (L%[1]ddaterange1)
            if type(L%[1]ddaterange1) == "table" and L%[1]ddaterange1[1] then
                dr1 = L%[1]ddaterange1[1]
            elseif type(L%[1]ddaterange1) == "table" and next(L%[1]ddaterange1) == nil then
                dr1 = nil
            else
                dr1 = L%[1]ddaterange1
            end
            if dr1 == nil then return 'grey' end
            if dr1.from.year < 2012 then return 'red' end
            if dr1.from.year == 2012 then return 'amber' end
            if dr1.from.year > 2012 then return 'green' end
        end
    `, i)
}

// Layout returns the sheet layout (built as the superadmin user).
func (s *DataSheet) Layout() *gads.Layout {
	if s.layout == nil {
		if s.e.SiteID != 0 {
			if _, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "site" WHERE "id" = ?`, s.e.SiteID); !ok {
				s.e.DB.InsertMap("site", map[string]any{"id": s.e.SiteID})
			}
		}
		if _, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "instance" WHERE "id" = ? AND "name" = ?`, s.InstanceID, fmt.Sprintf("Layout%d", s.InstanceID)); !ok {
			m := map[string]any{"id": s.InstanceID, "name": fmt.Sprintf("Layout%d", s.InstanceID)}
			if s.e.SiteID != 0 {
				m["site_id"] = s.e.SiteID
			} else {
				m["site_id"] = nil
			}
			s.e.DB.InsertMap("instance", m)
		}
		// The layout is created with the superadmin user, which builds all
		// the users (and group) before the internal columns are created
		u := s.userLayout
		if u == nil {
			u = s.User()
		}
		s.layout = s.e.NewLayout(s.InstanceID, u)
		s.layout.CreateInternalColumns()
	}
	return s.layout
}

// SetUserLayout sets the user the layout is built with (user_layout).
func (s *DataSheet) SetUserLayout(u *gads.User) { s.userLayout = u }

// ClearLayout removes the layout so that it is built again.
func (s *DataSheet) ClearLayout() { s.layout = nil }

// ShareUsers uses the users of another sheet (_users).
func (s *DataSheet) ShareUsers(other *DataSheet) {
	s.users = other.Users()
	s.usersBuilt = true
}

// Group returns the group1 group ID.
func (s *DataSheet) Group() int64 {
	if !s.groupBuilt {
		s.groupBuilt = true
		if id, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "group" WHERE "name" = ? ORDER BY "id" LIMIT 1`, "group1"); ok {
			s.group = id
			s.e.WriteGroup(id, "group1")
		} else {
			s.group = s.e.CreateGroup("group1")
		}
	}
	return s.group
}

// SetGroup shares a group with another sheet.
func (s *DataSheet) SetGroup(id int64) {
	s.group = id
	s.groupBuilt = true
}

// User returns the superadmin user.
func (s *DataSheet) User() *gads.User { return s.Users()["superadmin"] }

// Users returns the created users.
func (s *DataSheet) Users() map[string]*gads.User {
	if s.usersBuilt {
		return s.users
	}
	s.usersBuilt = true
	foo := s.e.InsertSite("team", map[string]any{"name": "Foo"})
	bar := s.e.InsertSite("team", map[string]any{"name": "Bar"})
	s.users = map[string]*gads.User{}
	count := int64(0)
	if s.e.SiteID != 0 {
		count = (s.e.SiteID - 1) * 5
	}
	for _, perm := range s.UsersToCreate {
		count++
		var perms []string
		if strings.Contains(perm, "normal") {
			perms = []string{"view_create"}
		} else if perm == "superadmin" {
			perms = []string{"superadmin", "link", "delete", "purge", "view_group", "download", "view_limit_extra"}
		} else {
			perms = []string{perm}
		}
		team := bar
		if perm == "normal1" {
			team = foo
		}
		s.users[perm] = s.CreateUser(perms, count, team, false, 0)
	}
	return s.users
}

func (s *DataSheet) organisationID() int64 {
	if s.organisation == 0 {
		s.organisation = s.e.InsertSite("organisation", map[string]any{"name": "My Organisation"})
	}
	return s.organisation
}

func (s *DataSheet) departmentID() int64 {
	if s.department == 0 {
		s.department = s.e.InsertSite("department", map[string]any{"name": "My Department"})
	}
	return s.department
}

func (s *DataSheet) permissionIDs() map[string]int64 {
	if s.permissions == nil {
		s.permissions = map[string]int64{}
		for _, p := range []string{"superadmin", "audit", "useradmin"} {
			id, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "permission" WHERE "name" = ? ORDER BY "id" LIMIT 1`, p)
			if !ok {
				id = s.e.DB.InsertMap("permission", map[string]any{"name": p})
			}
			s.permissions[p] = id
		}
	}
	return s.permissions
}

// CreateUser is DataSheet::create_user.
func (s *DataSheet) CreateUser(perms []string, userID int64, teamID int64, noGroup bool, instanceID int64) *gads.User {
	if instanceID == 0 {
		instanceID = s.InstanceID
	}
	temp := "TEMPUSER@EXAMPLE.COM"
	if userID != 0 {
		// find_or_create: the primary key is used for the find
		if _, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "user" WHERE "id" = ?`, userID); !ok {
			s.e.InsertUser(map[string]any{"id": userID, "username": temp, "email": temp})
		}
	} else {
		userID = s.e.InsertUser(map[string]any{"username": temp, "email": temp})
	}
	org := s.organisationID()
	dept := s.departmentID()
	var team any
	if teamID != 0 {
		team = teamID
	}
	s.e.UpdateUserFields(userID, map[string]any{
		"username":      fmt.Sprintf("user%d@example.com", userID),
		"email":         fmt.Sprintf("user%d@example.com", userID),
		"firstname":     fmt.Sprintf("User%d", userID),
		"surname":       fmt.Sprintf("User%d", userID),
		"value":         fmt.Sprintf("User%d, User%d", userID, userID),
		"organisation":  org,
		"department_id": dept,
		"team_id":       team,
	})
	if !noGroup && s.Group() != 0 {
		if _, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "user_group" WHERE "user_id" = ? AND "group_id" = ?`, userID, s.Group()); !ok {
			s.e.DB.InsertMap("user_group", map[string]any{"user_id": userID, "group_id": s.Group()})
		}
	}
	for _, p := range perms {
		if pid, ok := s.permissionIDs()[p]; ok && pid != 0 {
			if _, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "user_permission" WHERE "user_id" = ? AND "permission_id" = ?`, userID, pid); !ok {
				s.e.DB.InsertMap("user_permission", map[string]any{"user_id": userID, "permission_id": pid})
			}
		} else if !s.NoGroups {
			name := fmt.Sprintf("%s_%d", p, userID)
			gid, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "group" WHERE "name" = ? ORDER BY "id" LIMIT 1`, name)
			if !ok {
				gid = s.e.InsertSite("group", map[string]any{"name": name})
			}
			if _, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "instance_group" WHERE "instance_id" = ? AND "group_id" = ? AND "permission" = ?`, instanceID, gid, p); !ok {
				s.e.DB.InsertMap("instance_group", map[string]any{"instance_id": instanceID, "group_id": gid, "permission": p})
			}
			if _, ok := s.e.DB.QueryInt64(`SELECT "id" FROM "user_group" WHERE "user_id" = ? AND "group_id" = ?`, userID, gid); !ok {
				s.e.DB.InsertMap("user_group", map[string]any{"user_id": userID, "group_id": gid})
			}
		}
	}
	return s.e.FindUser(userID)
}

var defaultPermissions = []string{"read", "write_new", "write_existing", "write_new_no_approval", "write_existing_no_approval"}

func (s *DataSheet) perms() map[int64][]string {
	return map[int64][]string{s.Group(): defaultPermissions}
}

func (s *DataSheet) count(t string) int {
	if n, ok := s.ColumnCount[t]; ok && n > 0 {
		return n
	}
	return 1
}

func (s *DataSheet) mv(t string) bool { return s.Multivalue && s.MultivalueColumns[t] }

// Columns builds (if needed) and returns the columns.
func (s *DataSheet) Columns() map[string]*gads.Column {
	if s.columns != nil {
		return s.columns
	}
	layout := s.Layout()
	iid := s.InstanceID
	var built []*gads.Column
	write := func(c *gads.Column) {
		c.Write(gads.ColumnWriteOptions{})
	}
	for i := 1; i <= s.count("string"); i++ {
		c := layout.NewColumn("string")
		c.Optional = s.Optional
		c.Name = fmt.Sprintf("string%d", i)
		c.NameShort = fmt.Sprintf("L%dstring%d", iid, i)
		if s.mv("string") {
			c.Multivalue = true
		}
		c.SetPermissions(s.perms())
		write(c)
		built = append(built, c)
	}
	for i := 1; i <= s.count("integer"); i++ {
		c := layout.NewColumn("intgr")
		c.Optional = s.Optional
		c.Name = fmt.Sprintf("integer%d", i)
		c.NameShort = fmt.Sprintf("L%dinteger%d", iid, i)
		if s.mv("integer") {
			c.Multivalue = true
		}
		c.SetPermissions(s.perms())
		write(c)
		built = append(built, c)
	}
	for i := 1; i <= s.count("enum"); i++ {
		c := layout.NewColumn("enum")
		c.Optional = s.Optional
		c.Name = fmt.Sprintf("enum%d", i)
		c.NameShort = fmt.Sprintf("L%denum%d", iid, i)
		if s.mv("enum") {
			c.Multivalue = true
		}
		var ev []map[string]any
		for j := 1; j <= s.EnumvalsCount; j++ {
			ev = append(ev, map[string]any{"value": fmt.Sprintf("foo%d", j)})
		}
		c.SetEnumvals(ev)
		c.SetPermissions(s.perms())
		write(c)
		built = append(built, c)
	}
	for i := 1; i <= s.count("tree"); i++ {
		c := layout.NewColumn("tree")
		c.Optional = s.Optional
		c.Name = fmt.Sprintf("tree%d", i)
		c.NameShort = fmt.Sprintf("L%dtree%d", iid, i)
		c.SetPermissions(s.perms())
		if s.mv("tree") {
			c.Multivalue = true
		}
		write(c)
		c.TreeUpdate([]map[string]any{
			{"children": []any{}, "data": map[string]any{}, "text": "tree1", "id": "j1_1"},
			{"data": map[string]any{}, "text": "tree2", "id": "j1_2", "children": []any{
				map[string]any{"data": map[string]any{}, "text": "tree3", "children": []any{}, "id": "j1_3"},
			}},
		})
		built = append(built, c)
	}
	for i := 1; i <= s.count("date"); i++ {
		c := layout.NewColumn("date")
		c.Optional = s.Optional
		c.Name = fmt.Sprintf("date%d", i)
		c.NameShort = fmt.Sprintf("L%ddate%d", iid, i)
		c.SetPermissions(s.perms())
		if s.mv("date") {
			c.Multivalue = true
		}
		write(c)
		built = append(built, c)
	}
	for i := 1; i <= s.count("date"); i++ {
		c := layout.NewColumn("daterange")
		c.Optional = s.Optional
		c.Name = fmt.Sprintf("daterange%d", i)
		c.NameShort = fmt.Sprintf("L%ddaterange%d", iid, i)
		c.SetPermissions(s.perms())
		if s.mv("daterange") {
			c.Multivalue = true
		}
		write(c)
		built = append(built, c)
	}
	{
		c := layout.NewColumn("file")
		c.Optional = s.Optional
		c.Name = "file1"
		c.SetPermissions(s.perms())
		if s.mv("file") {
			c.Multivalue = true
		}
		write(c)
		built = append(built, c)
	}
	{
		c := layout.NewColumn("person")
		c.Optional = s.Optional
		c.Name = "person1"
		c.SetPermissions(s.perms())
		write(c)
		built = append(built, c)
	}
	if s.Curval != 0 {
		for i := 1; i <= s.count("curval"); i++ {
			c := layout.NewColumn("curval")
			c.Optional = s.Optional
			c.NameShort = fmt.Sprintf("L%dcurval%d", iid, i)
			c.SetRefersToInstanceID(s.Curval)
			ids := s.CurvalFieldIDs
			if ids == nil {
				ids = s.e.DB.QueryInt64s(`SELECT "id" FROM "layout" WHERE "type" != 'autocur' AND "internal" = 0 AND "instance_id" = ? ORDER BY "id"`, s.Curval)
			}
			c.SetCurvalFieldIDs(ids)
			c.Name = fmt.Sprintf("curval%d", i)
			if s.mv("curval") {
				c.Multivalue = true
			}
			c.SetPermissions(s.perms())
			write(c)
			built = append(built, c)
		}
	}
	if s.HasRag {
		c := layout.NewColumn("rag")
		c.SetCode(s.ragCode())
		c.Name = "rag1"
		c.SetPermissions(s.perms())
		write(c)
		built = append(built, c)
	}
	layout.Clear()
	{
		c := layout.NewColumn("calc")
		c.SetCode(s.calcCode())
		c.Name = "calc1"
		c.NameShort = fmt.Sprintf("L%dcalc1", iid)
		c.ReturnType = s.CalcReturnType
		c.SetPermissions(s.perms())
		if s.mv("calc") {
			c.Multivalue = true
		}
		write(c)
		built = append(built, c)
	}
	layout.Clear()
	s.columns = map[string]*gads.Column{}
	for _, c := range built {
		s.columns[c.Name] = layout.Column(c.ID)
	}
	return s.columns
}

// AddAutocur adds an autocur column (DataSheet::add_autocur).
func (s *DataSheet) AddAutocur(refersTo int64, relatedFieldID int64, curvalFieldIDs []int64) *gads.Column {
	layout := s.Layout()
	layout.Clear()
	c := layout.NewColumn("autocur")
	c.SetRefersToInstanceID(refersTo)
	if curvalFieldIDs == nil {
		curvalFieldIDs = s.e.DB.QueryInt64s(`SELECT "id" FROM "layout" WHERE "instance_id" = ? AND "internal" = 0 ORDER BY "id"`, refersTo)
	}
	c.SetCurvalFieldIDs(curvalFieldIDs)
	n, _ := s.e.DB.QueryInt64(`SELECT COUNT(*) FROM "layout" WHERE "type" = 'autocur' AND "instance_id" = ?`, layout.InstanceID)
	n++
	c.Name = fmt.Sprintf("autocur%d", n)
	c.NameShort = fmt.Sprintf("L%dautocur%d", layout.InstanceID, n)
	c.RelatedFieldID = &relatedFieldID
	if s.Group() != 0 {
		c.SetPermissions(s.perms())
	}
	c.Write(gads.ColumnWriteOptions{})
	s.Columns()[c.Name] = c
	return c
}

// CreateRecords writes the sheet data.
func (s *DataSheet) CreateRecords() {
	cols := s.Columns()
	// Perl's layout holds a weak reference to its record, so the records
	// created here no longer belong to the layout afterwards
	prev := s.Layout().Record()
	defer func() { s.Layout().SetRecord(prev) }()
	for _, datum := range s.data() {
		rec := s.e.NewRecord(s.Layout(), s.User())
		rec.Initialise(s.Layout().InstanceID)
		set := func(name string) {
			c := cols[name]
			if c == nil {
				return
			}
			rec.Fields()[c.ID].SetValue(datum[name], gads.SetValueOptions{})
		}
		for i := 1; i <= s.count("integer"); i++ {
			set(fmt.Sprintf("integer%d", i))
		}
		for i := 1; i <= s.count("string"); i++ {
			set(fmt.Sprintf("string%d", i))
		}
		for i := 1; i <= s.count("date"); i++ {
			set(fmt.Sprintf("date%d", i))
		}
		for i := 1; i <= s.count("daterange"); i++ {
			c := cols[fmt.Sprintf("daterange%d", i)]
			if c == nil {
				continue
			}
			v := datum[fmt.Sprintf("daterange%d", i)]
			if v == nil {
				v = []any{}
			}
			rec.Fields()[c.ID].SetValue(v, gads.SetValueOptions{})
		}
		for _, t := range []string{"enum", "tree"} {
			for i := 1; i <= s.count(t); i++ {
				name := fmt.Sprintf("%s%d", t, i)
				c := cols[name]
				v := datum[name]
				var values []any
				if arr, ok := v.([]any); ok {
					values = arr
				} else {
					values = []any{v}
				}
				var out []any
				for _, x := range values {
					str := fmt.Sprint(x)
					if x != nil && str != "" && !isDigits(str) {
						var found any
						for _, e := range c.Enumvals() {
							if fmt.Sprint(e["value"]) == str {
								found = e["id"]
							}
						}
						out = append(out, found)
					} else {
						out = append(out, x)
					}
				}
				rec.Fields()[c.ID].SetValue(out, gads.SetValueOptions{})
			}
		}
		if p := datum["person1"]; p != nil {
			pid := toInt64(p)
			if s.e.FindUser(pid) == nil {
				rec.Fields()[cols["person1"].ID].SetValue(map[string]any{
					"id":       pid,
					"username": fmt.Sprintf("user%d@example.com", pid),
					"email":    fmt.Sprintf("user%d@example.com", pid),
					"value":    fmt.Sprintf("User%d, User%d", pid, pid),
				}, gads.SetValueOptions{})
			}
		}
		rec.Fields()[cols["person1"].ID].SetValue(datum["person1"], gads.SetValueOptions{})
		if _, ok := cols["curval1"]; ok {
			for i := 1; i <= s.count("curval"); i++ {
				name := fmt.Sprintf("curval%d", i)
				rec.Fields()[cols[name].ID].SetValue(datum[name], gads.SetValueOptions{})
			}
		}
		if f, ok := datum["file1"]; ok {
			if f == nil {
				f = map[string]any{"name": "myfile.txt", "mimetype": "text/plain", "content": "My text file"}
			}
			rec.Fields()[cols["file1"].ID].SetValue(f, gads.SetValueOptions{})
		}
		rec.Write(gads.WriteOptions{NoAlerts: true})
	}
	if s.NoGroups {
		s.e.DeleteGroup(s.Group())
		s.groupBuilt = false
		s.group = 0
	}
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

func toInt64(v any) int64 {
	i, _ := db.AsInt64(v)
	return i
}

// SetMultivalue sets multivalue on all applicable columns
// (DataSheet::set_multivalue).
func (s *DataSheet) SetMultivalue(v bool) {
	for _, c := range s.Layout().All(gads.AllOptions{}) {
		if s.MultivalueColumns[c.Type] {
			c.Multivalue = v
			c.Write(gads.ColumnWriteOptions{Force: true})
		}
	}
	s.Layout().Clear()
}
