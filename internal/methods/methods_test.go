package methods

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yo-l1982/imgpull/internal/blobsync"
	"github.com/yo-l1982/imgpull/internal/imgref"
	"github.com/yo-l1982/imgpull/internal/testhelpers"
	"github.com/yo-l1982/imgpull/mock"
	"github.com/yo-l1982/imgpull/pkg/imgpull/types"
)

// Tests bearer auth
func TestV2(t *testing.T) {
	server, url := mock.Server(mock.NewMockParams(mock.BEARER, mock.NOTLS, mock.CertSetup{}))
	defer server.Close()
	rc, err := newRegClient("hello-world:latest", url, "")
	if err != nil {
		t.Fail()
	}
	status, auth, err := rc.V2ManifestsAuth()
	if err != nil {
		t.Fail()
	}
	if status != http.StatusUnauthorized {
		t.Fail()
	}
	if len(auth) != 1 {
		t.Fail()
	}
}

// If a blob file already exists on the file system then a call to v2/blobs
// should do nothing.
func TestV2BlobsExists(t *testing.T) {
	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)
	digest := testhelpers.MakeDigest()
	blobFile := filepath.Join(d, digest)
	err := os.WriteFile(blobFile, []byte(digest), 0644)
	if err != nil {
		t.Fail()
	}
	layer := types.Layer{
		MediaType: "unused.by.this.test",
		Digest:    digest,
		Size:      len(digest),
	}
	// if the logic that immediately returns if the blob file
	// already exists is executed, then the empty regClient struct
	// is ingored.
	if (RegClient{}).V2Blobs(layer, blobFile) != nil {
		t.Fail()
	}
}

// Tests getting a blob without concurrency.
func TestV2BlobsSimple(t *testing.T) {
	server, url := mock.Server(mock.NewMockParams(mock.NONE, mock.NOTLS, mock.CertSetup{}))
	rc, err := newRegClient("hello-world:latest", url, "")
	if err != nil {
		t.Fail()
	}
	defer server.Close()
	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)

	digest := "sha256:d2c94e258dcb3c5ac2798d32e1249e42ef01cba4841c2234249495f87264ac5a"
	blobFile := filepath.Join(d, digest)

	layer := types.Layer{
		MediaType: types.V2dockerLayerGzipMt,
		Digest:    digest,
		Size:      581, // mock/testfiles/d2c9.json
	}
	if rc.V2Blobs(layer, blobFile) != nil {
		t.Fail()
	}
}

// Tests concurrent blob fetch. Spins up multiple goroutines to get the
// same blob and verifies that only one goroutine actually called the
// v2/blobs endpoint. (The others were therefore enqueued.)
func TestV2BlobsConcur(t *testing.T) {
	blob := "zzzz"
	digest := testhelpers.MakeDigest()

	var httpMethodCnt atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/hello-world/blobs/sha256:"+digest {
			t.Fail()
		}
		httpMethodCnt.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Header().Add("Content-Length", strconv.Itoa(len(blob)))
		w.Header().Add("Content-Type", "application/octet-stream")
		for i := 0; i < len(blob); i++ {
			w.Write([]byte(blob[i : i+1]))
			time.Sleep(500 * time.Millisecond)
		}
	}))
	defer server.Close()

	d, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(d)

	blobsync.SetConcurrentBlobs(10)

	var wg sync.WaitGroup
	blobPullerCnt := 6
	for i := 0; i < blobPullerCnt; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			layer := types.Layer{
				MediaType: types.V2dockerLayerGzipMt,
				Digest:    "sha256:" + digest,
				Size:      len(blob),
			}
			rc, err := newRegClient("hello-world:latest", strings.ReplaceAll(server.URL, "http://", ""), "")
			if err != nil {
				t.Fail()
			}
			if rc.V2Blobs(layer, filepath.Join(d, digest)) != nil {
				t.Fail()
			}
		}()
		time.Sleep(100 * time.Millisecond)
	}
	wg.Wait()
	if int(httpMethodCnt.Load()) != 1 {
		t.Fail()
	}
}

// Test namespace query param for pull-through / mirror support
func TestNs(t *testing.T) {
	rc, err := newRegClient("hello-world:latest", "", "frobozz.io")
	if err != nil {
		t.Fail()
	}
	p := rc.nsQueryParm()
	if p != "?ns=frobozz.io" {
		t.Fail()
	}
}

// Test the value used for setting the Accept header
func TestAllMfstTypes(t *testing.T) {
	s := allManifestTypesStr()
	cnt := strings.Count(s, ",")
	if cnt != len(allManifestTypes)-1 {
		t.Fail()
	}
}

