package imgpull

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yo-l1982/imgpull/internal/tar"
	"github.com/yo-l1982/imgpull/internal/testhelpers"
	"github.com/yo-l1982/imgpull/internal/util"
	"github.com/yo-l1982/imgpull/mock"
	"github.com/yo-l1982/imgpull/pkg/imgpull/types"

	"github.com/opencontainers/go-digest"
)

// test auth header parsing
// ""
func TestAuthParse(t *testing.T) {
	authHdrTests := []struct {
		hdr     string
		realm   string
		service string
		scope   string
	}{
		{
			hdr:     `Bearer realm="https://quay.io/v2/auth",service="quay.io"`,
			realm:   "https://quay.io/v2/auth",
			service: "quay.io",
		},
		{
			hdr:     `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`,
			realm:   "https://auth.docker.io/token",
			service: "registry.docker.io",
		}, {
			hdr:     `Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:yo-l1982/ociregistry:pull"`,
			realm:   "https://ghcr.io/token",
			service: "ghcr.io",
			scope:   "repository:yo-l1982/ociregistry:pull",
		},
	}
	for _, authHdrTest := range authHdrTests {
		ba := parseBearer(authHdrTest.hdr)
		if ba.Realm != authHdrTest.realm || ba.Service != authHdrTest.service || ba.Scope != authHdrTest.scope {
			t.Fail()
		}
	}
}

// TestBasicCreds tests that the puller is doing basic auth correctly
func TestBasicCreds(t *testing.T) {
	user := "foobar"
	pass := "frobozz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" && r.URL.Path != "/v2/hello-world/manifests/latest" {
			t.Fail()
		}
		if r.Header.Get("Authorization") != "" {
			actual := r.Header.Get("Authorization")
			delimited := fmt.Sprintf("%s:%s", user, pass)
			encoded := base64.StdEncoding.EncodeToString([]byte(delimited))
			expected := "Basic " + encoded
			if expected != actual {
				t.Fail()
			}
			w.WriteHeader(http.StatusOK)
		} else {
			authUrl := `Basic realm="%s://%s"`
			authHdr := fmt.Sprintf(authUrl, "http", r.Host)
			w.Header().Set("Content-Length", "0")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
			w.Header().Set("Www-Authenticate", authHdr)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	pullOpts := PullerOpts{
		Url:      strings.ReplaceAll(fmt.Sprintf("%s/hello-world:latest", server.URL), "http://", ""),
		Scheme:   "http",
		OStype:   "linux",
		ArchType: "amd64",
		Username: user,
		Password: pass,
	}
	if p, err := NewPullerWith(pullOpts); err != nil {
		t.Fail()
	} else if err := (p.(*puller)).connect(); err != nil {
		t.Fail()
	}
}

// TestPullManifests pulls hello-world:latest manifest list and image manifest from the mock
// server with all permutations of auth and TLS supported by the CLI **except** for server
// cert verification from the OS trust store because that would require mocking the OS trust
// store OR getting a cert signed by a CA in the OS trust store or adding a fake cert that signed
// a test CA into the OS trust store.
func TestPullManifest(t *testing.T) {
	authTypes := []mock.AuthType{mock.BASIC, mock.BEARER, mock.NONE}
	tlsTypes := []mock.TlsType{mock.NOTLS, mock.ONEWAY_INSECURE, mock.ONEWAY_SECURE, mock.MTLS_INSECURE, mock.MTLS_SECURE}
	certSetup, err := mock.NewCertSetup()
	if err != nil {
		t.Fail()
	}
	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)

	for _, at := range authTypes {
		for _, tt := range tlsTypes {
			mp := mock.NewMockParams(at, tt, certSetup)
			server, url := mock.Server(mp)
			defer server.Close()
			pullOpts := PullerOpts{
				Url:      fmt.Sprintf("%s/hello-world:latest", url),
				OStype:   "linux",
				ArchType: "amd64",
			}
			// basic auth credentials aren't validated by the mock registry
			if at == mock.BASIC {
				pullOpts.Username = "foobar"
				pullOpts.Password = "frobozz"
			}
			switch tt {
			case mock.NOTLS:
				pullOpts.Scheme = "http"
			case mock.ONEWAY_INSECURE:
				pullOpts.Scheme = "https"
				pullOpts.Insecure = true
			case mock.ONEWAY_SECURE:
				pullOpts.Scheme = "https"
				pullOpts.Insecure = false
				pullOpts.CaCert = mp.Certs.CaToFile(d, "ca.crt")
			case mock.MTLS_INSECURE:
				pullOpts.Scheme = "https"
				pullOpts.Insecure = true
				pullOpts.TlsCert = mp.Certs.ClientCertToFile(d, "client.crt")
				pullOpts.TlsKey = mp.Certs.ClientCertPrivKeyToFile(d, "client.key")
			case mock.MTLS_SECURE:
				pullOpts.Scheme = "https"
				pullOpts.Insecure = false
				pullOpts.TlsCert = mp.Certs.ClientCertToFile(d, "client.crt")
				pullOpts.TlsKey = mp.Certs.ClientCertPrivKeyToFile(d, "client.key")
				pullOpts.CaCert = mp.Certs.CaToFile(d, "ca.crt")
			}
			p, err := NewPullerWith(pullOpts)
			if err != nil {
				t.Fail()
			}
			for _, mpt := range []ManifestPullType{ImageList, Image} {
				mh, err := p.GetManifestByType(mpt)
				if err != nil {
					t.Fail()
				}
				if mpt == ImageList && !mh.IsManifestList() {
					t.Fail()
				}
				if mpt == Image && mh.IsManifestList() {
					t.Fail()
				}
			}
		}
	}
}

