package main

import (
	"bufio"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/gads"
)

// fakeSMTP is a minimal SMTP server recording the transactions it receives
type fakeSMTP struct {
	ln       net.Listener
	starttls bool // whether STARTTLS is offered (it is never completed)
	mu       sync.Mutex
	auth     []string // the decoded AUTH PLAIN credentials
	mails    []fakeMail
}

type fakeMail struct {
	from, to, data string
}

func startFakeSMTP(t *testing.T, starttls bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, starttls: starttls}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(s string) { c.Write([]byte(s + "\r\n")) }
	say("220 fake ESMTP")
	var cur fakeMail
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			if f.starttls {
				say("250-fake")
				say("250-STARTTLS")
				say("250 AUTH PLAIN")
			} else {
				say("250-fake")
				say("250 AUTH PLAIN")
			}
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			f.mu.Lock()
			f.auth = append(f.auth, strings.ReplaceAll(string(b), "\x00", "|"))
			f.mu.Unlock()
			say("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			cur = fakeMail{from: strings.Trim(line[len("MAIL FROM:"):], "<> ")}
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			cur.to = strings.Trim(line[len("RCPT TO:"):], "<> ")
			say("250 ok")
		case cmd == "DATA":
			say("354 go on")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			cur.data = data.String()
			f.mu.Lock()
			f.mails = append(f.mails, cur)
			f.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("502 unknown")
		}
	}
}

func TestMailConfig(t *testing.T) {
	cases := []struct {
		yaml string
		want mailConfig
		err  string
	}{
		{"gads: {}\n", mailConfig{Via: "sendmail"}, ""},
		{"gads:\n  mail:\n    via: smtp\n    host: mx\n", mailConfig{Via: "smtp", Host: "mx", Port: 587, Security: "starttls"}, ""},
		{"gads:\n  mail:\n    via: SMTP\n    host: mx\n    security: tls\n", mailConfig{Via: "smtp", Host: "mx", Port: 465, Security: "tls"}, ""},
		{"gads:\n  mail:\n    via: smtp\n    host: mx\n    security: none\n    port: \"2525\"\n    username: u\n    password: p\n",
			mailConfig{Via: "smtp", Host: "mx", Port: 2525, Security: "none", Username: "u", Password: "p"}, ""},
		{"gads:\n  mail:\n    via: smtp\n", mailConfig{}, "smtp needs a host"},
		{"gads:\n  mail:\n    via: carrier-pigeon\n", mailConfig{}, "unknown via"},
		{"gads:\n  mail:\n    via: smtp\n    host: mx\n    security: ssl\n", mailConfig{}, "unknown security"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "config.yml"), []byte(c.yaml), 0o644)
		cfg, err := loadConfig(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := cfg.mailConfig()
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: error %v, want %q", c.yaml, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.yaml, err)
			continue
		}
		got.Timeout = 0
		if *got != c.want {
			t.Errorf("%q: %+v, want %+v", c.yaml, *got, c.want)
		}
	}
}

func TestSendSMTP(t *testing.T) {
	f := startFakeSMTP(t, false)
	cfg := &mailConfig{Via: "smtp", Host: "127.0.0.1", Port: f.port(), Security: "none",
		Username: "gads", Password: "secret", Timeout: 5 * time.Second}
	m := newMailer(cfg, `"Linkspace" <noreply@example.com>`)
	m.Send(gads.Email{Subject: "Données", To: []string{"a@example.com", "b@example.com"},
		Text: "Hello\n.dot line\n", HTML: "<p>Hello</p>", ReplyTo: "r@example.com"})
	if !m.Close(5 * time.Second) {
		t.Fatal("queue not delivered")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.mails) != 2 {
		t.Fatalf("%d messages, want one for each recipient", len(f.mails))
	}
	for i, to := range []string{"a@example.com", "b@example.com"} {
		mail := f.mails[i]
		if mail.from != "noreply@example.com" || mail.to != to {
			t.Errorf("envelope %s -> %s", mail.from, mail.to)
		}
		for _, want := range []string{
			"From: \"Linkspace\" <noreply@example.com>\r\n", "To: " + to + "\r\n", "Reply-To: r@example.com\r\n",
			"Subject: =?utf-8?q?Donn=C3=A9es?=\r\n", "Content-Type: multipart/alternative",
			"\r\nHello\r\n..dot line\r\n", "<p>Hello</p>",
		} {
			if !strings.Contains(mail.data, want) {
				t.Errorf("message to %s lacks %q:\n%s", to, want, mail.data)
			}
		}
		if strings.Contains(mail.data, "To: a@example.com, b@example.com") {
			t.Errorf("recipients see each other")
		}
	}
	// AUTH PLAIN is allowed without TLS only because the server is local
	if len(f.auth) != 2 || f.auth[0] != "|gads|secret" {
		t.Errorf("auth %v", f.auth)
	}
}

