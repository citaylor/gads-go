package gads

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// UploadsDir is GADS::Config uploads: the directory of uploaded files,
// which must exist and be writable
func (e *Engine) UploadsDir() string {
	path := ""
	if e.Config != nil {
		path = e.Config.Uploads
	}
	if path == "" {
		path = "/var/lib/GADS/uploads"
	}
	path = strings.TrimSuffix(path, "/")
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() || !writable(path) {
		Fail("Upload directory %s does not exist", path)
	}
	return path
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".w")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

var reThreeDigits = regexp.MustCompile(`(\d{3})`)

// FilevalPath is Schema::Result::Fileval file_to_id: the file of a stored
// file, in directories of 3 digits of its ID (000/000/012)
func (e *Engine) FilevalPath(id int64) string {
	s := reThreeDigits.ReplaceAllString(fmt.Sprintf("%09d", id), "$1/")
	s = strings.TrimPrefix(strings.TrimSuffix(s, "/"), "/")
	return filepath.Join(e.UploadsDir(), s)
}

// FilevalCreate is ResultSet::Fileval create_with_file: creates a file
// (columns of the fileval row) with its content written to the uploads
// directory
func (e *Engine) FilevalCreate(row map[string]any, content []byte) int64 {
	var id int64
	e.DB.Txn(func() {
		id = e.DB.InsertMap("fileval", row)
		path := e.FilevalPath(id)
		// Uploaded files are readable only by the application's user
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			Fail("mkdir %s: %s", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, 0o640); err != nil {
			Fail("Can't write to file '%s': %s", path, err)
		}
	})
	return id
}

// FilevalContent is Schema::Result::Fileval content: the content of a
// stored file
func (e *Engine) FilevalContent(id int64) []byte {
	path := e.FilevalPath(id)
	b, err := os.ReadFile(path)
	if err != nil {
		Fail("File not found!")
	}
	return b
}

// FilevalRemove is Schema::Result::Fileval remove_file: deletes a file and
// its content
func (e *Engine) FilevalRemove(id int64) {
	path := e.FilevalPath(id)
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		os.Remove(path)
	}
	e.DB.Exec(`DELETE FROM "fileval" WHERE "id" = ?`, id)
}

// contentBytes is the content of a file value given as a string or bytes
func contentBytes(v any) []byte {
	switch x := v.(type) {
	case []byte:
		return x
	case nil:
		return nil
	}
	return []byte(anyToString(v))
}

// FilevalFindWithPermission retrieves a file checking the user's access to
// it (ResultSet::Fileval::find_with_permission). It returns false if the
// file does not exist.
func (e *Engine) FilevalFindWithPermission(id int64, user *User, newFileOnly, renameExisting bool) bool {
	fv := e.DB.QueryRow(`SELECT * FROM "fileval" WHERE "id" = ?`, id)
	if fv == nil {
		return false
	}
	// Attached to a record value? (In theory can be more than one)
	file := e.DB.QueryRow(`SELECT "me"."layout_id", "record"."current_id", "layout"."instance_id" FROM "file" "me" LEFT JOIN "record" "record" ON "record"."id" = "me"."record_id" LEFT JOIN "layout" "layout" ON "layout"."id" = "me"."layout_id" WHERE "me"."value" = ? ORDER BY "me"."id" LIMIT 1`, id)
	if file != nil && file["layout_id"] != nil {
		if newFileOnly {
			Fail("Access to this file is not allowed as it is not a new file")
		}
		iid, _ := db.AsInt64(file["instance_id"])
		lid, _ := db.AsInt64(file["layout_id"])
		layout := e.NewLayout(iid, user)
		// The file datum checks for access to the field
		if col := layout.Column(lid); col != nil && !col.UserCan("read") {
			Fail("You do not have access to file ID %d", id)
		}
		// Also check that the user has access to the actual record
		rec := e.newRecordObject(layout, user)
		rec.SetColumns([]int64{})
		cid, _ := db.AsInt64(file["current_id"])
		rec.FindCurrentID(cid, FindOptions{})
	} else if !truthy(fv["is_independent"]) {
		// Uploaded via a record edit and not yet attached to a record
		eu, _ := db.AsInt64(fv["edit_user_id"])
		if eu == 0 || user == nil || eu != user.ID {
			Fail("Access to this file is not allowed")
		}
	} else if newFileOnly || renameExisting {
		Fail("Access to this file is not allowed")
	}
	return true
}
