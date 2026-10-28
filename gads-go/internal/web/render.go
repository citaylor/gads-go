package web

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// TemplateOptions are the options of Dancer2 template
type TemplateOptions struct {
	// NoLayout renders without the layout ({ layout => undef })
	NoLayout bool
	// Layout is the layout to use instead of main
	Layout string
}

// Template renders a view with the layout (Dancer2 template), running the
// GADS before_template hook.
func (c *Ctx) Template(view string, tokens *tt.Hash, opts *TemplateOptions) string {
	c.flushReports()
	if tokens == nil {
		tokens = tt.NewHash()
	}
	if opts == nil {
		opts = &TemplateOptions{}
	}
	c.prepareTokens(tokens)
	c.s.beforeTemplate(c, tokens)
	content := c.render(view, tokens)
	// after_template_render
	c.s.updatePersistent(c)
	if opts.NoLayout {
		return content
	}
	layout := opts.Layout
	if layout == "" {
		layout = "main"
	}
	c.prepareTokens(tokens)
	tokens.Set("content", content)
	full := c.render(filepath.Join("layouts", layout), tokens)
	// LogReport after_layout_render
	c.SessionSet("messages", []*Message{})
	return full
}

func (c *Ctx) render(view string, tokens *tt.Hash) string {
	if !strings.HasSuffix(view, ".tt") {
		view += ".tt"
	}
	out, err := c.s.TT.Process(filepath.Join(c.s.Views, view), tokens)
	if err != nil {
		gads.Panic("%s", err.Error())
	}
	return out
}

// prepareTokens adds the tokens added by Dancer2 (_prepare_tokens_options)
func (c *Ctx) prepareTokens(tokens *tt.Hash) {
	tokens.Set("perl_version", "v5.32.1")
	tokens.Set("dancer_version", "1.1.2")
	tokens.Set("settings", tt.HashOf("appname", "Linkspace", "charset", "UTF-8"))
	tokens.Set("request", &ttRequest{c: c})
	params := tt.NewHash()
	for _, mv := range []*multiValue{c.query, c.body} {
		for _, k := range mv.keys {
			vs := mv.vals[k]
			if len(vs) == 1 {
				params.Set(k, vs[0])
			} else {
				l := tt.NewList()
				for _, v := range vs {
					l.Items = append(l.Items, v)
				}
				params.Set(k, l)
			}
		}
	}
	for k, v := range c.route.params {
		if c.route.set[k] {
			params.Set(k, v)
		}
	}
	tokens.Set("params", params)
	vars := tt.NewHash()
	tokens.Set("vars", vars)
	tokens.Set("session", &ttSession{c: c})
}

// beforeTemplate is the GADS before_template hook
func (s *Server) beforeTemplate(c *Ctx, tokens *tt.Hash) {
	user := c.LoggedInUser()
	base := c.Base()
	if b, ok := tokens.Get("base"); ok && tt.True(b) {
		base = tt.Str(b)
	}
	u := tt.NewHash()
	if v, ok := tokens.Get("url"); ok {
		if h, ok := v.(*tt.Hash); ok {
			u = h
		}
	}
	u.Set("css", base+"css")
	u.Set("js", base+"js")
	page := base
	if i := strings.LastIndex(page, "/"); i >= 0 {
		page = page[i+1:]
	}
	u.Set("page", page)
	tokens.Set("url", u)
	if v, _ := tokens.Get("scheme"); !tt.True(v) {
		tokens.Set("scheme", c.Scheme())
	}
	tokens.Set("hostlocal", ttValue(s.Config.Gads["hostlocal"]))
	tokens.Set("header", ttValue(s.Config.Gads["header"]))

	layout := c.Layout()
	if user != nil && layout != nil && (layout.UserCan("approve_new") || layout.UserCan("approve_existing")) {
		tokens.Set("user_can_approve", int64(1))
		tokens.Set("approve_waiting", int64(s.E.ApprovalCount(layout, user)))
	}
	if user != nil {
		instances := c.Instances()
		all := tt.NewList()
		for _, l := range instances.All() {
			all.Items = append(all.Items, c.ttLayout(l))
		}
		tokens.Set("instances", all)
		if layout != nil {
			tokens.Set("instance_name", layout.Name())
		}
		tokens.Set("user", c.ttUser(user))
		tokens.Set("search", ttValue(c.SessionGet("search")))
		tokens.Set("instance_id", ttValue(c.persistent()["instance_id"]))
		tokens.Set("user_can_edit", ttBoolEmpty(layout != nil && layout.UserCan("write_existing")))
		tokens.Set("user_can_create", ttBoolEmpty(layout != nil && layout.UserCan("write_new")))
		if s.E.AnyCurrent() {
			tokens.Set("show_link", int64(1))
		} else {
			tokens.Set("show_link", int64(0))
		}
		if layout != nil {
			tokens.Set("layout", c.ttLayout(layout))
		} else {
			tokens.Set("layout", nil)
		}
		tokens.Set("v", c.ttView(c.CurrentView(layout)))
	}
	msgs := tt.NewList()
	if m, ok := c.SessionGet("messages").([]*Message); ok {
		for _, x := range m {
			msgs.Items = append(msgs.Items, x)
		}
		tokens.Set("messages", msgs)
		// The token is the session's array, so messages added before the
		// session's messages are reset are also displayed
		c.tokenMessages = msgs
	} else {
		tokens.Set("messages", nil)
	}
	if site, ok := c.Var("site").(*gads.Site); ok {
		tokens.Set("site", c.ttSite(site))
	} else {
		tokens.Set("site", nil)
	}
	tokens.Set("config", &ttConfig{s: s})
	tokens.Set("b64_filter", tt.Code(func(_ *tt.Context, args []any) ([]any, error) {
		b := gads.EncodeJSON(toJSONValue(argAt(args, 0)))
		return []any{base64.StdEncoding.EncodeToString([]byte(b))}, nil
	}))
	tokens.Set("actions", c.SessionGet("actions"))
	c.SessionDelete("actions")
	if !tt.True(c.SessionGet("csrf_token")) {
		c.updateCSRFToken()
	}
	tokens.Set("csrf_token", c.SessionGet("csrf_token"))
	if id := c.SessionGet("views_other_user_id"); tt.True(id) {
		if p, _ := tokens.Get("page"); strings.Contains(tt.Str(p), "data") || strings.Contains(tt.Str(p), "view") {
			if id64, ok := toInt64(id); ok {
				if u := s.E.FindUser(id64); u != nil {
					c.Notice("You are currently viewing, editing and creating views as " + u.Value)
				}
			}
		}
	}
	c.SessionSet("messages", []*Message{})
	c.tokenMessages = nil
}

func (c *Ctx) updateCSRFToken() { c.SessionSet("csrf_token", c.E.NewToken(32)) }

// updatePersistent is the after_template_render hook
func (s *Server) updatePersistent(c *Ctx) {
	user := c.LoggedInUser()
	if user == nil {
		return
	}
	b := encodeJSONPerl(c.SessionGet("persistent"))
	user.UpdateSessionSettings(b)
}

func argAt(args []any, i int) any {
	if i >= 0 && i < len(args) {
		return args[i]
	}
	return nil
}

// ttBoolEmpty is a Perl boolean result ("" for false)
func ttBoolEmpty(b bool) any {
	if b {
		return int64(1)
	}
	return ""
}
