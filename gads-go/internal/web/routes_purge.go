package web

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// deletedRecords are the deleted records of a table, with their children
// and no extra view limit (the Records of the purge route)
func (s *Server) deletedRecords(layout *gads.Layout, user *gads.User) *gads.Records {
	records := s.E.NewRecords(layout, user)
	records.SetColumns([]int64{})
	records.IsDeleted = true
	records.SetIncludeChildren(true)
	// view_limit_extra_id => undef overrides any value that may be set (the
	// default still applies to users who cannot change it)
	zero := int64(0)
	records.ViewLimitExtraID = &zero
	return records
}

// any ['get','post'] => /:layout_name/purge/? (GADS.pm:3140)
func (s *Server) hPurge(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	if !layout.UserCan("purge") {
		c.ForwardHome("danger", "You do not have permission to manage deleted records", "")
	}
	purge, _ := c.Param("purge")
	restore, _ := c.Param("restore")
	if truthy(purge) || truthy(restore) {
		var ids []int64
		for _, v := range c.body.getAll("record") {
			id, _ := toInt64(v)
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			c.ForwardHome("danger", "Please select some records before clicking an action", layout.Identifier()+"/purge")
		}
		records := s.deletedRecords(layout, user)
		records.SetLimitCurrentIDs(ids)
		if truthy(purge) {
			for record := records.Single(); record != nil; record = records.Single() {
				record.PurgeCurrent()
			}
			c.ForwardHome("success", "Records have now been purged", layout.Identifier()+"/purge")
		}
		for record := records.Single(); record != nil; record = records.Single() {
			record.Restore()
		}
		c.ForwardHome("success", "Records have now been restored", layout.Identifier()+"/purge")
	}
	// presentation(purge => 1): only the IDs and deletion of each record
	list := tt.NewList()
	for _, record := range s.deletedRecords(layout, user).Results() {
		var deleted, deletedby any
		if t := record.Deleted(); t != nil {
			deleted = &ttDateTime{t: *t}
		}
		if p := record.Deletedby(); p != nil {
			deletedby = p.AsString()
		}
		list.Items = append(list.Items, newLazy().
			val("current_id", record.CurrentID()).
			val("deleted", deleted).
			val("deletedby", deletedby))
	}
	base := c.Base()
	return c.Template("purge", tt.HashOf("page", "purge", "records", list,
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+layout.Identifier()+"/data", "Table: "+layout.Name()),
			crumb("", "Purge records"),
		)), nil)
}

// any ['get','post'] => /:layout_name/historic_purge/ (GADS.pm:3073)
func (s *Server) hHistoricPurge(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	if !layout.UserCan("purge") {
		c.ForwardHome("danger", "You do not have permission to access this page.", "")
	}
	user := c.LoggedInUser()
	records := s.E.NewRecords(layout, user)
	records.View = c.CurrentView(layout)
	var columns []*gads.Column
	for _, col := range records.ColumnsRender() {
		if !col.Internal {
			columns = append(columns, col)
		}
	}
	selected := tt.NewHash()
	if _, ok := c.Param("stage1"); ok {
		for _, id := range c.body.getAll("column_id") {
			selected.Set(id, int64(1))
		}
		var chosen []*gads.Column
		for _, col := range columns {
			if v, _ := selected.Get(fmtInt(col.ID)); truthy(v) {
				chosen = append(chosen, col)
			}
		}
		columns = chosen
		tableData := tt.NewList()
		for record := records.Single(); record != nil; record = records.Single() {
			values := tt.NewHash()
			for _, col := range columns {
				d := record.GetFieldValue(col)
				v, _ := c.datumPresentation(d).get("value")
				if !truthy(v) {
					v = "No value"
				}
				values.Set(fmtInt(col.ID), v)
			}
			tableData.Items = append(tableData.Items, tt.HashOf(
				"current_id", record.CurrentID(),
				"values", values,
			))
		}
		return c.Template("historic_purge/confirm", tt.HashOf(
			"table_data", tableData,
			"columns", c.ttColumns(columns),
		), nil)
	} else if _, ok := c.Param("purge"); ok {
		var currentIDs, layoutIDs []int64
		for _, v := range c.body.getAll("current_id") {
			id, _ := toInt64(v)
			currentIDs = append(currentIDs, id)
		}
		for _, v := range c.body.getAll("columns_selected") {
			selected.Set(v, int64(1))
			id, _ := toInt64(v)
			layoutIDs = append(layoutIDs, id)
		}
		if c.Process(func() {
			if len(currentIDs) == 0 {
				gads.Fail("Please select some values to delete")
			}
			if len(layoutIDs) == 0 {
				gads.Fail("Please select some layouts to delete")
			}
			// search({id => $current_ids}): each current record once, in
			// the order of the database
			for _, cid := range s.E.DB.QueryInt64s(`SELECT "id" FROM "current" WHERE "id" IN (` + sqlInts(currentIDs) + `) ORDER BY "id"`) {
				s.E.HistoricPurge(cid, user, layoutIDs)
			}
		}) {
			c.ForwardHome("success", "Records have now been purged", layout.Identifier()+"/data")
		}
	}
	return c.Template("historic_purge/initial", tt.HashOf(
		"columns_view", c.ttColumns(columns),
		"count", int64(records.Count()),
		"columns_selected", selected,
	), nil)
}

// any ['get','post'] => /:layout_name/tree:any?/:layout_id/? (GADS.pm:3043)
func (s *Server) hTree(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	idS, _ := c.RouteParam("layout_id")
	id, _ := toInt64(idS)
	tree := layout.Column(id)
	if tree == nil {
		gads.Fail("Invalid tree ID %s", idS)
	}
	if data, _ := c.Param("data"); truthy(data) {
		if !layout.UserCan("layout") {
			c.ForwardHome("danger", "You do not have permission to edit trees", "")
		}
		v, err := gads.DecodeJSON(data)
		if err != nil {
			gads.Panic("%s", err.Error())
		}
		var nodes []map[string]any
		for _, n := range v.([]any) {
			if m, ok := n.(map[string]any); ok {
				nodes = append(nodes, m)
			}
		}
		tree.TreeUpdate(nodes)
		return ""
	}
	var ids []int64
	for _, v := range c.query.getAll("ids") {
		i, _ := toInt64(v)
		ids = append(ids, i)
	}
	var out any = []any{}
	if tree.Type == "tree" {
		out = tree.TreeJSON(ids...)
	}
	c.Header("Cache-Control", "max-age=0, must-revalidate, private")
	c.ContentType("application/json")
	return gads.EncodeJSON(out)
}

// sqlInts is a list of IDs for an SQL IN clause
func sqlInts(ids []int64) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = fmtInt(id)
	}
	return strings.Join(s, ",")
}