// Tests the ability to initialize a "long lived" client TLS config struct
// and use it to initlize the puller. This enables clients using the puller
// as a library to perform client TLS init once, and use it over time to
// avoid the TLS re-initialization.
func TestPullReuseTlsCfg(t *testing.T) {
	certSetup, err := mock.NewCertSetup()
	if err != nil {
		t.Fail()
	}
	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)
	clientCert := certSetup.ClientCertToFile(d, "client.crt")
	clientKey := certSetup.ClientCertPrivKeyToFile(d, "client.key")
	caCert := certSetup.CaToFile(d, "ca.crt")

	mp := mock.NewMockParams(mock.NONE, mock.MTLS_SECURE, certSetup)
	server, url := mock.Server(mp)
	defer server.Close()

	// initialize a client TLS config struct for re-use
	cfg := &tls.Config{}
	if cert, err := tls.LoadX509KeyPair(clientCert, clientKey); err != nil {
		t.Fail()
	} else {
		cfg.Certificates = []tls.Certificate{cert}
		if caBytes, err := os.ReadFile(caCert); err != nil {
			t.Fail()
		} else {
			cp := x509.NewCertPool()
			cp.AppendCertsFromPEM(caBytes)
			cfg.RootCAs = cp
		}
	}

	testOpts := func() PullOpt {
		return func(p *PullerOpts) {
			p.Scheme = "https"
			p.OStype = "linux"
			p.ArchType = "amd64"
			p.TlsCfg = cfg
		}
	}
	for i := 0; i < 3; i++ {
		p, err := NewPuller(fmt.Sprintf("%s/hello-world:latest", url), testOpts())
		if err != nil {
			t.Fail()
		}
		_, err = p.GetManifestByType(ImageList)
		if err != nil {
			t.Fail()
		}

	}
}

func TestPullTarNotFound(t *testing.T) {
	mp := mock.NewMockParams(mock.NONE, mock.NOTLS, mock.CertSetup{})
	server, url := mock.Server(mp)
	defer server.Close()
	imgUrl := fmt.Sprintf("%s/nosuch/image:v1.2.3", url)
	pullOpts := PullerOpts{
		Url:      imgUrl,
		OStype:   "linux",
		ArchType: "amd64",
		Scheme:   "http",
	}
	p, err := NewPullerWith(pullOpts)
	if err != nil {
		t.Fail()
	}
	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)
	tarball := filepath.Join(d, "test.tar")
	if p.PullTar(tarball) == nil {
		t.Fail()
	}
}

