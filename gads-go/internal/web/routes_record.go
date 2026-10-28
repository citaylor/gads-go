package web

import (
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// Splat is Dancer2 splat: the captures of a regexp route
func (c *Ctx) Splat() []string { return c.route.splat }

// any qr{/(record|history|purge|purgehistory)/([0-9]+)} (GADS.pm:1896)
func (s *Server) hRecord(c *Ctx) string {
	splat := c.Splat()
	action := splat[0]
	id, _ := strconv.ParseInt(splat[1], 10, 64)
	user := c.LoggedInUser()

	record := s.E.NewRecordUnattached(user)
	record.Rewind = sessionRewind(c)

	// Wait until record has been found before logging to audit, so that
	// the instance_id is known
	if err := gads.Try(func() {
		switch action {
		case "history":
			record.FindRecordID(id, gads.FindOptions{})
			// The user must also have access to the current version of a
			// historical version (which a view limit may not allow)
			s.E.NewRecordUnattached(user).FindCurrentID(record.CurrentID(), gads.FindOptions{})
		case "purge":
			record.FindDeletedCurrentID(id)
		case "purgehistory":
			record.FindRecordID(id, gads.FindOptions{Deleted: true})
		default:
			record.FindCurrentID(id, gads.FindOptions{})
		}
	}); err != nil {
		c.auditLog()
		panic(err)
	}
	layout := record.Layout()
	c.SetVar("layout", layout)
	c.auditLog()

	if _, ok := c.Param("pdf"); ok && !truthy(layout.Row()["no_download_pdf"]) {
		gads.Panic("PDF downloads of records are not supported")
	}
	if rep, _ := c.QueryParam("report"); truthy(rep) {
		gads.Panic("Reports of records are not supported")
	}

	params, opts, raw := s.processEdit(c, id, record)
	if raw != "" {
		return raw
	}
	params.Set("is_history", ttBoolEmpty(action == "history"))
	reports := tt.NewList()
	for _, r := range layout.Reports() {
		reports.Items = append(reports.Items, newRow("report", r))
	}
	params.Set("reports", reports)
	return c.Template("edit", params, opts)
}

// any ['get', 'post'] => /edit/:id (GADS.pm:743)
func (s *Server) hEditRedirect(c *Ctx) string {
	id, _ := c.Param("id")
	c.Redirect("/record/" + id)
	return ""
}

// any ['get', 'post'] => /:layout_name/record/? (GADS.pm:4269)
func (s *Server) hNewRecord(c *Ctx) string {
	if c.Layout() == nil {
		c.Pass()
	}
	params, opts, raw := s.processEdit(c, 0, nil)
	if raw != "" {
		return raw
	}
	return c.Template("edit", params, opts)
}

// processEdit is GADS.pm _process_edit: it returns the template parameters
// and options, or a raw response (JSON of a validation)
func (s *Server) processEdit(c *Ctx, id int64, record *gads.Record) (*tt.Hash, *TemplateOptions, string) {
	user := c.LoggedInUser()
	var layout *gads.Layout

	if del := c.ParamS("delete"); truthy(del) {
		if c.Process(func() { record.DeleteCurrent(false, 0) }) {
			c.ForwardHome("success", "Record has been deleted successfully", record.Layout().Identifier()+"/data")
		}
	}

	switch {
	case id != 0 && record != nil:
		layout = record.Layout()
	case id != 0:
		record = s.E.NewRecordUnattached(user)
		o := gads.FindOptions{}
		if _, ok := c.Param("include_draft"); ok {
			o.IncludeDraft = true
			o.DraftuserID = user.ID
		}
		record.FindCurrentID(id, o)
		layout = record.Layout()
		c.SetVar("layout", layout)
	default:
		// New record (the layout is that of the URL)
		layout = c.Layout()
		record = s.E.NewRecord(layout, user)
		record.Initialise(0)
	}

	child := perlInt(c.ParamS("child"))
	if child == 0 {
		child = record.ParentID()
	}
	var modal int64
	if m := c.ParamS("modal"); truthy(m) {
		modal = perlInt(m)
	}
	cloneFrom := ""
	if !truthy(layout.Row()["no_copy_record"]) {
		cloneFrom = c.ParamS("from")
	}

	_, validate := c.Param("validate")
	_, draft := c.Param("draft")
	if truthy(c.ParamS("submit")) || truthy(c.ParamS("draft")) || modal != 0 || validate {
		failed := false
		if child != 0 && id == 0 && !layout.UserCan("create_child") {
			gads.Fail("You do not have permission to create a child record")
		}
		record.SetParentID(child)

		var validationErrors []string
		for _, col := range record.EditColumns(gads.EditColumnsOptions{New: id == 0, Modal: modal != 0}) {
			var newv []string
			if _, ok := c.BodyParam(col.Field()); !ok {
				continue
			}
			newv = c.body.getAll(col.Field())
			if !col.Userinput() {
				continue
			}
			value := make([]any, len(newv))
			for i, v := range newv {
				value[i] = v
			}
			datum := record.Fields()[col.ID]
			if validate {
				if err := gads.Try(func() { datum.SetValue(value, gads.SetValueOptions{}) }); err != nil {
					validationErrors = append(validationErrors, validationMessage(err))
				}
			} else {
				ok := c.Process(func() { datum.SetValue(value, gads.SetValueOptions{Draft: draft}) })
				failed = !ok || failed
			}
		}

		switch {
		case validate:
			// The "source" parameter is user input, make sure still valid
			sid, _ := toInt64(c.ParamS("source"))
			source := layout.Column(sid)
			if source != nil && !source.UserCan("read") {
				source = nil
			}
			if err := gads.Try(func() { record.Write(gads.WriteOptions{DryRun: true}) }); err != nil {
				validationErrors = append(validationErrors, validationMessage(err))
			}
			message := strings.Join(validationErrors, "; ")
			if source == nil {
				gads.Panic(`Can't call method "curval_fields" on an undefined value`)
			}
			values := map[string]any{}
			for _, f := range source.CurvalFields() {
				values[f.Field()] = record.GetFieldValue(f).AsString()
			}
			errFlag := 0
			if message != "" {
				errFlag = 1
			}
			c.ctype = `application/json; charset="utf-8"`
			return nil, nil, gads.EncodeJSON(map[string]any{"error": errFlag, "message": message, "values": values})
		case modal != 0:
			// Do nothing, just a live edit, no write required
		case truthy(c.ParamS("draft")):
			if c.Process(func() { record.Write(gads.WriteOptions{Draft: true, SubmissionToken: c.ParamS("submission_token")}) }) {
				c.ForwardHome("success", "Draft has been saved successfully", layout.Identifier()+"/data")
			} else if record.AlreadySubmittedError {
				c.ForwardHome("", "", layout.Identifier()+"/data")
			}
		case !failed:
			if c.Process(func() { record.Write(gads.WriteOptions{SubmissionToken: c.ParamS("submission_token")}) }) {
				forward := layout.Identifier() + "/data"
				if (id == 0 && truthy(layout.Row()["forward_record_after_create"])) || c.ParamS("submit") == "submit-and-remain" {
					forward = "record/" + fmtInt(record.CurrentID())
				}
				// Saved values of the form in the browser to be cleared
				var clear any = int64(0)
				if id != 0 {
					clear = fmtInt(id)
				}
				c.SessionSet("actions", map[string]any{"clear_saved_values": clear})
				c.ForwardHome("success", "Submission has been completed successfully for record ID "+fmtInt(record.CurrentID()), forward)
			} else if record.AlreadySubmittedError {
				c.ForwardHome("", "", layout.Identifier()+"/data")
			}
		}
	} else if id != 0 {
		// Do nothing, record already loaded
	} else if truthy(cloneFrom) {
		toclone := s.E.NewRecord(layout, user)
		toclone.FindCurrentID(perlInt(cloneFrom), gads.FindOptions{})
		record = toclone.Clone()
	} else {
		record.LoadRememberedValues(0)
	}

	if truthy(c.ParamS("delete_draft")) {
		layout = c.Layout()
		if c.Process(func() { record.DeleteUserDrafts() }) {
			c.ForwardHome("success", "Draft has been deleted successfully", layout.Identifier()+"/data")
		}
	}

	for _, col := range layout.All(gads.AllOptions{UserCanWrite: true}) {
		if !col.UserCan("read") {
			record.Fields()[col.ID].SetValue("", gads.SetValueOptions{})
		}
	}

	var childRec any
	if child != 0 && layout.UserCan("create_child") {
		childRec = perlInt(c.ParamS("child"))
	} else if record.ParentID() != 0 {
		childRec = record.ParentID()
	}
	if truthy(childRec) {
		c.Notice("Values entered on this page will have their own value in the child record. All other values will be inherited from the parent.")
	}

	base := c.Base()
	pres := c.recordPresentation(record, recordPresentOpts{edit: true, new: id == 0, child: child != 0, modal: modal})
	crumbText := "New " + layout.RecordName()
	if id != 0 {
		crumbText = ucfirst(layout.RecordName()) + " ID: " + fmtInt(record.CurrentID())
	}
	var submissionToken any = ""
	if modal == 0 {
		submissionToken = record.SubmissionToken()
	}
	params := tt.HashOf(
		"edit_modal", ttValue(nilIfZero(modal)),
		"page", "edit",
		"child", childRec,
		"layout_edit", c.ttLayout(layout),
		"clone", emptyNil(cloneFrom),
		"submission_token", submissionToken,
		"header_type", "table_title",
		"header_back_url", base+layout.Identifier()+"/data",
		"header_record_buttons", int64(1),
		"layout_obj", c.ttLayout(layout),
		"record", pres,
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+layout.Identifier()+"/data", "Table: "+layout.Name()),
			crumb("", crumbText),
		),
	)
	if id != 0 {
		params.Set("content_block_custom_classes", "content-block--record content-block--footer")
	} else {
		params.Set("content_block_custom_classes", "content-block--footer")
	}
	if truthy(cloneFrom) {
		params.Set("clone_from", cloneFrom)
	}
	var opts *TemplateOptions
	if modal != 0 {
		col := layout.Column(modal)
		params.Set("modal_field_ids", gads.EncodeJSON(col.CurvalFieldIDs()))
		opts = &TemplateOptions{NoLayout: true}
	}
	return params, opts, ""
}

// validationMessage is the message of an error in a validation request
func validationMessage(err error) string {
	if e, ok := err.(*gads.Error); ok && e.Level != gads.LevelPanic {
		return e.Msg
	}
	return "An unexpected error occurred"
}

// ucfirst is Perl ucfirst
func ucfirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
