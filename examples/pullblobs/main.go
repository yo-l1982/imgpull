package main

import (
	"fmt"
	"os"

	"github.com/yo-l1982/imgpull/pkg/imgpull"
)

// The program performs the following steps:
//  1. Initializes a Puller with an image reference. By default, the puller
//     selects the image based on the host OS and architecture.
//  2. Gets the image manifest for the image and saves the manifest to
//     the current directory as a formatted JSON file.
//  3. Pulls all the image blobs and saves them to the current working
//     directory.
func main() {
	imageref := "quay.io/curl/curl:8.11.1"
	puller, err := imgpull.NewPullerWith(imgpull.NewPullerOpts(imageref))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// get the image manifest (not the image *list* manifest)
	mh, err := puller.GetManifestByType(imgpull.Image)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// convert the manifest to JSON
	if manifest, err := mh.ToString(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else {
		// save the JSON manifest to the current working directory
		err := os.WriteFile("./curl-8.11.1.json", []byte(manifest), 0644)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	// get all the image blobs to the current working directory
	err = puller.PullBlobs(mh, "./")
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("done - no errors")
}
