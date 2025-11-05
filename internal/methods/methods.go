package methods

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/yo-l1982/imgpull/internal/blobsync"
	"github.com/yo-l1982/imgpull/internal/imgref"
	"github.com/yo-l1982/imgpull/internal/util"
	"github.com/yo-l1982/imgpull/pkg/imgpull/types"

	"github.com/opencontainers/go-digest"
	"golang.org/x/sys/unix"
)

// AuthHeader is a key/value struct that supports creating and setting an auth
// header for the supported auth type (basic, bearer).
type AuthHeader struct {
	Key   string
	Value string
}

// RegClient has everything needed to talk to an OCI Distribution server for the purposes
// of pulling an image. It is a subset of the 'Puller' struct.
type RegClient struct {
	// ImgRef is the parsed image url, e.g.: 'docker.io/hello-world:latest'
	ImgRef imgref.ImageRef
	// Client is the HTTP Client
	Client *http.Client
	// AuthHdr supports the various auth types (basic, bearer)
	AuthHdr AuthHeader
}

// ManifestGetResult is returned by the 'V2Manifests' function in this
// package. The manifest is contained within the 'ManifestBytes' struct
// member.
type ManifestGetResult struct {
	MediaType      types.MediaType
	ManifestBytes  []byte
	ManifestDigest string
}

// allManifestTypes lists all of the manifest types that this package
// will operate on.
var allManifestTypes []types.MediaType = []types.MediaType{
	types.V2dockerManifestListMt,
	types.V2dockerManifestMt,
	types.V1ociIndexMt,
	types.V1ociManifestMt,
}

// allManifestTypesStr concats all the manifest types supported to be pulled
// into a comma-separated string.
func allManifestTypesStr() string {
	toReturn := string(allManifestTypes[0])
	for i := 1; i < len(allManifestTypes); i++ {
		toReturn = fmt.Sprintf("%s,%s", toReturn, allManifestTypes[i])
	}
	return toReturn
}

// V2ManifestsAuth does a HEAD request for the manifest in the receiver, only looking for
// OK or unauthorized. Returns the http status code, an array of auth headers (which could
// be empty), and an error if one occurred or nil.
func (rc RegClient) V2ManifestsAuth() (int, []string, error) {
	url := rc.makeManifestUrl("")
	resp, err := rc.Client.Head(url)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return 0, nil, err
	}
	auth := getWwwAuthenticateHdrs(resp)
	return resp.StatusCode, auth, err
}

// V2Basic calls the 'v2' endpoint with a basic auth header formed from
// the username and password encoded in the passed string. If successful, the
// credentials are returned to the caller for use on subsequent calls.
func (rc RegClient) V2Basic(encoded string) (types.BasicAuth, error) {
	url := fmt.Sprintf("%s/v2/", rc.ImgRef.ServerUrl())
	req, _ := http.NewRequest(http.MethodHead, url, nil)
	req.Header.Set("Authorization", "Basic "+encoded)
	resp, err := rc.Client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return types.BasicAuth{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return types.BasicAuth{}, fmt.Errorf("basic auth returned status code %d", resp.StatusCode)
	}
	return types.BasicAuth{Encoded: encoded}, nil
}

