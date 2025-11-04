package tar

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/yo-l1982/imgpull/internal/testhelpers"
	"github.com/yo-l1982/imgpull/pkg/imgpull/types"
)

// TestWriteFiles tests writing a physical file and a string "file"
// to a tarfile.
func TestWriteFiles(t *testing.T) {
	d, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fail()
	}
	defer os.RemoveAll(d)
	foobar, err := os.Create(filepath.Join(d, "foobar"))
	if err != nil {
		t.Fail()
	}
	_, err = foobar.WriteString("frobozz")
	if err != nil {
		t.Fail()
	}
	foobar.Close()
	tarfile, err := os.Create(filepath.Join(d, "foobar.tar"))
	if err != nil {
		t.Fail()
	}
	defer tarfile.Close()
	tw := tar.NewWriter(tarfile)
	defer tw.Close()
	if err := addFile(tw, foobar.Name(), foobar.Name()); err != nil {
		t.Fail()
	}
	if err := addString(tw, "flathead", "flathead"); err != nil {
		t.Fail()
	}
	tw.Close()
	tarfile.Close()

	if testhelpers.UntarFile(filepath.Join(d, "foobar.tar")) != nil {
		t.Fail()
	}

	b1, err1 := os.ReadFile(filepath.Join(d, "foobar"))
	b2, err2 := os.ReadFile(filepath.Join(d, "foobar.extracted"))
	if err1 != nil || err2 != nil {
		t.Fail()
	}
	if !bytes.Equal(b1, b2) {
		t.Fail()
	}
	b1, err1 = os.ReadFile(filepath.Join(d, "flathead.extracted"))
	if err1 != nil {
		t.Fail()
	}
	if string(b1) != "flathead" {
		t.Fail()
	}
}

// TestTarNew creates an image tarball and then extracts it to make sure the files
// were put in correctly.
func TestTarNew(t *testing.T) {
	d, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fail()
	}
	defer os.RemoveAll(d)
	tarfile := filepath.Join(d, "test.tar")
	configDigest := testhelpers.MakeDigest()
	err = os.WriteFile(filepath.Join(d, configDigest), []byte(configDigest), 0644)
	if err != nil {
		t.Fail()
	}
	url := "flathead.io/frobozz/fizzbin:v1.2.3"
	layerDigest := testhelpers.MakeDigest()
	err = os.WriteFile(filepath.Join(d, layerDigest), []byte(layerDigest), 0644)
	if err != nil {
		t.Fail()
	}
	layers := []types.Layer{
		{
			MediaType: types.V2dockerLayerGzipMt,
			Digest:    layerDigest,
			Size:      62,
		},
	}
	dtm, err := ImageTarball{
		SourceDir:    d,
		ConfigDigest: configDigest,
		ImageUrl:     url,
		Layers:       layers,
	}.ToTar(tarfile)
	if err != nil {
		t.Fail()
	}
	err = testhelpers.UntarFile(tarfile)
	if err != nil {
		t.Fail()
	}
	b, err := os.ReadFile(filepath.Join(d, fmt.Sprintf("sha256:%s.extracted", configDigest)))
	if err != nil {
		t.Fail()
	}
	if string(b) != configDigest {
		t.Fail()
	}
	b, err = os.ReadFile(filepath.Join(d, layerDigest+".tar.gz.extracted"))
	if err != nil {
		t.Fail()
	}
	if string(b) != layerDigest {
		t.Fail()
	}
	b, err = os.ReadFile(filepath.Join(d, "manifest.json.extracted"))
	if err != nil {
		t.Fail()
	}
	manifest, err := dtm.toString()
	if err != nil {
		t.Fail()
	}
	if !bytes.Equal(b, manifest) {
		t.Fail()
	}
}
