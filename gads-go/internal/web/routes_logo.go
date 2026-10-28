package web

import (
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// post '/api/settings/logo' (API.pm:549): uploading the logo of the site
func (s *Server) hAPISettingsLogo(c *Ctx) string {
	site := c.Site()
	if !c.LoggedInUser().Permission()["superadmin"] {
		gads.Fail("You do not have permission to manage system settings")
	}
	file := c.Upload("file")
	if file == nil {
		gads.Fail("No file provided")
	}
	if !gads.IsImage(file.Content) {
		// Perl passes the upload object to get_filetype as the file name of
		// the message, and File::LibMagic dies
		gads.Panic("Files of mimetype %s are not allowed", gads.MimeType(file.Content))
	}
	s.E.DB.UpdateMap("site", map[string]any{"site_logo": file.Content}, map[string]any{"id": site.ID()})
	c.ContentType("application/json")
	// 201 is created rather than ok
	c.Status(201)
	return gads.EncodeJSON(map[string]any{"error": int64(0), "url": "/settings/logo"})
}

// get '/settings/logo' (GADS.pm:1353): the logo of the site, with the
// type of its content (Site load_logo)
func (s *Server) hSettingsLogo(c *Ctx) string {
	logo, _ := db.AsString(c.Site().Row()["site_logo"])
	if logo == "" {
		gads.Fail("No logo configured")
	}
	c.ContentType(gads.MimeType([]byte(logo)))
	return logo
}
