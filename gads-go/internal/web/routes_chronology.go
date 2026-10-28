package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// get '/record_body/:id' (GADS.pm:1834): the body of a record (or of one
// of its versions) for a modal
func (s *Server) hRecordBody(c *Ctx) string {
	idS, _ := c.RouteParam("id")
	id, _ := toInt64(idS)
	versionID, _ := toInt64(c.ParamS("version_id"))
	record := s.E.NewRecordUnattached(c.LoggedInUser())
	record.Rewind = sessionRewind(c)
	if err := gads.Try(func() {
		if versionID != 0 {
			record.FindRecordID(versionID, gads.FindOptions{})
		} else {
			record.FindCurrentID(id, gads.FindOptions{})
		}
	}); err != nil {
		c.auditLog()
		panic(err)
	}
	c.SetVar("layout", record.Layout())
	c.auditLog()
	params, opts, raw := s.processEdit(c, id, record)
	if raw != "" {
		return raw
	}
	if opts == nil {
		opts = &TemplateOptions{}
	}
	opts.NoLayout = true
	opts.Layout = ""
	// Assume modal if loaded via this route
	params.Set("view_modal", int64(1))
	return c.Template("edit", params, opts)
}

// get '/chronology/:id?' (GADS.pm:1869): the changes made in each version
// of a record
func (s *Server) hChronology(c *Ctx) string {
	idS, _ := c.RouteParam("id")
	id, _ := toInt64(idS)
	record := s.E.NewRecordUnattached(c.LoggedInUser())
	if truthy(idS) && !reDigitsOnly.MatchString(idS) {
		gads.Fail("Invalid record ID %s", idS)
	}
	if record.FindChronologyID(id) == nil {
		// find_chronology_id returns without finding a record, so the
		// layout cannot be built
		gads.Panic("instance_id not set to create layout object")
	}
	layout := record.Layout()
	base := c.Base()
	versions := tt.NewList()
	for _, v := range record.Chronology() {
		changed := tt.NewList()
		for i, d := range v.Changed {
			col := d.Column()
			dp := c.datumPresentation(d)
			if cd, ok := d.(*gads.CurcommonDatum); ok && col.Type == "curval" && v.Previous[i] != nil {
				links := chronologyCurvalLinks(v.Previous[i].(*gads.CurcommonDatum), cd)
				dp.def("links", func() any { return c.curvalLinks(col, links) })
			}
			changed.Items = append(changed.Items, c.columnPresentation(col, presentOpts{record: record, datum: d, datumPres: dp}))
		}
		editor := tt.HashOf("id", nil, "as_string", "")
		if v.Editor != nil {
			editor = tt.HashOf("id", nilIfZero(v.Editor.ID()), "as_string", v.Editor.AsString())
		}
		dt := ""
		if v.Datetime != nil {
			dt = v.Datetime.AsString()
		}
		versions.Items = append(versions.Items, tt.HashOf(
			"editor", editor,
			"datetime", tt.HashOf("as_string", dt),
			"changed", changed,
		))
	}
	rec := newLazy().
		val("current_id", record.CurrentID()).
		val("chronology", versions)
	return c.Template("chronology", tt.HashOf(
		"record", rec,
		"page", "chronology",
		"content_block_custom_classes", "content-block--record",
		"header_type", "table_tabs",
		"header_back_url", base+layout.Identifier()+"/record/"+fmtInt(record.CurrentID()),
		"layout", c.ttLayout(layout),
		"layout_obj", c.ttLayout(layout),
	), nil)
}

// chronologyCurvalLinks are the values of a curval changed in a version:
// those removed and changed (in the order of the old values), then those
// added
func chronologyCurvalLinks(old, cur *gads.CurcommonDatum) []curvalLink {
	newIDs := map[int64]gads.CurcommonValue{}
	var newOrder []int64
	for _, v := range cur.Values() {
		if _, ok := newIDs[v.ID]; !ok {
			newOrder = append(newOrder, v.ID)
		}
		newIDs[v.ID] = v
	}
	var links []curvalLink
	for _, o := range old.Values() {
		n, ok := newIDs[o.ID]
		if !ok {
			links = append(links, curvalLink{value: o, status: "Removed", versionID: o.Record.RecordID()})
			continue
		}
		delete(newIDs, o.ID)
		if o.Value == n.Value {
			continue
		}
		links = append(links, curvalLink{value: n, status: "Changed", versionID: n.Record.RecordID()})
	}
	for _, id := range newOrder {
		if n, ok := newIDs[id]; ok {
			links = append(links, curvalLink{value: n, status: "Added", versionID: n.Record.RecordID()})
		}
	}
	return links
}
