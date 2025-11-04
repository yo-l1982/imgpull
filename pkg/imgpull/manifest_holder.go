package imgpull

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yo-l1982/imgpull/internal/imgref"
	"github.com/yo-l1982/imgpull/internal/tar"
	"github.com/yo-l1982/imgpull/internal/util"
	"github.com/yo-l1982/imgpull/pkg/imgpull/types"
	"github.com/yo-l1982/imgpull/pkg/imgpull/v1oci"
	"github.com/yo-l1982/imgpull/pkg/imgpull/v2docker"
)

// ManifestType identifies the type of manifest the package can operate on.
type ManifestType int

const (
	V2dockerManifestList ManifestType = iota
	V2dockerManifest
	V1ociIndex
	V1ociManifest
	Undefined
)

// MediaTypeFrom translatest ManifestType which is exported into a media type
// so the standard media types can be exported from the package.
var MediaTypeFrom = map[ManifestType]string{
	V2dockerManifestList: "application/vnd.docker.distribution.manifest.list.v2+json",
	V2dockerManifest:     "application/vnd.docker.distribution.manifest.v2+json",
	V1ociIndex:           "application/vnd.oci.image.index.v1+json",
	V1ociManifest:        "application/vnd.oci.image.manifest.v1+json",
}

// ManifestPullType indicates whether to pull an image manifest or an
// image list manifest.
type ManifestPullType int

const (
	// An image list manifest
	ImageList ManifestPullType = iota
	// An image manifest
	Image
)

// ManifestPullTypeFrom translates a literal string into a ManifestPullType
var ManifestPullTypeFrom = map[string]ManifestPullType{
	"image": Image,
	"list":  ImageList,
}

// manifestTypeToString has string representations for all supported
// 'ManifestType's.
var manifestTypeToString = map[ManifestType]string{
	Undefined:            "Undefined",
	V2dockerManifestList: "V2dockerManifestList",
	V2dockerManifest:     "V2dockerManifest",
	V1ociIndex:           "V1ociIndex",
	V1ociManifest:        "V1ociManifest",
}

// ManifestHolder holds one of: v1 oci manifest list, v1 oci manifest, docker v2
// manifest list, or docker v2 manifest. The original data from the upstream
// is also in the 'Bytes' member of the struct. The 'Bytes' member is the authoritative
// representation of the upstream data: if you compute a digest from it, the digest
// will match the 'Digest' field (also from the upstream) The other fields like
// 'V1ociIndex' are cosmetic and may not produce the same digest as the 'Bytes'
// field.
//
// The struct contains two fields that are not used by this library: Created and Pulled.
// These are intended for library consumers to be able to track when a manifest was
// created, or, most recently used.
type ManifestHolder struct {
	Type                 ManifestType          `json:"type"`
	Digest               string                `json:"digest"`
	ImageUrl             string                `json:"imageUrl"`
	Bytes                []byte                `json:"bytes,omitempty"`
	V1ociIndex           v1oci.Index           `json:"v1.oci.index"`
	V1ociManifest        v1oci.Manifest        `json:"v1.oci.manifest"`
	V2dockerManifestList v2docker.ManifestList `json:"v2.docker.manifestList"`
	V2dockerManifest     v2docker.Manifest     `json:"v2.docker.manifest"`
	Created              string                `json:"created"`
	Pulled               string                `json:"pulled"`
}

// ToString renders the manifest held by the receiver into JSON. Only the
// embedded manifest is returned - which will be a docker or oci manifest
// list, or a docker or oci image manifest.
func (mh *ManifestHolder) ToString() (string, error) {
	var err error
	var marshalled []byte
	switch mh.Type {
	case V2dockerManifestList:
		marshalled, err = json.MarshalIndent(mh.V2dockerManifestList, "", "   ")
	case V2dockerManifest:
		marshalled, err = json.MarshalIndent(mh.V2dockerManifest, "", "   ")
	case V1ociIndex:
		marshalled, err = json.MarshalIndent(mh.V1ociIndex, "", "   ")
	case V1ociManifest:
		marshalled, err = json.MarshalIndent(mh.V1ociManifest, "", "   ")
	}
	return string(marshalled), err
}

