package imgpull

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/yo-l1982/imgpull/internal/methods"
	"github.com/yo-l1982/imgpull/internal/tar"
	"github.com/yo-l1982/imgpull/internal/util"
	"github.com/yo-l1982/imgpull/pkg/imgpull/types"
)

// Puller is the interface to the package for pulling images and manifests.
type Puller interface {
	// GetManifestByType pulls an image manifest or an image list manifest based on the value
	// of the 'mpt' arg.
	GetManifestByType(mpt ManifestPullType) (ManifestHolder, error)
	// GetManifest gets a manifest for the image in the receiver. If the receiver
	// is configured with a tag then the manifest returned is determined by the
	// upstream registry: if an image list manifest is available, it will be provided by
	// the registry. If no image list manifest is available then an image manifest
	// will be provided by the registry if available. Whatever the registry provides
	// is returned in a 'ManifestHolder' which holds all four supported manifest types,
	// only one of which will be populated.
	GetManifest() (ManifestHolder, error)
	// GetManifestByDigest is like GetManifest except uses the passed digest
	GetManifestByDigest(digest string) (ManifestHolder, error)
	// HeadManifest does a HEAD request for the image URL in the receiver. The
	// 'ManifestDescriptor' returned to the caller contains the image digest,
	// media type and manifest size, as provided by the upstream distribution
	// server.
	HeadManifest() (types.ManifestDescriptor, error)
	// PullBlobs pulls the blobs for an image, writing them into 'blobDir'.
	PullBlobs(mh ManifestHolder, blobDir string) error
	// PullTar pulls an image tarball from a registry based on the configuration
	// options in the receiver and writes it to the path/file name specified in the
	// 'dest' arg.
	PullTar(dest string) error
	// GetUrl returns the image ref from the receiver
	GetUrl() string
	// GetOpts returns puller options
	GetOpts() PullerOpts
}

// HTTP status codes that we will interpret as un-authorized
var unauth = []int{http.StatusUnauthorized, http.StatusForbidden}

func (p *puller) PullTar(dest string) error {
	if dest == "" {
		return fmt.Errorf("no destination specified for pull of %q", p.Opts.Url)
	}
	tmpDir, err := os.MkdirTemp("/tmp", "imgpull.")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if itb, err := p.pull(tmpDir); err != nil {
		return err
	} else {
		_, err := itb.ToTar(dest)
		return err
	}
}

func (p *puller) GetManifestByType(mpt ManifestPullType) (ManifestHolder, error) {
	if err := p.connect(); err != nil {
		return ManifestHolder{}, err
	}
	rc := p.regCliFrom()
	mr, err := rc.V2Manifests("")
	if err != nil {
		return ManifestHolder{}, err
	}
	mh, err := newManifestHolder(mr.MediaType, mr.ManifestBytes, mr.ManifestDigest, rc.ImgRef.Url())
	if err != nil {
		return ManifestHolder{}, err
	}
	if mh.IsManifestList() {
		if mpt == ImageList {
			return mh, nil
		}
		digest, err := mh.GetImageDigestFor(p.Opts.OStype, p.Opts.ArchType)
		if err != nil {
			return ManifestHolder{}, err
		}
		mr, err := rc.V2Manifests(digest)
		if err != nil {
			return ManifestHolder{}, err
		}
		mh, err = newManifestHolder(mr.MediaType, mr.ManifestBytes, mr.ManifestDigest, rc.ImgRef.UrlWithDigest(digest))
		if err != nil {
			return ManifestHolder{}, err
		}
		return mh, nil
	}
	// if we get here, then the registry did not have a manifest list and so
	// it provided an image manifest
	if mpt == Image {
		return mh, nil
	} else {
		return ManifestHolder{}, fmt.Errorf("server did not provide a manifest for %q", p.ImgRef.Url())
	}
}

func (p *puller) PullBlobs(mh ManifestHolder, blobDir string) error {
	if err := p.connect(); err != nil {
		return err
	}
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		return fmt.Errorf("unable to create directory %q, error: %q", blobDir, err)
	}
	rc := p.regCliFrom()
	for _, layer := range mh.Layers() {
		if err := rc.V2Blobs(layer, filepath.Join(blobDir, util.DigestFrom(layer.Digest))); err != nil {
			return err
		}
	}
	return nil
}

func (p *puller) HeadManifest() (types.ManifestDescriptor, error) {
	if err := p.connect(); err != nil {
		return types.ManifestDescriptor{}, err
	}
	return p.regCliFrom().V2ManifestsHead()
}

func (p *puller) GetManifest() (ManifestHolder, error) {
	return p.internalGetManifest("")
}

func (p *puller) GetManifestByDigest(digest string) (ManifestHolder, error) {
	return p.internalGetManifest(digest)
}

func (p *puller) internalGetManifest(digest string) (ManifestHolder, error) {
	if err := p.connect(); err != nil {
		return ManifestHolder{}, err
	}
	rc := p.regCliFrom()
	mr, err := rc.V2Manifests(digest)
	if err != nil {
		return ManifestHolder{}, err
	}
	return newManifestHolder(mr.MediaType, mr.ManifestBytes, mr.ManifestDigest, rc.ImgRef.Url())
}

func (p *puller) GetUrl() string {
	return p.ImgRef.Url()
}

func (p *puller) GetOpts() PullerOpts {
	return p.Opts
}

