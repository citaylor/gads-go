package web

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	mrand "math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/tt"
)

// The OAuth2 password grant of lib/GADS/API.pm
// (Net::OAuth2::AuthorizationServer::PasswordGrant with GADS's callbacks,
// tokens stored in the oauthtoken table)

// accessTokenTTL is the default access_token_ttl of the grant
const accessTokenTTL = 3600

// oauthToken is the oauthtoken row of a token of the type, or nil
// (ResultSet::Oauthtoken access_token and refresh_token)
func (s *Server) oauthToken(token, typ string) db.Row {
	r := s.E.DB.QueryRow(`SELECT * FROM "oauthtoken" "me" WHERE "me"."token" = ?`, token)
	if r == nil {
		return nil
	}
	if t, _ := db.AsString(r["type"]); t != typ {
		return nil
	}
	return r
}

// oauthVerifyResult is the list returned by the grant's verify callbacks:
// the client (a row, or a client ID), an error, and the user ID
type oauthVerifyResult struct {
	client any // nil (false), a token row, or a client ID
	err    string
	userID any
}

// verifyAccessToken is GADS's verify_access_token_cb
func (s *Server) verifyAccessToken(token string, isRefresh bool) oauthVerifyResult {
	if rt := s.oauthToken(token, "refresh"); isRefresh && rt != nil {
		return oauthVerifyResult{client: rt}
	}
	if at := s.oauthToken(token, "access"); at != nil {
		expires, _ := db.AsInt64(at["expires"])
		if expires <= s.E.Now().Unix() {
			// need to revoke the access token
			s.E.DB.Exec(`DELETE FROM "oauthtoken" WHERE "token" = ?`, token)
		} else {
			return oauthVerifyResult{client: at}
		}
	}
	return oauthVerifyResult{err: "invalid_grant"}
}

// verifyTokenAndScope is the grant's verify_token_and_scope
func (s *Server) verifyTokenAndScope(refreshToken string, authHeader string, hasAuthHeader bool) oauthVerifyResult {
	var token string
	if refreshToken == "" {
		if !hasAuthHeader || authHeader == "" {
			return oauthVerifyResult{err: "invalid_request"}
		}
		parts := perlSplit(` `, authHeader)
		if len(parts) == 0 || parts[0] != "Bearer" {
			return oauthVerifyResult{err: "invalid_request"}
		}
		if len(parts) > 1 {
			token = parts[1]
		}
	} else {
		token = refreshToken
	}
	if token == "" && refreshToken == "" {
		return oauthVerifyResult{err: "invalid_request"}
	}
	return s.verifyAccessToken(token, refreshToken != "")
}

// verifyUserPassword is GADS's verify_user_password_cb
func (s *Server) verifyUserPassword(clientID, clientSecret *string, username, password string) oauthVerifyResult {
	q := `SELECT * FROM "oauthclient" "me" WHERE `
	var args []any
	for i, f := range []struct {
		col string
		v   *string
	}{{"client_id", clientID}, {"client_secret", clientSecret}} {
		if i > 0 {
			q += " AND "
		}
		// DBIC: undef is IS NULL
		if f.v == nil {
			q += `"me"."` + f.col + `" IS NULL`
		} else {
			q += `"me"."` + f.col + `" = ?`
			args = append(args, *f.v)
		}
	}
	client := s.E.DB.QueryRow(q+` ORDER BY "me"."id" LIMIT 1`, args...)
	if client == nil {
		return oauthVerifyResult{err: "unauthorized_client"}
	}
	uq := `SELECT * FROM "user" "me" WHERE "me"."account_request" = 0 AND "me"."deleted" IS NULL AND "me"."username" = ?`
	if s.E.SiteID != 0 {
		uq += ` AND "me"."site_id" = ` + fmtInt(s.E.SiteID)
	}
	user := s.E.DB.QueryRow(uq+` ORDER BY "me"."id" LIMIT 1`, username)
	if user != nil {
		hashed, _ := db.AsString(user["password"])
		if saltedHashValidate(hashed, password) {
			cid, _ := db.AsInt64(client["id"])
			uid, _ := db.AsInt64(user["id"])
			return oauthVerifyResult{client: cid, userID: uid}
		}
	}
	return oauthVerifyResult{err: "access_denied"}
}

