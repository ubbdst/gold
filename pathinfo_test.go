package gold

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPathInfo(t *testing.T) {
	path := testServer.URL + "/_test/"
	sroot := serverDefaultRoot()

	p, err := handler.pathInfo("")
	assert.NotNil(t, err)

	p, err = handler.pathInfo(testServer.URL)
	assert.Nil(t, err)
	assert.Equal(t, testServer.URL+"/", p.URI)
	assert.Equal(t, testServer.URL, p.Base)
	assert.Equal(t, "", p.Path)
	assert.Equal(t, sroot, p.File)
	assert.Equal(t, testServer.URL+"/"+ACLSuffix, p.AclURI)
	assert.Equal(t, sroot+ACLSuffix, p.AclFile)
	assert.Equal(t, testServer.URL+"/"+METASuffix, p.MetaURI)
	assert.Equal(t, sroot+METASuffix, p.MetaFile)
	assert.True(t, p.Exists)

	p, err = handler.pathInfo(testServer.URL + "/")
	assert.Nil(t, err)
	assert.Equal(t, testServer.URL+"/", p.URI)
	assert.Equal(t, testServer.URL, p.Base)
	assert.Equal(t, "", p.Path)
	assert.Equal(t, sroot, p.File)
	assert.Equal(t, testServer.URL+"/"+ACLSuffix, p.AclURI)
	assert.Equal(t, sroot+ACLSuffix, p.AclFile)
	assert.Equal(t, testServer.URL+"/"+METASuffix, p.MetaURI)
	assert.Equal(t, sroot+METASuffix, p.MetaFile)
	assert.True(t, p.Exists)

	p, err = handler.pathInfo(path)
	assert.Nil(t, err)
	assert.Equal(t, path, p.URI)
	assert.Equal(t, testServer.URL, p.Base)
	assert.Equal(t, "_test/", p.Path)
	assert.Equal(t, sroot+"_test/", p.File)
	assert.Equal(t, path+ACLSuffix, p.AclURI)
	assert.Equal(t, sroot+"_test/"+ACLSuffix, p.AclFile)
	assert.Equal(t, path+METASuffix, p.MetaURI)
	assert.Equal(t, sroot+"_test/"+METASuffix, p.MetaFile)
	assert.True(t, p.Exists)

	p, err = handler.pathInfo(path + "abc")
	assert.Nil(t, err)
	assert.Equal(t, path+"abc", p.URI)
	assert.Equal(t, testServer.URL, p.Base)
	assert.Equal(t, "_test/abc", p.Path)
	assert.Equal(t, sroot+"_test/abc", p.File)
	assert.Equal(t, path+"abc"+ACLSuffix, p.AclURI)
	assert.Equal(t, sroot+"_test/abc"+ACLSuffix, p.AclFile)
	assert.Equal(t, path+"abc"+METASuffix, p.MetaURI)
	assert.Equal(t, sroot+"_test/abc"+METASuffix, p.MetaFile)
	assert.False(t, p.Exists)

	p, err = handler.pathInfo(path + ACLSuffix)
	assert.Nil(t, err)
	assert.Equal(t, path+ACLSuffix, p.URI)
	assert.Equal(t, testServer.URL, p.Base)
	assert.Equal(t, "_test/"+ACLSuffix, p.Path)
	assert.Equal(t, sroot+"_test/"+ACLSuffix, p.File)
	assert.Equal(t, path+ACLSuffix, p.AclURI)
	assert.Equal(t, sroot+"_test/"+ACLSuffix, p.AclFile)
	assert.Equal(t, path+ACLSuffix, p.MetaURI)
	assert.Equal(t, sroot+"_test/"+ACLSuffix, p.MetaFile)
	assert.False(t, p.Exists)

	p, err = handler.pathInfo(path + METASuffix)
	assert.Nil(t, err)
	assert.Equal(t, path+METASuffix, p.URI)
	assert.Equal(t, testServer.URL, p.Base)
	assert.Equal(t, "_test/"+METASuffix, p.Path)
	assert.Equal(t, sroot+"_test/"+METASuffix, p.File)
	assert.Equal(t, path+METASuffix+ACLSuffix, p.AclURI)
	assert.Equal(t, sroot+"_test/"+METASuffix+ACLSuffix, p.AclFile)
	assert.Equal(t, path+METASuffix, p.MetaURI)
	assert.Equal(t, sroot+"_test/"+METASuffix, p.MetaFile)
	assert.False(t, p.Exists)
}
