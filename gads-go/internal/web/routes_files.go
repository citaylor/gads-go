package web

import (
	"errors"
	"regexp"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// get '/file/?' (GADS.pm:1664): the independent files (superadmins only)
func (s *Server) hFiles(c *Ctx) string {
	if !c.LoggedInUser().Permission()["superadmin"] {
		c.ForwardHome("danger", "You do not have permission to manage files", "")
	}
	files := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT "me"."id", "me"."name", "me"."mimetype", "me"."is_independent", "me"."edit_user_id" FROM "fileval" "me" WHERE "me"."is_independent" = 1 ORDER BY "me"."id"`) {
		files.Items = append(files.Items, newRow("fileval", r))
	}
	return c.Template("files", tt.HashOf("files", files, "page", "files"), nil)
}

// findFile is ResultSet::Fileval find_with_permission with an ID from the
// request, or nil if there is no such file
func (s *Server) findFile(id string, user *gads.User, newFileOnly, renameExisting bool) db.Row {
	n, ok := toInt64(id)
	if !ok || !reDigitsOnly.MatchString(id) {
		return nil
	}
	if !s.E.FilevalFindWithPermission(n, user, newFileOnly, renameExisting) {
		return nil
	}
	return s.E.DB.QueryRow(`SELECT * FROM "fileval" WHERE "id" = ?`, n)
}

var reDigitsOnly = regexp.MustCompile(`^[0-9]+$`)

// get '/file/:id?' (GADS.pm:1679): the content of a file
func (s *Server) hFile(c *Ctx) string {
	id, _ := c.RouteParam("id")
	if !truthy(id) {
		gads.Fail("File ID missing")
	}
	if n, ok := toInt64(id); ok && n == -1 {
		return c.SendFile([]byte("Purged File"), "text/plain", "purged")
	}
	f := s.findFile(id, c.LoggedInUser(), false, false)
	if f == nil {
		gads.Fail("File ID %s cannot be found", id)
	}
	fid, _ := db.AsInt64(f["id"])
	content := s.E.FilevalContent(fid)
	mime, _ := db.AsString(f["mimetype"])
	name, _ := db.AsString(f["name"])
	return c.SendFile(content, mime, name)
}

// post '/file/:id?' (GADS.pm:1694): uploading an independent file through
// the "manage files" page, or deleting one
func (s *Server) hFilePost(c *Ctx) string {
	// Hardened (M-3): only superadmins manage files. Perl lets anyone
	// upload one (that every user can read), checking only deletions.
	if s.E.Config.Hardened && !c.LoggedInUser().Permission()["superadmin"] {
		gads.Fail("You do not have permission to manage files")
	}
	if up := c.Upload("file"); up != nil && up.Filename != "" {
		mime := gads.CheckUpload(up.Filename, up.Content, gads.CheckOptions{CheckName: true})
		var id int64
		if c.Process(func() {
			id = s.E.FilevalCreate(map[string]any{
				"name": up.Filename, "mimetype": mime,
				"is_independent": int64(1), "edit_user_id": nil,
			}, up.Content)
		}) {
			c.ForwardHome("success", "File has been uploaded as ID "+fmtInt(id), "file/")
		}
	}
	// Otherwise assume delete
	if !c.LoggedInUser().Permission()["superadmin"] {
		gads.Fail("You do not have permission to delete files")
	}
	id, _ := c.BodyParam("delete")
	if !truthy(id) {
		gads.Fail("File ID missing for deletion request")
	}
	var row db.Row
	if reDigitsOnly.MatchString(id) {
		row = s.E.DB.QueryRow(`SELECT "id" FROM "fileval" WHERE "id" = ?`, id)
	}
	if row == nil {
		gads.Fail("File ID %s cannot be found", id)
	}
	rid, _ := db.AsInt64(row["id"])
	if c.Process(func() { s.E.FilevalRemove(rid) }) {
		c.ForwardHome("success", "File has been deleted successsfully", "file/")
	}
	return ""
}

// put '/api/file/:id' (GADS.pm:1730): renaming a file. A new file (not yet
// attached to a record) is renamed; otherwise a copy is made with the new
// name.
func (s *Server) hAPIFileRename(c *Ctx) string {
	id, _ := c.RouteParam("id")
	isNew := c.ParamS("is_new")
	newname := c.ParamS("filename")
	if !truthy(newname) {
		gads.Fail("Filename is required")
	}
	gads.CheckFileName(newname)
	user := c.LoggedInUser()
	if truthy(isNew) {
		f := s.findFile(id, user, true, false)
		if f == nil {
			gads.Fail("File ID %s cannot be found", id)
		}
		s.E.DB.UpdateMap("fileval", map[string]any{"name": newname}, map[string]any{"id": f["id"]})
		c.ContentType("application/json")
		return gads.EncodeJSON(map[string]any{"id": f["id"], "name": newname, "is_ok": int64(1)})
	}
	f := s.findFile(id, user, false, true)
	if f == nil {
		gads.Fail("File ID %s cannot be found", id)
	}
	fid, _ := db.AsInt64(f["id"])
	newID := s.E.FilevalCreate(map[string]any{
		"name": newname, "mimetype": f["mimetype"],
		"is_independent": int64(0), "edit_user_id": user.ID,
	}, s.E.FilevalContent(fid))
	c.ContentType("application/json")
	return gads.EncodeJSON(map[string]any{"id": newID, "name": newname, "is_ok": int64(1)})
}

var reFileNameStrip = regexp.MustCompile(`[^a-zA-Z0-9\._\-\(\) ]`)

// post '/api/file/?' (GADS.pm:1779): uploading a file of a record edit, or
// deleting a file
func (s *Server) hAPIFile(c *Ctx) string {
	user := c.LoggedInUser()
	if del := c.ParamS("delete"); truthy(del) {
		if !user.Permission()["superadmin"] {
			gads.Fail("You do not have permission to delete files")
		}
		row := s.E.DB.QueryRow(`SELECT "id" FROM "fileval" WHERE "id" = ?`, del)
		if row == nil {
			panic(errors.New(`Can't call method "delete" on an undefined value`))
		}
		rid, _ := db.AsInt64(row["id"])
		s.E.FilevalRemove(rid)
		c.ForwardHome("success", "The file has been deleted successfully", "file/")
	}
	if up := c.Upload("file"); up != nil && up.Filename != "" {
		columnID, _ := c.BodyParam("column_id")
		if !truthy(columnID) {
			gads.Fail("Missing column ID")
		}
		cid, _ := toInt64(columnID)
		column := s.E.NewLayout(0, user).Column(cid)
		if column == nil {
			panic(errors.New(`Can't call method "override_types" on an undefined value`))
		}
		mime := gads.CheckUpload(up.Filename, up.Content, gads.CheckOptions{ExtraTypes: column.OverrideTypes()})
		// Invalid characters are removed from the name
		filename := reFileNameStrip.ReplaceAllString(up.Filename, "")
		var id int64
		if c.Process(func() {
			id = s.E.FilevalCreate(map[string]any{
				"name": filename, "mimetype": mime,
				"is_independent": int64(0), "edit_user_id": user.ID,
			}, up.Content)
		}) {
			return gads.EncodeJSON(map[string]any{"id": id, "filename": filename, "url": "/file/" + fmtInt(id), "is_ok": int64(1)})
		}
		gads.Panic("encountered object, but neither allow_blessed, convert_blessed nor allow_tags settings are enabled")
	}
	return gads.EncodeJSON(map[string]any{"is_ok": int64(0), "error": "No file was submitted"})
}
