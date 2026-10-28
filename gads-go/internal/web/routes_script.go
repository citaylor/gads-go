package web

import (
	"log"

	"github.com/ctrlo/gads-go/internal/gads"
)

// get '/api/get_key' (API.pm:1462): the user's encryption key, as
// to_json (so served as HTML)
func (s *Server) hAPIGetKey(c *Ctx) string {
	return gads.EncodeJSON(map[string]any{"error": int64(0), "key": c.LoggedInUser().EncryptionKey()})
}

// post '/api/script_error' (API.pm:1473): an error of the browser scripts,
// logged at info level
func (s *Server) hAPIScriptError(c *Ctx) string {
	body := c.decodeJSONBody()
	log.Printf("info: SCRIPT ERROR: %s - %s", fmtAny(apiValue(body["url"])), fmtAny(apiValue(body["description"])))
	return c.apiSuccess("Script error logged successfully")
}