// V2Auth calls the 'v2/auth' endpoint with the passed bearer struct which has
// realm and service. These are used to build the auth URL. The realm might be different
// than the server that we have been requested to pull from.  If successful, the
// bearer token is returned to the caller for use on subsequent calls.
func (rc RegClient) V2Auth(ba types.BearerAuth, encoded string) (types.BearerToken, error) {
	url := fmt.Sprintf("%s?scope=repository:%s:pull&service=%s", ba.Realm, rc.ImgRef.Repository, ba.Service)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if encoded != "" {
		req.Header.Set("Authorization", "Basic "+encoded)
	}
	resp, err := rc.Client.Do(req)
	if err != nil {
		return types.BearerToken{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return types.BearerToken{}, fmt.Errorf("auth attempt failed. Status: %d", resp.StatusCode)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	var token types.BearerToken
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&token)
	if err != nil {
		return types.BearerToken{}, err
	}
	return token, nil
}

// V2Blobs wraps a call to 'v2BlobsInternal' in concurrency handling if needed.
// This supports using the package as a library by synchronizing multiple goroutines
// pulling the same blob.
func (rc RegClient) V2Blobs(layer types.Layer, toFile string) error {
	if f, err := os.Stat(toFile); err == nil && f.Size() == int64(layer.Size) {
		// already exists on the file system
		return nil
	}
	if !blobsync.ConcurrentBlobs {
		return rc.V2BlobsInternal(layer, toFile)
	}
	so := blobsync.EnqueueGet(layer.Digest)
	var err error
	go func() {
		if so.Result == blobsync.NotEnqueued {
			defer blobsync.DoneGet(layer.Digest)
			err = rc.V2BlobsInternal(layer, toFile)
		}
	}()
	waitResult := blobsync.Wait(so)
	if err != nil {
		// blob pull err
		return err
	}
	return waitResult
}

// V2BlobsInternal calls the 'v2/<repository>/blobs' endpoint to get a blob by the digest in the
// passed 'layer' arg. The blob is stored in the location specified by 'toFile'.
func (rc RegClient) V2BlobsInternal(layer types.Layer, toFile string) error {
	url := ""
	if rc.ImgRef.NsInPath {
		url = fmt.Sprintf("%s/v2/%s/%s/blobs/%s", rc.ImgRef.ServerUrl(), rc.ImgRef.Namespace, rc.ImgRef.Repository, layer.Digest)
	} else {
		url = fmt.Sprintf("%s/v2/%s/blobs/%s%s", rc.ImgRef.ServerUrl(), rc.ImgRef.Repository, layer.Digest, rc.nsQueryParm())
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	rc.setAuthHdr(req)
	resp, err := rc.Client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("404 from server for blob digest %q", layer.Digest)
	}
	blobFile, err := os.Create(toFile)
	if err != nil {
		return err
	}
	defer blobFile.Close()

	// Use explicit buffer for efficient download
	buf := make([]byte, 64*1024)
	bytesRead, err := io.CopyBuffer(blobFile, resp.Body, buf)
	if err != nil {
		return err
	}
	if int(bytesRead) != layer.Size {
		return fmt.Errorf("error getting blob - expected %d bytes, got %d bytes instead", layer.Size, bytesRead)
	}

	// Tell kernel to drop this file from page cache (POSIX_FADV_DONTNEED)
	// This prevents page cache buildup when downloading large blobs
	unix.Fadvise(int(blobFile.Fd()), 0, 0, unix.FADV_DONTNEED)

	return nil
}

// V2Manifests calls the 'v2/<repository>/manifests' endpoint. The resulting manifest is returned in
// a ManifestHolder struct and could be any one of the types defined in the 'allManifestTypes' array.
// If you pass an empty string in 'sha', then the GET will use the image url that was used to initialize
// the Puller. (Probably a tag.) If you provide a digest in 'sha', the digest will override the tag.
//
// Generally speaking: pull by tag returns an image list from the registry if one is available and pull
// by digest (SHA) returns an image manifest. But this might not be true all the time.
func (rc RegClient) V2Manifests(sha string) (ManifestGetResult, error) {
	url := rc.makeManifestUrl(sha)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", allManifestTypesStr())
	rc.setAuthHdr(req)
	resp, err := rc.Client.Do(req)
	if err != nil {
		return ManifestGetResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ManifestGetResult{}, fmt.Errorf("get manifests attempt failed. Status: %d", resp.StatusCode)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	mediaType := resp.Header.Get("Content-Type")
	manifestBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ManifestGetResult{}, err
	}
	manifestDigest := resp.Header.Get("Docker-Content-Digest")
	computedDigest := digest.FromBytes(manifestBytes).Hex()
	if manifestDigest == "" {
		manifestDigest = computedDigest
	} else {
		manifestDigest = util.DigestFrom(manifestDigest)
		if computedDigest != manifestDigest {
			return ManifestGetResult{}, fmt.Errorf("digest mismatch for %q", url)
		}
	}
	return ManifestGetResult{
		MediaType:      types.MediaType(mediaType),
		ManifestBytes:  manifestBytes,
		ManifestDigest: manifestDigest,
	}, nil
}

// V2ManifestsHead is like V2Manifests but does a HEAD request. The result is returned in a
// smaller struct with only media type, digest, and size (of manifest). We don't allow overriding
// the ref becuase the use case for this method is to HEAD the manifest list.
func (rc RegClient) V2ManifestsHead() (types.ManifestDescriptor, error) {
	url := rc.makeManifestUrl("")
	req, _ := http.NewRequest(http.MethodHead, url, nil)
	req.Header.Set("Accept", allManifestTypesStr())
	rc.setAuthHdr(req)
	resp, err := rc.Client.Do(req)
	if err != nil {
		return types.ManifestDescriptor{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return types.ManifestDescriptor{}, fmt.Errorf("head manifests for %q failed with status %d", url, resp.StatusCode)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	mediaType := resp.Header.Get("Content-Type")
	if mediaType == "" {
		return types.ManifestDescriptor{}, fmt.Errorf("head manifests for %q did not return content type", url)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return types.ManifestDescriptor{}, fmt.Errorf("head manifests for %q did not return digest", url)
	}
	return types.ManifestDescriptor{
		MediaType: types.MediaType(mediaType),
		Digest:    digest,
		Size:      int(resp.ContentLength),
	}, nil
}

// makeManifestUrl is a help that forms  the URL string for the v2/.../manifests API call. It
// returns a URL taking into account whether the image ref in the receiver is namespaced, and
// whether the namespace is path-based or parameter based.
func (rc RegClient) makeManifestUrl(sha string) string {
	ref := rc.ImgRef.Ref
	if sha != "" {
		ref = sha
	}
	if rc.ImgRef.NsInPath {
		return fmt.Sprintf("%s/v2/%s/%s/manifests/%s", rc.ImgRef.ServerUrl(), rc.ImgRef.Namespace, rc.ImgRef.Repository, ref)
	} else {
		return fmt.Sprintf("%s/v2/%s/manifests/%s%s", rc.ImgRef.ServerUrl(), rc.ImgRef.Repository, ref, rc.nsQueryParm())
	}
}

// setAuthHdr sets an auth header (e.g. "Bearer", "Basic") on the passed request
// if the receiver is configured with such a header.
func (rc RegClient) setAuthHdr(req *http.Request) {
	if rc.AuthHdr != (AuthHeader{}) {
		req.Header.Set(rc.AuthHdr.Key, rc.AuthHdr.Value)
	}
}

// nsQueryParm checks if the receiver is configured with a namespace for pull-through,
// and if it is, returns the namespace as a query param in the form: '?ns=X' where 'X'
// is the receiver's namespace. If no namespace is configured, then the function
// returns the empty string.
func (rc RegClient) nsQueryParm() string {
	if rc.ImgRef.Namespace != "" {
		return "?ns=" + rc.ImgRef.Namespace
	} else {
		return ""
	}
}

// getWwwAuthenticateHdrs gets all "www-authenticate" headers from
// the passed response.
func getWwwAuthenticateHdrs(r *http.Response) []string {
	hdrs := []string{}
	for key, vals := range r.Header {
		for _, val := range vals {
			if strings.ToLower(key) == "www-authenticate" {
				hdrs = append(hdrs, val)
			}
		}
	}
	return hdrs
}
