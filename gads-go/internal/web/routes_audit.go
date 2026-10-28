package web

import (
	"time"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// any '/settings/audit/?' (GADS.pm:1364): the audit log
func (s *Server) hAudit(c *Ctx) string {
	if truthy(c.ParamS("audit_filtering")) {
		f := map[string]any{}
		for _, k := range []string{"method", "type", "user", "from", "to"} {
			if v, ok := c.Param(k); ok {
				f[k] = v
			} else {
				f[k] = nil
			}
		}
		c.SessionSet("audit_filtering", f)
	} else if _, ok := c.Param("clear"); ok {
		c.SessionSet("audit_filtering", nil)
	}
	stored, _ := c.SessionGet("audit_filtering").(map[string]any)
	if stored == nil {
		stored = map[string]any{}
	}
	filtering := s.E.NewAuditFiltering(stored)

	if _, ok := c.Param("download"); ok {
		csv := s.E.AuditCSV(filtering)
		now := s.E.Now().Format("2006-01-02T15:04:05")
		header := ""
		if h, _ := c.s.Config.Gads["header"].(string); h != "" {
			csv = h + "\n" + csv
			header = "-" + h
		}
		return c.SendFile([]byte(csv), `text/csv; charset="utf-8"`, now+header+".csv")
	}

	logs := tt.NewList()
	for _, l := range s.E.AuditLogs(filtering) {
		var user any
		if l.User != nil {
			user = newRow("user", l.User)
		}
		logs.Items = append(logs.Items, tt.HashOf(
			"id", l.ID,
			"type", l.Type,
			"datetime", l.Datetime,
			"description", ttValue(l.Description),
			"user", user,
		))
	}
	filterUser := ttValue(filtering.User)
	if truthy(filtering.User) {
		filterUser = nil
		if n, ok := toInt64(fmtAny(filtering.User)); ok {
			if u := s.E.FindListItem("user", n); u != nil {
				filterUser = newRow("user", u)
			}
		}
	}
	types := tt.NewList()
	for _, t := range gads.AuditTypes {
		types.Items = append(types.Items, t)
	}
	return c.Template("admin/audit", tt.HashOf(
		"logs", logs,
		"filtering", tt.HashOf(
			"method", ttValue(filtering.Method),
			"type", ttValue(filtering.Type),
			"user", ttValue(filtering.User),
			"from", &ttDateTime{t: filtering.From.In(time.UTC)},
			"to", &ttDateTime{t: filtering.To.In(time.UTC)},
		),
		"filter_user", filterUser,
		"audit_types", types,
		"page", "audit",
	), nil)
}
