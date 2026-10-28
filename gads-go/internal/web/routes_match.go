package web

import (
	"errors"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// get '/helptext/:id?' (GADS.pm:1656): the help text of a column of the
// first table
func (s *Server) hHelptext(c *Ctx) string {
	var col *gads.Column
	if all := c.Instances().All(); len(all) > 0 {
		if id, ok := toInt64(c.ParamS("id")); ok {
			col = all[0].Column(id)
		}
	}
	tokens := tt.HashOf()
	if col != nil {
		tokens.Set("column", c.ttColumn(col))
	} else {
		tokens.Set("column", nil)
	}
	return c.Template("helptext", tokens, &TemplateOptions{NoLayout: true})
}

// get '/match/user/' (GADS.pm:1956 and the table route GADS.pm:4451)
func (s *Server) hMatchUser(c *Ctx) string {
	c.ContentType("application/json")
	return gads.EncodeJSON(s.E.MatchUsers(c.ParamS("q")))
}

// get '/:layout_name/match/user/' (GADS.pm:4451)
func (s *Server) hLayoutMatchUser(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	if !layout.UserCan("layout") {
		gads.Fail("No access to search for users")
	}
	return s.hMatchUser(c)
}

// post '/:layout_name/match/layout/:layout_id' (GADS.pm:4489): the values
// of a column beginning with the query (typeahead)
func (s *Server) hMatchLayout(c *Ctx) string {
	layout := c.Layout()
	if layout == nil {
		c.Pass()
	}
	var col *gads.Column
	if id, ok := toInt64(c.ParamS("layout_id")); ok {
		col = layout.ColumnPerm(id, "read")
	}
	if col == nil {
		// Perl calls values_beginning_with on undef
		panic(errors.New(`Can't call method "values_beginning_with" on an undefined value`))
	}
	noempty, _ := c.QueryParam("noempty")
	useID, _ := c.QueryParam("use_id")
	records := []any{}
	for _, x := range col.ValuesBeginningWith(c.ParamS("q"), truthy(noempty), truthy(useID)) {
		if _, ok := x["id"]; ok {
			records = append(records, map[string]any{"id": x["id"], "label": x["label"]})
		} else {
			records = append(records, x["label"])
		}
	}
	c.ContentType("application/json")
	return gads.EncodeJSON(map[string]any{"error": int64(0), "records": records})
}
