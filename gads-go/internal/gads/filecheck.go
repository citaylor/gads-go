package gads

import (
	"bytes"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MimeType is the MIME type of file content as libmagic reports it
// (GADS::Filecheck get_filetype, File::LibMagic). It covers the types of
// common uploads: images, PDF, HTML, SVG and plain text; other content is
// application/octet-stream.
func MimeType(data []byte) string {
	if len(data) == 0 {
		return "inode/x-empty"
	}
	prefixes := []struct{ magic, mime string }{
		{"\x89PNG\r\n\x1a\n", "image/png"},
		{"\xff\xd8\xff", "image/jpeg"},
		{"GIF87a", "image/gif"},
		{"GIF89a", "image/gif"},
		{"BM", "image/bmp"},
		{"II*\x00", "image/tiff"},
		{"MM\x00*", "image/tiff"},
		{"%PDF-", "application/pdf"},
	}
	for _, p := range prefixes {
		if bytes.HasPrefix(data, []byte(p.magic)) {
			return p.mime
		}
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	if !isText(data) {
		return "application/octet-stream"
	}
	head := strings.ToLower(strings.TrimLeft(string(data), " \t\r\n"))
	switch {
	case strings.HasPrefix(head, "<svg") || (strings.HasPrefix(head, "<?xml") && strings.Contains(head, "<svg")):
		return "image/svg+xml"
	case strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") || strings.HasPrefix(head, "<head") || strings.HasPrefix(head, "<body"):
		return "text/html"
	case strings.HasPrefix(head, "<?xml"):
		return "text/xml"
	}
	return "text/plain"
}

// isText is whether content is text (UTF-8 without control characters
// other than whitespace)
func isText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' && b != 0x1b {
			return false
		}
		if b == 0x7f {
			return false
		}
	}
	return true
}

// IsImage is GADS::Filecheck is_image
func IsImage(data []byte) bool { return strings.HasPrefix(MimeType(data), "image/") }

var (
	reAllowedMime = regexp.MustCompile(`(?i)^(text/|image/|video/|audio/|application/x-abiword|application/msword|application/vnd\.openxmlformats-officedocument\.wordprocessingml\.document|application/vnd\.oasis\.opendocument\.presentation|application/vnd\.oasis\.opendocument\.spreadsheet|application/vnd\.oasis\.opendocument\.text|application/pdf|application/vnd\.ms-powerpoint|application/vnd\.openxmlformats-officedocument\.presentationml\.presentation|application/rtf|application/vnd\.ms-excel|application/vnd\.openxmlformats-officedocument\.spreadsheetml\.sheet|application/json)`)
	reFileExt     = regexp.MustCompile(`(?i)\.([a-z]+)$`)
	reAllowedExt  = regexp.MustCompile(`(?i)^(doc|docx|pdf|jpeg|jpg|png|wav|rtf|xls|xlsx|ods|ppt|pptx|odf|odg|odt|ott|sda|sdc|sdd|sdw|sxc|sxw|odp|sdp|csv|txt|msg|tif|svg)$`)
	reFileName    = regexp.MustCompile(`^[-+_ a-zA-Z0-9\(\)]{1,200}\.[a-zA-Z0-9]{1,10}$`)
)

// FileType is an extra file type allowed for a file field (an
// override_types entry: MIME type name and extension)
type FileType struct {
	Name      string
	Extension string
}

// CheckOptions are the options of the file checks
type CheckOptions struct {
	// CheckName checks the file name too (default in Perl)
	CheckName bool
	// ExtraTypes are types allowed in addition to the defaults
	ExtraTypes []FileType
}

// overrideRegexes is GADS::Filecheck create_regex (nil without extra
// types; an empty list matches nothing but the empty string)
func overrideRegexes(types []FileType) (mime, ext *regexp.Regexp) {
	if types == nil {
		return nil, nil
	}
	var m, e []string
	for _, t := range types {
		m = append(m, regexp.QuoteMeta(t.Name))
		e = append(e, regexp.QuoteMeta(t.Extension))
	}
	return regexp.MustCompile(`^(` + strings.Join(m, "|") + `)$`), regexp.MustCompile(`^(` + strings.Join(e, "|") + `)$`)
}

// checkFile is GADS::Filecheck _check_file: the type of the content must be
// allowed and the file name must have an allowed extension (and be a valid
// name, if CheckName). Returns the type.
func checkFile(filename string, content []byte, o CheckOptions) string {
	mime := MimeType(content)
	mimeRe, extRe := overrideRegexes(o.ExtraTypes)
	if !(mimeRe != nil && mimeRe.MatchString(mime)) && !CheckType(mime) {
		Fail("Files of mimetype %s are not allowed", mime)
	}
	checkFilename(filename, extRe)
	if o.CheckName {
		CheckFileName(filename)
	}
	return mime
}

// CheckUpload is GADS::Filecheck check_upload: the upload must be at most
// 50 MB and pass the file checks. Returns the type.
func CheckUpload(filename string, content []byte, o CheckOptions) string {
	if len(content) > 50*1024*1024 {
		Fail("Maximum file size is 50 MB")
	}
	return checkFile(filename, content, o)
}

// CheckFileval is GADS::Filecheck check_fileval: the checks of a stored file
func (e *Engine) CheckFileval(id int64, name string, o CheckOptions) string {
	return checkFile(name, e.FilevalContent(id), o)
}

func checkFilename(filename string, extRe *regexp.Regexp) {
	m := reFileExt.FindStringSubmatch(filename)
	if m == nil {
		Fail("Files without extensions cannot be uploaded")
	}
	if !(extRe != nil && extRe.MatchString(m[1])) && !CheckExtension(m[1]) {
		Fail("Files with extension of %s are not allowed", m[1])
	}
}

// CheckType is GADS::Filecheck check_type: whether a MIME type is allowed
// by default
func CheckType(mime string) bool { return reAllowedMime.MatchString(mime) }

// CheckExtension is GADS::Filecheck check_extension: whether a file
// extension is allowed by default
func CheckExtension(ext string) bool { return reAllowedExt.MatchString(ext) }

// CheckFileName is GADS::Filecheck check_name
func CheckFileName(filename string) {
	if !reFileName.MatchString(filename) {
		Fail("The filename %s is not allowed. Filenames can only contain alphanumeric characters and a single dot", filename)
	}
}
