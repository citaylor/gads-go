package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

func init() {
	Commands["import.pl"] = Import
}

// pv is a value as Log::Report writes it in a message: undef for an
// undefined value
func pv(v any) string {
	switch t := v.(type) {
	case nil:
		return "undef"
	case *int64:
		if t == nil {
			return "undef"
		}
		return strconv.FormatInt(*t, 10)
	case time.Time:
		return t.Format("2006-01-02T15:04:05")
	case *time.Time:
		if t == nil {
			return "undef"
		}
		return t.Format("2006-01-02T15:04:05")
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return ""
	}
	return fmt.Sprint(v)
}

// ps is a value as a Perl string ("" for undef)
func ps(v any) string {
	if v == nil {
		return ""
	}
	if p, ok := v.(*int64); ok && p == nil {
		return ""
	}
	return pv(v)
}

// pn is a value as a Perl number (0 for undef or a string that is not one)
func pn(v any) float64 {
	s := strings.TrimSpace(ps(v))
	m := regexp.MustCompile(`^[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?`).FindString(s)
	f, _ := strconv.ParseFloat(m, 64)
	return f
}

// jint is a JSON value as an ID (nil for none)
func jint(v any) *int64 {
	switch t := v.(type) {
	case nil:
		return nil
	case float64:
		n := int64(t)
		return &n
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			f, _ := t.Float64()
			n = int64(f)
		}
		return &n
	case string:
		if t == "" {
			return nil
		}
		n, _ := strconv.ParseInt(t, 10, 64)
		return &n
	case int64:
		return &t
	}
	return nil
}

func jtrue(v any) bool { return !perlFalse(ps(v)) && v != nil }

func jstr(v any) string { return ps(v) }

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func idOrNil(p *int64) any {
	if p == nil || *p == 0 {
		return nil
	}
	return *p
}

// importer is the state of bin/import.pl
type importer struct {
	env                       *Env
	e                         *gads.Engine
	d                         *db.DB
	reportOnly, merge, add    bool
	force, createUsers, purge bool
	ignoreFields              map[string]bool
	groupMapping              map[string]int64
	hasGroupMapping           bool
	userMapping               map[string]int64
	hasUserMapping            bool
	columnMapping             map[string]int64
	metricsMapping            map[string]int64
	recordMapping             map[string]int64
	enumMapping               map[int64]int64
	valuesToImport            map[int64][]map[string]any
}

func (im *importer) notice(format string, a ...any) {
	im.env.SayErr("notice: "+format, a...)
}

// warnUninitialized is the warning of Perl of an undefined value
func (im *importer) warnUninitialized(op string) {
	fmt.Fprintf(im.env.Stderr, "Use of uninitialized value in %s at bin/import.pl line 1.\n", op)
}

// dir is the files of a directory, sorted, as JSON
func (im *importer) dir(name string) []map[string]any {
	f, err := os.Open(im.env.Path(name))
	if err != nil {
		gads.Fail("Cannot open directory %s: %s", name, perlErr(err))
	}
	names, _ := f.Readdirnames(-1)
	f.Close()
	sort.Strings(names)
	var out []map[string]any
	for _, n := range names {
		out = append(out, im.loadJSON(name+"/"+n))
	}
	return out
}

func (im *importer) loadJSON(file string) map[string]any {
	b, err := os.ReadFile(im.env.Path(file))
	if err != nil {
		Die("Failed to read file %s: %s", file, perlErr(err))
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		Die("malformed JSON string: %s", err)
	}
	h, _ := v.(map[string]any)
	return normaliseJSON(h).(map[string]any)
}

// normaliseJSON turns the numbers of decoded JSON into int64 (or float64)
func normaliseJSON(v any) any {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		f, _ := t.Float64()
		return f
	case map[string]any:
		for k, x := range t {
			t[k] = normaliseJSON(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = normaliseJSON(x)
		}
		return t
	}
	return v
}

