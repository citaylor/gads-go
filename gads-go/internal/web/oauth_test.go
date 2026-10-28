package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// The bearer token of a deleted user gives no API user
func TestAPITokenOfDeletedUser(t *testing.T) {
	d, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Deploy(); err != nil {
		t.Fatal(err)
	}
	e := &gads.Engine{DB: d, Clock: time.Now}
	s := &Server{E: e, Config: DefaultConfig()}
	d.InsertMap("user", map[string]any{"id": int64(1), "username": "a@example.com"})
	d.InsertMap("oauthclient", map[string]any{"id": int64(1), "client_id": "c", "client_secret": "s"})
	d.InsertMap("oauthtoken", map[string]any{"token": "tok", "related_token": "r", "oauthclient_id": int64(1),
		"user_id": int64(1), "type": "access", "expires": time.Now().Unix() + 3600})
	apiUser := func() any {
		c := &Ctx{s: s, E: e, Req: httptest.NewRequest("GET", "/api/x", nil), vars: map[string]any{}, headers: http.Header{}}
		c.Req.Header.Set("Authorization", "Bearer tok")
		s.apiBeforeHook(c)
		return c.Var("api_user")
	}
	if apiUser() == nil {
		t.Fatal("valid token gives no user")
	}
	d.Exec(`UPDATE "user" SET "deleted" = '2020-01-01 00:00:00' WHERE "id" = 1`)
	if apiUser() != nil {
		t.Error("token of a deleted user accepted")
	}
}
