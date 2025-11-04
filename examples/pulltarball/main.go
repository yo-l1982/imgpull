package main

import (
	"fmt"
	"os"

	"github.com/yo-l1982/imgpull/pkg/imgpull"
)

// The program pulls an image tarball to the current working directory. The
// image will match the OS and architecture of the host.
func main() {
	imageref := "quay.io/curl/curl:8.11.1"
	tarfile := "./curl-8.11.1.tar"
	if puller, err := imgpull.NewPullerWith(imgpull.NewPullerOpts(imageref)); err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else {
		if err = puller.PullTar(tarfile); err != nil {
			fmt.Println(err)
		} else {
			fmt.Printf("Successfully pulled %q to %q\n", imageref, tarfile)
		}
	}
}