func isDir(env *Env, p string) bool {
	st, err := os.Stat(env.Path(p))
	return err == nil && st.IsDir()
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

func hashOf(v any) map[string]any {
	h, _ := v.(map[string]any)
	return h
}

// Import is bin/import.pl: imports the configuration (and data) of a site
// from the directory _export, as written by bin/export.pl
func Import(env *Env) {
	var siteID string
	var ignoreFields []string
	im := &importer{env: env, e: env.E, d: env.E.DB}
	var updateCached bool
	if !env.GetOptions(
		Option{Name: "site-id", Value: true, String: &siteID},
		Option{Name: "purge", Bool: &im.purge},
		Option{Name: "add", Bool: &im.add},
		Option{Name: "report-only", Bool: &im.reportOnly},
		Option{Name: "merge", Bool: &im.merge},
		Option{Name: "update-cached", Bool: &updateCached},
		Option{Name: "force", Bool: &im.force},
		Option{Name: "create-users", Bool: &im.createUsers},
		Option{Name: "ignore-field", Value: true, Multi: true, Ints: &ignoreFields},
	) {
		env.Exit(0)
	}
	if perlFalse(siteID) {
		gads.Fail("Please provide site ID with --site-id")
	}
	if !isDir(env, "_export") {
		gads.Fail("Export directory does not exist")
	}
	e := im.e
	old := e.IgnorePermissions
	e.IgnorePermissions = true
	defer func() { e.IgnorePermissions = old }()
	e.SiteID, _ = strconv.ParseInt(siteID, 10, 64)
	im.ignoreFields = map[string]bool{}
	for _, f := range ignoreFields {
		im.ignoreFields[f] = true
	}
	im.columnMapping = map[string]int64{}
	im.metricsMapping = map[string]int64{}
	im.recordMapping = map[string]int64{}
	im.enumMapping = map[int64]int64{}
	im.valuesToImport = map[int64][]map[string]any{}
	e.EnumMapping = im.enumMapping
	defer func() { e.EnumMapping = nil }()
	im.d.Txn(func() {
		im.run(updateCached)
	})
}

type importLayout struct {
	values map[string]any
	layout *gads.Layout
	graphs string
	views  string
}

type importColumn struct {
	column  *gads.Column
	values  map[string]any
	updated bool
}

func (im *importer) siteGroups() []db.Row {
	return im.d.Query(`SELECT * FROM "group" "me" WHERE 1 = 1` + siteAnd(im.e, "me") + ` ORDER BY "me"."name"`)
}

func (im *importer) run(updateCached bool) {
	env, e, d := im.env, im.e, im.d
	if im.purge && !im.reportOnly {
		for _, l := range overrideLayouts(e) {
			l.Purge()
		}
		for _, g := range im.siteGroups() {
			deleteGroup(d, g["id"])
		}
	}
	if len(im.siteGroups()) > 0 && !im.add && !im.reportOnly && !im.merge {
		gads.Fail("Groups already exists. Use --purge to remove everything from this site before import or --add to add config")
	}
	if !isDir(env, "_export/groups") {
		gads.Fail("Groups directory does not exist")
	}
	im.groupMapping = map[string]int64{}
	im.hasGroupMapping = false
	groups := im.siteGroups()
	existingGroups := map[int64]db.Row{}
	var existingOrder []int64
	for _, g := range groups {
		id, _ := db.AsInt64(g["id"])
		existingGroups[id] = g
		existingOrder = append(existingOrder, id)
	}
	for _, g := range im.dir("_export/groups") {
		var groupID int64
		for _, eg := range groups {
			if str(eg["name"]) == jstr(g["name"]) {
				groupID, _ = db.AsInt64(eg["id"])
				break
			}
		}
		if groupID == 0 {
			im.notice("Creation: New group %s to be created", pv(g["name"]))
			groupID = d.InsertMap("group", map[string]any{"name": nil, "site_id": e.SiteID})
			d.UpdateMap("group", map[string]any{
				"name":                               g["name"],
				"default_read":                       0,
				"default_write_new":                  0,
				"default_write_existing":             0,
				"default_approve_new":                0,
				"default_approve_existing":           0,
				"default_write_new_no_approval":      0,
				"default_write_existing_no_approval": 0,
			}, map[string]any{"id": groupID})
		}
		im.groupMapping[jstr(g["id"])] = groupID
		im.hasGroupMapping = true
		delete(existingGroups, groupID)
	}
	if !(im.merge || im.add) {
		// GADS goes through these in the order of a hash
		for _, id := range existingOrder {
			g, ok := existingGroups[id]
			if !ok {
				continue
			}
			if im.reportOnly {
				im.notice("Deletion: Group %s to be deleted", pv(g["name"]))
			} else {
				im.notice("Deletion: Group %s being deleted", pv(g["name"]))
				deleteGroup(d, id)
			}
		}
	}
	im.userMapping = map[string]int64{}
	if isDir(env, "_export/users") {
		im.hasUserMapping = true
		for _, user := range im.dir("_export/users") {
			var groupIDs []int64
			for _, g := range listOf(user["groups"]) {
				groupIDs = append(groupIDs, im.groupMapping[jstr(g)])
			}
			var uid int64
			if im.createUsers {
				uid = im.importUser(user, groupIDs)
			} else {
				id, ok := d.QueryInt64(`SELECT "me"."id" FROM "user" "me" WHERE "account_request" = 0 AND "me"."deleted" IS NULL AND "username" = ?`+siteAnd(e, "me")+` ORDER BY "me"."id" LIMIT 1`, user["username"])
				if !ok {
					gads.Fail("User %s not found", jstr(user["username"]))
				}
				uid = id
			}
			im.userMapping[jstr(user["id"])] = uid
		}
	}
	root, err := os.Open(env.Path("_export"))
	if err != nil {
		gads.Fail("Cannot open directory _export: %s", perlErr(err))
	}
	// The instances in the order of the directory (not sorted)
	entries, _ := root.Readdirnames(-1)
	root.Close()
	var allColumns []importColumn
	var allLayouts []importLayout
	for _, ins := range entries {
		if !strings.HasPrefix(ins, "instance") {
			continue
		}
		info := im.loadJSON("_export/" + ins + "/instance")
		existing := d.Query(`SELECT * FROM "instance" "me" WHERE "name" = ?`+siteAnd(e, "me")+` ORDER BY "me"."id"`, info["name"])
		var instanceID int64
		if len(existing) > 0 {
			if len(existing) > 1 {
				gads.Fail("More than one existing instance name %s", jstr(info["name"]))
			}
			instanceID, _ = db.AsInt64(existing[0]["id"])
			if im.reportOnly && !im.merge {
				im.notice("Existing: Instance %s already exists", pv(info["name"]))
			}
			if !im.merge {
				gads.Fail("Instance %s already exists", jstr(info["name"]))
			}
		} else {
			if im.merge && !im.add {
				gads.Fail("Instance name %s does not exist. Specify --add to create it.", jstr(info["name"]))
			}
			im.notice("Creation: Instance name %s created", pv(info["name"]))
			instanceID = d.InsertMap("instance", map[string]any{"name": info["name"], "site_id": e.SiteID})
		}
		layout := e.NewLayout(instanceID, nil)
		layout.CreateInternalColumns()
		for _, p := range listOf(info["permissions"]) {
			ph := hashOf(p)
			if ph == nil {
				continue
			}
			ph["group_id"] = mappedOrNil(im.groupMapping, ph["group_id"])
		}
		set := im.layoutImportHash(layout, info)
		if !im.reportOnly {
			layout.Write(set)
		}
		topicMapping := map[string]int64{}
		existingTopics := map[int64]db.Row{}
		var topicOrder []int64
		for _, t := range d.Query(`SELECT * FROM "topic" WHERE "instance_id" = ? ORDER BY "id"`, instanceID) {
			id, _ := db.AsInt64(t["id"])
			existingTopics[id] = t
			topicOrder = append(topicOrder, id)
		}
		if isDir(env, "_export/"+ins+"/topics") {
			for _, topic := range im.dir("_export/" + ins + "/topics") {
				topicHash := map[string]any{
					"name":                  topic["name"],
					"description":           topic["description"],
					"initial_state":         topic["initial_state"],
					"click_to_edit":         topic["click_to_edit"],
					"prevent_edit_topic_id": topic["prevent_edit_topic_id"],
					"instance_id":           instanceID,
				}
				var topID int64
				if im.reportOnly || im.merge {
					tops := d.Query(`SELECT * FROM "topic" WHERE "instance_id" = ? AND "name" = ? ORDER BY "id"`, instanceID, topic["name"])
					if len(tops) > 1 {
						gads.Fail("More than one topic named %s already exists", jstr(topic["name"]))
					}
					if len(tops) == 0 && im.reportOnly {
						im.notice("Creation: Topic %s to be created", pv(topic["name"]))
					}
					if len(tops) > 0 {
						topID, _ = db.AsInt64(tops[0]["id"])
						upd := im.topicImportHash(tops[0], topicHash)
						if !im.reportOnly {
							d.UpdateMap("topic", upd, map[string]any{"id": topID})
						}
					}
				}
				if topID == 0 {
					topID = d.InsertMap("topic", topicHash)
				}
				topicMapping[jstr(topic["id"])] = topID
				delete(existingTopics, topID)
			}
			for _, id := range topicOrder {
				t, ok := existingTopics[id]
				if !ok {
					continue
				}
				if im.reportOnly {
					im.notice("Deletion: Topic %s to be deleted", pv(t["name"]))
				} else {
					im.notice("Deletion: Topic %s being deleted", pv(t["name"]))
					d.Exec(`DELETE FROM "topic" WHERE "id" = ?`, id)
				}
			}
		}
		existingColumns := map[int64]*gads.Column{}
		var columnOrder []int64
		for _, c := range layout.All(gads.AllOptions{}) {
			existingColumns[c.ID] = c
			columnOrder = append(columnOrder, c.ID)
		}
		var highestUpdate int64
		var created []map[string]any
		for _, col := range im.dir("_export/" + ins + "/layout") {
			updated := false
			var column *gads.Column
			if jtrue(col["internal"]) {
				column = layout.ColumnByNameShort(jstr(col["name_short"]))
			} else {
				column = layout.ColumnByName(jstr(col["name"]))
			}
			colID := derefID(jint(col["id"]))
			if jtrue(col["internal"]) {
				if column == nil {
					Die(`Can't call method "id" on an undefined value`)
				}
				im.columnMapping[jstr(col["id"])] = column.ID
				continue
			}
			if column != nil && im.ignoreFields[column.Name] {
				im.columnMapping[jstr(col["id"])] = column.ID
				continue
			}
			if column != nil {
				if !im.merge {
					gads.Fail("Column %s already exists", jstr(col["name"]))
				}
				if jstr(col["type"]) != column.Type {
					gads.Fail("Existing column type does not match import for column name %s in table %s", jstr(col["name"]), layout.Name())
				}
				if highestUpdate == 0 || colID > highestUpdate {
					highestUpdate = colID
				}
				updated = true
			}
			if column == nil {
				im.notice("Creation: Column %s to be created", pv(col["name"]))
				created = append(created, col)
				typ := jstr(col["type"])
				if !columnClasses[typ] {
					class := columnClass(typ)
					Die(`Can't locate object method "new" via package "%s" (perhaps you forgot to load "%s"?)`, class, class)
				}
				column = layout.NewColumn(typ)
				column.Optional = false
			}
			im.columnImportHash(column, col)
			if t := jint(col["topic_id"]); t != nil && *t != 0 {
				if n, ok := topicMapping[jstr(col["topic_id"])]; ok {
					column.TopicID = &n
				} else {
					column.TopicID = nil
				}
			}
			perms := map[int64][]string{}
			for oldID, p := range hashOf(col["permissions"]) {
				newID := im.groupMapping[oldID]
				for _, x := range listOf(p) {
					perms[newID] = append(perms[newID], jstr(x))
				}
			}
			column.SetPermissions(perms)
			f := false
			column.Write(gads.ColumnWriteOptions{Override: true, NoDBAdd: true, NoCacheUpdate: boolPtr(true), UpdateDependents: &f, AllowInternalWrite: true})
			im.columnImportAfterWrite(column, col, updated && im.reportOnly)
			im.columnMapping[jstr(col["id"])] = column.ID
			allColumns = append(allColumns, importColumn{column: column, values: col, updated: updated})
			delete(existingColumns, column.ID)
		}
		for _, c := range created {
			if highestUpdate != 0 && derefID(jint(c["id"])) < highestUpdate {
				im.notice("Suspected name updated: column %s was created but its ID is less than those already existing. Could it be an updated name?", pv(c["name"]))
			}
		}
		if im.merge {
			for _, id := range columnOrder {
				c, ok := existingColumns[id]
				if !ok || im.ignoreFields[c.Name] {
					continue
				}
				im.notice("Deletion: Column %s no longer exist", c.Name)
				if !im.reportOnly {
					c.Delete()
				}
			}
		}
		if isDir(env, "_export/"+ins+"/metrics") {
			for _, mg := range im.dir("_export/" + ins + "/metrics") {
				var mgID int64
				if id, ok := d.QueryInt64(`SELECT "id" FROM "metric_group" WHERE "name" = ? ORDER BY "id" LIMIT 1`, mg["name"]); ok {
					mgID = id
				} else {
					mgID = d.InsertMap("metric_group", map[string]any{"name": mg["name"], "instance_id": instanceID})
				}
				im.metricGroupImportHash(mgID, instanceID, mg)
				im.metricsMapping[jstr(mg["id"])] = mgID
			}
		}
		layout.Clear()
		allLayouts = append(allLayouts, importLayout{values: info, layout: layout, graphs: "_export/" + ins + "/graphs", views: "_export/" + ins + "/views"})
		if isDir(env, "_export/"+ins+"/records") {
			for _, record := range im.dir("_export/" + ins + "/records") {
				cid := im.importCurrent(record, instanceID)
				im.recordMapping[jstr(record["id"])] = cid
			}
		}
	}
	viewMapping := map[string]int64{}
	for _, l := range allLayouts {
		layout := l.layout
		set := im.layoutImportAfterAll(layout, l.values)
		layout.Write(set)
		layout.Clear()
		gq := `SELECT "id", "title" FROM "graph" WHERE "instance_id" = ?`
		if !im.hasUserMapping {
			gq += ` AND "user_id" IS NULL`
		}
		if !im.hasGroupMapping {
			gq += ` AND "group_id" IS NULL`
		}
		existingGraphs := map[int64]db.Row{}
		var graphOrder []int64
		for _, g := range d.Query(gq+` ORDER BY "id"`, layout.InstanceID) {
			id, _ := db.AsInt64(g["id"])
			existingGraphs[id] = g
			graphOrder = append(graphOrder, id)
		}
		if isDir(env, l.graphs) {
			for _, g := range im.dir(l.graphs) {
				if jtrue(g["x_axis"]) {
					g["x_axis"] = mappedOrNil(im.columnMapping, g["x_axis"])
				}
				if jtrue(g["x_axis_link"]) {
					g["x_axis_link"] = mappedOrNil(im.columnMapping, g["x_axis_link"])
				}
				if g["y_axis"] == nil {
					im.warnUninitialized("hash element")
				}
				g["y_axis"] = mappedOrNil(im.columnMapping, g["y_axis"])
				if jtrue(g["group_by"]) {
					g["group_by"] = mappedOrNil(im.columnMapping, g["group_by"])
				}
				if jtrue(g["metric_group_id"]) {
					g["metric_group_id"] = mappedOrNil(im.metricsMapping, g["metric_group_id"])
				}
				if im.hasUserMapping && jtrue(g["user_id"]) {
					g["user_id"] = mappedOrNil(im.userMapping, g["user_id"])
				}
				if im.hasGroupMapping && jtrue(g["group_id"]) {
					g["group_id"] = mappedOrNil(im.groupMapping, g["group_id"])
				}
				var graphRow db.Row
				found := false
				if im.merge || im.reportOnly {
					rows := d.Query(`SELECT * FROM "graph" WHERE "instance_id" = ? AND "title" = ? ORDER BY "id"`, layout.InstanceID, g["title"])
					if len(rows) > 1 {
						gads.Fail("More than one existing graph titled {title}")
					}
					if len(rows) > 0 {
						graphRow = rows[0]
						found = true
					} else {
						im.notice("Graph to be created: %s", pv(g["title"]))
					}
				}
				id := im.graphImport(layout, graphRow, found, g)
				if id == 0 {
					im.warnUninitialized("delete")
				} else {
					delete(existingGraphs, id)
				}
			}
		}
		if isDir(env, l.views) {
			for _, v := range im.dir(l.views) {
				var cols []int64
				for _, c := range listOf(v["columns"]) {
					cols = append(cols, im.columnMapping[jstr(c)])
				}
				var sorts, groups []map[string]any
				for _, s := range listOf(v["sorts"]) {
					sh := hashOf(s)
					var parent any = sh["parent_id"]
					if jtrue(parent) {
						parent = mappedOrNil(im.columnMapping, parent)
					}
					sorts = append(sorts, map[string]any{"layout_id": mappedOrNil(im.columnMapping, sh["layout_id"]), "parent_id": parent, "type": sh["type"], "order": sh["order"]})
				}
				for _, s := range listOf(v["groups"]) {
					sh := hashOf(s)
					var parent any = sh["parent_id"]
					if jtrue(parent) {
						parent = mappedOrNil(im.columnMapping, parent)
					}
					groups = append(groups, map[string]any{"layout_id": mappedOrNil(im.columnMapping, sh["layout_id"]), "parent_id": parent, "order": sh["order"]})
				}
				if jtrue(v["group_id"]) {
					v["group_id"] = mappedOrNil(im.groupMapping, v["group_id"])
				}
				filter := gads.NewFilterHash(hashOf(v["filter"]), nil)
				skip := false
				for _, cond := range filter.Filters() {
					var id string
					if m := regexp.MustCompile(`^([0-9]+)_([0-9]+)$`).FindStringSubmatch(jstr(cond["id"])); m != nil {
						parent, pok := im.columnMapping[m[1]]
						if !pok || parent == 0 {
							im.env.SayErr("warning: Field ID %s does not seem to exist in source view filter", m[1])
						}
						child, cok := im.columnMapping[m[2]]
						if !cok || child == 0 {
							im.env.SayErr("warning: Field ID %s does not seem to exist in source view filter", m[2])
						}
						if parent != 0 && child != 0 {
							id = fmt.Sprintf("%d_%d", parent, child)
						}
					} else if n, ok := im.columnMapping[jstr(cond["id"])]; ok && n != 0 {
						id = strconv.FormatInt(n, 10)
					}
					if id == "" {
						skip = true
						cond["id"], cond["field"] = nil, nil
					} else if n, err := strconv.ParseInt(id, 10, 64); err == nil {
						cond["id"], cond["field"] = n, n
					} else {
						cond["id"], cond["field"] = id, id
					}
				}
				var viewRow db.Row
				if im.merge || im.reportOnly {
					rows := d.Query(`SELECT * FROM "view" WHERE "instance_id" = ? AND "name" = ? ORDER BY "id"`, layout.InstanceID, v["name"])
					if len(rows) > 1 {
						gads.Fail("More than one existing view called %s", jstr(v["name"]))
					}
					if len(rows) > 0 {
						viewRow = rows[0]
					} else {
						im.notice("View to be created: %s", pv(v["name"]))
					}
				}
				var vfilter *gads.Filter
				if !skip {
					vfilter = filter
				}
				id := im.viewImport(layout, viewRow, v, vfilter, cols, sorts, groups)
				if id == 0 {
					im.warnUninitialized("delete")
				}
				viewMapping[jstr(v["id"])] = id
			}
		}
		if vl := l.values["view_limit_id"]; jtrue(vl) {
			set := layoutRowSet(d, layout)
			set["view_limit_id"] = idOrNil(int64Ptr(viewMapping[jstr(vl)]))
			layout.Write(set)
		}
		if vl := l.values["default_view_limit_extra_id"]; jtrue(vl) {
			set := layoutRowSet(d, layout)
			set["default_view_limit_extra_id"] = idOrNil(int64Ptr(viewMapping[jstr(vl)]))
			layout.Write(set)
		}
		for _, id := range graphOrder {
			g, ok := existingGraphs[id]
			if !ok {
				continue
			}
			im.notice("Deletion: Graph %s to be deleted", pv(g["title"]))
			if !im.reportOnly {
				e.LoadGraph(id, layout, nil).Delete()
			}
		}
	}
	layoutsIndex := map[int64]*gads.Layout{}
	for _, l := range allLayouts {
		layoutsIndex[l.layout.InstanceID] = l.layout
	}
	// The fields of filtered values are written last (GADS sorts with a
	// comparison that is not consistent, which leaves the order of the
	// others to its implementation of sort)
	sort.SliceStable(allColumns, func(i, j int) bool {
		return allColumns[i].column.Type != "filval" && allColumns[j].column.Type == "filval"
	})
	for _, ac := range allColumns {
		c := ac.column
		col := layoutsIndex[c.InstanceID].Column(c.ID)
		if col == nil || im.ignoreFields[col.Name] {
			continue
		}
		for _, val := range im.valuesToImport[col.ID] {
			switch col.Type {
			case "curval":
				if jtrue(val["value"]) {
					val["value"] = mappedOrNil(im.recordMapping, val["value"])
				}
			case "enum", "tree":
				if jtrue(val["value"]) {
					if n, ok := im.enumMapping[derefID(jint(val["value"]))]; ok {
						val["value"] = n
					} else {
						val["value"] = nil
					}
				}
			case "person":
				if jtrue(val["value"]) {
					val["value"] = mappedOrNil(im.userMapping, val["value"])
				}
			}
			im.importValue(col, val)
		}
		im.columnImportAfterAll(col, ac.values, im.reportOnly && ac.updated)
		t := true
		col.Write(gads.ColumnWriteOptions{AllowInternalWrite: true, NoCacheUpdate: &t, UpdateDependents: &t, ReportOnly: im.reportOnly})
	}
	if !im.reportOnly && updateCached {
		for _, ac := range allColumns {
			if ac.column.IsCode() {
				ac.column.UpdateCached(true)
			}
		}
	}
	if im.reportOnly {
		env.Exit(0)
	}
}

func boolPtr(b bool) *bool { return &b }

func int64Ptr(n int64) *int64 { return &n }

func mappedOrNil(m map[string]int64, v any) any {
	if n, ok := m[jstr(v)]; ok {
		return n
	}
	return nil
}

func deleteGroup(d *db.DB, id any) {
	d.Exec(`DELETE FROM "layout_group" WHERE "group_id" = ?`, id)
	d.Exec(`DELETE FROM "instance_group" WHERE "group_id" = ?`, id)
	d.Exec(`DELETE FROM "user_group" WHERE "group_id" = ?`, id)
	d.Exec(`DELETE FROM "group" WHERE "id" = ?`, id)
}

// layoutRowSet is the values that Layout->write writes, from the row of
// the instance
func layoutRowSet(d *db.DB, layout *gads.Layout) map[string]any {
	row := d.QueryRow(`SELECT * FROM "instance" WHERE "id" = ?`, layout.InstanceID)
	set := map[string]any{}
	for _, k := range []string{"name", "name_short", "hide_in_selector", "homepage_text", "homepage_text2", "sort_type", "sort_layout_id", "view_limit_id", "default_view_limit_extra_id"} {
		set[k] = row[k]
	}
	return set
}

// layoutImportHash is Layout->import_hash: the values to write
func (im *importer) layoutImportHash(layout *gads.Layout, values map[string]any) map[string]any {
	set := layoutRowSet(im.d, layout)
	report := im.reportOnly
	name := set["name"]
	if report && ps(name) != ps(values["name"]) {
		im.notice("Update: name from %s to %s for layout %s", pv(name), pv(values["name"]), pv(name))
	}
	set["name"] = values["name"]
	if report && ps(set["name_short"]) != ps(values["name_short"]) {
		im.notice("Update: name_short from %s to %s for layout %s", pv(set["name_short"]), pv(values["name_short"]), pv(set["name"]))
	}
	set["name_short"] = values["name_short"]
	if report && pn(set["hide_in_selector"]) != pn(values["hide_in_selector"]) {
		im.notice("Update: hide_in_selector from %s to %s for layout %s", pv(set["hide_in_selector"]), pv(values["hide_in_selector"]), pv(set["name"]))
	}
	set["hide_in_selector"] = values["hide_in_selector"]
	if report && ps(set["homepage_text"]) != ps(values["homepage_text"]) {
		im.notice("Update homepage_text for layout %s", pv(set["name"]))
	}
	set["homepage_text"] = values["homepage_text"]
	if report && ps(set["homepage_text2"]) != ps(values["homepage_text2"]) {
		im.notice("Update homepage_text2 for layout %s", pv(set["name"]))
	}
	set["homepage_text2"] = values["homepage_text2"]
	if report && ps(set["sort_type"]) != ps(values["sort_type"]) {
		im.notice("Update: sort_type from %s to %s for layout %s", pv(set["sort_type"]), pv(values["sort_type"]), pv(set["name"]))
	}
	set["sort_type"] = values["sort_type"]
	if report {
		existing := map[string]bool{}
		var old []db.Row
		for _, r := range im.d.Query(`SELECT "group_id", "permission" FROM "instance_group" WHERE "instance_id" = ? ORDER BY "id"`, layout.InstanceID) {
			existing[str(r["group_id"])+"_"+str(r["permission"])] = true
			old = append(old, r)
		}
		newHash := map[string]bool{}
		for _, p := range listOf(values["permissions"]) {
			ph := hashOf(p)
			key := ps(ph["group_id"]) + "_" + ps(ph["permission"])
			newHash[key] = true
			if !existing[key] {
				im.notice("Adding permission %s for group ID %s", pv(ph["permission"]), pv(ph["group_id"]))
			}
		}
		for _, r := range old {
			if !newHash[str(r["group_id"])+"_"+str(r["permission"])] {
				im.notice("Removing permission %s from group ID %s", pv(r["permission"]), pv(r["group_id"]))
			}
		}
	}
	var groups []string
	for _, p := range listOf(values["permissions"]) {
		ph := hashOf(p)
		groups = append(groups, ps(ph["group_id"])+"_"+ps(ph["permission"]))
	}
	layout.SetGroups(groups)
	return set
}

// layoutImportAfterAll is Layout->import_after_all: the values to write
func (im *importer) layoutImportAfterAll(layout *gads.Layout, values map[string]any) map[string]any {
	set := layoutRowSet(im.d, layout)
	if sl, ok := values["sort_layout_id"]; ok {
		var newID any
		if jtrue(sl) {
			newID = mappedOrNil(im.columnMapping, sl)
		}
		if im.reportOnly && pn(set["sort_layout_id"]) != pn(newID) {
			im.notice("Update: sort_layout_id from %s to %s for %s", pv(set["sort_layout_id"]), pv(newID), pv(set["name"]))
		}
		set["sort_layout_id"] = newID
	}
	return set
}

// topicImportHash is the import_hash of a topic: the values to write
func (im *importer) topicImportHash(row db.Row, values map[string]any) map[string]any {
	report := im.reportOnly
	name := row["name"]
	if report && ps(name) != ps(values["name"]) {
		im.notice("Update: name from %s to %s for topic %s", pv(name), pv(values["name"]), pv(name))
	}
	name = values["name"]
	if report && ps(row["description"]) != ps(values["description"]) {
		im.notice("Update: description from %s to %s for topic %s", pv(row["description"]), pv(values["description"]), pv(name))
	}
	if report && ps(row["initial_state"]) != ps(values["initial_state"]) {
		im.notice("Update: initial_state from %s to %s for topic %s", pv(row["initial_state"]), pv(values["initial_state"]), pv(name))
	}
	if report && pn(row["click_to_edit"]) != pn(values["click_to_edit"]) {
		im.notice("Update: click_to_edit from %s to %s for topic %s", pv(row["click_to_edit"]), pv(values["click_to_edit"]), pv(name))
	}
	if report && (jtrue(row["prevent_edit_topic_id"]) != jtrue(values["prevent_edit_topic_id"])) && pn(row["prevent_edit_topic_id"]) != pn(values["prevent_edit_topic_id"]) {
		im.notice("Update: prevent_edit_topic_id from %s to %s for topic %s", pv(row["prevent_edit_topic_id"]), pv(values["prevent_edit_topic_id"]), pv(name))
	}
	return map[string]any{
		"name":                  values["name"],
		"description":           values["description"],
		"initial_state":         values["initial_state"],
		"click_to_edit":         values["click_to_edit"],
		"prevent_edit_topic_id": values["prevent_edit_topic_id"],
	}
}

// columnImportHash is Column->import_hash, with that of the type before
func (im *importer) columnImportHash(c *gads.Column, values map[string]any) {
	report := im.reportOnly && c.ID != 0
	name := func() string { return c.Name }
	switch c.Type {
	case "file":
		if report && ((c.Filesize == nil) != (values["filesize"] == nil) || (c.Filesize != nil && values["filesize"] != nil && float64(*c.Filesize) != pn(values["filesize"]))) {
			im.notice("Update: filesize from %s to %s", pv(c.Filesize), pv(values["filesize"]))
		}
		c.Filesize = jint(values["filesize"])
	case "calc":
		if report && c.CodeFn() != jstr(values["code"]) {
			im.notice("Update: code has been changed for field %s", name())
		}
		c.SetCode(jstr(values["code"]))
		if report && c.ReturnTypeFn() != jstr(values["return_type"]) {
			im.notice("Update: return_type from %s to %s for field %s", c.ReturnTypeFn(), pv(values["return_type"]), name())
		}
		c.SetReturnType(jstr(values["return_type"]))
		if report && c.ReturnTypeFn() == "numeric" && ((c.DecimalPlaces == nil) != (values["decimal_places"] == nil) || (c.DecimalPlaces != nil && values["decimal_places"] != nil && float64(*c.DecimalPlaces) != pn(values["decimal_places"]))) {
			im.notice("Update: decimal_places from %s to %s for field %s", pv(c.DecimalPlaces), pv(values["decimal_places"]), name())
		}
		c.DecimalPlaces = jint(values["decimal_places"])
	case "string":
		if report && int01(c.Textbox) != pn(values["textbox"]) {
			im.notice("Update: textbox from %s to %s", pv(int01(c.Textbox)), pv(values["textbox"]))
		}
		c.Textbox = jtrue(values["textbox"])
		if report && c.ForceRegex != jstr(values["force_regex"]) {
			im.notice("Update: force_regex from %s to %s", pv(nilIfEmpty(c.ForceRegex)), pv(values["force_regex"]))
		}
		c.ForceRegex = jstr(values["force_regex"])
	case "rag":
		if report && c.CodeFn() != jstr(values["code"]) {
			im.notice("Update: RAG code has changed for %s", name())
		}
		c.SetCode(jstr(values["code"]))
	case "enum":
		im.enumImportHash(c, values, report)
	case "tree":
		if report && int01(c.EndNodeOnly) != pn(values["end_node_only"]) {
			im.notice("Update: end_node_only from %s to %s", pv(int01(c.EndNodeOnly)), pv(values["end_node_only"]))
		}
		c.EndNodeOnly = jtrue(values["end_node_only"])
	}
	if report && c.Name != jstr(values["name"]) {
		im.notice("Update: name from %s to %s for %s", c.Name, pv(values["name"]), c.Name)
	}
	c.Name = jstr(values["name"])
	if report && c.NameShort != jstr(values["name_short"]) {
		im.notice("Update: name_short from %s to %s for %s", pv(nilIfEmpty(c.NameShort)), pv(values["name_short"]), c.Name)
	}
	c.NameShort = jstr(values["name_short"])
	if report && int01(c.Optional) != pn(values["optional"]) {
		im.notice("Update: optional from %s to %s for %s", pv(int01(c.Optional)), pv(values["optional"]), c.Name)
	}
	c.Optional = jtrue(values["optional"])
	if report && int01(c.Remember) != pn(values["remember"]) {
		im.notice("Update: remember from %s to %s for %s", pv(int01(c.Remember)), pv(values["remember"]), c.Name)
	}
	c.Remember = jtrue(values["remember"])
	if report && int01(c.IsUnique) != pn(values["isunique"]) {
		im.notice("Update: isunique from %s to %s for %s", pv(int01(c.IsUnique)), pv(values["isunique"]), c.Name)
	}
	c.IsUnique = jtrue(values["isunique"])
	if report && int01(c.CanChild()) != pn(values["can_child"]) {
		im.notice("Update: can_child from %s to %s for %s", pv(int01(c.CanChild())), pv(values["can_child"]), c.Name)
	}
	c.SetCanChild(jtrue(values["can_child"]))
	if report && float64(derefID(c.Position)) != pn(values["position"]) {
		im.notice("Update: position from %s to %s for %s", pv(c.Position), pv(values["position"]), c.Name)
	}
	c.Position = jint(values["position"])
	if report && c.Description != jstr(values["description"]) {
		im.notice("Update: description from %s to %s for %s", pv(nilIfEmpty(c.Description)), pv(values["description"]), c.Name)
	}
	c.Description = jstr(values["description"])
	if report && c.Aggregate != jstr(values["aggregate"]) {
		im.notice("Update: aggregate from %s to %s for %s", pv(nilIfEmpty(c.Aggregate)), pv(values["aggregate"]), c.Name)
	}
	c.Aggregate = jstr(values["aggregate"])
	if report && c.GroupDisplay() != jstr(values["group_display"]) {
		im.notice("Update: group_display from %s to %s for %s", pv(nilIfEmpty(c.GroupDisplay())), pv(values["group_display"]), c.Name)
	}
	c.SetGroupDisplay(jstr(values["group_display"]))
	if report && float64(c.Width) != pn(values["width"]) {
		im.notice("Update: width from %d to %s for %s", c.Width, pv(values["width"]), c.Name)
	}
	c.Width = derefID(jint(values["width"]))
	if report && c.Helptext != jstr(values["helptext"]) {
		im.notice("Update: helptext from %d chars to %d chars for %s", len([]rune(c.Helptext)), len([]rune(jstr(values["helptext"]))), c.Name)
	}
	c.Helptext = jstr(values["helptext"])
	if report && int01(c.Multivalue) != pn(values["multivalue"]) {
		im.notice("Update: multivalue from %s to %s for %s", pv(int01(c.Multivalue)), pv(values["multivalue"]), c.Name)
	}
	c.Multivalue = jtrue(values["multivalue"])
	if report && c.LookupEndpoint != jstr(values["lookup_endpoint"]) {
		im.notice("Update: lookup_endpoint from %s to %s for %s", pv(nilIfEmpty(c.LookupEndpoint)), pv(values["lookup_endpoint"]), pv(nilIfEmpty(c.LookupEndpoint)))
	}
	c.LookupEndpoint = jstr(values["lookup_endpoint"])
	if report && c.LookupGroup != jstr(values["lookup_group"]) {
		im.notice("Update: lookup_group from %s to %s for %s", pv(nilIfEmpty(c.LookupGroup)), pv(values["lookup_group"]), pv(nilIfEmpty(c.LookupGroup)))
	}
	// As GADS, the lookup group is set as the lookup endpoint
	c.LookupEndpoint = jstr(values["lookup_group"])
	oldFilter := c.FilterFn().AsJSON()
	f := gads.NewFilterJSON(jstr(values["filter"]), nil)
	c.SetFilter(f)
	if report && !reflect.DeepEqual(decodeJSONValue(oldFilter), decodeJSONValue(f.AsJSON())) {
		im.notice("Update: filter from %s to %s for %s", f.AsJSON(), pv(values["filter"]), c.Name)
	}
	for _, o := range c.OptionNames() {
		old := c.OptionValue(o)
		if report && ps(old) != ps(values[o]) {
			im.notice("Update: %s from %s to %s for %s", o, pv(old), pv(values[o]), c.Name)
		}
		c.SetOption(o, values[o])
	}
}

func decodeJSONValue(s string) any {
	var v any
	json.Unmarshal([]byte(s), &v)
	return v
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func int01(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// enumImportHash is the before import_hash of GADS::Column::Enum
func (im *importer) enumImportHash(c *gads.Column, values map[string]any, report bool) {
	var newVals []map[string]any
	for _, v := range listOf(values["enumvals"]) {
		if h := hashOf(v); h != nil {
			newVals = append(newVals, h)
		}
	}
	byID := func(l []map[string]any) {
		sort.SliceStable(l, func(i, j int) bool { return pn(l[i]["id"]) < pn(l[j]["id"]) })
	}
	byID(newVals)
	var toWrite []map[string]any
	var old []map[string]any
	if c.ID != 0 {
		for _, v := range c.Enumvals() {
			m := map[string]any{}
			for k, x := range v {
				m[k] = x
			}
			old = append(old, m)
		}
	}
	if len(old) > 0 {
		byID(old)
		var oldSorted, newSorted []string
		for _, o := range old {
			oldSorted = append(oldSorted, jstr(o["value"]))
		}
		for _, n := range newVals {
			newSorted = append(newSorted, jstr(n["value"]))
		}
		sort.Strings(oldSorted)
		sort.Strings(newSorted)
		if strings.Join(oldSorted, " ") == strings.Join(newSorted, " ") {
			for _, o := range old {
				var n map[string]any
				if len(newVals) > 0 {
					n, newVals = newVals[0], newVals[1:]
				}
				o["source_id"] = n["id"]
				if pn(o["position"]) != pn(n["position"]) && report {
					im.notice(`Position changed for enum value "%s" for field "%s"`, jstr(o["value"]), c.Name)
				}
				o["position"] = n["position"]
				toWrite = append(toWrite, o)
			}
		} else {
			for len(old) > 0 {
				o := old[0]
				old = old[1:]
				if len(newVals) == 0 {
					continue
				}
				n := newVals[0]
				newVals = newVals[1:]
				if jstr(o["value"]) == jstr(n["value"]) {
					if pn(o["position"]) != pn(n["position"]) && report {
						im.notice(`Position changed for enum value "%s" for field "%s"`, jstr(o["value"]), c.Name)
					}
					n["source_id"] = n["id"]
					n["id"] = o["id"]
					toWrite = append(toWrite, n)
					continue
				}
				if len(old) > 0 && len(newVals) > 0 && jstr(old[0]["value"]) == jstr(newVals[0]["value"]) {
					if report {
						im.notice(`Changing enum value "%s" to "%s" for field "%s"`, jstr(o["value"]), jstr(n["value"]), c.Name)
					}
					if pn(o["position"]) != pn(n["position"]) && report {
						im.notice(`Position changed for enum value "%s" for field "%s"`, jstr(o["value"]), c.Name)
					}
					n["source_id"] = n["id"]
					n["id"] = o["id"]
					toWrite = append(toWrite, n)
				} else if im.force {
					im.notice("Unknown enumval update %s, forcing as requested", jstr(n["value"]))
					n["source_id"] = n["id"]
					delete(n, "id")
					toWrite = append(toWrite, n)
				} else if report {
					im.notice(`Error: don't know how to handle enumval updates for "%s" in field "%s", manual intervention required. Old value: %s, new value: %s`, c.Name, c.Name, jstr(o["value"]), jstr(n["value"]))
					return
				} else {
					gads.Fail("Error: don't know how to handle enumval updates for %s, manual intervention required", c.Name)
				}
			}
			for _, n := range newVals {
				n["source_id"] = n["id"]
				delete(n, "id")
			}
			toWrite = append(toWrite, newVals...)
		}
	} else {
		for _, n := range newVals {
			n["source_id"] = n["id"]
			delete(n, "id")
		}
		toWrite = newVals
	}
	c.SetEnumvals(toWrite)
	c.Ordering = jstr(values["ordering"])
}

// columnImportAfterWrite is import_after_write: the tree of a tree field
func (im *importer) columnImportAfterWrite(c *gads.Column, values map[string]any, report bool) {
	if c.Type != "tree" {
		return
	}
	var newTree []map[string]any
	for _, t := range listOf(values["tree"]) {
		if h := hashOf(t); h != nil {
			newTree = append(newTree, h)
		}
	}
	var toWrite []map[string]any
	oldTree := treeMaps(c.TreeJSON())
	if len(oldTree) > 0 {
		toWrite = im.importBranch(c, oldTree, newTree, report)
	} else {
		var toSource func(l []map[string]any)
		toSource = func(l []map[string]any) {
			for _, t := range l {
				t["source_id"] = t["id"]
				delete(t, "id")
				if kids := treeMaps(t["children"]); kids != nil {
					toSource(kids)
					t["children"] = kids
				}
			}
		}
		toSource(newTree)
		toWrite = newTree
	}
	c.TreeUpdate(toWrite)
}

func treeMaps(v any) []map[string]any {
	var out []map[string]any
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if h := hashOf(x); h != nil {
				out = append(out, h)
			}
		}
	case []map[string]any:
		out = t
	}
	return out
}

// importBranch is _import_branch of GADS::Column::Tree
func (im *importer) importBranch(c *gads.Column, oldIn, newIn []map[string]any, report bool) []map[string]any {
	byText := func(l []map[string]any) []map[string]any {
		out := append([]map[string]any{}, l...)
		sort.SliceStable(out, func(i, j int) bool { return jstr(out[i]["text"]) < jstr(out[j]["text"]) })
		return out
	}
	old := byText(oldIn)
	newL := byText(newIn)
	var toWrite []map[string]any
	for len(old) > 0 {
		o := old[0]
		old = old[1:]
		var n map[string]any
		if len(newL) > 0 {
			n, newL = newL[0], newL[1:]
		} else {
			n = map[string]any{}
		}
		if jtrue(o["text"]) && jtrue(n["text"]) && jstr(o["text"]) == jstr(n["text"]) {
			n["source_id"] = n["id"]
			n["id"] = o["id"]
			toWrite = append(toWrite, n)
		} else if len(old) > 0 && len(newL) > 0 && jstr(old[0]["text"]) == jstr(newL[0]["text"]) {
			if report {
				im.notice("Changing tree value %s to %s", pv(o["text"]), pv(n["text"]))
			}
			n["source_id"] = n["id"]
			n["id"] = o["id"]
			toWrite = append(toWrite, n)
		} else if len(newL) > 0 && jstr(o["text"]) == jstr(newL[0]["text"]) {
			if report {
				im.notice("Adding tree value %s", pv(n["text"]))
			}
			n["source_id"] = n["id"]
			delete(n, "id")
			toWrite = append(toWrite, n)
			old = append([]map[string]any{o}, old...)
		} else if im.force {
			if jtrue(n["text"]) {
				im.notice("Unknown treeval update %s, forcing as requested", pv(n["text"]))
				n["source_id"] = n["id"]
				delete(n, "id")
				toWrite = append(toWrite, n)
			} else {
				im.notice("Treeval %s appears to no longer exist, force removing as requested", pv(o["text"]))
			}
		} else if report {
			im.notice("Error: don't know how to handle tree updates for %s, manual intervention required. (failed at old %s new %s)", c.Name, pv(o["text"]), pv(n["text"]))
			return nil
		} else {
			gads.Fail("Error: don't know how to handle tree updates for %s, manual intervention required (failed at old %s new %s, column %s)", c.Name, jstr(o["text"]), jstr(n["text"]), c.Name)
		}
		if kids := treeMaps(n["children"]); len(kids) > 0 {
			n["children"] = im.importBranch(c, treeMaps(o["children"]), kids, report)
		}
	}
	for _, n := range newL {
		delete(n, "id")
	}
	return append(toWrite, newL...)
}

// columnImportAfterAll is import_after_all: the display fields and link
// parent, and the fields and filter of fields of other tables
func (im *importer) columnImportAfterAll(c *gads.Column, values map[string]any, report bool) {
	switch c.Type {
	case "autocur", "filval":
		newID := mappedOrNil(im.columnMapping, values["related_field_id"])
		if report && float64(derefID(c.RelatedFieldID)) != pn(newID) {
			im.notice("Update: related_field_id from %s to %s", pv(c.RelatedFieldID), pv(newID))
		}
		c.RelatedFieldID = jint(newID)
	}
	switch c.Type {
	case "curval", "autocur", "filval":
		var ids []int64
		for _, id := range listOf(values["curval_field_ids"]) {
			ids = append(ids, im.columnMapping[jstr(id)])
		}
		c.SetCurvalFieldIDs(ids)
		filter := gads.NewFilterJSON(jstr(values["filter"]), nil)
		for _, f := range filter.Filters() {
			fieldID := jstr(f["id"])
			var newID string
			if m := regexp.MustCompile(`^([0-9]+)_([0-9]+)$`).FindStringSubmatch(fieldID); m != nil {
				newID = fmt.Sprintf("%s_%s", ps(mappedOrNil(im.columnMapping, m[1])), ps(mappedOrNil(im.columnMapping, m[2])))
			} else {
				newID = ps(mappedOrNil(im.columnMapping, fieldID))
			}
			if perlFalse(newID) {
				gads.Panic("Missing ID")
			}
			if n, err := strconv.ParseInt(newID, 10, 64); err == nil {
				f["id"], f["field"] = n, n
			} else {
				f["id"], f["field"] = newID, newID
			}
			delete(f, "column_id")
		}
		filter.SetLayout(c.Layout())
		c.SetFilter(filter)
	}
	var rules []any
	for _, df := range listOf(values["display_fields"]) {
		h := hashOf(df)
		h["id"] = mappedOrNil(im.columnMapping, h["id"])
		rules = append(rules, h)
	}
	oldDF := c.DisplayFields().AsJSON()
	var newDF map[string]any
	if len(rules) > 0 {
		cond := jstr(values["display_condition"])
		if perlFalse(cond) {
			cond = "AND"
		}
		newDF = map[string]any{"condition": cond, "rules": rules}
	} else {
		newDF = map[string]any{}
	}
	df := gads.NewFilterHash(newDF, c.Layout())
	c.SetDisplayFields(df)
	if report && !reflect.DeepEqual(decodeJSONValue(oldDF), decodeJSONValue(df.AsJSON())) {
		im.notice("Update: display_fields has been updated for %s", c.Name)
	}
	var newID *int64
	if jtrue(values["link_parent"]) {
		if n, ok := im.columnMapping[jstr(values["link_parent"])]; ok {
			newID = &n
		}
	}
	if report && derefID(c.LinkParentID) != derefID(newID) {
		im.notice("Update: link_parent from %s to %s for %s", pv(c.LinkParentID), pv(newID), c.Name)
	}
	c.LinkParentID = newID
}

// metricGroupImportHash is MetricGroup->import_hash: the metrics are
// replaced if they differ
func (im *importer) metricGroupImportHash(id, instanceID int64, values map[string]any) {
	d := im.d
	var existing []any
	rows := d.Query(`SELECT "me".* FROM "metric" "me" JOIN "metric_group" "metric_group" ON "metric_group"."id" = "me"."metric_group" WHERE "me"."metric_group" = ? AND "metric_group"."instance_id" = ? ORDER BY "me"."x_axis_value", "me"."y_axis_grouping_value"`, id, instanceID)
	for _, m := range rows {
		existing = append(existing, map[string]any{
			"x_axis_value":          str(m["x_axis_value"]),
			"target":                ps(m["target"]),
			"y_axis_grouping_value": str(m["y_axis_grouping_value"]),
		})
	}
	var incoming []any
	for _, m := range listOf(values["metrics"]) {
		h := hashOf(m)
		delete(h, "id")
		incoming = append(incoming, map[string]any{
			"x_axis_value":          ps(h["x_axis_value"]),
			"target":                ps(h["target"]),
			"y_axis_grouping_value": ps(h["y_axis_grouping_value"]),
		})
	}
	if reflect.DeepEqual(existing, incoming) {
		return
	}
	name := d.QueryRow(`SELECT "name" FROM "metric_group" WHERE "id" = ?`, id)
	im.notice("Metrics are different for %s. Will delete and recreate.", pv(name["name"]))
	for _, m := range rows {
		d.Exec(`DELETE FROM "metric" WHERE "id" = ?`, m["id"])
	}
	for _, m := range listOf(values["metrics"]) {
		h := hashOf(m)
		d.InsertMap("metric", map[string]any{
			"target":                h["target"],
			"metric_group":          id,
			"x_axis_value":          h["x_axis_value"],
			"y_axis_grouping_value": h["y_axis_grouping_value"],
		})
	}
}

// graphImport is Graph->import_hash and write: the ID of the graph (0
// if not written)
func (im *importer) graphImport(layout *gads.Layout, row db.Row, found bool, values map[string]any) int64 {
	e := im.e
	var g *gads.Graph
	var cur map[string]any
	if found {
		id, _ := db.AsInt64(row["id"])
		g = e.LoadGraph(id, layout, nil)
		cur = map[string]any{}
		for k, v := range row {
			cur[k] = v
		}
		cur["metric_group_id"] = row["metric_group"]
	} else {
		g = e.NewGraph(layout, nil)
		cur = map[string]any{"stackseries": int64(0), "is_shared": int64(0), "as_percent": int64(0)}
	}
	report := im.reportOnly
	title := func() string { return pv(cur["title"]) }
	strField := func(k string) {
		if report && ps(cur[k]) != ps(values[k]) {
			im.notice("Updating %s from %s to %s for graph %s", k, pv(cur[k]), pv(values[k]), title())
		}
		cur[k] = values[k]
	}
	numField := func(k string) {
		if report && pn(cur[k]) != pn(values[k]) {
			im.notice("Updating %s from %s to %s for graph %s", k, pv(cur[k]), pv(values[k]), title())
		}
		cur[k] = values[k]
	}
	strField("title")
	strField("description")
	numField("y_axis")
	strField("y_axis_stack")
	numField("y_axis_link")
	strField("y_axis_label")
	numField("x_axis")
	numField("x_axis_link")
	strField("x_axis_grouping")
	numField("group_by")
	numField("stackseries")
	numField("trend")
	dateField := func(k string) {
		var t any
		if jtrue(values[k]) {
			if tt, err := time.Parse("2006-01-02T15:04:05", jstr(values[k])); err == nil {
				t = tt
			}
		}
		old := cur[k]
		if ot, ok := db.AsTime(old); ok && old != nil {
			old = ot
		}
		if report && ps(old) != ps(t) {
			im.notice("Updating %s from %s to %s for graph %s", k, pv(old), pv(t), title())
		}
		cur[k] = t
	}
	dateField("from")
	dateField("to")
	strField("x_axis_range")
	numField("is_shared")
	numField("user_id")
	numField("group_id")
	numField("as_percent")
	strField("type")
	numField("metric_group_id")
	if report {
		if found {
			return g.ID
		}
		return 0
	}
	g.Title = ps(cur["title"])
	g.Description = ps(cur["description"])
	g.YAxis = derefID(jint(cur["y_axis"]))
	g.YAxisStack = ps(cur["y_axis_stack"])
	g.YAxisLink = derefID(jint(cur["y_axis_link"]))
	g.YAxisLabel = ps(cur["y_axis_label"])
	g.XAxis = derefID(jint(cur["x_axis"]))
	g.XAxisLink = derefID(jint(cur["x_axis_link"]))
	g.XAxisGrouping = ps(cur["x_axis_grouping"])
	g.GroupBy = derefID(jint(cur["group_by"]))
	g.Stackseries = jtrue(cur["stackseries"])
	g.Trend = ps(cur["trend"])
	g.From, g.To = nil, nil
	if t, ok := cur["from"].(time.Time); ok {
		g.From = &t
	}
	if t, ok := cur["to"].(time.Time); ok {
		g.To = &t
	}
	g.XAxisRange = ps(cur["x_axis_range"])
	g.IsShared = jtrue(cur["is_shared"])
	g.UserID = derefID(jint(cur["user_id"]))
	g.GroupID = derefID(jint(cur["group_id"]))
	g.AsPercent = jtrue(cur["as_percent"])
	g.Type = ps(cur["type"])
	g.MetricGroupID = derefID(jint(cur["metric_group_id"]))
	g.Write()
	return g.ID
}

// viewImport is View->import_hash (which writes the view): its ID (0 if
// not written)
func (im *importer) viewImport(layout *gads.Layout, row db.Row, values map[string]any, filter *gads.Filter, cols []int64, sorts, groups []map[string]any) int64 {
	e, d := im.e, im.d
	var v *gads.View
	var cur map[string]any
	if row != nil {
		id, _ := db.AsInt64(row["id"])
		v = e.LoadViewOverride(id, layout)
		cur = map[string]any{"global": row["global"], "is_admin": row["is_admin"], "group_id": row["group_id"], "name": row["name"]}
	} else {
		v = e.NewView(layout)
		cur = map[string]any{"global": int64(0), "is_admin": int64(0)}
	}
	report := im.reportOnly
	name := func() string { return pv(cur["name"]) }
	field := func(k, label string) {
		if report && ps(cur[k]) != ps(values[k]) {
			im.notice("Updating %s from %s to %s for view %s", label, pv(cur[k]), pv(values[k]), name())
		}
		cur[k] = values[k]
	}
	field("global", "global")
	field("is_admin", "is_admin")
	field("group_id", "group")
	field("name", "name")
	if filter != nil {
		v.Filter().SetHash(filter.AsHash())
	}
	if report && v.Filter().Changed() {
		im.notice("Updating filter for view %s", name())
	}
	v.SetColumns(cols)
	if report {
		return v.ID
	}
	v.Global = jtrue(cur["global"])
	v.IsAdmin = jtrue(cur["is_admin"])
	v.GroupID = jint(cur["group_id"])
	if v.GroupID != nil && *v.GroupID == 0 {
		v.GroupID = nil
	}
	v.Name = ps(cur["name"])
	v.Write(false, false)
	d.Exec(`DELETE FROM "sort" WHERE "view_id" = ?`, v.ID)
	for _, s := range sorts {
		d.InsertMap("sort", map[string]any{"view_id": v.ID, "layout_id": s["layout_id"], "parent_id": s["parent_id"], "order": s["order"], "type": s["type"]})
	}
	d.Exec(`DELETE FROM "view_group" WHERE "view_id" = ?`, v.ID)
	for _, g := range groups {
		d.InsertMap("view_group", map[string]any{"view_id": v.ID, "layout_id": g["layout_id"], "parent_id": g["parent_id"], "order": g["order"]})
	}
	return v.ID
}

// parseISO is DateTime::Format::ISO8601->parse_datetime, as the database
// writes it (nil if none)
func parseISO(v any) any {
	if !jtrue(v) {
		return nil
	}
	s := jstr(v)
	for _, f := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(f, s); err == nil {
			return db.FormatDateTime(t)
		}
	}
	gads.Fail("Invalid date %s", s)
	return nil
}

