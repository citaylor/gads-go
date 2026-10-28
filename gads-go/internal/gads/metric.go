package gads

import (
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
)

// MetricGroup is a port of GADS::MetricGroup: a named set of metrics of a
// table, the targets of graphs
type MetricGroup struct {
	e          *Engine
	InstanceID int64
	// ID is the ID as given (a string from a route), empty for a new group
	ID any

	row       db.Row
	rowLoaded bool
	name      any
	nameSet   bool
}

// NewMetricGroup is GADS::MetricGroup->new
func (e *Engine) NewMetricGroup(id any, instanceID int64) *MetricGroup {
	return &MetricGroup{e: e, ID: id, InstanceID: instanceID}
}

func idTrue(id any) bool {
	s := anyToString(id)
	return id != nil && s != "" && s != "0"
}

// rset is the group's row (_rset), checked to be of the table
func (m *MetricGroup) rset() db.Row {
	if !m.rowLoaded {
		m.rowLoaded = true
		if !idTrue(m.ID) {
			return nil
		}
		id := anyToString(m.ID)
		if !reDigits.MatchString(id) {
			Fail("Invalid ID format %s", id)
		}
		m.row = m.e.DB.QueryRow(`SELECT * FROM "metric_group" WHERE "id" = ? AND "instance_id" = ?`, id, m.InstanceID)
		if m.row == nil {
			Fail("Metric Group ID %s not found", id)
		}
	}
	return m.row
}

// Name is the name of the group (as set, or as written)
func (m *MetricGroup) Name() any {
	if !m.nameSet {
		m.nameSet = true
		if idTrue(m.ID) {
			m.name = m.rset()["name"]
		}
	}
	return m.name
}

// SetName sets the name to write
func (m *MetricGroup) SetName(v any) { m.name, m.nameSet = v, true }

// Metrics are the metrics of the group, by X axis value then Y axis
// grouping value
func (m *MetricGroup) Metrics() []db.Row {
	return m.e.DB.Query(`SELECT "me".* FROM "metric" "me" LEFT JOIN "metric_group" "metric_group" ON "metric_group"."id" = "me"."metric_group" WHERE "me"."metric_group" = ? AND "metric_group"."instance_id" = ? ORDER BY "x_axis_value", "y_axis_grouping_value"`, m.ID, m.InstanceID)
}

// Write writes the group's name, creating the group if new
func (m *MetricGroup) Write() {
	name := m.Name()
	if !perlTrue(name) {
		Fail("Please enter a name for the metric")
	}
	if idTrue(m.ID) {
		m.e.DB.UpdateMap("metric_group", map[string]any{"name": name}, map[string]any{"id": m.rset()["id"]})
		return
	}
	m.ID = m.e.DB.InsertMap("metric_group", map[string]any{"name": name, "instance_id": m.InstanceID})
}

// Delete deletes the group and its metrics, unless used by graphs
func (m *MetricGroup) Delete() {
	var names []string
	for _, g := range m.e.DB.Query(`SELECT "title" FROM "graph" WHERE "metric_group" = ?`, m.ID) {
		names = append(names, anyToString(g["title"]))
	}
	if len(names) > 0 {
		Fail("The metric is being used in the following graphs: %s. Please remove it first.", strings.Join(names, ", "))
	}
	m.e.DB.Exec(`DELETE FROM "metric" WHERE "metric_group" = ?`, m.ID)
	row := m.rset()
	if row == nil {
		Panic(`Can't call method "delete" on an undefined value`)
	}
	m.e.DB.Exec(`DELETE FROM "metric_group" WHERE "id" = ?`, row["id"])
}

// DeleteMetric deletes a metric of the group
func (m *MetricGroup) DeleteMetric(metricID any) {
	m.e.DB.Exec(`DELETE FROM "metric" WHERE "id" = ? AND "metric_group" = ?`, metricID, m.ID)
}

// MetricInput are the values of a metric to write (GADS::Metric)
type MetricInput struct {
	ID, MetricGroupID, XAxisValue, YAxisGroupingValue, Target any
}

// WriteMetric is GADS::Metric->write: a metric created or updated
func (e *Engine) WriteMetric(in MetricInput) {
	if !reDigits.MatchString(anyToString(in.Target)) {
		Fail("Please enter an integer value for the target")
	}
	var id any
	if idTrue(in.ID) {
		s := anyToString(in.ID)
		if !reDigits.MatchString(s) {
			Fail("Invalid id %s", s)
		}
		row := e.DB.QueryRow(`SELECT "id" FROM "metric" WHERE "id" = ?`, s)
		if row == nil {
			Fail("Metric ID %s not found", s)
		}
		id = row["id"]
	} else {
		if !reDigits.MatchString(anyToString(in.MetricGroupID)) {
			Fail("Invalid metric group ID %s", anyToString(in.MetricGroupID))
		}
		id = e.DB.InsertMap("metric", map[string]any{"metric_group": in.MetricGroupID})
	}
	e.DB.UpdateMap("metric", map[string]any{
		"metric_group":          in.MetricGroupID,
		"x_axis_value":          in.XAxisValue,
		"y_axis_grouping_value": in.YAxisGroupingValue,
		"target":                in.Target,
	}, map[string]any{"id": id})
}

// MetricGroups are the metric groups of a table, by name
func (e *Engine) MetricGroups(instanceID int64) []*MetricGroup {
	var out []*MetricGroup
	for _, r := range e.DB.Query(`SELECT "me".* FROM "metric_group" "me" WHERE "me"."instance_id" = ? ORDER BY "me"."name"`, instanceID) {
		mg := e.NewMetricGroup(r["id"], instanceID)
		mg.SetName(r["name"])
		out = append(out, mg)
	}
	return out
}
