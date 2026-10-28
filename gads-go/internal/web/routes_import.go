package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// importLayout is the table of the import routes, which need permission to
// import into it
func (c *Ctx) importLayout() *gads.Layout {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	if !layout.UserCan("bulk_import") {
		c.ForwardHome("danger", "You do not have permission to import data", "")
	}
	return layout
}

// ttRowsObj is a resultset of rows, of which a template takes all
type ttRowsObj struct{ rows *tt.List }

func (o *ttRowsObj) TTMethod(name string, args []any) ([]any, bool, error) {
	switch name {
	case "all":
		return []any{o.rows}, true, nil
	case "count":
		return []any{int64(len(o.rows.Items))}, true, nil
	}
	return nil, false, nil
}

// any ['get','post'] => /:layout_name/import/? (GADS.pm:4278)
func (s *Server) hImports(c *Ctx) string {
	layout := c.importLayout()
	if truthy(c.ParamS("clear")) {
		s.E.DB.Exec(`DELETE FROM "import" WHERE "instance_id" = ? AND "completed" IS NOT NULL`, layout.InstanceID)
	}
	imports := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT * FROM "import" "me" WHERE "me"."instance_id" = ? ORDER BY "me"."completed" DESC`, layout.InstanceID) {
		imports.Items = append(imports.Items, newRow("import", r))
	}
	base := c.Base()
	lo := layout.Identifier()
	return c.Template("import", tt.HashOf(
		"imports", imports,
		"page", "imports",
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/data",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb("", "Import records"),
		),
	), nil)
}

// get '/:layout_name/import/rows/:import_id' (GADS.pm:4316)
func (s *Server) hImportRows(c *Ctx) string {
	layout := c.importLayout()
	importID := c.ParamS("import_id")
	imp := s.E.DB.QueryRow(`SELECT * FROM "import" "me" WHERE "me"."id" = ? AND "me"."instance_id" = ? LIMIT 1`, importID, layout.InstanceID)
	if imp == nil {
		gads.Fail("Requested import not found")
	}
	rows := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT * FROM "import_row" "me" WHERE "me"."import_id" = ? ORDER BY "me"."id" ASC`, imp["id"]) {
		rows.Items = append(rows.Items, newRow("import_row", r))
	}
	base := c.Base()
	lo := layout.Identifier()
	return c.Template("import/rows", tt.HashOf(
		"import_id", importID,
		"rows", &ttRowsObj{rows: rows},
		"page", "import/row",
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/import",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb(base+lo+"/import", "Import records"),
			crumb("", "Import "+importID),
		),
	), nil)
}

// any ['get','post'] => /:layout_name/import/data/? (GADS.pm:4356)
func (s *Server) hImportData(c *Ctx) string {
	user := c.LoggedInUser()
	layout := c.importLayout()
	if truthy(c.ParamS("submit")) {
		if up := c.Upload("file"); up != nil {
			var o gads.ImportOptions
			for _, opt := range c.body.getAll("import_options") {
				switch opt {
				case "dry_run":
					o.DryRun = true
				case "force_mandatory":
					o.ForceMandatory = true
				case "split_multiple":
					o.SplitMultiple = true
				case "blank_invalid_enum":
					o.BlankInvalidEnum = true
				case "no_change_unless_blank":
					o.NoChangeUnlessBlank = "skip_new"
				case "take_first_enum":
					o.TakeFirstEnum = true
				case "ignore_string_zeros":
					o.IgnoreStringZeros = true
				case "round_integers":
					o.RoundIntegers = true
				case "short_names":
					o.ShortNames = true
				case "update_only":
					o.UpdateOnly = true
				case "no_report_changes":
					o.NoReportChanges = true
				}
			}
			if v := c.ParamS("update_unique"); truthy(v) {
				o.UpdateUnique, _ = toInt64(v)
			}
			if v := c.ParamS("skip_existing_unique"); truthy(v) {
				o.SkipExistingUnique, _ = toInt64(v)
			}
			imp := s.E.NewImport(layout, user, up.Content, o)
			if c.Process(func() { imp.Process() }) {
				c.ForwardHome("success", "The file import process has been started and can be monitored using the Import Status below", layout.Identifier()+"/import")
			}
		} else {
			c.ReportError("Please select a file to upload")
		}
	}
	base := c.Base()
	lo := layout.Identifier()
	return c.Template("import/data", tt.HashOf(
		"layout", c.ttLayout(layout),
		"page", "import",
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/import",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb(base+lo+"/import", "Import records"),
			crumb("", "Upload"),
		),
	), nil)
}