// parseISODate is parseISO for a column of a date (which the database
// writes without its time)
func parseISODate(v any) any {
	t := parseISO(v)
	if s, ok := t.(string); ok && len(s) >= 10 {
		return s[:10]
	}
	return t
}

// importUser is the import_hash of the users: an active user of the email,
// else a new user, with the groups and permissions of the import
func (im *importer) importUser(user map[string]any, groups []int64) int64 {
	d := im.d
	var uid int64
	if !jtrue(user["deleted"]) {
		uid, _ = d.QueryInt64(`SELECT "me"."id" FROM "user" "me" WHERE "account_request" = 0 AND "me"."deleted" IS NULL AND "email" = ?`+siteAnd(im.e, "me")+` ORDER BY "me"."id" LIMIT 1`, user["email"])
	}
	if uid == 0 {
		uid = im.e.InsertUser(map[string]any{
			"firstname":             user["firstname"],
			"surname":               user["surname"],
			"value":                 user["value"],
			"email":                 user["email"],
			"username":              user["username"],
			"freetext1":             user["freetext1"],
			"freetext2":             user["freetext2"],
			"password":              user["password"],
			"pwchanged":             parseISO(user["pwchanged"]),
			"deleted":               parseISO(user["deleted"]),
			"lastlogin":             parseISO(user["lastlogin"]),
			"account_request":       user["account_request"],
			"account_request_notes": user["account_request_notes"],
			"created":               parseISO(user["created"]),
		})
	}
	u := im.e.FindUser(uid)
	u.SetGroups(nil, groups)
	var perms []string
	for _, p := range listOf(user["permissions"]) {
		perms = append(perms, jstr(p))
	}
	u.SetPermissions(perms)
	return uid
}

