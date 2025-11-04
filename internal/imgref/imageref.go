package imgref

import (
	"fmt"
	"strings"

	"github.com/yo-l1982/imgpull/internal/util"
)

// imgPullType specifies whether pulling my tag or digest
type imgPullType int

const (
	// Pull by tag
	byTag imgPullType = iota
	// Pull by digest
	byDigest
)

// ImageRef has the components of an image reference.
type ImageRef struct {
	// e.g.: foo.io/bar/baz:v1.2.3
	raw string
	// if 'Raw' is foo.io/bar/baz:v1.2.3 then 'pullType' is 'byTag'
	pullType imgPullType
	// if 'Raw' is foo.io/bar/baz:v1.2.3 then 'Registry' is 'foo.io'
	Registry string
	// if 'Raw' is foo.io/bar/baz:v1.2.3 then 'server' is 'foo.io'
	server string
	// if 'Raw' is foo.io/bar/baz:v1.2.3 then 'Repository' is 'bar/baz'
	Repository string
	// if 'Raw' is foo.io/bar/baz:v1.2.3 then 'org' is 'bar'
	org string
	// if 'Raw' is foo.io/bar/baz:v1.2.3 then 'image' is 'baz'
	image string
	// if 'Raw' is foo.io/bar/baz:v1.2.3 then 'Ref' is 'v1.2.3'
	Ref string
	// 'http' or 'https'
	scheme string
	// Namespace supports pull-through and mirroring, i.e. pull
	// 'localhost:5000/hello-world:latest' with Namespace 'docker.io' to
	// pull from localhost if localhost is a mirror or a pull-through
	// registry.
	Namespace string
	// if the url was provided with the namespace in the path like
	// localhost:8080/docker.io/hello-world:latest then this is set to
	// true, else it is false.
	NsInPath bool
	// addedOrg is true if the org was added by the NewImageRef function,
	// as in the case when docker.io/hello-world:latest is requsted the
	// org has be made "library".
	addedOrg bool
}

// NewImageRef parses the passed image url (e.g. docker.io/hello-world:latest,
// or docker.io/library/hello-world@sha256:...) into an 'imageRef' struct. The url
// MUST begin with a registry hostname (e.g. quay.io or localhost:8080) - it is not
// (and cannot be) inferred.
func NewImageRef(url, scheme, namespace string) (ImageRef, error) {
	ir := ImageRef{
		raw:       strings.ToLower(url),
		pullType:  byTag,
		scheme:    strings.ToLower(scheme),
		Namespace: namespace,
	}
	parts := strings.Split(ir.raw, "/")
	ir.Registry = parts[0]
	ir.server = ir.Registry
	if ir.Registry == "docker.io" {
		ir.server = "index.docker.io"
	}
	if len(parts) == 2 {
		ir.image = parts[1]
		if ir.Registry == "docker.io" {
			ir.org = "library"
			ir.addedOrg = true
		}
	} else if len(parts) == 3 {
		if strings.Contains(parts[1], ".") {
			ir.Namespace = parts[1]
			ir.NsInPath = true
			ir.image = parts[2]
		} else {
			ir.org = parts[1]
			ir.image = parts[2]
		}
	} else if len(parts) == 4 && strings.Contains(parts[1], ".") {
		ir.Namespace = parts[1]
		ir.NsInPath = true
		ir.org = parts[2]
		ir.image = parts[3]
	} else {
		return ImageRef{}, fmt.Errorf("unable to parse image url %q", ir.raw)
	}
	ref_separators := []struct {
		separator string
		pt        imgPullType
	}{
		{separator: "@", pt: byDigest},
		{separator: ":", pt: byTag},
	}
	// split image and tag or digest
	for _, rs := range ref_separators {
		if strings.Contains(ir.image, rs.separator) {
			imgParts := strings.Split(ir.image, rs.separator)
			ir.image = imgParts[0]
			ir.Ref = imgParts[1]
			ir.pullType = rs.pt
			break
		}
	}
	if ir.org == "" {
		ir.Repository = ir.image
	} else {
		ir.Repository = fmt.Sprintf("%s/%s", ir.org, ir.image)
	}
	if ir.image == "" {
		return ImageRef{}, fmt.Errorf("unable to parse image url: %q", url)
	}
	return ir, nil
}

// Url returns the image url in the receiver exactly as represented in
// the receiver.
func (ir *ImageRef) Url() string {
	return ir.makeUrl("", false)
}

// UrlWithNs returns the image url in the receiver with registry component
// replaced by the namespace in the receiver if the namespace is non-empty.
// E.g. if the image url used to actually pull an image is
// 'localhost:8080/jetstack/cert-manager-controller:v1.16.2' and the namespace
// in the receiver is 'quay.io' then the function returns:
// quay.io/jetstack/cert-manager-controller:v1.16.2"
func (ir *ImageRef) UrlWithNs() string {
	return ir.makeUrl("", true)
}

// UrlWithDigest returns the image url in the receiver allowing to override
// the image reference (i.e. tag) in the receiver with the passed digest.
func (ir *ImageRef) UrlWithDigest(digest string) string {
	return ir.makeUrl(digest, false)
}

// makeUrl does the actual work for 'ImageUrl', 'UrlWithNs', and
// 'UrlWithDigest'
func (ir *ImageRef) makeUrl(sha string, withNs bool) string {
	regToUse := ir.Registry
	if withNs && ir.Namespace != "" {
		regToUse = ir.Namespace
	}
	var refToUse string
	if strings.HasPrefix(ir.Ref, "sha256:") {
		refToUse = "@" + ir.Ref
	} else if sha != "" {
		refToUse = "@sha256:" + util.DigestFrom(sha)
	} else {
		refToUse = ":" + ir.Ref
	}
	repositoryToUse := ir.Repository
	if ir.addedOrg {
		repositoryToUse = ir.image
	}
	return fmt.Sprintf("%s/%s%s", regToUse, repositoryToUse, refToUse)
}

// ServerUrl handles the case where an image is pulled from docker.io but the package
// has to access the DockerHub API on host index.docker.io so the receiver would have
// a 'Registry' value of docker.io and a 'Server' value of index.docker.io. This function
// is used whenver API calls are made - to return 'Server'. This seems to be unique to
// DockerHub.
func (ir *ImageRef) ServerUrl() string {
	return fmt.Sprintf("%s://%s", ir.scheme, ir.server)
}