// Test setting auth header in request
func TestSetAuthHdr(t *testing.T) {
	rc, err := newRegClient("hello-world:latest", "docker.io", "")
	if err != nil {
		t.Fail()
	}
	rc.AuthHdr = AuthHeader{
		Key:   "foobar",
		Value: "frobozz",
	}
	r := &http.Request{}
	r.Header = make(map[string][]string)
	rc.setAuthHdr(r)
	if r.Header.Get("foobar") != "frobozz" {
		t.Fail()
	}
}

// Test getting auth header from response
func TestGetAuthHdr(t *testing.T) {
	image := "hello-world:latest"
	mp := mock.NewMockParams(mock.BASIC, mock.NOTLS, mock.CertSetup{})
	server, url := mock.Server(mp)
	defer server.Close()
	rc, err := newRegClient(image, url, "")
	if err != nil {
		t.Fail()
	}
	imgUrl := fmt.Sprintf("http://%s/v2/hello-world/manifests/latest", url)
	resp, err := rc.Client.Head(imgUrl)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fail()
	}
	expAuth := fmt.Sprintf(`Basic realm="%s"`, url)
	actualAuth := getWwwAuthenticateHdrs(resp)
	if len(actualAuth) != 1 && expAuth != actualAuth[0] {
		t.Fail()
	}
}

// test getting a bearer token
func TestV2Bearer(t *testing.T) {
	image := "hello-world:latest"
	mp := mock.NewMockParams(mock.BEARER, mock.NOTLS, mock.CertSetup{})
	server, url := mock.Server(mp)
	defer server.Close()
	rc, err := newRegClient(image, url, "")
	if err != nil {
		t.Fail()
	}
	ba := types.BearerAuth{
		Realm:   fmt.Sprintf("http://%s/v2/auth", url),
		Service: url,
	}
	token, err := rc.V2Auth(ba, "")
	if err != nil {
		t.Fail()
	}
	// hard-coded token in the mock distr server
	if token.Token != "FROBOZZ" {
		t.Fail()
	}
}

func TestV2Basic(t *testing.T) {
	// future: the mock distribution server doesn't do basic auth
}

// test getting image list and image manifests
func TestV2Manifests(t *testing.T) {
	image := "hello-world:latest"
	mp := mock.NewMockParams(mock.BEARER, mock.NOTLS, mock.CertSetup{})
	server, url := mock.Server(mp)
	defer server.Close()
	rc, err := newRegClient(image, url, "")
	if err != nil {
		t.Fail()
	}
	mr, err := rc.V2Manifests("")
	if err != nil {
		t.Fail()
	}
	if mr.MediaType != types.V1ociIndexMt {
		t.Fail()
	}
	mr, err = rc.V2Manifests("sha256:e2fc4e5012d16e7fe466f5291c476431beaa1f9b90a5c2125b493ed28e2aba57")
	if err != nil {
		t.Fail()
	}
	if mr.MediaType != types.V1ociManifestMt {
		t.Fail()
	}
}

type headtest struct {
	ref string
	mt  types.MediaType
}

// test manifest head for image list manifest and image manifest
func TestV2ManifestHead(t *testing.T) {
	tests := []headtest{
		{ref: ":latest", mt: types.V1ociIndexMt},
		{ref: "@sha256:e2fc4e5012d16e7fe466f5291c476431beaa1f9b90a5c2125b493ed28e2aba57", mt: types.V1ociManifestMt},
	}
	for _, tst := range tests {
		image := fmt.Sprintf("hello-world%s", tst.ref)
		func() {
			mp := mock.NewMockParams(mock.BEARER, mock.NOTLS, mock.CertSetup{})
			server, url := mock.Server(mp)
			defer server.Close()
			rc, err := newRegClient(image, url, "")
			rc.AuthHdr = AuthHeader{Key: "Authorization", Value: "Bearer: FROBOZZ"}
			if err != nil {
				t.Fail()
			}
			md, err := rc.V2ManifestsHead()
			if err != nil {
				t.Fail()
			}
			if md.MediaType != tst.mt {
				t.Fail()
			}
		}()
	}
}

// newRegClient is a helper function to initialize a 'RegClient' struct
func newRegClient(image string, url string, namespace string) (RegClient, error) {
	ir, err := imgref.NewImageRef(fmt.Sprintf("%s/%s", url, image), "http", namespace)
	if err != nil {
		return RegClient{}, err
	}
	return RegClient{
		ImgRef:  ir,
		Client:  &http.Client{},
		AuthHdr: AuthHeader{},
	}, nil
}