// importCurrent is the import_hash of a record: the current, its versions
// and the values to import once the fields are written
func (im *importer) importCurrent(record map[string]any, instanceID int64) int64 {
	d := im.d
	var deletedby any
	if jtrue(record["deletedby"]) {
		deletedby = mappedOrNil(im.userMapping, record["deletedby"])
	}
	cid := d.InsertMap("current", map[string]any{
		"instance_id": instanceID,
		"serial":      record["serial"],
		"deleted":     record["deleted"],
		"deletedby":   deletedby,
	})
	for _, r := range listOf(record["records"]) {
		rec := hashOf(r)
		if jtrue(rec["record_id"]) {
			gads.Fail("Import of record_id value not yet support")
		}
		var approvedby any
		if jtrue(rec["approved_by"]) {
			approvedby = mappedOrNil(im.userMapping, rec["approvedby"])
		}
		rid := d.InsertMap("record", map[string]any{
			"current_id": cid,
			"created":    parseISO(rec["created"]),
			"createdby":  mappedOrNil(im.userMapping, rec["createdby"]),
			"approvedby": approvedby,
			"approval":   rec["approval"],
		})
		for _, v := range listOf(rec["values"]) {
			value := hashOf(v)
			colID := im.columnMapping[jstr(value["layout_id"])]
			delete(value, "layout_id")
			value["record_id"] = rid
			im.valuesToImport[colID] = append(im.valuesToImport[colID], value)
		}
	}
	return cid
}