// saltedHashValidate is Crypt::SaltedHash->validate: only a hashed
// password ({SCHEME}...) can match
func saltedHashValidate(hashed, password string) bool {
	if !strings.HasPrefix(hashed, "{") {
		return false
	}
	return MatchPassword(password, hashed)
}

// oauthNewToken is the grant's token for a grant without a JWT secret:
// base64 of the time, a random number and a random string
func (s *Server) oauthNewToken() string {
	now := s.E.Now()
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 30)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[n.Int64()]
	}
	raw := fmt.Sprintf("%d-%d-%v-%s", now.Unix(), now.Nanosecond()/1000, mrand.Float64(), string(b))
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// storeAccessToken is GADS's store_access_token_cb
func (s *Server) storeAccessToken(clientID any, userID any, access, refresh string, expiresIn int64, oldRefresh string) {
	d := s.E.DB
	if oldRefresh != "" {
		prevRT := s.oauthToken(oldRefresh, "refresh")
		if prevRT == nil {
			gads.Panic(`Can't call method "related_token" on an undefined value`)
		}
		related, _ := db.AsString(prevRT["related_token"])
		prevAT := s.oauthToken(related, "access")
		if prevAT == nil {
			gads.Panic(`Can't call method "delete" on an undefined value`)
		}
		d.Exec(`DELETE FROM "oauthtoken" WHERE "token" = ?`, related)
	}
	// The client of a refresh grant is the refresh token row (Perl's
	// verify callback returns only the row), which cannot be stored
	cid, ok := clientID.(int64)
	if !ok {
		gads.Panic("Unable to store an access token without a client ID (the refresh grant returns only the token row)")
	}
	// If the client has an existing refresh token, revoke it
	if userID == nil {
		d.Exec(`DELETE FROM "oauthtoken" WHERE "type" = 'refresh' AND "oauthclient_id" = ? AND "user_id" IS NULL`, cid)
	} else {
		d.Exec(`DELETE FROM "oauthtoken" WHERE "type" = 'refresh' AND "oauthclient_id" = ? AND "user_id" = ?`, cid, userID)
	}
	d.InsertMap("oauthtoken", map[string]any{
		"type": "access", "token": access, "expires": s.E.Now().Unix() + expiresIn,
		"related_token": refresh, "oauthclient_id": cid, "user_id": userID,
	})
	d.InsertMap("oauthtoken", map[string]any{
		"type": "refresh", "token": refresh, "related_token": access,
		"oauthclient_id": cid, "user_id": userID,
	})
}

// apiBeforeHook is the before hook of API.pm: the user of a valid bearer
// token is the API user
func (s *Server) apiBeforeHook(c *Ctx) {
	auth, has := c.RequestHeader("Authorization")
	res := s.verifyTokenAndScope("", auth, has)
	at, ok := res.client.(db.Row)
	if !ok {
		return
	}
	// $client->user (the related user row, whatever its state)
	if uid, ok := db.AsInt64(at["user_id"]); ok {
		if u := s.E.FindUser(uid); u != nil {
			c.SetVar("api_user", u)
		}
	}
}

// get '/clientcredentials/?' (API.pm:315)
func (s *Server) hClientCredentials(c *Ctx) string {
	row := s.E.DB.QueryRow(`SELECT * FROM "oauthclient" "me" ORDER BY "me"."id" LIMIT 1`)
	if row == nil {
		id := s.E.DB.InsertMap("oauthclient", map[string]any{
			"client_id":     s.E.NewToken(12),
			"client_secret": s.E.NewToken(12),
		})
		row = s.E.DB.QueryRow(`SELECT * FROM "oauthclient" WHERE "id" = ?`, id)
	}
	return c.Template("api/clientcredentials", tt.HashOf("credentials", newRow("oauthclient", row)), nil)
}

