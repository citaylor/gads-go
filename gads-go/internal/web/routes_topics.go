package web

import (
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// topicLayout is the table of the topic routes, which need permission to
// administer it
func (c *Ctx) topicLayout() *gads.Layout {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	if !layout.UserCan("layout") {
		c.ForwardHome("danger", "You do not have permission to manage topics", "")
	}
	return layout
}

// tableTopics are the topics of a table (in the order of the database)
func (s *Server) tableTopics(instanceID int64) *tt.List {
	l := tt.NewList()
	for _, r := range s.E.DB.Query(`SELECT * FROM "topic" "me" WHERE "me"."instance_id" = ?`, instanceID) {
		l.Items = append(l.Items, newRow("topic", r))
	}
	return l
}

// any ['get','post'] => /:layout_name/topic/:id (GADS.pm:3382)
func (s *Server) hTopic(c *Ctx) string {
	layout := c.topicLayout()
	instanceID := layout.InstanceID
	id, _ := c.Param("id")
	var topic db.Row
	if truthy(id) {
		topic = s.E.DB.QueryRow(`SELECT * FROM "topic" "me" WHERE "me"."id" = ? AND "me"."instance_id" = ? LIMIT 1`, id, instanceID)
		if topic == nil {
			gads.Fail("Topic ID %s not found", id)
		}
	}
	param := func(k string) any {
		if v, ok := c.Param(k); ok {
			return v
		}
		return nil
	}
	lo := layout.Identifier()
	if v, _ := c.Param("submit"); truthy(v) {
		initial := param("initial_state")
		if !truthy(initial) {
			initial = "collapsed"
		}
		prevent := param("prevent_edit_topic_id")
		if !truthy(prevent) {
			prevent = nil
		}
		set := map[string]any{
			"name":                  param("name"),
			"description":           param("description"),
			"click_to_edit":         param("click_to_edit"),
			"initial_state":         initial,
			"prevent_edit_topic_id": prevent,
		}
		if c.Process(func() {
			if topic == nil {
				set["instance_id"] = instanceID
				s.E.DB.InsertMap("topic", set)
			} else {
				s.E.DB.UpdateMap("topic", set, map[string]any{"id": topic["id"]})
			}
		}) {
			action := "created"
			if truthy(id) {
				action = "updated"
			}
			c.ForwardHome("success", "Topic has been "+action+" successfully", lo+"/topics")
		}
	}
	if v, _ := c.Param("delete"); truthy(v) {
		if topic == nil {
			gads.Panic(`Can't call method "delete" on an undefined value`)
		}
		if c.Process(func() {
			// before_delete: the fields of the topic are removed from it
			s.E.DB.Exec(`UPDATE "layout" SET "topic_id" = NULL WHERE "topic_id" = ?`, topic["id"])
			s.E.DB.Exec(`DELETE FROM "topic" WHERE "id" = ?`, topic["id"])
		}) {
			c.ForwardHome("success", "The topic has been deleted successfully", lo+"/topics")
		}
	}
	base := c.Base()
	page, last := "topic_add", "Add a topic"
	var topicTok any
	if truthy(id) {
		page, last = "topic_edit", "Edit topic: "+fmtAny(topic["name"])
		topicTok = newRow("topic", topic)
	}
	return c.Template("topic", tt.HashOf(
		"page", page,
		"content_block_custom_classes", "content-block--footer",
		"header_type", "table_title",
		"header_back_url", base+lo+"/topics",
		"layout_obj", c.ttLayout(layout),
		"topic", topicTok,
		"topics", s.tableTopics(instanceID),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb(base+lo+"/topics", "Topics"),
			crumb("", last),
		),
	), nil)
}

// get '/:layout_name/topics/?' (GADS.pm:3445)
func (s *Server) hTopics(c *Ctx) string {
	layout := c.topicLayout()
	base := c.Base()
	lo := layout.Identifier()
	return c.Template("topics", tt.HashOf(
		"page", "topics",
		"content_block_custom_classes", "content-block--footer",
		"header_type", "table_tabs",
		"layout", c.ttLayout(layout),
		"layout_obj", c.ttLayout(layout),
		"topics", s.tableTopics(layout.InstanceID),
		"breadcrumbs", tt.NewList(
			crumb(base+"table/", "Tables"),
			crumb(base+lo+"/data", "Table: "+layout.Name()),
			crumb("", "Topics"),
		),
	), nil)
}
