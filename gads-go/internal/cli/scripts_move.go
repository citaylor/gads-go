package cli

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/yamlxs"
)

func init() {
	Commands["move-layout.pl"] = MoveLayout
}

// columnClasses are the types of field with a GADS::Column class
var columnClasses = map[string]bool{
	"autocur": true, "calc": true, "createdby": true, "createddate": true, "curval": true,
	"date": true, "daterange": true, "deletedby": true, "enum": true, "file": true,
	"filval": true, "id": true, "intgr": true, "person": true, "rag": true, "serial": true,
	"string": true, "tree": true,
}

// columnClass is the GADS::Column class of a type (String::CamelCase
// camelize)
func columnClass(typ string) string {
	var parts []string
	for _, p := range strings.Split(typ, "_") {
		if p != "" {
			parts = append(parts, strings.ToUpper(p[:1])+p[1:])
		}
	}
	return "GADS::Column::" + strings.Join(parts, "")
}

// writeProps is the start of write_props of move-layout.pl: the setters
// up to display_field, which columns no longer have
func writeProps(typ string) {
	Die(`Can't locate object method "display_field" via package "%s"`, columnClass(typ))
}

// scheduleAll is Algorithm::Dependency::Ordered->schedule_all: the items
// (sorted as strings) with each after its dependencies
func scheduleAll(deps map[string][]string) []string {
	var queue []string
	for id := range deps {
		queue = append(queue, id)
	}
	sort.Strings(queue)
	selected := map[string]bool{}
	marker := ""
	var schedule []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == marker {
			Die("Can't use an undefined value as an ARRAY reference")
		}
		missing := false
		for _, d := range deps[id] {
			if !selected[d] {
				missing = true
			}
		}
		if missing {
			if marker == "" {
				marker = id
			}
			queue = append(queue, id)
			continue
		}
		schedule = append(schedule, id)
		selected[id] = true
		marker = ""
	}
	return schedule
}

