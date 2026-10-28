package cli

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/mimeinfo"
)

func init() {
	Commands["export-tree.pl"] = ExportTree
	Commands["import-tree.pl"] = ImportTree
	Commands["import-doc.pl"] = ImportDoc
}

// perlFalse is whether a string is false in Perl
func perlFalse(s string) bool { return s == "" || s == "0" }

func findLayout(env *Env, id string) db.Row {
	l := env.E.DB.QueryRow(`SELECT * FROM "layout" WHERE "id" = ?`, id)
	if l == nil {
		Die("Layout ID %s not found in database", id)
	}
	return l
}

type treeNode struct {
	name      string
	daughters []*treeNode
}

// ExportTree is bin/export-tree.pl: prints the values of a tree field as
// CSV, a column for each level
func ExportTree(env *Env) {
	var layoutID string
	if len(env.Args) > 0 {
		layoutID = env.Args[0]
	}
	if perlFalse(layoutID) {
		Die("Usage: bin/export-tree.pl layout-id")
	}
	l := findLayout(env, layoutID)
	env.SayErr("Using field %s", str(l["name"]))
	type entry struct {
		node   *treeNode
		parent int64
	}
	tree := map[int64]*entry{}
	var order []int64
	for _, e := range env.E.DB.Query(`SELECT "me"."id", "me"."value", "parent"."id" AS "parent_id" FROM "enumval" "me" LEFT JOIN "enumval" "parent" ON "parent"."id" = "me"."parent" WHERE "me"."deleted" = ? AND "me"."layout_id" = ? ORDER BY "me"."value"`, 0, layoutID) {
		id, _ := db.AsInt64(e["id"])
		parent, _ := db.AsInt64(e["parent_id"])
		tree[id] = &entry{node: &treeNode{name: str(e["value"])}, parent: parent}
		order = append(order, id)
	}
	root := &treeNode{name: "Root"}
	for _, n := range order {
		node := tree[n]
		if node.parent != 0 {
			p, ok := tree[node.parent]
			if !ok {
				Die(`Can't call method "add_daughter" on an undefined value`)
			}
			p.node.daughters = append(p.node.daughters, node.node)
		} else {
			root.daughters = append(root.daughters, node.node)
		}
	}
	newline := false
	var do func(node *treeNode, level int)
	do = func(node *treeNode, level int) {
		for _, child := range node.daughters {
			// The number of ancestors of the child
			level := level + 1
			if newline {
				fmt.Fprint(env.Stdout, strings.Repeat(",", level-1))
			}
			fmt.Fprint(env.Stdout, `"`+child.name+`"`)
			if len(child.daughters) > 0 {
				fmt.Fprint(env.Stdout, ",")
				newline = false
			} else {
				fmt.Fprint(env.Stdout, "\n")
				newline = true
			}
			do(child, level)
		}
	}
	do(root, 0)
}

// ImportTree is bin/import-tree.pl: creates the values of a tree field
// from CSV on the standard input, a column for each level
func ImportTree(env *Env) {
	var layoutID, parentTop string
	if len(env.Args) > 0 {
		layoutID = env.Args[0]
	}
	if len(env.Args) > 1 {
		parentTop = env.Args[1]
	}
	if perlFalse(layoutID) {
		Die("Usage: bin/import-tree.pl layout-id [parent-id] (use STDIN for the CSV data)")
	}
	l := findLayout(env, layoutID)
	env.SayErr("Using field %s", str(l["name"]))
	parents := map[int]any{}
	for {
		line := env.ReadLine()
		if line == "" {
			break
		}
		r := csv.NewReader(strings.NewReader(line))
		r.FieldsPerRecord = -1
		row, err := r.Read()
		if err != nil {
			Die("Failed to parse link %s", line)
		}
		for i, col := range row {
			count := i + 1
			if perlFalse(col) {
				continue
			}
			parent := parents[count-1]
			if parent == nil || perlFalse(fmt.Sprint(parent)) {
				parent = nil
				if !perlFalse(parentTop) {
					parent = parentTop
				}
			}
			parents[count] = env.E.DB.InsertMap("enumval", map[string]any{
				"value":     col,
				"layout_id": layoutID,
				"parent":    parent,
			})
		}
	}
}

// ImportDoc is bin/import-doc.pl: uploads a file as an independent file
func ImportDoc(env *Env) {
	var file string
	if len(env.Args) > 0 {
		file = env.Args[0]
	}
	if perlFalse(file) {
		Die("Usage: bin/import-doc.pl filename")
	}
	path := env.Path(file)
	mime := mimeinfo.Mimetype(path)
	content, err := os.ReadFile(path)
	if err != nil {
		Die("read_file '%s' - open: %s", file, perlErr(err))
	}
	_, _ = mime, content
	// The content of files is no longer a column of fileval (they are stored
	// in the uploads directory), so the create of the script dies
	Die("DBIx::Class::Row::store_column(): No such column 'content' on GADS::Schema::Result::Fileval")
}

// perlErr is $! of an error of the operating system
func perlErr(err error) string {
	if os.IsNotExist(err) {
		return "No such file or directory"
	}
	if os.IsPermission(err) {
		return "Permission denied"
	}
	return err.Error()
}