func TestPullTar(t *testing.T) {
	mp := mock.NewMockParams(mock.NONE, mock.NOTLS, mock.CertSetup{})
	server, url := mock.Server(mp)
	defer server.Close()
	imgUrl := fmt.Sprintf("%s/hello-world:latest", url)
	pullOpts := PullerOpts{
		Url:      imgUrl,
		OStype:   "linux",
		ArchType: "amd64",
		Scheme:   "http",
	}
	p, err := NewPullerWith(pullOpts)
	if err != nil {
		t.Fail()
	}
	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)
	tarball := filepath.Join(d, "test.tar")
	if p.PullTar(tarball) != nil {
		t.Fail()
	}
	if testhelpers.UntarFile(tarball) != nil {
		t.Fail()
	}
	manifest, err := os.ReadFile(filepath.Join(d, "manifest.json.extracted"))
	if err != nil {
		t.Fail()
	}
	dtmActual := []tar.DockerTarManifest{}
	if json.Unmarshal(manifest, &dtmActual) != nil {
		t.Fail()
	}
	dtmExp := tar.DockerTarManifest{
		Config:   "sha256:d2c94e258dcb3c5ac2798d32e1249e42ef01cba4841c2234249495f87264ac5a",
		RepoTags: []string{imgUrl},
		Layers:   []string{"c1ec31eb59444d78df06a974d155e597c894ab4cda84f08294145e845394988e.tar.gz"},
	}
	if !reflect.DeepEqual(dtmExp, dtmActual[0]) {
		t.Fail()
	}
	layers := []string{
		"c1ec31eb59444d78df06a974d155e597c894ab4cda84f08294145e845394988e.tar.gz.extracted",
		"sha256:d2c94e258dcb3c5ac2798d32e1249e42ef01cba4841c2234249495f87264ac5a.extracted",
	}
	for _, layer := range layers {
		bytes, err := os.ReadFile(filepath.Join(d, layer))
		if err != nil {
			t.Fail()
		}
		hasher := sha256.New()
		hasher.Write(bytes)
		digestActual := digest.FromBytes(bytes).Hex()
		digestExp := util.DigestFrom(layer)
		if digestExp != digestActual {
			t.Fail()
		}
	}
}

func TestHeadManifest(t *testing.T) {
	mp := mock.NewMockParams(mock.NONE, mock.NOTLS, mock.CertSetup{})
	server, url := mock.Server(mp)
	defer server.Close()
	imgUrl := fmt.Sprintf("%s/hello-world:latest", url)
	pullOpts := PullerOpts{
		Url:      imgUrl,
		OStype:   "linux",
		ArchType: "amd64",
		Scheme:   "http",
	}
	p, err := NewPullerWith(pullOpts)
	if err != nil {
		t.Fail()
	}
	md, err := p.HeadManifest()
	if err != nil {
		t.Fail()
	}
	if md.MediaType != types.V1ociIndexMt {
		t.Fail()
	}
	mh, err := p.GetManifest()
	if err != nil {
		t.Fail()
	}
	if mh.Type != V1ociIndex {
		t.Fail()
	}
}

// Tests the 'PullBlobs' function
func TestPullBlobs(t *testing.T) {
	mp := mock.NewMockParams(mock.NONE, mock.NOTLS, mock.CertSetup{})
	server, url := mock.Server(mp)
	defer server.Close()
	imgUrl := fmt.Sprintf("%s/hello-world:latest", url)
	pullOpts := PullerOpts{
		Url:      imgUrl,
		OStype:   "linux",
		ArchType: "amd64",
		Scheme:   "http",
	}
	p, err := NewPullerWith(pullOpts)
	if err != nil {
		t.Fail()
	}
	mh, err := p.GetManifestByType(Image)
	if err != nil {
		t.Fail()
	}
	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)
	p.PullBlobs(mh, d)

	expBlobs := []string{
		"c1ec31eb59444d78df06a974d155e597c894ab4cda84f08294145e845394988e",
		"d2c94e258dcb3c5ac2798d32e1249e42ef01cba4841c2234249495f87264ac5a",
	}
	for _, digest := range expBlobs {
		if _, err := os.Stat(filepath.Join(d, digest)); err != nil {
			t.Fail()
		}
	}
}