// MoveLayout is bin/move-layout.pl: dumps the fields or the global views
// of a table to YAML, or loads them from it. Its field setters include
// display_field, which columns no longer have, so it fails for each
// field, as GADS does.
func MoveLayout(env *Env) {
	var instanceID, siteID, loadFile, dumpFile string
	var globalViews bool
	if !env.GetOptions(
		Option{Name: "instance-id", Value: true, String: &instanceID},
		Option{Name: "site-id", Value: true, String: &siteID},
		Option{Name: "load-file", Value: true, String: &loadFile},
		Option{Name: "dump-file", Value: true, String: &dumpFile},
		Option{Name: "global-views", Bool: &globalViews},
	) {
		env.Exit(0)
	}
	if perlFalse(instanceID) {
		Die("Need --instance-id")
	}
	if perlFalse(siteID) {
		Die("Need --site-id")
	}
	if perlFalse(loadFile) && perlFalse(dumpFile) {
		Die("Need either --load-file or --dump-file")
	}
	e := env.E
	e.SiteID, _ = strconv.ParseInt(siteID, 10, 64)
	iid, _ := strconv.ParseInt(instanceID, 10, 64)
	layout := e.NewLayout(iid, nil)
	d := e.DB
	if !perlFalse(loadFile) {
		content, err := os.ReadFile(env.Path(loadFile))
		if err != nil {
			Die("YAML::XS::LoadFile Can't open '%s' for input:\n%s", loadFile, perlErr(err))
		}
		var loaded any
		if err := yaml.Unmarshal(content, &loaded); err != nil {
			Die("YAML::XS::Load Error: %s", err)
		}
		array, ok := loaded.([]any)
		if !ok {
			Die("Not an ARRAY reference")
		}
		d.Txn(func() {
			if globalViews {
				for _, imp := range array {
					h, _ := imp.(map[string]any)
					id := yamlStr(h["id"])
					if n, _ := d.QueryInt64(`SELECT COUNT(*) FROM "view" WHERE "id" = ? AND "instance_id" != ?`, id, iid); n > 0 {
						Die("View ID %s already exists but for wrong instance", id)
					}
					if d.QueryRow(`SELECT "id" FROM "view" WHERE "id" = ? AND "instance_id" = ?`, id, iid) == nil {
						d.InsertMap("view", map[string]any{"id": id, "instance_id": iid})
					}
					vid, _ := strconv.ParseInt(id, 10, 64)
					view := e.LoadView(vid, layout)
					var cols []int64
					for _, c := range asAnyList(h["columns"]) {
						n, _ := strconv.ParseInt(yamlStr(c), 10, 64)
						cols = append(cols, n)
					}
					view.SetColumns(cols)
					view.Global = yamlTrue(h["is_admin"])
					view.IsAdmin = yamlTrue(h["is_admin"])
					view.Name = yamlStr(h["name"])
					view.Filter().SetJSON(yamlStr(h["filter"]))
					view.Write(false, false)
					// set_sorts is given the fields and types as two lists
					Die(`isa check for "set_sorts" failed: ARRAY(0x0) is not a HashRef!`)
				}
				return
			}
			loadedByID := map[string]map[string]any{}
			for _, item := range array {
				h, _ := item.(map[string]any)
				loadedByID[yamlStr(h["id"])] = h
			}
			missing := map[string]map[string]any{}
			for id, h := range loadedByID {
				missing[id] = h
			}
			cols := layout.All(gads.AllOptions{})
			for _, c := range cols {
				delete(missing, strconv.FormatInt(c.ID, 10))
			}
			if len(missing) > 0 {
				deps := map[string][]string{}
				for id, h := range missing {
					deps[id] = []string{}
					if df := yamlStr(h["display_field"]); !perlFalse(df) {
						if _, ok := missing[df]; ok {
							deps[id] = []string{df}
						}
					}
				}
				for _, id := range scheduleAll(deps) {
					h := missing[id]
					env.SayErr("Creating missing field %s (%s)", yamlStr(h["id"]), yamlStr(h["name"]))
					typ := yamlStr(h["type"])
					if !columnClasses[typ] {
						class := columnClass(typ)
						Die(`Can't locate object method "new" via package "%s" (perhaps you forgot to load "%s"?)`, class, class)
					}
					writeProps(typ)
				}
			}
			for _, c := range cols {
				if _, ok := loadedByID[strconv.FormatInt(c.ID, 10)]; ok {
					writeProps(c.Type)
				}
				env.SayErr("Field %s (ID %d) not in updated layout - needs manual deletion", c.Name, c.ID)
			}
			// The fields that are not user input are written last, by no
			// user
			layout.Clear()
			for _, c := range layout.All(gads.AllOptions{}) {
				if !c.Userinput() {
					gads.Fail("You do not have permission to manage fields")
				}
			}
		})
		return
	}
	var out []any
	if globalViews {
		for _, id := range d.QueryInt64s(`SELECT "id" FROM "view" WHERE "instance_id" = ?`, iid) {
			view := e.LoadView(id, layout)
			if !view.Exists() || !(view.Global || view.IsAdmin) {
				continue
			}
			row := d.QueryRow(`SELECT "global", "is_admin" FROM "view" WHERE "id" = ?`, id)
			var columns []any
			for _, c := range view.Columns() {
				columns = append(columns, c)
			}
			if columns == nil {
				columns = []any{}
			}
			// GADS dumps the sorts as database rows (with their schema);
			// they are written as their fields and types instead
			sorts := []any{}
			for _, s := range view.Sorts() {
				sorts = append(sorts, map[string]any{"layout_id": s.LayoutID, "type": s.Type})
			}
			g, _ := db.AsInt64(row["global"])
			a, _ := db.AsInt64(row["is_admin"])
			out = append(out, map[string]any{
				"id":       id,
				"name":     view.Name,
				"global":   g,
				"is_admin": a,
				"filter":   view.Filter().AsJSON(),
				"columns":  columns,
				"sorts":    sorts,
			})
		}
	} else {
		for _, c := range layout.All(gads.AllOptions{}) {
			writeProps(c.Type)
		}
	}
	if out == nil {
		out = []any{}
	}
	if err := os.WriteFile(env.Path(dumpFile), []byte(yamlxs.Dump(out)), 0o666); err != nil { // #nosec G306 -- as Perl creates the file (0666, less the umask)
		Die("YAML::XS::DumpFile Can't open '%s' for output:\n%s", dumpFile, perlErr(err))
	}
}

func yamlStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "1"
		}
		return ""
	}
	s, _ := db.AsString(v)
	return s
}

func yamlTrue(v any) bool { return !perlFalse(yamlStr(v)) }

func asAnyList(v any) []any {
	l, _ := v.([]any)
	return l
}
