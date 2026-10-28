package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// any ['get','post'] => /:layout_name/link/:id? (GADS.pm:3985)
func (s *Server) hLink(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	idS, _ := c.Param("id")
	record := s.E.NewRecord(layout, c.LoggedInUser())
	if truthy(idS) {
		id, _ := toInt64(idS)
		record.FindCurrentID(id, gads.FindOptions{})
	}
	if truthy(c.ParamS("submit")) {
		raw, has := c.Param("linked_id")
		linkedID, isInt := toInt64(raw)
		writeLinked := func() {
			if has && !isInt {
				// The value is bound to the database as an integer, which
				// fails (after any blanking and writing of the values,
				// which are rolled back)
				if !layout.UserCan("link") {
					gads.Fail("You do not have permission to link records")
				}
				gads.Panic("datatype mismatch: bind param (0) %s as integer", raw)
			}
			record.WriteLinkedID(linkedID)
		}
		var result bool
		if truthy(idS) {
			result = c.Process(writeLinked)
		} else {
			record.Initialise(0)
			result = c.Process(func() { record.Write(gads.WriteOptions{}) }) && c.Process(writeLinked)
		}
		if result {
			c.ForwardHome("success", "Record has been linked successfully", layout.Identifier()+"/data")
		}
	}
	base := c.Base()
	lo := layout.Identifier()
	return c.Template("link", tt.HashOf(
		"record", &ttRawRecord{c: c, r: record},
		"page", "link",
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/data",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb("", "Add a linked record"),
		),
	), nil)
}
