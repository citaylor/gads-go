package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ctrlo/gads-go/internal/gads"
)

// mailConfig is the transport of emails, the "mail" key of the gads
// section:
//
//	gads:
//	  mail:
//	    via: smtp            # or sendmail (the default)
//	    host: smtp.example.com
//	    port: 587            # default 587 (465 with security tls, 25 with none)
//	    username: user       # optional: AUTH PLAIN (only over TLS, or to localhost)
//	    password: secret
//	    security: starttls   # starttls (default), tls (implicit TLS) or none
//	    helo: gads.example.com
//	    timeout: 30          # seconds
//
// GADS itself always uses sendmail (Mail::Transport::Sendmail); SMTP is an
// addition of the Go application.
type mailConfig struct {
	Via      string
	Host     string
	Port     int
	Username string
	Password string
	Security string
	Helo     string
	Timeout  time.Duration
}

func (c *appConfig) mailConfig() (*mailConfig, error) {
	m := c.section("gads", "mail")
	mc := &mailConfig{
		Via:      strings.ToLower(asString(m["via"])),
		Host:     asString(m["host"]),
		Port:     int(asInt(m["port"])),
		Username: asString(m["username"]),
		Password: asString(m["password"]),
		Security: strings.ToLower(asString(m["security"])),
		Helo:     asString(m["helo"]),
		Timeout:  time.Duration(asInt(m["timeout"])) * time.Second,
	}
	if mc.Via == "" {
		mc.Via = "sendmail"
	}
	if mc.Timeout == 0 {
		mc.Timeout = 30 * time.Second
	}
	switch mc.Via {
	case "sendmail":
		return mc, nil
	case "smtp":
	default:
		return nil, fmt.Errorf("gads mail: unknown via %q (sendmail or smtp)", mc.Via)
	}
	if mc.Host == "" {
		return nil, fmt.Errorf("gads mail: smtp needs a host")
	}
	if mc.Security == "" {
		mc.Security = "starttls"
	}
	switch mc.Security {
	case "starttls", "tls", "none":
	default:
		return nil, fmt.Errorf("gads mail: unknown security %q (starttls, tls or none)", mc.Security)
	}
	if mc.Port == 0 {
		switch mc.Security {
		case "tls":
			mc.Port = 465
		case "none":
			mc.Port = 25
		default:
			mc.Port = 587
		}
	}
	return mc, nil
}

// mailer sends the emails of the engine: one message to each recipient,
// as GADS::Email does (so recipients do not see each other). Emails are
// queued and delivered in the background, so that a slow mail server does
// not hold up the engine (and so every request) while it is sent.
type mailer struct {
	cfg  *mailConfig
	from string // the From header (email_from)
	// send delivers a message to one recipient (sendmail or SMTP)
	send func(envelopeFrom, to string, msg []byte) error

	mu    sync.Mutex
	queue chan gads.Email // nil once closed
	done  chan struct{}   // closed when the queue has been delivered
}

// mailQueueSize is the number of emails waiting before Send blocks
const mailQueueSize = 1000

func newMailer(cfg *mailConfig, from string) *mailer {
	m := &mailer{cfg: cfg, from: from, queue: make(chan gads.Email, mailQueueSize), done: make(chan struct{})}
	if cfg.Via == "smtp" {
		m.send = m.sendSMTP
	} else {
		m.send = sendmailTo
	}
	go func(queue <-chan gads.Email) {
		defer close(m.done)
		for e := range queue {
			m.deliver(e)
		}
	}(m.queue)
	return m
}

// Send queues an email (delivered at once after Close)
func (m *mailer) Send(e gads.Email) {
	e.To = append([]string(nil), e.To...)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queue == nil {
		m.deliver(e)
		return
	}
	m.queue <- e
}

// Close delivers the queued emails, waiting at most timeout for them. It
// reports whether all were delivered.
func (m *mailer) Close(timeout time.Duration) bool {
	m.mu.Lock()
	if m.queue != nil {
		close(m.queue)
		m.queue = nil
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		return true
	case <-time.After(timeout):
		fmt.Fprintln(os.Stderr, "Emails still unsent at exit")
		return false
	}
}

// deliver sends an email, reporting failures as GADS does not fail on them
func (m *mailer) deliver(e gads.Email) {
	envelope := envelopeAddress(m.from)
	if envelope == "" {
		envelope = m.cfg.Username
	}
	for _, to := range e.To {
		if err := m.send(envelope, to, buildMessage(m.from, to, e)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to send email to %s: %s\n", to, err)
		}
	}
}

// envelopeAddress is the address of a From header ('"Name" <a@b>' or a@b)
func envelopeAddress(from string) string {
	if from == "" {
		return ""
	}
	if a, err := mail.ParseAddress(from); err == nil {
		return a.Address
	}
	return from
}

// buildMessage is the message of an email to one recipient: plain text, or
// plain text and HTML alternatives
func buildMessage(from, to string, m gads.Email) []byte {
	var b bytes.Buffer
	// Line breaks in addresses would add headers (recipients for
	// sendmail -t); the subject is encoded
	from, to, replyTo := oneLine(from), oneLine(to), oneLine(m.ReplyTo)
	if from != "" {
		fmt.Fprintf(&b, "From: %s\r\n", from)
	}
	fmt.Fprintf(&b, "To: %s\r\n", to)
	if replyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", replyTo)
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", randomID(), messageIDHost(from))
	b.WriteString("MIME-Version: 1.0\r\n")
	if m.HTML == "" {
		fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", crlf(m.Text))
	} else {
		boundary := "gads-alternative-" + randomID()
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
		fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, crlf(m.Text))
		fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n", boundary, crlf(m.HTML))
		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	}
	return b.Bytes()
}

// oneLine replaces the line breaks of a header value with spaces
func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
}

// crlf gives text the line endings of a message
func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

func randomID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func messageIDHost(from string) string {
	if a := envelopeAddress(from); strings.Contains(a, "@") {
		return a[strings.LastIndex(a, "@")+1:]
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "localhost"
}

// sendmailTo sends a message by sendmail, as Mail::Transport::Sendmail
// (with -f the envelope sender)
func sendmailTo(envelopeFrom, to string, msg []byte) error {
	path, err := exec.LookPath("sendmail")
	if err != nil {
		for _, p := range []string{"/usr/sbin/sendmail", "/usr/lib/sendmail"} {
			if _, serr := os.Stat(p); serr == nil {
				path, err = p, nil
				break
			}
		}
	}
	if err != nil {
		return fmt.Errorf("sendmail not found")
	}
	args := []string{"-t", "-oi"}
	if envelopeFrom != "" {
		args = append(args, "-f", envelopeFrom)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = bytes.NewReader(msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sendSMTP sends a message to one recipient by the SMTP server
func (m *mailer) sendSMTP(envelopeFrom, to string, msg []byte) error {
	cfg := m.cfg
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	tlsConfig := &tls.Config{ServerName: cfg.Host}
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	var conn net.Conn
	var err error
	if cfg.Security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(cfg.Timeout))
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if cfg.Helo != "" {
		if err := c.Hello(cfg.Helo); err != nil {
			return err
		}
	}
	if cfg.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS (set security: none to send without it)", addr)
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if cfg.Username != "" {
		// PlainAuth refuses to send the password unencrypted, except to
		// localhost
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(envelopeFrom); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
