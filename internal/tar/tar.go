package tar

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yo-l1982/imgpull/internal/util"
	"github.com/yo-l1982/imgpull/pkg/imgpull/types"
)

// DockerTarManifest is the structure of 'manifest.json' that you would find
// in a tarball produced by 'docker save'.
type DockerTarManifest struct {
	Config   string   `json:"config"`
	RepoTags []string `json:"repoTags"`
	Layers   []string `json:"layers"`
}

// ImageTarball is used to build an image tarball.
type ImageTarball struct {
	// SourceDir has the config digest blob and the layer blobs
	SourceDir string
	// ImageUrl is the image url, like docker.io/hello-world:latest
	ImageUrl string
	// ConfigDigest is the digest of the image config layer
	ConfigDigest string
	// Layers is an array of blob Layers
	Layers []types.Layer
}

// ToTar creates an image tarball as configured in the receiver and writes it
// to the path/file specified in the 'tarfile' arg. The function returns a
// 'DockerTarManifest' struct that looks exactly like the 'manifest.json' file
// in the tarball.
func (tb ImageTarball) ToTar(tarfile string) (DockerTarManifest, error) {
	dtm := DockerTarManifest{
		Config:   "sha256:" + tb.ConfigDigest,
		RepoTags: []string{tb.ImageUrl},
	}
	file, err := os.Create(tarfile)
	if err != nil {
		return DockerTarManifest{}, err
	}
	defer file.Close()
	tw := tar.NewWriter(file)
	defer tw.Close()

	for _, layer := range tb.Layers {
		if ext, err := extensionForLayer(layer.MediaType); err != nil {
			return DockerTarManifest{}, err
		} else {
			fname := util.DigestFrom(layer.Digest)
			dtm.Layers = append(dtm.Layers, fname+ext)
			err = addFile(tw, filepath.Join(tb.SourceDir, fname), fname+ext)
			if err != nil {
				return DockerTarManifest{}, err
			}
		}
	}
	manifest, err := dtm.toString()
	if err != nil {
		return DockerTarManifest{}, err
	}
	err = addString(tw, string(manifest), "manifest.json")
	if err != nil {
		return DockerTarManifest{}, err
	}
	err = addFile(tw, filepath.Join(tb.SourceDir, tb.ConfigDigest), dtm.Config)
	if err != nil {
		return DockerTarManifest{}, err
	}
	return dtm, nil
}

// toString renders the docker tar manifest in the receiver as a JSON-formatted
// string exactly as it is required to be represented in an image tarball. Specifically.
// the manifest has be contained within in an array of DockerTarManifest. The output
// of this function can be written directly to the 'manifest.json' file in an
// image tarball.
func (dtm *DockerTarManifest) toString() ([]byte, error) {
	manifestArray := make([]DockerTarManifest, 1)
	manifestArray[0] = *dtm
	return json.MarshalIndent(manifestArray, "", "   ")
}

// addFile adds a file identified by the passed 'actualFile' to the
// passed tar file. The 'fileNameInTar' arg allows to give the file in the
// tarball a filename different from the file name on the file system.
func addFile(tw *tar.Writer, actualFile, fileNameInTar string) error {
	file, err := os.Open(actualFile)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = filepath.Base(fileNameInTar)
	err = tw.WriteHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(tw, file)
	return err
}

// addString adds the passed string to the tarfile as though it were
// a file. When you untar the file the extracted string behaves like
// any other file in the tar file. The intended use case is to write
// a manifest represented in a string as though it was a file.
func addString(tw *tar.Writer, content, name string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gname := ""
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return err
	} else {
		gname = g.Name
	}
	now := time.Now()
	header := tar.Header{
		Typeflag:   tar.TypeReg,
		Name:       name,
		Linkname:   "",
		Size:       int64(len(content)),
		Mode:       436,
		Uid:        uid,
		Gid:        gid,
		Uname:      u.Username,
		Gname:      gname,
		ModTime:    now,
		AccessTime: now,
		ChangeTime: now,
		Devmajor:   0,
		Devminor:   0,
		Xattrs:     nil,
		PAXRecords: nil,
		Format:     tar.FormatUnknown,
	}
	err = tw.WriteHeader(&header)
	if err != nil {
		return err
	}
	_, err = io.Copy(tw, strings.NewReader(content))
	return err
}

// extensionForLayer returns '.tar', '.tar.gz', or '.tar.zstd' based on the
// passed media type.
func extensionForLayer(mediaType types.MediaType) (string, error) {
	switch mediaType {
	case types.V1ociLayerMt, types.V2dockerLayerMt:
		return ".tar", nil
	case "", types.V2dockerLayerGzipMt, types.V1ociLayerGzipMt:
		return ".tar.gz", nil
	case types.V2dockerLayerZstdMt, types.V1ociLayerZstdMt:
		return ".tar.zstd", nil
	}
	return "", fmt.Errorf("unsupported layer media type %q", mediaType)
}