// post '/api/token' (API.pm:328)
func (s *Server) hAPIToken(c *Ctx) string {
	var clientIDSubmit, clientSecret *string
	// RFC6749 says try auth header first, then fall back to body params
	if auth, _ := c.RequestHeader("Authorization"); auth != "" {
		if m := regexp.MustCompile(`^Basic (.+)`).FindStringSubmatch(auth); m != nil {
			if decoded := perlDecodeBase64(m[1]); decoded != "" {
				f := perlSplit(`:`, decoded)
				if len(f) > 0 {
					clientIDSubmit = &f[0]
				}
				if len(f) > 1 {
					clientSecret = &f[1]
				}
			}
		}
	} else {
		if v, ok := c.Param("client_id"); ok {
			clientIDSubmit = &v
		}
		if v, ok := c.Param("client_secret"); ok {
			clientSecret = &v
		}
	}

	var res oauthVerifyResult
	var oldRefresh string
	var response map[string]any
	switch grant := c.ParamS("grant_type"); grant {
	case "password":
		res = s.verifyUserPassword(clientIDSubmit, clientSecret, c.ParamS("username"), c.ParamS("password"))
	case "refresh_token":
		rt := c.ParamS("refresh_token")
		auth, has := c.RequestHeader("Authorization")
		res = s.verifyTokenAndScope(rt, auth, has)
		oldRefresh = rt
	default:
		response = map[string]any{
			"error":             "invalid_request",
			"error_description": "Invalid grant type: " + grant,
		}
	}

	if res.client != nil {
		access := s.oauthNewToken()
		refresh := s.oauthNewToken()
		s.storeAccessToken(res.client, res.userID, access, refresh, accessTokenTTL, oldRefresh)
		response = map[string]any{
			"access_token":  access,
			"token_type":    "Bearer",
			"expires_in":    int64(accessTokenTTL),
			"refresh_token": refresh,
		}
	} else if response == nil {
		response = map[string]any{"error": res.err}
	}

	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.ContentType("application/json;charset=UTF-8")
	return gads.EncodeJSON(response)
}

// perlSplit is Perl split with a regexp: trailing empty fields are removed
func perlSplit(pattern, s string) []string {
	parts := regexp.MustCompile(pattern).Split(s, -1)
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// perlDecodeBase64 is MIME::Base64 decode_base64: characters outside the
// alphabet are ignored, as is anything after padding
func perlDecodeBase64(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '=' {
			break
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' {
			b.WriteRune(r)
		}
	}
	clean := b.String()
	// A final single character cannot be decoded
	if len(clean)%4 == 1 {
		clean = clean[:len(clean)-1]
	}
	out, _ := base64.RawStdEncoding.DecodeString(clean)
	return string(out)
}

// apiLayout is var('instances')->layout_by_shortname (borks on not found)
func (c *Ctx) apiLayout() *gads.Layout {
	sheet, _ := c.Param("sheet")
	layout := c.Instances().LayoutByShortname(sheet)
	if layout == nil {
		gads.Fail("Table not found: %s", sheet)
	}
	return layout
}

// apiUser is var('api_user')
func (c *Ctx) apiUser() *gads.User {
	u, _ := c.Var("api_user").(*gads.User)
	return u
}

// apiRequestJSON is decode_json request->body: JSON numbers keep their
// text, as Perl values
func (c *Ctx) apiRequestJSON() map[string]any {
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		gads.Panic("malformed JSON string: %s", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		gads.Panic("Not a HASH reference")
	}
	return m
}

// apiValue is a decoded JSON value as passed to set_value
func apiValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "1"
		}
		return "0"
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = apiValue(e)
		}
		return out
	}
	return v
}

