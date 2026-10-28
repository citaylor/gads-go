package web

import (
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/saml"
)

// samlSSOXML is GADS::SAML sso_xml: the issuer of requests
func (c *Ctx) samlSSOXML() string { return c.Base() + "saml/xml" }

// samlInitiate is GADS::SAML initiate as the login page does it: the
// request is redirected to the identity provider, remembering its ID and
// the provider in the session
func (s *Server) samlInitiate(c *Ctx, auth db.Row) {
	metadata, _ := db.AsString(auth["xml"])
	idp, err := saml.ParseIdP(metadata)
	if err != nil {
		gads.Panic("Invalid identity provider metadata: %s", err)
	}
	id := saml.GenerateID()
	if s.E.SAMLIDGenerator != nil {
		id = s.E.SAMLIDGenerator()
	}
	ssoURL := idp.SSOURL(saml.BindingRedirect)
	req := &saml.AuthnRequest{
		ID:           id,
		IssueInstant: s.E.Now(),
		Issuer:       c.samlSSOXML(),
		Destination:  ssoURL,
	}
	redirect, err := saml.RedirectURI(ssoURL, req.XML())
	if err != nil {
		gads.Panic("%s", err)
	}
	c.SessionSet("request_id", id)
	c.SessionSet("authentication_id", auth["id"])
	c.Redirect(redirect)
}

// post '/saml' (GADS.pm:478): the response of the identity provider.
// GADS::SAML gives the POST binding of Net::SAML2 no certificate to verify
// the response with, which Net::SAML2 0.89 refuses, so this always fails
// (before the response or the provider is looked at).
func (s *Server) hSamlPost(c *Ctx) string {
	gads.Panic("%s", saml.ErrNoVerification)
	return ""
}