// importValue is Column->import_value
func (im *importer) importValue(col *gads.Column, value map[string]any) {
	d := im.d
	base := map[string]any{"record_id": value["record_id"], "layout_id": col.ID, "child_unique": value["child_unique"]}
	switch col.Type {
	case "date":
		base["value"] = parseISODate(value["value"])
		d.InsertMap("date", base)
	case "daterange":
		base["from"] = parseISODate(value["from"])
		base["to"] = parseISODate(value["to"])
		base["value"] = value["value"]
		d.InsertMap("daterange", base)
	case "file":
		var file any
		if jtrue(value["content"]) {
			content, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(jstr(value["content"]), "\n", ""))
			file = im.env.E.FilevalCreate(map[string]any{"name": value["name"], "mimetype": value["mimetype"]}, content)
		}
		base["value"] = file
		d.InsertMap("file", base)
	case "curval":
		base["value"] = value["value"]
		d.InsertMap("curval", base)
	case "intgr":
		base["value"] = value["value"]
		d.InsertMap("intgr", base)
	case "person":
		base["value"] = value["value"]
		d.InsertMap("person", base)
	case "string":
		base["value"] = value["value"]
		base["value_index"] = value["value_index"]
		d.InsertMap("string", base)
	case "enum", "tree":
		base["value"] = value["value"]
		d.InsertMap("enum", base)
	default:
		Die(`Can't locate object method "import_value" via package "%s"`, columnClass(col.Type))
	}
}
