// TODO: test acl with glob
package gold

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	// "github.com/drewolson/testflight"
	"github.com/stretchr/testify/assert"
)

const (
	aclDir = "/_test/acldir/"
)

var (
	user1, user2         string
	user1g, user2g       *Graph
	user1k, user2k       *rsa.PrivateKey
	user1p, user2p       *rsa.PublicKey
	user1h, user2h       *http.Client
	user1cert, user2cert *tls.Certificate
	testServer           *httptest.Server
)

func init() {
	handler.Config.Debug = true
	testServer = httptest.NewUnstartedServer(handler)
	testServer.TLS = new(tls.Config)
	testServer.TLS.ClientAuth = tls.RequestClientCert
	testServer.TLS.NextProtos = []string{"http/1.1"}
	testServer.StartTLS()
	testServer.URL = strings.Replace(testServer.URL, "127.0.0.1", "localhost", 1)
}

func TestACLInit(t *testing.T) {
	var err error

	user1 = testServer.URL + "/_test/user1#id"
	user1g, user1k, user1p, err = NewWebIDProfileWithKeys(user1)
	user1cert, err = NewRSAcert(user1, "User 1", user1k)
	assert.NoError(t, err)
	user1h = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates:       []tls.Certificate{*user1cert},
				InsecureSkipVerify: true,
			},
		},
	}
	user1n3, err := user1g.Serialize("text/turtle")
	assert.NoError(t, err)
	req1, err := http.NewRequest("PUT", user1, strings.NewReader(user1n3))
	assert.NoError(t, err)
	resp1, err := httpClient.Do(req1)
	assert.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, 201, resp1.StatusCode)

	user2 = testServer.URL + "/_test/user2#id"
	user2g, user2k, user2p, err = NewWebIDProfileWithKeys(user2)
	user2cert, err = NewRSAcert(user2, "User 2", user2k)
	assert.NoError(t, err)
	user2h = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates:       []tls.Certificate{*user2cert},
				InsecureSkipVerify: true,
				Rand:               rand.Reader,
			},
		},
	}
	user2n3, err := user2g.Serialize("text/turtle")
	assert.NoError(t, err)
	req2, err := http.NewRequest("PUT", user2, strings.NewReader(user2n3))
	assert.NoError(t, err)
	resp2, err := httpClient.Do(req2)
	assert.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, 201, resp2.StatusCode)

	req1, err = http.NewRequest("GET", user1, nil)
	assert.NoError(t, err)
	resp1, err = user1h.Do(req1)
	assert.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, user1, resp1.Header.Get("User"))

	req2, err = http.NewRequest("GET", user2, nil)
	assert.NoError(t, err)
	resp2, err = user2h.Do(req2)
	assert.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, user2, resp2.Header.Get("User"))
}

func TestACLEmpty(t *testing.T) {
	request, err := http.NewRequest("MKCOL", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")
	assert.NotNil(t, acl)

	request, err = http.NewRequest("PUT", acl, strings.NewReader(""))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	request, err = http.NewRequest("PUT", testServer.URL+aclDir+"abc", strings.NewReader("<a> <b> <c> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	acl = ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")
	assert.NotNil(t, acl)

	request, err = http.NewRequest("PUT", acl, strings.NewReader(""))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
}

func TestACLOrigin(t *testing.T) {
	origin1 := "http://example.org/"
	origin2 := "http://example.com/"

	request, err := http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")

	body := "<#Owner>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + testServer.URL + aclDir + ">, <" + acl + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agent> <" + user1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#origin> <" + origin1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read>, <http://www.w3.org/ns/auth/acl#Write> ." +
		"<#Public>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + testServer.URL + aclDir + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agentClass> <http://xmlns.com/foaf/0.1/Agent>;" +
		"	<http://www.w3.org/ns/auth/acl#origin> <" + origin1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read> ."
	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)
	assert.Equal(t, "10", response.Header.Get("Triples"))

	// user1
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	request.Header.Add("Origin", origin1)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	request.Header.Add("Origin", origin2)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	// agent
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	request.Header.Add("Origin", origin1)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	request.Header.Add("Origin", origin2)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)
}

