package gold

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	_path "path"
	"path/filepath"
	"strings"

	"github.com/gorilla/securecookie"
	"github.com/presbrey/magicmime"
	"golang.org/x/net/webdav"
)

const (
	// HCType is the header Content-Type
	HCType = "Content-Type"
	// METASuffix is the generic name for metadata corresponding to a given resource
	METASuffix = ".meta"
	// ACLSuffix is the generic name for the acl corresponding to a given resource
	ACLSuffix = ".acl"
	// SystemPrefix is the generic name for the system-reserved namespace (e.g. APIs)
	SystemPrefix = ".system"
	// ProxyPath provides CORS proxy (empty to disable)
	ProxyPath = ".proxy"
)

var (
	// Streaming (stream data or not)
	Streaming = false // experimental

	debugFlags  = log.Flags() | log.Lshortfile
	debugPrefix = "[debug] "

	magic *magicmime.Magic

	methodsAll = []string{
		"OPTIONS", "HEAD", "GET",
		"PATCH", "POST", "PUT", "MKCOL", "DELETE",
		"COPY", "MOVE", "LOCK", "UNLOCK",
	}
)

func init() {
	var err error

	magic, err = magicmime.New()
	if err != nil {
		panic(err)
	}
}

type errorString struct {
	s string
}

func (e *errorString) Error() string {
	return e.s
}

type httpRequest struct {
	*http.Request
	*Server
}

func (req httpRequest) BaseURI() string {
	scheme := "http"
	if req.TLS != nil || req.Header.Get("X-Forwarded-Proto") == "https" {
		scheme += "s"
	}
	host, port, err := net.SplitHostPort(req.Host)
	if err != nil {
		host = req.Host
	}
	if len(host) == 0 {
		host = "localhost"
	}
	if len(port) > 0 {
		port = ":" + port
	}
	if (scheme == "https" && port == ":443") || (scheme == "http" && port == ":80") {
		port = ""
	}
	return scheme + "://" + host + port + req.URL.Path
}

func (req httpRequest) ifMatch(etag string) bool {
	if len(etag) == 0 {
		return true
	}
	if len(req.Header.Get("If-Match")) == 0 {
		return true
	}
	val := strings.Split(req.Header.Get("If-Match"), ",")
	for _, v := range val {
		v = strings.TrimSpace(v)
		if v == "*" || v == etag {
			return true
		}
	}
	return false
}

func (req httpRequest) ifNoneMatch(etag string) bool {
	if len(etag) == 0 {
		return true
	}
	if len(req.Header.Get("If-None-Match")) == 0 {
		return true
	}
	val := strings.Split(req.Header.Get("If-None-Match"), ",")
	for _, v := range val {
		v = strings.TrimSpace(v)
		if v != "*" && v != etag {
			return true
		}
	}
	return false
}

func handleStatusText(status int, err error) string {
	switch status {
	case 200:
		return "HTTP 200 - OK"
	case 401:
		return "HTTP 401 - Unauthorized\n\n" + err.Error()
	case 403:
		return "HTTP 403 - Forbidden\n\n" + err.Error()
	case 404:
		return "HTTP 404 - Not found\n\n" + err.Error()
	case 500:
		return "HTTP 500 - Internal Server Error\n\n" + err.Error()
	default: // 501
		return "HTTP 501 - Not implemented\n\n" + err.Error()
	}
}

// Server object contains http handler, root where the data is found and whether it uses vhosts or not
type Server struct {
	http.Handler

	Config     *ServerConfig
	cookie     *securecookie.SecureCookie
	cookieSalt []byte
	debug      *log.Logger
	webdav     *webdav.Handler
}

// NewServer is used to create a new Server instance
func NewServer(config *ServerConfig) *Server {
	s := &Server{
		Config:     config,
		cookie:     securecookie.New(securecookie.GenerateRandomKey(64), securecookie.GenerateRandomKey(32)),
		cookieSalt: securecookie.GenerateRandomKey(32),
		webdav: &webdav.Handler{
			FileSystem: webdav.Dir(config.DataRoot),
			LockSystem: webdav.NewMemLS(),
		},
	}
	if config.Debug {
		s.debug = log.New(os.Stderr, debugPrefix, debugFlags)
	} else {
		s.debug = log.New(ioutil.Discard, "", 0)
	}
	s.debug.Println("---- starting server ----")
	s.debug.Printf("config: %#v\n", s.Config)
	return s
}

type response struct {
	status  int
	headers http.Header

	argv []interface{}
}