// NewManifestHolder is callable from outside the package with a string media type.
func NewManifestHolder(mediaType string, bytes []byte, digest string, imageUrl string) (ManifestHolder, error) {
	return newManifestHolder(types.MediaType(mediaType), bytes, digest, imageUrl)
}

// newManifestHolder initializes and returns a ManifestHolder struct for the passed
// manifest bytes. The manifest bytes will be deserialized into one of the four manifest
// variables based on the 'mediaType' arg.
func newManifestHolder(mediaType types.MediaType, bytes []byte, digest string, imageUrl string) (ManifestHolder, error) {
	mt := toManifestType(mediaType)
	if mt == Undefined {
		return ManifestHolder{}, fmt.Errorf("unknown manifest type %q", mediaType)
	}
	mh := ManifestHolder{
		Type:     mt,
		Digest:   digest,
		ImageUrl: imageUrl,
		Bytes:    bytes,
	}
	err := mh.unMarshalManifest(mt, bytes)
	if err != nil {
		return ManifestHolder{}, err
	}
	return mh, nil
}

// toManifestType returns the 'ManifestType' corrresponding to the passed
// 'mediaType'. If the media type does not match one of the supported types then
// the function returns 'Undefined'.
func toManifestType(mediaType types.MediaType) ManifestType {
	switch mediaType {
	case types.V2dockerManifestListMt:
		return V2dockerManifestList
	case types.V2dockerManifestMt:
		return V2dockerManifest
	case types.V1ociIndexMt:
		return V1ociIndex
	case types.V1ociManifestMt:
		return V1ociManifest
	default:
		return Undefined
	}
}

// MediaType returns the string media type of the receiver
func (mh *ManifestHolder) MediaType() string {
	switch mh.Type {
	case V2dockerManifestList:
		return string(types.V2dockerManifestListMt)
	case V2dockerManifest:
		return string(types.V2dockerManifestMt)
	case V1ociIndex:
		return string(types.V1ociIndexMt)
	case V1ociManifest:
		return string(types.V1ociManifestMt)
	default:
		return ""
	}
}

// unMarshalManifest unmarshals the passed bytes and stores the resulting typed manifest struct
// in the corresponding manifest variable in the receiver struct.
func (mh *ManifestHolder) unMarshalManifest(mt ManifestType, bytes []byte) error {
	var err error
	switch mt {
	case V2dockerManifestList:
		err = json.Unmarshal(bytes, &mh.V2dockerManifestList)
	case V2dockerManifest:
		err = json.Unmarshal(bytes, &mh.V2dockerManifest)
	case V1ociIndex:
		err = json.Unmarshal(bytes, &mh.V1ociIndex)
	case V1ociManifest:
		err = json.Unmarshal(bytes, &mh.V1ociManifest)
	default:
		err = fmt.Errorf("unknown manifest type: %d", mt)
	}
	return err
}

// IsManifestList returns true of the manifest held by the ManifestHolder
// receiver is a manifest list.
func (mh *ManifestHolder) IsManifestList() bool {
	return mh.Type == V2dockerManifestList || mh.Type == V1ociIndex
}

// IsImageManifest returns true of the manifest held by the ManifestHolder
// receiver is an image manifest.
func (mh *ManifestHolder) IsImageManifest() bool {
	return !mh.IsManifestList()
}

// IsLatest returns true if the manifest held by the ManifestHolder
// receiver has tag "latest".
func (mh *ManifestHolder) IsLatest() (bool, error) {
	// NewImageRef will ignore scheme and namespace
	if ir, err := imgref.NewImageRef(mh.ImageUrl, "", ""); err != nil {
		return false, err
	} else {
		return strings.ToLower(ir.Ref) == "latest", nil
	}
}

