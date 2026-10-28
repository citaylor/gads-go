package gads

// Email is a message sent by GADS::Email->send.
type Email struct {
	Subject string
	To      []string
	Text    string
	HTML    string
	ReplyTo string
}

// SendEmail sends an email (GADS::Email->send) by the Engine's Emailer,
// with its checks and its removal of duplicate recipients. Without an
// Emailer the email is dropped.
func (e *Engine) SendEmail(m Email) {
	if len(m.To) == 0 {
		Fail("Please specify some recipients to send an email to")
	}
	if m.Subject == "" {
		Fail("Please enter a subject for the email")
	}
	if m.Text == "" && m.HTML == "" {
		Panic("No plain or HTML email text supplied")
	}
	seen := map[string]bool{}
	var to []string
	for _, a := range m.To {
		if !seen[a] {
			seen[a] = true
			to = append(to, a)
		}
	}
	m.To = to
	if e.Emailer != nil {
		e.Emailer(m)
	}
}
