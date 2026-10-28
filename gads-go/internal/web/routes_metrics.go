package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// metricLayout is the table of the metric routes, which need permission to
// administer it
func (c *Ctx) metricLayout() *gads.Layout {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	if !layout.UserCan("layout") {
		c.ForwardHome("danger", "You do not have permission to manage metrics", "")
	}
	return layout
}

func (c *Ctx) metricCrumbs(layout *gads.Layout, last ...*tt.Hash) *tt.List {
	base := c.Base()
	l := tt.NewList(
		crumb(base+"table/", "Tables"),
		crumb(base+layout.Identifier()+"/data", "Table: "+layout.Name()),
	)
	for _, h := range last {
		l.Items = append(l.Items, h)
	}
	return l
}

// ttMetricGroup is a GADS::MetricGroup object
func ttMetricGroup(mg *gads.MetricGroup) *ttLazy {
	return newLazy().
		def("id", func() any { return ttValue(mg.ID) }).
		def("name", func() any { return ttValue(mg.Name()) }).
		def("metrics", func() any {
			l := tt.NewList()
			for _, m := range mg.Metrics() {
				l.Items = append(l.Items, newRow("metric", m))
			}
			return l
		})
}

// get '/:layout_name/metrics/?' (GADS.pm:3259)
func (s *Server) hMetrics(c *Ctx) string {
	layout := c.metricLayout()
	metrics := tt.NewList()
	for _, mg := range s.E.MetricGroups(layout.InstanceID) {
		metrics.Items = append(metrics.Items, ttMetricGroup(mg))
	}
	return c.Template("metrics", tt.HashOf(
		"layout", c.ttLayout(layout),
		"page", "metrics",
		"metrics", metrics,
		"header_type", "table_title",
		"header_back_url", c.Base()+layout.Identifier()+"/data",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", c.metricCrumbs(layout, crumb("", "Metrics")),
	), nil)
}

// any ['get','post'] => /:layout_name/metric/:id (GADS.pm:3293)
func (s *Server) hMetric(c *Ctx) string {
	layout := c.metricLayout()
	id, _ := c.Param("id")
	mg := s.E.NewMetricGroup(id, layout.InstanceID)
	param := func(k string) any {
		if v, ok := c.Param(k); ok {
			return v
		}
		return nil
	}
	lo := layout.Identifier()
	if v, _ := c.Param("delete_all"); truthy(v) {
		if c.Process(mg.Delete) {
			c.ForwardHome("success", "The metric has been deleted successfully", lo+"/metrics")
		}
	}
	if v, _ := c.Param("delete_metric"); truthy(v) {
		if c.Process(func() { mg.DeleteMetric(param("metric_id")) }) {
			c.ForwardHome("success", "The metric has been deleted successfully", lo+"/metric/"+id)
		}
	}
	action := "created"
	if truthy(id) {
		action = "updated"
	}
	if v, _ := c.Param("submit"); truthy(v) {
		mg.SetName(param("name"))
		if c.Process(mg.Write) {
			c.ForwardHome("success", "Metric has been "+action+" successfully", lo+"/metrics")
		}
	}
	if v, _ := c.Param("update_metric"); truthy(v) {
		in := gads.MetricInput{
			ID:                 param("metric_id"),
			MetricGroupID:      id,
			XAxisValue:         param("x_axis_value"),
			YAxisGroupingValue: param("y_axis_grouping_value"),
			Target:             param("target"),
		}
		if !truthy(in.ID) {
			in.ID = nil
		}
		if c.Process(func() { s.E.WriteMetric(in) }) {
			c.ForwardHome("success", "Metric has been "+action+" successfully", lo+"/metric/"+id)
		}
	}
	last := "Add metric"
	if truthy(id) {
		last = "Edit metric"
	}
	return c.Template("metric", tt.HashOf(
		"layout", c.ttLayout(layout),
		"page", "metric",
		"metricgroup", ttMetricGroup(mg),
		"header_type", "table_title",
		"header_back_url", c.Base()+lo+"/metrics",
		"layout_obj", c.ttLayout(layout),
		"breadcrumbs", c.metricCrumbs(layout, crumb(c.Base()+lo+"/metrics", "Metrics"), crumb("", last)),
	), nil)
}