func TestACLOwnerOnly(t *testing.T) {
	request, err := http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")

	body := "<#Owner>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + testServer.URL + aclDir + ">, <" + acl + ">;" +
		"	<http://www.w3.org/ns/auth/acl#owner> <" + user1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Control> ."
	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)
	assert.Equal(t, "4", response.Header.Get("Triples"))

	// user1
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "4", response.Header.Get("Triples"))

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+"/_test/acldir", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	// user2
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("PUT", acl, strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	// agent
	request, err = http.NewRequest("PUT", acl, strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)
}

func TestACLReadOnly(t *testing.T) {
	request, err := http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")

	body := "<#Owner>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + testServer.URL + aclDir + ">, <" + acl + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agent> <" + user1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read>, <http://www.w3.org/ns/auth/acl#Write> ." +
		"<#Public>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + testServer.URL + aclDir + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agentClass> <http://xmlns.com/foaf/0.1/Agent>;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read> ."
	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	// user1
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "8", response.Header.Get("Triples"))

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	// user2
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("PUT", acl, strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	// agent
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("PUT", acl, strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)

	request, err = http.NewRequest("DELETE", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
}

func TestACLAppendOnly(t *testing.T) {
	request, err := http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")

	body := "<#Owner>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + testServer.URL + aclDir + "abc>, <" + acl + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agent> <" + user1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read>, <http://www.w3.org/ns/auth/acl#Write> ." +
		"<#AppendOnly>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + testServer.URL + aclDir + "abc>;" +
		"	<http://www.w3.org/ns/auth/acl#agentClass> <http://xmlns.com/foaf/0.1/Agent>;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Append> ."
	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	// user1
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "8", response.Header.Get("Triples"))

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abc", strings.NewReader("<a> <b> <c> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "1", response.Header.Get("Triples"))

	// user2
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abc", strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "2", response.Header.Get("Triples"))

	// agent
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abc", strings.NewReader("<g> <h> <i> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "3", response.Header.Get("Triples"))

	request, err = http.NewRequest("DELETE", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
}