func (r *response) respond(status int, a ...interface{}) *response {
	r.status = status
	r.argv = a
	return r
}

// ServeHTTP handles the response
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if ProxyPath != "" && strings.HasPrefix(req.URL.Path, "/"+ProxyPath) {
		uri, err := url.Parse(req.FormValue("uri"))
		if err != nil {
			s.debug.Println(req.RequestURI, err.Error())
		}
		req.URL = uri
		req.Host = uri.Host
		req.RequestURI = uri.RequestURI()
		proxy.ServeHTTP(w, req)
		return
	}
	if websocketUpgrade(req) {
		websocketServe(w, req)
		return
	}

	defer func() {
		req.Body.Close()
	}()
	r := s.handle(w, &httpRequest{req, s})
	for key := range r.headers {
		w.Header().Set(key, r.headers.Get(key))
	}
	if r.status > 0 {
		w.WriteHeader(r.status)
	}
	if len(r.argv) > 0 {
		fmt.Fprint(w, r.argv...)
	}
}

func (s *Server) handle(w http.ResponseWriter, req *httpRequest) (r *response) {
	r = new(response)
	var err error

	s.debug.Println("\n------ New " + req.Method + " request from " + req.RemoteAddr + " ------")

	// CORS
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Expose-Headers", "User, Location, Link, Vary, Last-Modified, Content-Length")
	w.Header().Set("Access-Control-Max-Age", "1728000")

	// RWW
	w.Header().Set("MS-Author-Via", "DAV, SPARQL")
	w.Header().Set("Updates-Via", "wss://"+req.Host+"/")

	// TODO: WAC
	origin := ""
	origins := req.Header["Origin"] // all CORS requests
	if len(origins) > 0 {
		origin = origins[0]
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}

	// Authentication
	user := req.authn(w)
	w.Header().Set("User", user)
	acl := NewWAC(req, s, w, user)

	// Intercept API requests
	if strings.HasPrefix(req.Request.URL.Path, "/"+SystemPrefix) && req.Method != "OPTIONS" {
		resp := HandleSystem(w, req, s)
		if resp.Bytes != nil && len(resp.Bytes) > 0 {
			// copy raw bytes
			io.Copy(w, bytes.NewReader(resp.Bytes))
			return
		}
		return r.respond(resp.Status, resp.Body)
	}

	resource, _ := s.pathInfo(req.BaseURI())
	s.debug.Println(req.RemoteAddr + " requested resource URI: " + resource.URI)
	s.debug.Println(req.RemoteAddr + " requested resource Path: " + resource.File)

	dataMime := req.Header.Get(HCType)
	dataMime = strings.Split(dataMime, ";")[0]
	dataHasParser := len(mimeParser[dataMime]) > 0
	if len(dataMime) > 0 {
		s.debug.Println("Content-Type: " + dataMime)
		if dataMime != "multipart/form-data" && !dataHasParser && req.Method != "PUT" && req.Method != "HEAD" && req.Method != "OPTIONS" {
			s.debug.Println("Request contains unsupported Media Type:" + dataMime)
			return r.respond(415, "HTTP 415 - Unsupported Media Type:", dataMime)
		}
	}

	// Content Negotiation
	contentType := "text/turtle"
	acceptList, _ := req.Accept()
	if len(acceptList) > 0 && acceptList[0].SubType != "*" {
		contentType, err = acceptList.Negotiate(serializerMimes...)
		if err != nil {
			s.debug.Println("Accept type not acceptable: " + err.Error())
			return r.respond(406, "HTTP 406 - Accept type not acceptable: "+err.Error())
		}
	}

	// set ACL Link header
	w.Header().Set("Link", brack(resource.AclURI)+"; rel=\"acl\", "+brack(resource.MetaURI)+"; rel=\"meta\"")

	// generic headers
	w.Header().Set("Accept-Patch", "application/json, application/sparql-update")
	w.Header().Set("Accept-Post", "text/turtle, application/ld+json, application/json")
	w.Header().Set("Allow", strings.Join(methodsAll, ", "))

	switch req.Method {
	case "OPTIONS":
		// TODO: WAC
		corsReqH := req.Header["Access-Control-Request-Headers"] // CORS preflight only
		if len(corsReqH) > 0 {
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(corsReqH, ", "))
		}
		corsReqM := req.Header["Access-Control-Request-Method"] // CORS preflight only
		if len(corsReqM) > 0 {
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(corsReqM, ", "))
		} else {
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(methodsAll, ", "))
		}
		if len(origin) < 1 {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		// set LDP Link headers
		stat, err := os.Stat(resource.File)
		if err == nil && stat.IsDir() {
			w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#BasicContainer")+"; rel=\"type\"")
		}
		w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#Resource")+"; rel=\"type\"")

		// set API Link headers
		//@@TODO create the vocabulary
		w.Header().Add("Link", brack(resource.Base+"/"+SystemPrefix+"/newCert")+"; rel=\"http://example.org/services#newCert\"")
		w.Header().Add("Link", brack(resource.Base+"/"+SystemPrefix+"/accountRecovery")+"; rel=\"http://example.org/services#accountRecovery\"")
		w.Header().Add("Link", brack(resource.Base+"/"+SystemPrefix+"/newAccount")+"; rel=\"http://example.org/services#newAccount\"")
		w.Header().Add("Link", brack(resource.Base+"/"+SystemPrefix+"/accountInfo")+"; rel=\"http://example.org/services#accountInfo\"")

		return r.respond(200)

	case "GET", "HEAD":
		var (
			magicType = resource.FileType
			maybeRDF  bool
			glob      bool
			globPath  string
			etag      string
		)

		// check for glob
		glob = false
		if strings.Contains(resource.Obj.Path, "*") {
			glob = true
			path := filepath.Dir(resource.Obj.Path)
			globPath = resource.File
			if path == "." {
				path = ""
			} else {
				path += "/"
			}
			resource, err = s.pathInfo(resource.Base + "/" + path)
			if err != nil {
				return r.respond(500, err)
			}
		}

		// overwrite ACL Link header
		w.Header().Set("Link", brack(resource.AclURI)+"; rel=\"acl\", "+brack(resource.MetaURI)+"; rel=\"meta\"")

		// check if resource exists and set LDP Link headers
		stat, err := os.Stat(resource.File)
		if err != nil {
			// redirect to skin
			if s.Config.Vhosts && resource.Base == strings.TrimRight(req.BaseURI(), "/") && contentType == "text/html" && req.Method != "HEAD" {
				w.Header().Set(HCType, contentType)
				urlStr := s.Config.SignUpSkin + url.QueryEscape(resource.Obj.Scheme+"://"+resource.Obj.Host+"/"+SystemPrefix+"/accountStatus")
				http.Redirect(w, req.Request, urlStr, 303)
				return
			}
			s.debug.Println("Got a stat error: " + err.Error())

			return r.respond(404, Skins["404"])
		}

		if stat.IsDir() {
			w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#BasicContainer")+"; rel=\"type\"")
		}
		if req.Method == "HEAD" && stat != nil {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
		}
		w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#Resource")+"; rel=\"type\"")

		status := 501
		aclStatus, err := acl.AllowRead(resource.URI)
		if aclStatus > 200 || err != nil {
			return r.respond(aclStatus, handleStatusText(aclStatus, err))
		}

		unlock := lock(resource.File)
		defer unlock()

		etag, err = NewETag(resource.File)
		if err != nil {
			return r.respond(500, err)
		}
		w.Header().Set("ETag", "\""+etag+"\"")

		if !req.ifMatch("\"" + etag + "\"") {
			return r.respond(412, "412 - Precondition Failed")
		}
		if !req.ifNoneMatch("\""+etag+"\"") && !stat.IsDir() && contentType != "text/html" {
			// do not return cached views of dirs for html requests
			return r.respond(304, "304 - Not Modified")
		}

		g := NewGraph(resource.URI)

		switch {
		case stat.IsDir():
			if len(s.Config.DirIndex) > 0 && contentType == "text/html" {
				magicType = "text/html"
				maybeRDF = false
				for _, dirIndex := range s.Config.DirIndex {
					_, xerr := os.Stat(resource.File + dirIndex)
					status = 200
					if xerr == nil {
						resource, err = s.pathInfo(resource.Base + "/" + resource.Path + dirIndex)
						if err != nil {
							return r.respond(500, err)
						}
						w.Header().Set("Link", brack(resource.MetaURI)+"; rel=\"meta\", "+brack(resource.AclURI)+"; rel=\"acl\"")
						break
					} else if req.Method != "HEAD" {
						//TODO load file manager skin from local preference file
						w.Header().Set(HCType, contentType)
						urlStr := s.Config.DirSkin + resource.Obj.Scheme + "/" + resource.Obj.Host + "/" + resource.Obj.Path
						http.Redirect(w, req.Request, urlStr, 303)
						return
					}
				}
			} else {
				w.Header().Add("Link", brack(resource.MetaURI)+"; rel=\"meta\"")

				root := NewResource(resource.URI)
				g.AddTriple(root, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), NewResource("http://www.w3.org/ns/posix/stat#Directory"))
				g.AddTriple(root, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), NewResource("http://www.w3.org/ns/ldp#Container"))
				g.AddTriple(root, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), NewResource("http://www.w3.org/ns/ldp#BasicContainer"))

				g.AddTriple(root, NewResource("http://www.w3.org/ns/posix/stat#mtime"), NewLiteral(fmt.Sprintf("%d", stat.ModTime().Unix())))
				g.AddTriple(root, NewResource("http://www.w3.org/ns/posix/stat#size"), NewLiteral(fmt.Sprintf("%d", stat.Size())))

				kb := NewGraph(resource.MetaURI)
				kb.ReadFile(resource.MetaFile)
				if kb.Len() > 0 {
					for triple := range kb.IterTriples() {
						var subject Term
						if kb.One(NewResource(resource.MetaURI), nil, nil) != nil {
							subject = NewResource(resource.URI)
						} else {
							subject = triple.Subject
						}
						g.AddTriple(subject, triple.Predicate, triple.Object)
					}
				}

				if glob {
					matches, err := filepath.Glob(globPath)
					if err == nil {
						for _, file := range matches {
							stat, serr := os.Stat(file)
							if !stat.IsDir() && serr == nil {
								// TODO: check acls
								guessType, _ := magic.TypeByFile(file)
								if guessType == "text/plain" {
									res, err := s.pathInfo(resource.Base + "/" + filepath.Dir(resource.Path) + "/" + filepath.Base(file))
									if err != nil {
										return r.respond(500, err)
									}
									aclStatus, err = acl.AllowRead(resource.URI)
									if aclStatus == 200 && err == nil {
										g.AppendFile(res.File, res.URI)
										g.AddTriple(root, NewResource("http://www.w3.org/ns/ldp#contains"), NewResource(res.URI))
									}
								}
							}
						}
					}
				} else {
					showContainment := true
					showEmpty := false
					pref := ParsePreferHeader(req.Header.Get("Prefer"))
					if len(pref.headers) > 0 {
						w.Header().Set("Preference-Applied", "return=representation")
					}
					for _, include := range pref.Includes() {
						switch include {
						case "http://www.w3.org/ns/ldp#PreferContainment":
							showContainment = true
						case "http://www.w3.org/ns/ldp#PreferEmptyContainer":
							showEmpty = true
						}
					}
					for _, omit := range pref.Omits() {
						switch omit {
						case "http://www.w3.org/ns/ldp#PreferContainment":
							showContainment = false
						case "http://www.w3.org/ns/ldp#PreferEmptyContainer":
							showEmpty = false
						}
					}

					if infos, err := ioutil.ReadDir(resource.File); err == nil {
						var _s Term
						for _, info := range infos {
							if info != nil {
								res := resource.URI + info.Name()
								if info.IsDir() {
									res += "/"
								}
								f, err := s.pathInfo(res)
								if err != nil {
									r.respond(500, err)
								}
								if info.IsDir() {
									_s = NewResource(f.URI)
									if !showEmpty {
										g.AddTriple(_s, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), NewResource("http://www.w3.org/ns/posix/stat#Directory"))
										g.AddTriple(_s, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), NewResource("http://www.w3.org/ns/ldp#BasicContainer"))
										g.AddTriple(_s, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), NewResource("http://www.w3.org/ns/ldp#Container"))
									}
									kb := NewGraph(f.URI)
									kb.ReadFile(f.MetaFile)
									if kb.Len() > 0 {
										for _, st := range kb.All(_s, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), nil) {
											if st != nil && st.Object != nil {
												g.AddTriple(_s, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), st.Object)
											}
										}
									}
								} else {
									_s = NewResource(f.URI)
									if !showEmpty {
										g.AddTriple(_s, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), NewResource("http://www.w3.org/ns/posix/stat#File"))
										// add type if RDF resource
										//infoUrl, _ := url.Parse(info.Name())
										guessType, _ := magic.TypeByFile(f.File)

										if guessType == "text/plain" {
											// open file and attempt to read the first line
											// Open an input file, exit on error.
											fd, err := os.Open(f.File)
											if err != nil {
												s.debug.Println("GET find mime type error:" + err.Error())
											}
											defer fd.Close()

											scanner := bufio.NewScanner(fd)

											//@@TODO don't sniff
											// stop after the first line
											for scanner.Scan() {
												if strings.HasPrefix(scanner.Text(), "@prefix") {
													kb := NewGraph(f.URI)
													kb.ReadFile(f.File)
													if kb.Len() > 0 {
														for _, st := range kb.All(NewResource(f.URI), NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), nil) {
															if st != nil && st.Object != nil {
																g.AddTriple(_s, NewResource("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"), st.Object)
															}
														}
													}
												}
												break
											}
											// log potential errors
											if err := scanner.Err(); err != nil {
												s.debug.Println("GET scan err: " + scanner.Err().Error())
											}
										}
									}
								}
								if !showEmpty {
									g.AddTriple(_s, NewResource("http://www.w3.org/ns/posix/stat#mtime"), NewLiteral(fmt.Sprintf("%d", info.ModTime().Unix())))
									g.AddTriple(_s, NewResource("http://www.w3.org/ns/posix/stat#size"), NewLiteral(fmt.Sprintf("%d", info.Size())))
								}
								if showContainment {
									g.AddTriple(root, NewResource("http://www.w3.org/ns/ldp#contains"), _s)
								}
							}
						}
					}
				}
				status = 200
				maybeRDF = true
			}
		default:
			maybeRDF = magicType == "text/plain"
			status = 200

			if req.Method == "GET" && strings.Contains(contentType, "text/html") {
				// delete ETag to force load the skin
				w.Header().Del("ETag")
				w.Header().Set("Link", brack(resource.MetaURI)+"; rel=\"meta\", "+brack(resource.AclURI)+"; rel=\"acl\"")
				if maybeRDF {
					w.Header().Set(HCType, contentType)
					return r.respond(200, Skins[s.Config.DataSkin])
				}
				w.Header().Set(HCType, magicType)
				w.WriteHeader(200)
				f, err := os.Open(resource.File)
				if err == nil {
					defer func() {
						if err := f.Close(); err != nil {
							s.debug.Println("GET os.Open err: " + err.Error())
						}
					}()
					io.Copy(w, f)
				}
				return
			} else if !maybeRDF && !strings.Contains(contentType, "text/html") {
				maybeRDF = true
			}
		}

		if status != 200 {
			return r.respond(status)
		}

		if extn := strings.LastIndex(resource.File, "."); extn >= 0 {
			if mime, known := mimeTypes[resource.File[extn:]]; known {
				magicType = mime
				maybeRDF = false
			}
		}

		if req.Method == "HEAD" {
			w.Header().Set(HCType, magicType)
			return r.respond(status)
		}

		//@@TODO don't sniff
		if maybeRDF {
			g.ReadFile(resource.File)
			if g.Len() == 0 {
				maybeRDF = false
			} else {
				w.Header().Set(HCType, contentType)
			}
		}

		if !maybeRDF && len(magicType) > 0 {
			w.Header().Set(HCType, magicType)

			if status == 200 {
				f, err := os.Open(resource.File)
				if err == nil {
					defer func() {
						if err := f.Close(); err != nil {
							s.debug.Println("GET f.Close err:" + err.Error())
						}
					}()
					io.Copy(w, f)
				}
			} else {
				w.WriteHeader(status)
			}
			return
		}

		data := ""
		if Streaming {
			errCh := make(chan error, 8)
			go func() {
				rf, wf, err := os.Pipe()
				if err != nil {
					errCh <- err
					return
				}
				go func() {
					defer wf.Close()
					err := g.WriteFile(wf, contentType)
					if err != nil {
						errCh <- err
					}
				}()
				go func() {
					defer rf.Close()
					_, err := io.Copy(w, rf)
					if err != nil {
						errCh <- err
					} else {
						errCh <- nil
					}
				}()
			}()
			err = <-errCh
		} else {
			data, err = g.Serialize(contentType)
		}

		if err != nil {
			return r.respond(500, err)
		} else if len(data) > 0 {
			fmt.Fprint(w, data)
		}
		return

	case "PATCH":
		unlock := lock(resource.File)
		defer unlock()

		// check append first
		aclAppend, err := acl.AllowAppend(resource.URI)
		if aclAppend > 200 || err != nil {
			// check if we can write then
			aclWrite, err := acl.AllowWrite(resource.URI)
			if aclWrite > 200 || err != nil {
				return r.respond(aclWrite, handleStatusText(aclWrite, err))
			}
		}

		etag, _ := NewETag(resource.File)
		if !req.ifMatch("\"" + etag + "\"") {
			return r.respond(412, "412 - Precondition Failed")
		}
		if !req.ifNoneMatch("\"" + etag + "\"") {
			return r.respond(412, "412 - Precondition Failed")
		}

		if dataHasParser {
			g := NewGraph(resource.URI)
			g.ReadFile(resource.File)

			switch dataMime {
			case "application/json":
				g.JSONPatch(req.Body)
			case "application/sparql-update":
				sparql := NewSPARQLUpdate(g.URI())
				sparql.Parse(req.Body)
				g.SPARQLUpdate(sparql)
			default:
				if dataHasParser {
					g.Parse(req.Body, dataMime)
				}
			}

			f, err := os.OpenFile(resource.File, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				s.debug.Println("PATCH os.OpenFile err: " + err.Error())
				return r.respond(500, err)
			}
			defer f.Close()

			err = g.WriteFile(f, "text/turtle")
			if err != nil {
				s.debug.Println("PATCH g.WriteFile err: " + err.Error())
			}

			if err != nil {
				return r.respond(500, err)
			}

			onUpdateURI(resource.URI)

			return r.respond(200)
		}

	case "POST":
		unlock := lock(resource.File)
		defer unlock()

		// check append first
		aclAppend, err := acl.AllowAppend(resource.URI)
		if aclAppend > 200 || err != nil {
			// check if we can write then
			aclWrite, err := acl.AllowWrite(resource.URI)
			if aclWrite > 200 || err != nil {
				return r.respond(aclWrite, handleStatusText(aclWrite, err))
			}
		}

		etag, _ := NewETag(resource.File)
		if !req.ifMatch("\"" + etag + "\"") {
			return r.respond(412, "412 - Precondition Failed")
		}
		if !req.ifNoneMatch("\"" + etag + "\"") {
			return r.respond(412, "412 - Precondition Failed")
		}

		// LDP
		isNew := false
		stat, err := os.Stat(resource.File)
		if err == nil && stat.IsDir() && dataMime != "multipart/form-data" {
			link := ParseLinkHeader(req.Header.Get("Link")).MatchRel("type")
			slug := req.Header.Get("Slug")

			uuid, err := newUUID()
			if err != nil {
				s.debug.Println("POST LDP UUID err: " + err.Error())
				return r.respond(500, err)
			}
			uuid = uuid[:6]

			if !strings.HasSuffix(resource.Path, "/") {
				resource.Path += "/"
			}

			if len(slug) > 0 {
				if strings.HasPrefix(slug, "/") {
					slug = strings.TrimLeft(slug, "/")
				}
				if strings.HasSuffix(slug, "/") {
					slug = strings.TrimRight(slug, "/")
				}
				st, _ := os.Stat(resource.File + slug)
				if st != nil {
					s.debug.Println("POST LDP - A resource with the same name already exists: " + resource.Path + slug)
					return r.respond(409, "409 - Conflict! A resource with the same name already exists.")
				}
			} else {
				slug = uuid
			}
			resource.Path += slug

			if len(link) > 0 && link == "http://www.w3.org/ns/ldp#BasicContainer" {
				if !strings.HasSuffix(resource.Path, "/") {
					resource.Path += "/"
				}
				resource, err = s.pathInfo(resource.Base + "/" + resource.Path)
				if err != nil {
					s.debug.Println("POST LDPC s.pathInfo err: " + err.Error())
					return r.respond(500, err)
				}

				w.Header().Set("Location", resource.URI)
				w.Header().Set("Link", brack(resource.MetaURI)+"; rel=\"meta\", "+brack(resource.AclURI)+"; rel=\"acl\"")
				w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#Resource")+"; rel=\"type\"")
				w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#BasicContainer")+"; rel=\"type\"")

				err = os.MkdirAll(resource.File, 0755)
				if err != nil {
					s.debug.Println("POST LDPC os.MkdirAll err: " + err.Error())
					return r.respond(500, err)
				}
				s.debug.Println("Created dir " + resource.File)

				//Replace the subject with the dir path instead of the meta file path
				if dataHasParser {
					g := NewGraph(resource.URI)
					mg := NewGraph(resource.URI)
					mg.Parse(req.Body, dataMime)
					for triple := range mg.IterTriples() {
						subject := NewResource(".")
						g.AddTriple(subject, triple.Predicate, triple.Object)
					}

					f, err := os.OpenFile(resource.MetaFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
					if err != nil {
						s.debug.Println("POST LDPC os.OpenFile err: " + err.Error())
						return r.respond(500, err)
					}
					defer f.Close()

					if g.Len() > 0 {
						if err = g.WriteFile(f, ""); err != nil {
							s.debug.Println("POST LDPC g.WriteFile err: " + err.Error())
							return r.respond(500, err)
						}
					}
				}
				w.Header().Set("Location", resource.URI)
				onUpdateURI(resource.URI)
				return r.respond(201)
			}
			resource, err = s.pathInfo(resource.Base + "/" + resource.Path)
			if err != nil {
				s.debug.Println("POST LDPR s.pathInfo err: " + err.Error())
				return r.respond(500, err)
			}
			w.Header().Set("Location", resource.URI)
			w.Header().Set("Link", brack(resource.MetaURI)+"; rel=\"meta\", "+brack(resource.AclURI)+"; rel=\"acl\"")
			// LDP header
			w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#Resource")+"; rel=\"type\"")
			isNew = true
		}

		if stat == nil {
			err = os.MkdirAll(_path.Dir(resource.File), 0755)
			if err != nil {
				s.debug.Println("POST MkdirAll err: " + err.Error())
				return r.respond(500, err)
			}
			s.debug.Println("Created resource " + _path.Dir(resource.File))
		}

		if dataMime == "multipart/form-data" {
			err := req.ParseMultipartForm(100000)
			if err != nil {
				s.debug.Println("POST parse multipart data err: " + err.Error())
			} else {
				m := req.MultipartForm
				for elt := range m.File {
					files := m.File[elt]
					for i := range files {
						file, err := files[i].Open()
						defer file.Close()
						if err != nil {
							s.debug.Println("POST multipart/form f.Open err: " + err.Error())
							return r.respond(500, err)
						}
						newFile := ""
						if filepath.Base(resource.Path) == files[i].Filename {
							newFile = resource.File
						} else {
							newFile = resource.File + files[i].Filename
						}
						dst, err := os.OpenFile(newFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
						defer dst.Close()
						if err != nil {
							s.debug.Println("POST multipart/form os.Create err: " + err.Error())
							return r.respond(500, err)
						}
						if _, err := io.Copy(dst, file); err != nil {
							s.debug.Println("POST multipart/form io.Copy err: " + err.Error())
							return r.respond(500, err)
						}
						w.Header().Add("Location", resource.URI+files[i].Filename)
					}
				}
				onUpdateURI(resource.URI)
				return r.respond(201)
			}
		} else {
			stat, err = os.Stat(resource.File)
			if os.IsNotExist(err) {
				isNew = true
			} else if os.IsExist(err) && stat.IsDir() {
				resource.File = resource.File + "/" + METASuffix
			}

			if dataHasParser {
				g := NewGraph(resource.URI)
				g.ReadFile(resource.File)

				switch dataMime {
				case "application/json":
					g.JSONPatch(req.Body)
				case "application/sparql-update":
					sparql := NewSPARQLUpdate(g.URI())
					sparql.Parse(req.Body)
					g.SPARQLUpdate(sparql)
				default:
					g.Parse(req.Body, dataMime)
				}
				f, err := os.OpenFile(resource.File, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
				if err != nil {
					s.debug.Println("POST os.OpenFile err: " + err.Error())
					return r.respond(500, err.Error())
				}
				defer f.Close()
				if g.Len() > 0 {
					err = g.WriteFile(f, "text/turtle")
					if err != nil {
						s.debug.Println("POST g.WriteFile err: " + err.Error())
					} else {
						s.debug.Println("Wrote resource file: " + resource.File)
					}
				}
			} else {
				f, err := os.OpenFile(resource.File, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
				if err != nil {
					s.debug.Println("POST os.OpenFile err: " + err.Error())
					return r.respond(500, err.Error())
				}
				defer f.Close()
				_, err = io.Copy(f, req.Body)
				if err != nil {
					s.debug.Println("POST os.OpenFile err: " + err.Error())
					return r.respond(500, err.Error())
				}
			}

			onUpdateURI(resource.URI)
			if isNew {
				return r.respond(201)
			}
			return r.respond(200)
		}

	case "PUT":
		unlock := lock(resource.File)
		defer unlock()

		// LDP header
		w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#Resource")+"; rel=\"type\"")

		// check append first
		aclAppend, err := acl.AllowAppend(resource.URI)
		if aclAppend > 200 || err != nil {
			// check if we can write then
			aclWrite, err := acl.AllowWrite(resource.URI)
			if aclWrite > 200 || err != nil {
				return r.respond(aclWrite, handleStatusText(aclWrite, err))
			}
		}

		etag, _ := NewETag(resource.File)
		if !req.ifMatch("\"" + etag + "\"") {
			return r.respond(412, "412 - Precondition Failed")
		}
		if !req.ifNoneMatch("\"" + etag + "\"") {
			return r.respond(412, "412 - Precondition Failed")
		}

		// LDP PUT should be merged with LDP POST into a common LDP "method" switch
		link := ParseLinkHeader(req.Header.Get("Link")).MatchRel("type")
		if len(link) > 0 && link == "http://www.w3.org/ns/ldp#BasicContainer" {
			err := os.MkdirAll(resource.File, 0755)
			if err != nil {
				s.debug.Println("PUT MkdirAll err: " + err.Error())
				return r.respond(500, err)
			}
			// refresh resource and set the right headers
			resource, err = s.pathInfo(resource.URI)
			w.Header().Set("Link", brack(resource.MetaURI)+"; rel=\"meta\", "+brack(resource.AclURI)+"; rel=\"acl\"")
			// LDP header
			w.Header().Add("Link", brack("http://www.w3.org/ns/ldp#Resource")+"; rel=\"type\"")

			onUpdateURI(resource.URI)
			return r.respond(201)
		}
		err = os.MkdirAll(_path.Dir(resource.File), 0755)
		if err != nil {
			s.debug.Println("PUT MkdirAll err: " + err.Error())
			return r.respond(500, err)
		}

		isNew := true
		stat, err := os.Stat(resource.File)
		if os.IsExist(err) {
			isNew = false
		}

		f, err := os.OpenFile(resource.File, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			s.debug.Println("PUT os.OpenFile err: " + err.Error())
			if stat.IsDir() {
				w.Header().Add("Link", brack(resource.URI)+"; rel=\"describedby\"")
				return r.respond(406, "406 - Cannot use PUT on a directory.")
			}
			return r.respond(500, err)
		}
		defer f.Close()

		//@@TODO don't sniff
		if dataHasParser {
			g := NewGraph(resource.URI)
			g.Parse(req.Body, dataMime)
			err = g.WriteFile(f, "text/turtle")
			if err != nil {
				s.debug.Println("PUT g.WriteFile err: " + err.Error())
			}
		}
		_, err = io.Copy(f, req.Body)
		if err != nil {
			s.debug.Println("PUT io.Copy err: " + err.Error())
		}

		if err != nil {
			return r.respond(500, err)
		}

		w.Header().Set("Location", resource.URI)

		onUpdateURI(resource.URI)
		if isNew {
			return r.respond(201)
		}
		return r.respond(200)

	case "DELETE":
		unlock := lock(resource.Path)
		defer unlock()

		aclWrite, err := acl.AllowWrite(resource.URI)
		if aclWrite > 200 || err != nil {
			return r.respond(aclWrite, handleStatusText(aclWrite, err))
		}

		if len(resource.Path) == 0 {
			return r.respond(500, "500 - Cannot DELETE /")
		}
		err = os.Remove(resource.File)
		if err != nil {
			if os.IsNotExist(err) {
				return r.respond(404, Skins["404"])
			}
			return r.respond(500, err)
		}
		_, err = os.Stat(resource.File)
		if err == nil {
			return r.respond(409, err)
		}
		onDeleteURI(resource.URI)
		return

	case "MKCOL":
		unlock := lock(resource.File)
		defer unlock()

		aclWrite, err := acl.AllowWrite(resource.URI)
		if aclWrite > 200 || err != nil {
			return r.respond(aclWrite, handleStatusText(aclWrite, err))
		}

		err = os.MkdirAll(resource.File, 0755)
		if err != nil {
			switch err.(type) {
			case *os.PathError:
				return r.respond(409, err)
			default:
				return r.respond(500, err)
			}
		} else {
			_, err := os.Stat(resource.File)
			if err != nil {
				return r.respond(409, err)
			}
		}
		onUpdateURI(resource.URI)
		return r.respond(201)

	case "COPY", "MOVE", "LOCK", "UNLOCK":
		aclWrite, err := acl.AllowWrite(resource.URI)
		if aclWrite > 200 || err != nil {
			return r.respond(aclWrite, handleStatusText(aclWrite, err))
		}
		s.webdav.ServeHTTP(w, req.Request)

	default:
		return r.respond(405, "405 - Method Not Allowed:", req.Method)
	}
	return
}
