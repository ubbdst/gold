package gold

import (
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// WAC WebAccessControl object
type WAC struct {
	req  *httpRequest
	srv  *Server
	w    http.ResponseWriter
	user string
}

// NewWAC creates a new WAC object
func NewWAC(req *httpRequest, srv *Server, w http.ResponseWriter, user string) *WAC {
	if len(req.Header.Get("On-Behalf-Of")) > 0 {
		delegator := debrack(req.Header.Get("On-Behalf-Of"))
		if verifyDelegator(delegator, user) {
			srv.debug.Println("Request User ID (delegation):", user)
			user = delegator
		}
	}
	return &WAC{req: req, srv: srv, w: w, user: user}
}

// Return an HTTP code and error (200 if authd, 401 if auth required, 403 if not authorized, 500 if error)
func (acl *WAC) allow(mode string, path string) (int, error) {
	origin := acl.req.Header.Get("Origin")
	accessType := "accessTo"
	p, err := acl.srv.pathInfo(path)
	if err != nil {
		return 500, err
	}
	depth := strings.Split(p.Path, "/")

	for i := 0; i < len(depth); i++ {
		p, err := acl.srv.pathInfo(path)
		if err != nil {
			return 500, err
		}

		if !p.Exists {
			continue
		}

		acl.srv.debug.Println("Checking " + accessType + " <" + mode + "> to " + p.URI + " for WebID: " + acl.user)
		acl.srv.debug.Println("Looking for policies in " + p.AclFile)

		aclGraph := NewGraph(p.AclURI)
		aclGraph.ReadFile(p.AclFile)
		if aclGraph.Len() > 0 {
			acl.srv.debug.Println("Found policies in " + p.AclFile)
			// TODO make it more elegant instead of duplicating code
			for _, i := range aclGraph.All(nil, ns.acl.Get("mode"), ns.acl.Get("Control")) {
				for range aclGraph.All(i.Subject, ns.acl.Get(accessType), NewResource(p.URI)) {
					for range aclGraph.All(i.Subject, ns.acl.Get("owner"), NewResource(acl.user)) {
						acl.srv.debug.Println(mode + " access allowed (as owner) for: " + acl.user)
						return 200, nil
					}
					for range aclGraph.All(i.Subject, ns.acl.Get("agent"), NewResource(acl.user)) {
						acl.srv.debug.Println(mode + " access allowed (as agent) for: " + acl.user)
						return 200, nil
					}
					for _, t := range aclGraph.All(i.Subject, ns.acl.Get("agentClass"), nil) {
						// check for foaf groups
						acl.srv.debug.Println("Found agentClass policy")
						if t.Object.Equal(ns.foaf.Get("Agent")) {
							acl.srv.debug.Println(mode + " access allowed as FOAF Agent")
							return 200, nil
						}

						groupURI := debrack(t.Object.String())
						groupGraph := NewGraph(groupURI)
						groupGraph.LoadURI(groupURI)
						if groupGraph.Len() > 0 && groupGraph.One(t.Object, ns.rdf.Get("type"), ns.foaf.Get("Group")) != nil {
							for range groupGraph.All(t.Object, ns.foaf.Get("member"), NewResource(acl.user)) {
								acl.srv.debug.Println(acl.user + " listed as a member of the group " + groupURI)
								return 200, nil
							}
						}
					}
				}
			}
			for _, i := range aclGraph.All(nil, ns.acl.Get("mode"), ns.acl.Get(mode)) {
				acl.srv.debug.Println("Found " + accessType + " policy for <" + mode + ">")

				for range aclGraph.All(i.Subject, ns.acl.Get(accessType), NewResource(p.URI)) {
					origins := aclGraph.All(i.Subject, ns.acl.Get("origin"), nil)
					if len(origin) > 0 && len(origins) > 0 {
						acl.srv.debug.Println("Origin set to: " + brack(origin))
						for _, o := range origins {
							if brack(origin) == o.Object.String() {
								acl.srv.debug.Println("Found policy for origin: " + o.Object.String())
								goto allowOrigin
							}
						}
						continue
					} else {
						acl.srv.debug.Println("No origin found, moving on")
					}
				allowOrigin:
					acl.srv.debug.Println("In allowOrigin")
					for range aclGraph.All(i.Subject, ns.acl.Get("owner"), NewResource(acl.user)) {
						acl.srv.debug.Println(mode + " access allowed (as owner) for: " + acl.user)
						return 200, nil
					}
					for range aclGraph.All(i.Subject, ns.acl.Get("agent"), NewResource(acl.user)) {
						acl.srv.debug.Println(mode + " access allowed (as agent) for: " + acl.user)
						return 200, nil
					}
					for _, t := range aclGraph.All(i.Subject, ns.acl.Get("agentClass"), nil) {
						// check for foaf groups
						acl.srv.debug.Println("Found agentClass policy")
						if t.Object.Equal(ns.foaf.Get("Agent")) {
							acl.srv.debug.Println(mode + " access allowed as FOAF Agent")
							return 200, nil
						}
						groupURI := debrack(t.Object.String())
						groupGraph := NewGraph(groupURI)
						groupGraph.LoadURI(groupURI)
						if groupGraph.Len() > 0 && groupGraph.One(t.Object, ns.rdf.Get("type"), ns.foaf.Get("Group")) != nil {
							for range groupGraph.All(t.Object, ns.foaf.Get("member"), NewResource(acl.user)) {
								acl.srv.debug.Println(acl.user + " listed as a member of the group " + groupURI)
								return 200, nil
							}
						}
					}
				}
			}
			if len(acl.user) == 0 {
				acl.srv.debug.Println("Authentication required")
				tokenValues := map[string]string{
					"secret": string(acl.srv.cookieSalt),
				}
				// set validity for now + 1 min
				validity := 1 * time.Minute
				token, err := NewSecureToken("WWW-Authenticate", tokenValues, validity, acl.srv)
				if err != nil {
					acl.srv.debug.Println("Error generating Auth token: ", err)
					return 500, err
				}
				wwwAuth := `WebID-RSA nonce="` + token + `"`
				acl.w.Header().Set("WWW-Authenticate", wwwAuth)
				return 401, errors.New("Access to " + p.URI + " requires authentication")
			}
			acl.srv.debug.Println(mode + " access denied for: " + acl.user)
			return 403, errors.New("Access denied for: " + acl.user)
		}

		accessType = "defaultForNew"

		if i == 0 {
			if strings.HasSuffix(p.Path, "/") {
				if filepath.Dir(filepath.Dir(p.Path)) == "." {
					path = p.Base
				} else {
					path = p.Base + "/" + filepath.Dir(filepath.Dir(p.Path))
				}
			} else {
				path = p.Base + "/" + filepath.Dir(p.Path)
			}
		} else {
			if len(p.Path) == 0 {
				break
			} else if filepath.Dir(filepath.Dir(p.Path)) == "." {
				path = p.Base
			} else {
				path = p.Base + "/" + filepath.Dir(filepath.Dir(p.Path))
			}
		}

		path += "/"
	}
	acl.srv.debug.Println("No ACL policies present - access allowed")
	return 200, nil
}

// AllowRead checks if Read access is allowed
func (acl *WAC) AllowRead(path string) (int, error) {
	return acl.allow("Read", path)
}

// AllowWrite checks if Write access is allowed
func (acl *WAC) AllowWrite(path string) (int, error) {
	return acl.allow("Write", path)
}

// AllowAppend checks if Append access is allowed
func (acl *WAC) AllowAppend(path string) (int, error) {
	return acl.allow("Append", path)
}

// AllowControl checks if Control access is allowed
func (acl *WAC) AllowControl(path string) (int, error) {
	return acl.allow("Control", path)
}

func verifyDelegator(delegator string, delegatee string) bool {
	g := NewGraph(delegator)
	err := g.LoadURI(delegator)
	if err != nil {
		log.Println("Error loading graph for " + delegator)
	}

	for _, val := range g.All(NewResource(delegator), NewResource("http://www.w3.org/ns/auth/acl#delegates"), nil) {
		if debrack(val.Object.String()) == delegatee {
			return true
		}
	}
	return false
}
