package web

import (
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// Message is a Dancer2::Plugin::LogReport::Message kept in the session for
// display on the next page.
type Message struct {
	Reason  string
	Text    string
	Classes []string
}

var reason2color = map[string]string{
	"TRACE": "info", "ASSERT": "info", "INFO": "info", "NOTICE": "info",
	"WARNING": "warning", "MISTAKE": "warning",
}

func (m *Message) inClass(c string) bool {
	for _, x := range m.Classes {
		if x == c {
			return true
		}
	}
	return false
}

// TTMethod implements tt.Object
func (m *Message) TTMethod(name string, args []any) ([]any, bool, error) {
	switch name {
	case "bootstrap_color", "bootstrapColor":
		if m.inClass("success") {
			return []any{"success"}, true, nil
		}
		if c, ok := reason2color[m.Reason]; ok {
			return []any{c}, true, nil
		}
		return []any{"danger"}, true, nil
	case "reason":
		return []any{m.Reason}, true, nil
	case "inClass", "taggedWith":
		if len(args) > 0 && m.inClass(tt.Str(args[0])) {
			return []any{int64(1)}, true, nil
		}
		return []any{""}, true, nil
	case "toString", "toHTML":
		return []any{m.Text}, true, nil
	}
	return nil, false, nil
}

// TTString implements tt.Stringer
func (m *Message) TTString() string { return m.Text }

// session message reasons (plugins.LogReport.session_messages)
var sessionReasons = map[string]bool{
	"NOTICE": true, "MISTAKE": true, "WARNING": true, "ERROR": true,
	"FAULT": true, "ALERT": true, "FAILURE": true, "PANIC": true,
}

// addMessage adds a message to the session (LogReport _message_add),
// hiding the text of unexpected errors unless show_errors is set. It
// returns the message as stored.
func (c *Ctx) addMessage(m *Message) *Message {
	// Messages reported by the engine come first
	c.flushReports()
	if !sessionReasons[m.Reason] {
		return m
	}
	switch m.Reason {
	case "FAULT", "ALERT", "FAILURE", "PANIC":
		if !c.s.Config.ShowErrors {
			m = &Message{Reason: m.Reason, Text: "An unexpected error has occurred"}
		}
	}
	msgs, _ := c.SessionGet("messages").([]*Message)
	msgs = append(msgs, m)
	c.SessionSet("messages", msgs)
	if c.tokenMessages != nil {
		c.tokenMessages.Items = append(c.tokenMessages.Items, m)
	}
	return m
}

// Notice reports a notice (notice __"...")
func (c *Ctx) Notice(text string) { c.addMessage(&Message{Reason: "NOTICE", Text: text}) }

// Success reports success (success __"..."): a NOTICE tagged success
func (c *Ctx) Success(text string) {
	c.addMessage(&Message{Reason: "NOTICE", Text: text, Classes: []string{"success"}})
}

// ReportError reports a non-fatal error (report {is_fatal => 0}, ERROR => ...)
func (c *Ctx) ReportError(text string) { c.addMessage(&Message{Reason: "ERROR", Text: text}) }

// Mistake reports a mistake
func (c *Ctx) Mistake(text string) { c.addMessage(&Message{Reason: "MISTAKE", Text: text}) }

// Warning reports a warning
func (c *Ctx) Warning(text string) { c.addMessage(&Message{Reason: "WARNING", Text: text}) }

// Error raises a fatal user error (error __"..."): the message is shown
// on the home page
func (c *Ctx) Error(text string) { gads.Fail("%s", text) }

// ForwardHome is GADS::forwardHome: report the message (danger, notice or
// success) and redirect to the page.
func (c *Ctx) ForwardHome(kind, text, page string) {
	switch kind {
	case "":
	case "danger":
		c.ReportError(text)
	case "notice":
		c.Notice(text)
	default:
		c.addMessage(&Message{Reason: "NOTICE", Text: text, Classes: []string{"success"}})
	}
	c.Redirect("/" + page)
}

// flushReports adds the messages reported by the engine to the session, as
// the Log::Report dispatcher of the Dancer2 plugin does when they are
// reported
func (c *Ctx) flushReports() {
	reports := c.E.Reports
	c.E.Reports = nil
	for _, r := range reports {
		c.addMessage(&Message{Reason: string(r.Level), Text: r.Msg})
	}
}