func TestSendSMTPStartTLSRequired(t *testing.T) {
	f := startFakeSMTP(t, false)
	cfg := &mailConfig{Via: "smtp", Host: "127.0.0.1", Port: f.port(), Security: "starttls", Timeout: 5 * time.Second}
	m := newMailer(cfg, "noreply@example.com")
	err := m.sendSMTP("noreply@example.com", "a@example.com", buildMessage("noreply@example.com", "a@example.com", gads.Email{Subject: "s", Text: "t"}))
	if err == nil || !strings.Contains(err.Error(), "does not offer STARTTLS") {
		t.Errorf("error %v", err)
	}
	if len(f.mails) != 0 {
		t.Errorf("sent without STARTTLS")
	}
}

func TestEnvelopeAddress(t *testing.T) {
	for in, want := range map[string]string{
		`"Linkspace" <user@example.com>`: "user@example.com",
		"user@example.com":               "user@example.com",
		"":                               "",
	} {
		if got := envelopeAddress(in); got != want {
			t.Errorf("envelopeAddress(%q) = %q", in, got)
		}
	}
	if !strings.Contains(string(buildMessage("", "x@y", gads.Email{Subject: "plain", Text: "a"})), "Content-Type: text/plain") {
		t.Errorf("plain message")
	}
}

func TestBuildMessageHeaderInjection(t *testing.T) {
	msg := string(buildMessage("a@x\r\nBcc: evil@y", "b@x\nBcc: evil@y", gads.Email{ReplyTo: "c@x\rBcc: evil@y", Subject: "s\r\nBcc: evil@y", Text: "t"}))
	head := msg[:strings.Index(msg, "\r\n\r\n")]
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") || strings.ContainsAny(line, "\r\n") {
			t.Errorf("injected header line %q", line)
		}
	}
}

// Send queues: a slow mail server does not hold up the caller, and Close
// delivers what is queued
func TestMailQueue(t *testing.T) {
	m := newMailer(&mailConfig{Via: "sendmail"}, "noreply@example.com")
	var mu sync.Mutex
	var sent []string
	m.send = func(_, to string, _ []byte) error {
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		sent = append(sent, to)
		mu.Unlock()
		return nil
	}
	start := time.Now()
	m.Send(gads.Email{Subject: "s", Text: "t", To: []string{"a@example.com", "b@example.com"}})
	m.Send(gads.Email{Subject: "s", Text: "t", To: []string{"c@example.com"}})
	if d := time.Since(start); d > 40*time.Millisecond {
		t.Errorf("Send waited %s for delivery", d)
	}
	if !m.Close(5 * time.Second) {
		t.Fatal("queue not delivered")
	}
	if strings.Join(sent, ",") != "a@example.com,b@example.com,c@example.com" {
		t.Errorf("sent %v", sent)
	}
	// After Close, an email is delivered at once
	m.Send(gads.Email{Subject: "s", Text: "t", To: []string{"d@example.com"}})
	if len(sent) != 4 {
		t.Errorf("email after close not delivered")
	}
}