func TestACLRestricted(t *testing.T) {
	request, err := http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")

	body := "<#Owner>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + aclDir + "abc>, <" + acl + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agent> <" + user1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read>, <http://www.w3.org/ns/auth/acl#Write> ." +
		"<#Restricted>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + aclDir + "abc>;" +
		"	<http://www.w3.org/ns/auth/acl#agent> <" + user2 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read>, <http://www.w3.org/ns/auth/acl#Write>."
	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	// user1
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "9", response.Header.Get("Triples"))

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("PUT", testServer.URL+aclDir+"abc", strings.NewReader("<a> <b> <c> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)
	assert.Equal(t, "1", response.Header.Get("Triples"))

	// user2
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abc", strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "2", response.Header.Get("Triples"))

	// agent
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abc", strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)

	request, err = http.NewRequest("DELETE", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
}

func TestACLGroup(t *testing.T) {
	request, err := http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")

	groupTriples := "<#> a <http://xmlns.com/foaf/0.1/Group>;" +
		"	<http://xmlns.com/foaf/0.1/member> <a>, <b>, <" + user2 + ">."

	request, err = http.NewRequest("PUT", testServer.URL+aclDir+"group", strings.NewReader(groupTriples))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	body := "<#Owner>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + aclDir + "abc>, <" + acl + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agent> <" + user1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#defaultForNew> <" + aclDir + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read>, <http://www.w3.org/ns/auth/acl#Write> ." +
		"<#Group>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + aclDir + "abc>;" +
		"	<http://www.w3.org/ns/auth/acl#agentClass> <" + testServer.URL + aclDir + "group#>;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read> ."
	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	// user1
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	request.Header.Add("Accept", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "9", response.Header.Get("Triples"))

	request, err = http.NewRequest("PUT", testServer.URL+aclDir+"abc", strings.NewReader("<a> <b> <c> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)
	assert.Equal(t, "1", response.Header.Get("Triples"))

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	// user2
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	response, err = user2h.Do(request)
	response.Body.Close()
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abc", strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	// agent
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abc", strings.NewReader("<d> <e> <f> ."))
	request.Header.Add("Content-Type", "text/turtle")
	response, err = httpClient.Do(request)
	response.Body.Close()
	assert.NoError(t, err)
	assert.Equal(t, 401, response.StatusCode)

	request, err = http.NewRequest("DELETE", testServer.URL+aclDir+"group", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("DELETE", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
}

func TestACLDefaultForNew(t *testing.T) {
	request, err := http.NewRequest("HEAD", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	acl := ParseLinkHeader(response.Header.Get("Link")).MatchRel("acl")

	body := "<#Owner>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + aclDir + ">, <" + acl + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agent> <" + user1 + ">;" +
		"	<http://www.w3.org/ns/auth/acl#defaultForNew> <" + aclDir + ">;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read>, <http://www.w3.org/ns/auth/acl#Write> ." +
		"<#Default>" +
		"	<http://www.w3.org/ns/auth/acl#accessTo> <" + aclDir + ">;" +
		"	<http://www.w3.org/ns/auth/acl#defaultForNew> <" + aclDir + ">;" +
		"	<http://www.w3.org/ns/auth/acl#agentClass> <http://xmlns.com/foaf/0.1/Agent>;" +
		"	<http://www.w3.org/ns/auth/acl#mode> <http://www.w3.org/ns/auth/acl#Read> ."
	request, err = http.NewRequest("PUT", acl, strings.NewReader(body))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)

	// user1
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
	assert.Equal(t, "10", response.Header.Get("Triples"))

	request, err = http.NewRequest("PUT", testServer.URL+aclDir+"abcd", strings.NewReader("<a> <b> <c> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 201, response.StatusCode)
	assert.Equal(t, "1", response.Header.Get("Triples"))

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abcd", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	// user2
	request, err = http.NewRequest("HEAD", acl, nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abcd", nil)
	assert.NoError(t, err)
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abcd", strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 403, response.StatusCode)

	// agent
	request, err = http.NewRequest("HEAD", testServer.URL+aclDir+"abcd", nil)
	assert.NoError(t, err)
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abcd", strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err = httpClient.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 401, response.StatusCode)
}

func TestACLWebIDDelegation(t *testing.T) {
	request, err := http.NewRequest("POST", user1, strings.NewReader("<"+user1+"> <http://www.w3.org/ns/auth/acl#delegates> <"+user2+"> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("POST", testServer.URL+aclDir+"abcd", strings.NewReader("<d> <e> <f> ."))
	assert.NoError(t, err)
	request.Header.Add("Content-Type", "text/turtle")
	request.Header.Add("On-Behalf-Of", "<"+user1+">")
	response, err = user2h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
}

func TestACLCleanUp(t *testing.T) {
	request, err := http.NewRequest("DELETE", testServer.URL+aclDir+"abcd", nil)
	assert.NoError(t, err)
	response, err := user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("DELETE", testServer.URL+aclDir+"abc", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("DELETE", testServer.URL+aclDir+",acl", nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)

	request, err = http.NewRequest("DELETE", testServer.URL+aclDir, nil)
	assert.NoError(t, err)
	response, err = user1h.Do(request)
	assert.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 200, response.StatusCode)
}

// func TestACLCleanUsers(t *testing.T) {
// 	testflight.WithServer(handler, func(r *testflight.Requester) {
// 		response := r.Delete("/_test/user1", "", "")
// 		assert.Equal(t, 200, response.StatusCode)
// 		response = r.Delete("/_test/user2", "", "")
// 		assert.Equal(t, 200, response.StatusCode)
// 	})
// }