// Layers returns an array of 'Layer' for the manifest contained by the ManifestHolder
// receiver. The Config is also returned since that is obtained using the v2/blobs
// endpoint just like the image Layers.
func (mh *ManifestHolder) Layers() []types.Layer {
	layers := make([]types.Layer, 0)
	switch mh.Type {
	case V2dockerManifest:
		for _, l := range mh.V2dockerManifest.Layers {
			nl := types.Layer{
				Digest:    l.Digest,
				MediaType: types.MediaType(l.MediaType),
				Size:      int(l.Size),
			}
			layers = append(layers, nl)
		}
		nl := types.Layer{
			Digest:    mh.V2dockerManifest.Config.Digest,
			MediaType: types.MediaType(mh.V2dockerManifest.Config.MediaType),
			Size:      int(mh.V2dockerManifest.Config.Size),
		}
		layers = append(layers, nl)
	case V1ociManifest:
		for _, l := range mh.V1ociManifest.Layers {
			nl := types.Layer{
				Digest:    l.Digest,
				MediaType: types.MediaType(l.MediaType),
				Size:      int(l.Size),
			}
			layers = append(layers, nl)
		}
		nl := types.Layer{
			Digest:    mh.V1ociManifest.Config.Digest,
			MediaType: types.MediaType(mh.V1ociManifest.Config.MediaType),
			Size:      int(mh.V1ociManifest.Config.Size),
		}
		layers = append(layers, nl)
	}
	return layers
}

// ImageManifestDigests returns an array of the image manifest digests from the image list
// manifest in the receiver. If called for a manifest holder wrapping an image manifest, then
// an empty array is returned.
func (mh *ManifestHolder) ImageManifestDigests() []string {
	ims := []string{}
	if !mh.IsImageManifest() {
		switch mh.Type {
		case V2dockerManifestList:
			for _, m := range mh.V2dockerManifestList.Manifests {
				ims = append(ims, m.Digest)
			}
		case V1ociIndex:
			for _, m := range mh.V1ociIndex.Manifests {
				ims = append(ims, m.Digest)
			}
		}
	}
	return ims
}

// GetImageDigestFor looks in the manifest list in the receiver for a manifest in the list
// matching the passed OS and architecture and if found returns it. Otherwise an error is
// returned.
func (mh *ManifestHolder) GetImageDigestFor(os string, arch string) (string, error) {
	switch mh.Type {
	case V2dockerManifestList:
		for _, mfst := range mh.V2dockerManifestList.Manifests {
			if mfst.Platform.OS == os && mfst.Platform.Architecture == arch {
				return mfst.Digest, nil
			}
		}
	case V1ociIndex:
		for _, mfst := range mh.V1ociIndex.Manifests {
			if mfst.Platform.Os == os && mfst.Platform.Architecture == arch {
				return mfst.Digest, nil
			}
		}
	}
	return "", fmt.Errorf("unable to get manifest SHA for os %q, arch %q", os, arch)
}

// newImageTarball creates an 'imageTarball' struct from the passed receiver and args.
// The 'sourceDir' arg specifies where the blob files can be found. The function doesn't
// create the tarball but the struct that is returned has everything needed for the
// caller to create the tarball.
func (mh *ManifestHolder) newImageTarball(iref imgref.ImageRef, sourceDir string) (tar.ImageTarball, error) {
	itb := tar.ImageTarball{
		SourceDir: sourceDir,
	}
	switch mh.Type {
	case V2dockerManifest:
		itb.ConfigDigest = util.DigestFrom(mh.V2dockerManifest.Config.Digest)
		itb.ImageUrl = iref.UrlWithNs()
		for _, layer := range mh.V2dockerManifest.Layers {
			itb.Layers = append(itb.Layers, types.NewLayer(types.MediaType(layer.MediaType), layer.Digest, layer.Size))
		}
	case V1ociManifest:
		itb.ConfigDigest = util.DigestFrom(mh.V1ociManifest.Config.Digest)
		itb.ImageUrl = iref.UrlWithNs()
		for _, layer := range mh.V1ociManifest.Layers {
			itb.Layers = append(itb.Layers, types.NewLayer(types.MediaType(layer.MediaType), layer.Digest, layer.Size))
		}
	default:
		return itb, fmt.Errorf("can't create docker tar manifest from %q kind of manifest", manifestTypeToString[mh.Type])
	}
	return itb, nil
}
