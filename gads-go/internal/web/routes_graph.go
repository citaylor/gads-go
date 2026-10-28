package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// any ['get','post'] => /:layout_name/graph/:id (GADS.pm:3195)
func (s *Server) hGraph(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	id, _ := c.Param("id")
	var graph *gads.Graph
	if truthy(id) {
		graph = s.E.LoadGraphLazy(id, layout, user)
	} else {
		graph = s.E.NewGraph(layout, user)
	}
	param := func(k string) any {
		if v, ok := c.Param(k); ok {
			return v
		}
		return nil
	}
	lo := layout.Identifier()
	if v, _ := c.Param("delete"); truthy(v) {
		if c.Process(graph.Delete) {
			c.ForwardHome("success", "The graph has been deleted successfully", lo+"/graphs")
		}
	}
	submitted := false
	if v, _ := c.Param("submit"); truthy(v) {
		submitted = true
		graph.SetForm(param)
		if c.Process(graph.Write) {
			action := "created"
			if truthy(id) {
				action = "updated"
			}
			c.ForwardHome("success", "Graph has been "+action+" successfully", lo+"/graphs")
		}
	}
	if graph.LoadErr() != "" && !submitted {
		// The graph is first needed while rendering the template, once the
		// messages have been taken for the page
		c.SessionSet("messages", []*Message{})
		gads.Fail("%s", graph.LoadErr())
	}
	base := c.Base()
	iid, _ := toInt64(c.persistent()["instance_id"])
	metricGroups := tt.NewList()
	for _, mg := range s.E.MetricGroups(iid) {
		metricGroups.Items = append(metricGroups.Items, ttMetricGroup(mg))
	}
	last := "Add graph"
	if truthy(id) {
		last = "Edit graph"
	}
	return c.Template("graph", tt.HashOf(
		"layout", c.ttLayout(layout),
		"page", "graph",
		"graph", c.ttGraph(graph),
		"metric_groups", metricGroups,
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/graphs",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb(base+lo+"/graphs", "Manage graphs"),
			crumb("", last),
		),
	), nil)
}

// any ['get','post'] => /:layout_name/graphs/? (GADS.pm:4411)
func (s *Server) hGraphs(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	user := c.LoggedInUser()
	lo := layout.Identifier()
	if v, _ := c.Param("graphsubmit"); truthy(v) {
		if c.Process(func() { user.SetGraphs(layout.InstanceID, c.body.getAll("graphs")) }) {
			c.ForwardHome("success", "The selected graphs have been updated", lo+"/data")
		}
	}
	graphs := tt.NewList()
	for _, g := range s.E.AllGraphs(layout, user) {
		graphs.Items = append(graphs.Items, c.ttGraph(g))
	}
	base := c.Base()
	return c.Template("graphs", tt.HashOf(
		"graphs", graphs,
		"page", "graphs",
		"header_type", "table_title",
		"content_block_custom_classes", "content-block--footer",
		"header_back_url", base+lo+"/data",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb("", "Manage graphs"),
		),
	), nil)
}
