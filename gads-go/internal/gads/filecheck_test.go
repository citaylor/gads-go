package gads

import (
	"errors"
	"testing"
)

// Expected types as reported by File::LibMagic for the same content
func TestMimeType(t *testing.T) {
	for _, c := range []struct{ data, want string }{
		{"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "image/png"},
		{"GIF89a\x01\x00\x01\x00\x00\x00\x00;", "image/gif"},
		{"\xff\xd8\xff\xe0\x00\x10JFIF\x00", "image/jpeg"},
		{"BM\x3a\x00\x00\x00\x00\x00\x00\x00\x36\x00", "image/bmp"},
		{"%PDF-1.4\n", "application/pdf"},
		{"<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>\n", "image/svg+xml"},
		{"<html><body>x</body></html>\n", "text/html"},
		{"Hello world\n", "text/plain"},
		{"\x00\x01\x02\x03\xfe", "application/octet-stream"},
		{"", "inode/x-empty"},
	} {
		if got := MimeType([]byte(c.data)); got != c.want {
			t.Errorf("MimeType(%q) = %s, want %s", c.data, got, c.want)
		}
	}
	if !IsImage([]byte("GIF89a")) || IsImage([]byte("text")) {
		t.Error("IsImage")
	}
}

// GADS::Email->send: its checks and the removal of duplicate recipients
func TestSendEmail(t *testing.T) {
	var sent []Email
	e := &Engine{Emailer: func(m Email) { sent = append(sent, m) }}
	e.SendEmail(Email{Subject: "S", To: []string{"a@x", "b@x", "a@x"}, Text: "T"})
	if len(sent) != 1 || len(sent[0].To) != 2 || sent[0].To[1] != "b@x" {
		t.Fatalf("sent %#v", sent)
	}
	for _, m := range []Email{{Subject: "S", Text: "T"}, {To: []string{"a@x"}, Text: "T"}, {Subject: "S", To: []string{"a@x"}}} {
		if err := Try(func() { e.SendEmail(m) }); err == nil {
			t.Errorf("no error for %#v", m)
		}
	}
	// Without an Emailer the email is dropped
	(&Engine{}).SendEmail(Email{Subject: "S", To: []string{"a@x"}, Text: "T"})
}

// Plural matches Lingua::EN::Inflect PL (expected values from Perl)
func TestPlural(t *testing.T) {
	for in, want := range map[string]string{
		"Company": "Companys", "company": "companies", "Box": "Boxes", "Team": "Teams",
		"Mary": "Marys", "Church": "Churches", "Person": "People", "Status": "Statuses",
		"man": "men", "Man": "Men", "child": "children", "mouse": "mice", "Record": "Records",
		"Category": "Categorys", "category": "categories", "key": "keys", "Organisation": "Organisations",
	} {
		if got := Plural(in); got != want {
			t.Errorf("Plural(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestExceptionString(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&Error{Msg: "User x already exists", Level: LevelError}, "error: User x already exists\n"},
		{&Error{Msg: "No level"}, "error: No level\n"},
		{&Error{Msg: "Oops", Level: LevelPanic}, "panic: Oops\n"},
		{errors.New("runtime"), "panic: runtime\n"},
	}
	for _, c := range cases {
		if got := ExceptionString(c.err); got != c.want {
			t.Errorf("ExceptionString(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestCheckFile(t *testing.T) {
	cases := []struct {
		name, content string
		checkName     bool
		want, err     string
	}{
		{"notes.txt", "Some notes\n", true, "text/plain", ""},
		{"data.txt", "\x00\x01\x02\x03\x04\x05\x06\x07", true, "", "Files of mimetype application/octet-stream are not allowed"},
		{"notes", "Some notes\n", true, "", "Files without extensions cannot be uploaded"},
		{"notes.html", "Some notes\n", true, "", "Files with extension of html are not allowed"},
		{"my.notes.txt", "Some notes\n", true, "", "The filename my.notes.txt is not allowed. Filenames can only contain alphanumeric characters and a single dot"},
		{"my.notes.txt", "Some notes\n", false, "text/plain", ""},
		{"Report (1).TXT", "Some notes\n", true, "text/plain", ""},
	}
	for _, c := range cases {
		var got string
		err := Try(func() { got = CheckUpload(c.name, []byte(c.content), CheckOptions{CheckName: c.checkName}) })
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if got != c.want || msg != c.err {
			t.Errorf("CheckUpload(%q) = %q, %q; want %q, %q", c.name, got, msg, c.want, c.err)
		}
	}
}
