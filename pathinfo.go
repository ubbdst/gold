package gold

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
)

type pathInfo struct {
	Obj      *url.URL
	URI      string
	Base     string
	Path     string
	Root     string
	File     string
	FileType string
	AclURI   string
	AclFile  string
	MetaURI  string
	MetaFile string
	Exists   bool
}

func (s *Server) pathInfo(path string) (*pathInfo, error) {
	res := &pathInfo{}

	if len(path) == 0 {
		return nil, errors.New("missing resource path")
	}

	// hack - if source URI contains "one%2b+%2btwo" then it is
	// normally decoded to "one+ +two", but Go parses it to
	// "one+++two", so we replace the plus with a blank space
	// strings.Replace(path, "+", "%20", -1)

	p, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	res.Base = p.Scheme + "://" + p.Host
	res.Root = s.Config.DataRoot
	// include host and port if running in vhosts mode
	host, port, _ := net.SplitHostPort(p.Host)
	if len(host) == 0 {
		host = p.Host
	}
	if len(port) > 0 {
		host += ":" + port
	}
	if s.Config.Vhosts {
		res.Root = s.Config.DataRoot + host + "/"
		res.Base = p.Scheme + "://" + host
	}

	if strings.HasPrefix(p.Path, "/") && len(p.Path) > 0 {
		p.Path = strings.TrimLeft(p.Path, "/")
	}

	res.Exists = true
	// check if file exits first
	if stat, err := os.Stat(res.Root + p.Path); os.IsNotExist(err) {
		res.Exists = false
	} else {
		// Add missing trailing slashes for dirs
		if stat.IsDir() && !strings.HasSuffix(p.Path, "/") && len(p.Path) > 1 {
			p.Path += "/"
		}
		// get filetype
		res.FileType, err = magic.TypeByFile(res.Root + p.Path)
		if err != nil {
			s.debug.Println(err)
		}
	}

	if len(p.Path) == 0 {
		res.URI = p.String() + "/"
	} else {
		res.URI = p.String()
	}
	res.Obj = p
	res.File = p.Path
	res.Path = p.Path

	if s.Config.Vhosts {
		res.File = res.Root + p.Path
	} else if len(s.Config.DataRoot) > 0 {
		res.File = s.Config.DataRoot + p.Path
	}

	if strings.HasSuffix(p.Path, ACLSuffix) {
		res.AclURI = res.URI
		res.AclFile = res.File
		res.MetaURI = res.URI
		res.MetaFile = res.File
	} else if strings.HasSuffix(p.Path, METASuffix) {
		res.AclURI = res.URI + ACLSuffix
		res.AclFile = res.File + ACLSuffix
		res.MetaURI = res.URI
		res.MetaFile = res.File
	} else {
		res.AclURI = res.URI + ACLSuffix
		res.AclFile = res.File + ACLSuffix
		// println("s.Config.Vhosts: res.File=" + res.File + " / res.AclFile=" + res.AclFile)
		res.MetaURI = res.URI + METASuffix
		res.MetaFile = res.File + METASuffix
	}

	return res, nil
}
