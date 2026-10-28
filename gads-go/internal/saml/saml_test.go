package saml

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
)

const metadata = `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/idp">
<md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso?realm=main"/>
<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://idp.example.com/post"/>
</md:IDPSSODescriptor>
</md:EntityDescriptor>`

func TestParseIdP(t *testing.T) {
	idp, err := ParseIdP(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if idp.EntityID != "https://idp.example.com/idp" {
		t.Errorf("entity ID = %q", idp.EntityID)
	}
	if got := idp.SSOURL(BindingRedirect); got != "https://idp.example.com/sso?realm=main" {
		t.Errorf("redirect SSO URL = %q", got)
	}
	if _, err := ParseIdP(`<other/>`); err == nil {
		t.Error("expected an error for metadata of another namespace")
	}
}

func TestAuthnRequestXML(t *testing.T) {
	a := &AuthnRequest{
		ID:           "NETSAML2_1",
		IssueInstant: time.Date(2014, 10, 10, 1, 0, 0, 0, time.UTC),
		Issuer:       "http://localhost/saml/xml",
		Destination:  "https://idp/sso?a=1&b=2",
	}
	want := `<samlp:AuthnRequest Destination="https://idp/sso?a=1&amp;b=2" ID="NETSAML2_1" IssueInstant="2014-10-10T01:00:00Z" Version="2.0" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"><saml:Issuer>http://localhost/saml/xml</saml:Issuer></samlp:AuthnRequest>`
	if got := a.XML(); got != want {
		t.Errorf("XML =\n%s\nwant\n%s", got, want)
	}
}

// The request of a redirect inflates back to the XML, and the parameter is
// added to the query of the SSO URL
func TestRedirectURI(t *testing.T) {
	uri, err := RedirectURI("https://idp.example.com/sso?realm=main", "<x>request</x>")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(uri)
	if u.Query().Get("realm") != "main" || !strings.Contains(uri, "?realm=main&SAMLRequest=") {
		t.Errorf("uri = %s", uri)
	}
	raw, err := base64.StdEncoding.DecodeString(u.Query().Get("SAMLRequest"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
	if string(got) != "<x>request</x>" {
		t.Errorf("inflated = %q", got)
	}
}

func TestGenerateID(t *testing.T) {
	id := GenerateID()
	if !strings.HasPrefix(id, "NETSAML2_") || len(id) != len("NETSAML2_")+64 || id == GenerateID() {
		t.Errorf("id = %q", id)
	}
}
