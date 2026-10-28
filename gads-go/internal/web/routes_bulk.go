package web

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// The notices of the bulk page (with the white space of the Perl source)
var bulkNotices = map[string][3]string{
	"update": {
		"Use this page to update all records in the\n                    current search results. Tick the fields whose values should be\n                    updated. Fields that are not ticked will retain their existing value.\n                    The current search is \"{search}\"",
		"Use this page to update all currently selected records.\n                    Tick the fields whose values should be updated. Fields that are\n                    not ticked will retain their existing value.\n                    The current number of selected records is {count}.",
		"Use this page to update all records in the\n                    currently selected view. Tick the fields whose values should be\n                    updated. Fields that are not ticked will retain their existing value.\n                    The current view is \"{view}\"",
	},
	"clone": {
		"Use this page to bulk clone all of the records in\n                    the current search results. The cloned records will be created using\n                    the same existing values by default, but replaced with the values below\n                    where that value is ticked. Values that are not ticked will be cloned\n                    with their current value. The current search is \"{search}\"",
		"Use this page to bulk clone all currently selected records.\n                    The cloned records will be created using\n                    the same existing values by default, but replaced with the values below\n                    where that value is ticked. Values that are not ticked will be cloned\n                    with their current value.\n                    The current number of selected records is {count}.",
		"Use this page to bulk clone all of the records in\n                    the currently selected view. The cloned records will be created using\n                    the same existing values by default, but replaced with the values below\n                    where that value is ticked. Values that are not ticked will be cloned\n                    with their current value. The current view is \"{view}\"",
	},
}

// xn is Log::Report __xn: the singular or plural of a message by count
func xn(singular, plural string, count int, typ string) string {
	msg := plural
	if count == 1 {
		msg = singular
	}
	msg = strings.ReplaceAll(msg, "{_count}", fmtInt(int64(count)))
	return strings.ReplaceAll(msg, "{type}", typ)
}

// any ['get','post'] => /:layout_name/bulk/:type/? (GADS.pm:4099)
func (s *Server) hBulk(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	view := c.CurrentView(layout)
	typ, _ := c.Param("type")
	lo := layout.Identifier()

	if !layout.UserCan("bulk_update") {
		c.ForwardHome("danger", "You do not have permission to perform bulk operations", lo+"/data")
	}
	if typ != "update" && typ != "clone" {
		gads.Fail("Invalid bulk type: %s", typ)
	}

	// The dummy record to test for updates
	record := s.E.NewRecord(layout, user)
	record.Initialise(0)

	// The records to update
	newRecords := func() *gads.Records {
		records := s.E.NewRecords(layout, user)
		records.View = view
		noGroup := false
		records.IsGroupOverride = &noGroup
		if search, ok := sessionSearch(c); ok {
			records.SetSearch(search)
		}
		// All columns are needed to be able to write the updated records
		var cols []int64
		for _, col := range layout.All(gads.AllOptions{}) {
			cols = append(cols, col.ID)
		}
		records.SetColumns(cols)
		records.ViewLimitExtraID = c.currentViewLimitExtraID(layout)
		records.ViewLimitOverrideID = c.currentViewLimitOverrideID(layout)
		return records
	}
	records := newRecords()
	limitIDs := c.query.getAll("id")
	if len(limitIDs) > 0 {
		records.SetLimitCurrentIDs(int64List(limitIDs))
	}

	if truthy(c.ParamS("submit")) {
		// See which ones to update
		failedInitial := false
		var updated []*gads.Column
		for _, col := range record.EditColumns(gads.EditColumnsOptions{New: true, Bulk: typ}) {
			newv := c.body.getAll(col.Field())
			included, _ := c.BodyParam("bulk_inc_" + fmtInt(col.ID))
			if strings.Join(newv, "") != "" && !truthy(included) {
				c.Warning(`Field "` + col.Name + `" contained a submitted value but was not checked to be included`)
			}
			if !truthy(included) {
				continue
			}
			datum := record.Fields()[col.ID]
			success := c.Process(func() { datum.SetValue(stringsToAny(newv), gads.SetValueOptions{Bulk: true}) })
			if success {
				updated = append(updated, col)
			}
			failedInitial = failedInitial || !success
		}
		if !failedInitial {
			var success, failures int
			for update := records.Single(); update != nil; update = records.Single() {
				if typ == "clone" {
					update.RemoveID()
				}
				failed := false
				for _, col := range updated {
					newv := stringsToAny(c.body.getAll(col.Field()))
					if failed = !c.Process(func() { update.Fields()[col.ID].SetValue(newv, gads.SetValueOptions{Bulk: true}) }); failed {
						break
					}
				}
				for _, col := range layout.All(gads.AllOptions{HasCache: true}) {
					if cd, ok := update.Fields()[col.ID].(gads.CodeDatum); ok {
						cd.ReEvaluate(gads.ReEvalOptions{Force: true})
					}
				}
				if !failed {
					// force_mandatory skips "was previously blank" warnings. No
					// records will be made blank, as they would not be written
					if c.Process(func() { update.Write(gads.WriteOptions{ForceMandatory: true}) }) {
						success++
					} else {
						failures++
					}
				} else {
					failures++
				}
			}
			switch {
			case success == 0 && failures == 0:
				c.Notice("No updates have been made")
			case failures == 0:
				c.ForwardHome("success", xn("{_count} record was {type}d successfully", "{_count} records were {type}d successfully", success, typ), lo+"/data")
			default:
				c.Mistake(xn("{_count} record was {type}d successfully", "{_count} records were {type}d successfully", success, typ) +
					xn(", {_count} record failed to be {type}d", ", {_count} records failed to be {type}d", failures, typ))
			}
		}
	}

	viewName := "All data"
	if view != nil {
		viewName = view.Name
	}
	// The number of records in the view, as a sanity check for the user
	count := records.Count()
	countMsg := xn(", which contains 1 record.", ", which contains {_count} records.", count, "")
	notices := bulkNotices[typ]
	var msg string
	if search, _ := sessionSearch(c); truthy(search) {
		msg = strings.ReplaceAll(notices[0], "{search}", search)
	} else if len(limitIDs) > 0 {
		msg = strings.ReplaceAll(notices[1], "{count}", fmtInt(int64(len(limitIDs))))
	} else {
		msg = strings.ReplaceAll(notices[2], "{view}", viewName)
	}
	if len(limitIDs) == 0 {
		msg += countMsg
	}
	c.Notice(msg)

	base := c.Base()
	return c.Template("edit", tt.HashOf(
		"view", c.ttView(view),
		"record", c.recordPresentation(record, recordPresentOpts{edit: true, new: true, bulk: typ}),
		"bulk_type", typ,
		"page", "bulk",
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/data",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb("", "Bulk "+typ+" records"),
		),
	), nil)
}

func stringsToAny(vals []string) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return out
}