// apiUpdateRecord is API.pm _update_record (Perl's key order is that of its
// hash, so a request with several errors may report any of them first)
func apiUpdateRecord(record *gads.Record, request map[string]any) {
	keys := make([]string, 0, len(request))
	for k := range request {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, field := range keys {
		col := record.Layout().ColumnByNameShort(field)
		if col == nil {
			gads.Fail("Column not found: %s", field)
		}
		record.GetFieldValue(col).SetValue(apiValue(request[field]), gads.SetValueOptions{})
	}
	record.Write(gads.WriteOptions{}) // borks on error
}

// apiSerial is the serial of find_serial_id (which errors on an invalid ID)
func apiSerial(id string) int64 {
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(id) {
		gads.Fail("Invalid serial ID %s", id)
	}
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}

// post '/api/:sheet/record' (API.pm:164)
func (s *Server) hAPIRecordCreate(c *Ctx) string {
	layout := c.apiLayout()
	request := c.apiRequestJSON()
	if _, ok := request["id"]; ok {
		gads.Fail("Invalid field: id; Use the PUT method to update an existing record")
	}
	record := s.E.NewRecord(layout, c.apiUser())
	record.Initialise(0)
	apiUpdateRecord(record, request)
	c.Status(201)
	c.Header("Location", c.Base()+"record/"+fmtInt(record.CurrentID()))
	return ""
}

// put '/api/:sheet/record/:id' (API.pm:192)
func (s *Server) hAPIRecordPut(c *Ctx) string {
	layout := c.apiLayout()
	user := c.apiUser()
	id, _ := c.Param("id")
	request := c.apiRequestJSON()
	find := s.E.NewRecord(layout, user)
	var record *gads.Record
	if index := layout.APIIndexLayout(); index != nil {
		record = find.FindUnique(index, id, 0)
		if record == nil {
			record = s.E.NewRecord(layout, user)
			record.Initialise(0)
			request[index.NameShort] = id
		}
	} else {
		if id == "0" {
			gads.Panic(`Can't call method "layout" on an undefined value`)
		}
		record = find.FindSerialID(apiSerial(id))
	}
	apiUpdateRecord(record, request)
	c.Status(204)
	// Use supplied ID for return - will either have been created as that or
	// will have borked early with error and not got here
	c.Header("Location", c.Base()+"record/"+id)
	return ""
}

// get '/api/:sheet/record/:id' (API.pm:237)
func (s *Server) hAPIRecordGet(c *Ctx) string {
	layout := c.apiLayout()
	id, _ := c.Param("id")
	record := s.E.NewRecord(layout, c.apiUser())
	if index := layout.APIIndexLayout(); index != nil {
		found := record.FindUnique(index, id, 0)
		if found == nil {
			gads.Fail("Record ID %s not found", id)
		}
		record = found
		record.FindCurrentID(record.CurrentID(), gads.FindOptions{})
	} else {
		if id == "0" {
			gads.Panic("The record of serial ID 0 is not supported")
		}
		record = record.FindSerialID(apiSerial(id))
	}
	c.ContentType("application/json; charset=UTF-8")
	return record.AsJSON(nil)
}

// get '/api/record/:id' (API.pm:264): the record has no layout, so finding
// it by serial fails once the ID is valid
func (s *Server) hAPIRecordAny(c *Ctx) string {
	id, _ := c.Param("id")
	if id != "" && id != "0" {
		apiSerial(id)
	}
	gads.Panic(`Can't call method "instance_id" on an undefined value`)
	return ""
}

// get '/api/:sheet/records/view/:id' (API.pm:280)
func (s *Server) hAPIRecordsView(c *Ctx) string {
	layout := c.apiLayout()
	viewID, _ := c.RouteParam("id")
	vid, _ := strconv.ParseInt(viewID, 10, 64)
	view := s.E.LoadView(vid, layout)
	if !view.Exists() {
		gads.Fail("View id %s not found", viewID)
	}
	records := s.E.NewRecords(layout, c.apiUser())
	records.View = view
	records.Rows = 100
	records.Page = 1
	if p, _ := c.QueryParam("page"); truthy(p) {
		records.Page = int(perlInt(p))
	}
	out := []any{}
	for _, rec := range records.Results() {
		out = append(out, rec.AsJSON(view))
	}
	c.ContentType("application/json; charset=UTF-8")
	return gads.EncodeJSON(out)
}
