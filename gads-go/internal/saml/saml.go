// Package saml is the SAML 2.0 single sign-on of GADS (GADS::SAML), with
// the behaviour of the Net::SAML2 (0.89) that it uses.
package saml

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"

	"net/url"
	"strings"
	"time"
)

const (
	nsMetadata  = "urn:oasis:names:tc:SAML:2.0:metadata"
	nsProtocol  = "urn:oasis:names:tc:SAML:2.0:protocol"
	nsAssertion = "urn:oasis:names:tc:SAML:2.0:assertion"

	// BindingRedirect is the HTTP-Redirect binding
	BindingRedirect = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
)

// IdP is the metadata of an identity provider (Net::SAML2::IdP)
type IdP struct {
	EntityID string
	// SSOURLs are the single sign-on URLs by binding
	SSOURLs map[string]string
}

type mdService struct {
	Binding  string `xml:"Binding,attr"`
	Location string `xml:"Location,attr"`
}

type mdEntity struct {
	XMLName  xml.Name `xml:"EntityDescriptor"`
	EntityID string   `xml:"entityID,attr"`
	IDP      []struct {
		SSO []mdService `xml:"urn:oasis:names:tc:SAML:2.0:metadata SingleSignOnService"`
	} `xml:"urn:oasis:names:tc:SAML:2.0:metadata IDPSSODescriptor"`
}

// ParseIdP is IdP->new_from_xml: the metadata of an identity provider
func ParseIdP(metadata string) (*IdP, error) {
	var ent mdEntity
	if err := xml.Unmarshal([]byte(metadata), &ent); err != nil {
		return nil, err
	}
	if ent.XMLName.Space != nsMetadata {
		return nil, fmt.Errorf("not SAML metadata: %s", ent.XMLName.Space)
	}
	idp := &IdP{EntityID: ent.EntityID, SSOURLs: map[string]string{}}
	for _, d := range ent.IDP {
		for _, s := range d.SSO {
			idp.SSOURLs[s.Binding] = s.Location
		}
	}
	return idp, nil
}

// SSOURL is IdP->sso_url: the single sign-on URL of a binding
func (idp *IdP) SSOURL(binding string) string { return idp.SSOURLs[binding] }

// GenerateID is Net::SAML2::Util generate_id: "NETSAML2_" and 32 random
// bytes in hex
func GenerateID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "NETSAML2_" + hex.EncodeToString(b)
}

// AuthnRequest is Net::SAML2::Protocol::AuthnRequest (with an issuer and
// a destination only, as GADS makes it)
type AuthnRequest struct {
	ID           string
	IssueInstant time.Time
	Issuer       string
	Destination  string
}

// escape is XML::Generator's escaping of attribute values and text
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// XML is AuthnRequest->as_xml. XML::Generator puts the namespaces on the
// root element; Perl's attributes are in the order of a hash, these are
// sorted.
func (a *AuthnRequest) XML() string {
	var b strings.Builder
	b.WriteString("<samlp:AuthnRequest")
	attrs := [][2]string{}
	if a.Destination != "" {
		attrs = append(attrs, [2]string{"Destination", a.Destination})
	}
	attrs = append(attrs,
		[2]string{"ID", a.ID},
		[2]string{"IssueInstant", a.IssueInstant.UTC().Format("2006-01-02T15:04:05Z")},
		[2]string{"Version", "2.0"},
		[2]string{"xmlns:saml", nsAssertion},
		[2]string{"xmlns:samlp", nsProtocol},
	)
	for _, at := range attrs {
		fmt.Fprintf(&b, ` %s="%s"`, at[0], escape(at[1]))
	}
	b.WriteString("><saml:Issuer>")
	b.WriteString(escape(a.Issuer))
	b.WriteString("</saml:Issuer></samlp:AuthnRequest>")
	return b.String()
}

// RedirectURI is Net::SAML2::Binding::Redirect->get_redirect_uri of an
// insecure (unsigned) request: the request deflated, in base64, as the
// SAMLRequest parameter of the URL
func RedirectURI(ssoURL, request string) (string, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return "", err
	}
	if _, err := w.Write([]byte(request)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	u, err := url.Parse(ssoURL)
	if err != nil {
		return "", err
	}
	// URI::QueryParam query_param appends the parameter to the query
	q := u.RawQuery
	if q != "" {
		q += "&"
	}
	u.RawQuery = q + "SAMLRequest=" + url.QueryEscape(base64.StdEncoding.EncodeToString(buf.Bytes()))
	return u.String(), nil
}

// ErrNoVerification is the error of Net::SAML2::Binding::POST
// handle_response (0.89) when the binding has nothing to verify the
// signature of a response with. GADS::SAML makes its binding without a
// CA certificate or certificate, so every response fails.
var ErrNoVerification = "Net::SAML2::Binding::POST::handle_response requires 'cacert' " +
	"or 'cert_text' on the binding to verify SAML response " +
	"signatures. To explicitly disable signature verification " +
	"(test/dev only), pass insecure_trust_embedded_cert => 1 to new()."