// pull pulls the image specified in the receiver, saving blobs to the passed 'blobDir'.
// An 'imageTarball' struct is returned that describes the pulled image. The directory
// specfied by 'blobDir' will be populated with:
//
//  1. The configuration blob
//  2. The layer blobs.
//
// All blobs are saved into this directory with filenames consisting of 64-character digests.
func (p *puller) pull(blobDir string) (tar.ImageTarball, error) {
	if err := p.connect(); err != nil {
		return tar.ImageTarball{}, err
	}
	rc := p.regCliFrom()
	mr, err := rc.V2Manifests("")
	if err != nil {
		return tar.ImageTarball{}, err
	}
	mh, err := newManifestHolder(mr.MediaType, mr.ManifestBytes, mr.ManifestDigest, rc.ImgRef.Url())
	if err != nil {
		return tar.ImageTarball{}, err
	}
	if mh.IsManifestList() {
		digest, err := mh.GetImageDigestFor(p.Opts.OStype, p.Opts.ArchType)
		if err != nil {
			return tar.ImageTarball{}, err
		}
		mr, err := rc.V2Manifests(digest)
		if err != nil {
			return tar.ImageTarball{}, err
		}
		mh, err = newManifestHolder(mr.MediaType, mr.ManifestBytes, mr.ManifestDigest, rc.ImgRef.UrlWithDigest(digest))
		if err != nil {
			return tar.ImageTarball{}, err
		}
	}
	for _, layer := range mh.Layers() {
		if err := rc.V2Blobs(layer, filepath.Join(blobDir, util.DigestFrom(layer.Digest))); err != nil {
			return tar.ImageTarball{}, err
		}
	}
	return mh.newImageTarball(p.ImgRef, blobDir)
}

// connect calls the 'v2' endpoint and looks for an auth header. If an auth
// header is provided by the remote registry then this function will attempt
// to negotiate the auth handshake for Bearer if the remote requests it, or
// Basic using the user/pass in the receiver. Once successfully authenticated,
// the auth credential (bearer token or encrypted user/pass) are retained in
// the receiver for all the other API methods to build an auth header with.
//
// If the function has already been called on the receiver, it immediately
// returns taking no action.
func (p *puller) connect() error {
	if p.Connected {
		return nil
	}
	status, auth, err := p.regCliFrom().V2ManifestsAuth()
	if err != nil {
		return err
	}
	if status != http.StatusOK && slices.Contains(unauth, status) {
		err := p.authenticate(auth)
		if err != nil {
			return err
		}
	}
	p.Connected = true
	return nil
}

// authenticate scans the passed list of auth headers received from a distribution
// server and attempts to perform authentication for each in the following order:
//
//  1. bearer
//  2. basic (using the user/pass that the puller receiver was initialized from)
//
// If successful then the receiver is initialized with the corresponding auth
// struct so that it is available to be used for all subsequent API calls to the
// distribution server. For example if 'bearer' then the token received from the
// remote registry will be added to the receiver.
func (p *puller) authenticate(auth []string) error {
	rc := p.regCliFrom()
	for _, hdr := range auth {
		if strings.HasPrefix(strings.ToLower(hdr), "bearer") {
			ba := parseBearer(hdr)
			encoded := ""
			if p.Opts.Username != "" && p.Opts.Password != "" {
				delimited := fmt.Sprintf("%s:%s", p.Opts.Username, p.Opts.Password)
				encoded = base64.StdEncoding.EncodeToString([]byte(delimited))
			}
			bt, err := rc.V2Auth(ba, encoded)
			if err != nil {
				return err
			}
			p.Token = bt
			return nil
		} else if strings.HasPrefix(strings.ToLower(hdr), "basic") {
			delimited := fmt.Sprintf("%s:%s", p.Opts.Username, p.Opts.Password)
			encoded := base64.StdEncoding.EncodeToString([]byte(delimited))
			ba, err := rc.V2Basic(encoded)
			if err != nil {
				return err
			}
			p.Basic = ba
			return nil
		}
	}
	return fmt.Errorf("unable to parse auth param: %v", auth)
}

// regCliFrom creates a 'RegClient' from the receiver, consisting of a subset of receiver
// fields needed to interact with the OCI Distribution Server V2 REST API. It supports
// a looser coupling of the Puller from actually interacting with the distribution server.
//
// If this function is intended to return a regClient to make API calls that require auth
// headers, then the Connect function must previously have been called on the receiver so
// that the auth struct in the receiver is initialized by virtue of that call. The auth
// struct is copied into the returned regClient struct which is used to set auth headers.
func (p *puller) regCliFrom() methods.RegClient {
	rc := methods.RegClient{
		ImgRef: p.ImgRef,
		Client: p.Client,
	}
	if k, v := p.authHdr(); k != "" {
		rc.AuthHdr = methods.AuthHeader{
			Key:   k,
			Value: v,
		}
	}
	return rc
}

// parseBearer parses the passed auth header which the caller should ensure is a bearer
// type "www-authenticate" header like:
//
//		Bearer realm="https://auth.docker.io/token",service="registry.docker.io"
//	    Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:yo-l1982/ociregistry:pull"
//
// The function returns the parsed result in a 'BearerAuth' struct.
func parseBearer(authHdr string) types.BearerAuth {
	ba := types.BearerAuth{}
	parts := []string{"realm", "service", "scope"}
	expr := `%s[\s]*=[\s]*"{1}([0-9A-Za-z\-:/.,]*)"{1}`
	for _, part := range parts {
		srch := fmt.Sprintf(expr, part)
		m := regexp.MustCompile(srch)
		matches := m.FindStringSubmatch(authHdr)
		if len(matches) == 2 {
			switch part {
			case "realm":
				ba.Realm = strings.ReplaceAll(matches[1], "\"", "")
			case "scope":
				ba.Scope = strings.ReplaceAll(matches[1], "\"", "")
			default:
				ba.Service = strings.ReplaceAll(matches[1], "\"", "")
			}
		}
	}
	return ba
}
