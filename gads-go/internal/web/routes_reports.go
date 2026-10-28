package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// reportLayout is the table of the report routes, which need permission to
// administer it
func (c *Ctx) reportLayout() *gads.Layout {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	if !layout.UserCan("layout") {
		c.ForwardHome("danger", "You do not have permission to edit reports", "")
	}
	return layout
}

// reportTokens are the common tokens of the report pages, with the
// breadcrumbs of the page (the report list, or one of the reports with its
// own crumb such as "Add Report")
func (c *Ctx) reportTokens(layout *gads.Layout, page string) *tt.Hash {
	base := c.Base()
	crumbs := tt.NewList(
		crumb(base+"table/", "Tables"),
		crumb(base+layout.Identifier()+"/data", "Table: "+layout.Name()),
	)
	if page == "" {
		crumbs.Items = append(crumbs.Items, crumb("", "Reports"))
	} else {
		crumbs.Items = append(crumbs.Items, crumb(base+layout.Identifier()+"/report", "Reports"), crumb("", page))
	}
	return tt.HashOf(
		"header_type", "table_tabs",
		"layout_obj", c.ttLayout(layout),
		"header_back_url", base+"table",
		"breadcrumbs", crumbs,
	)
}

// reportInput is the report values of the edit form
func (c *Ctx) reportInput() gads.ReportInput {
	get := func(k string) any {
		if v, ok := c.BodyParam(k); ok {
			return v
		}
		return nil
	}
	return gads.ReportInput{
		Name: get("report_name"), Title: get("report_title"), Description: get("report_description"),
		SecurityMarking: get("security_marking"),
		Layouts:         c.body.getAll("checkboxes"), Groups: c.body.getAll("groups"),
	}
}

func (c *Ctx) ttGroupRows(user *gads.User) *tt.List {
	l := tt.NewList()
	for _, g := range user.GroupsViewableRows() {
		l.Items = append(l.Items, newRow("group", g))
	}
	return l
}

// any ['get','post'] => /:layout_name/report (GADS.pm:2860)
func (s *Server) hReports(c *Ctx) string {
	layout := c.reportLayout()
	lo, _ := c.RouteParam("layout_name")
	if del, _ := c.BodyParam("delete"); truthy(del) {
		row := s.E.DB.QueryRow(`SELECT * FROM "report" WHERE "id" = ?`, del)
		if row == nil {
			gads.Fail("No report found for %s", del)
		}
		if c.Process(func() { s.E.RemoveReport(row) }) {
			c.ForwardHome("success", "Report deleted", lo+"/report")
		}
		// forwardHome is called with the page as the message
		gads.Panic(`Can't use string ("%s/report") as a HASH ref while "strict refs" in use`, lo)
	}
	if v, _ := c.BodyParam("submit"); truthy(v) {
		if c.Process(func() {
			marking, ok := c.BodyParam("security_marking")
			if ok {
				layout.SetMarking(marking)
			} else {
				layout.SetMarking(nil)
			}
		}) {
			c.ForwardHome("success", "Security marking updated", lo+"/report")
		}
	}
	reports := tt.NewList()
	for _, r := range layout.AllReports() {
		reports.Items = append(reports.Items, newRow("report", r))
	}
	tokens := c.reportTokens(layout, "")
	tokens.Set("reports", reports)
	tokens.Set("security_marking", layout.SecurityMarking())
	return c.Template("reports/view", tokens, nil)
}

// any ['get','post'] => /:layout_name/report/add (GADS.pm:2914)
func (s *Server) hReportAdd(c *Ctx) string {
	layout := c.reportLayout()
	user := c.LoggedInUser()
	if v, _ := c.BodyParam("submit"); truthy(v) {
		s.E.CreateReport(user, layout.InstanceID, c.reportInput())
		lo, _ := c.RouteParam("layout_name")
		c.ForwardHome("success", "Report created", lo+"/report")
	}
	tokens := c.reportTokens(layout, "Add Report")
	tokens.Set("layout", c.ttLayout(layout))
	tokens.Set("viewtype", "add")
	tokens.Set("fields", c.ttColumns(layout.All(gads.AllOptions{UserCanRead: true})))
	tokens.Set("groups", c.ttGroupRows(user))
	return c.Template("reports/edit", tokens, nil)
}

// any ['get','post'] => /:layout_name/report/edit:id (GADS.pm:2972)
func (s *Server) hReportEdit(c *Ctx) string {
	layout := c.reportLayout()
	user := c.LoggedInUser()
	id, _ := c.Param("id")
	if v, _ := c.BodyParam("submit"); truthy(v) {
		report := s.E.LoadReportForEdit(id)
		s.E.UpdateReport(report, c.reportInput())
		lo, _ := c.RouteParam("layout_name")
		c.ForwardHome("success", "Report updated", lo+"/report")
	}
	report := s.E.LoadReportForEdit(id)
	checked := map[int64]bool{}
	for _, l := range s.E.ReportLayoutIDs(report["id"]) {
		checked[l] = true
	}
	fields := tt.NewList()
	for _, col := range layout.All(gads.AllOptions{UserCanRead: true}) {
		var isChecked any
		if checked[col.ID] {
			isChecked = int64(1)
		}
		fields.Items = append(fields.Items, tt.HashOf("id", col.ID, "name", col.Name, "is_checked", isChecked))
	}
	rep := newRow("report", report)
	groupIDs := s.E.ReportGroupIDs(report["id"])
	rep.set("group_ids", func([]any) any { return ttValue(groupIDs) })
	tokens := c.reportTokens(layout, "Edit Report")
	tokens.Set("layout", c.ttLayout(layout))
	tokens.Set("report", rep)
	tokens.Set("fields", fields)
	tokens.Set("viewtype", "edit")
	tokens.Set("groups", c.ttGroupRows(user))
	return c.Template("reports/edit", tokens, nil)
}
