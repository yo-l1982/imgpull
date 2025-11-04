package main

import (
	"fmt"
	"os"
	"time"

	"github.com/yo-l1982/imgpull/pkg/imgpull"
)

func main() {
	cmdline, err := parseArgs()
	if err != nil {
		fmt.Println(err)
		showUsageAndExit(nil)
	}
	puller, err := imgpull.NewPullerWith(pullerOptsFrom(cmdline))
	if err == nil {
		if cmdline.getVal(manifestOpt) != "" {
			err = showManifest(puller, cmdline.getVal(manifestOpt))
		} else {
			err = pullTar(puller, cmdline.getVal(destOpt))
		}
	}
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func showManifest(puller imgpull.Puller, manifestType string) error {
	mt := imgpull.ManifestPullTypeFrom[manifestType]
	if mh, err := puller.GetManifestByType(mt); err != nil {
		return err
	} else if manifest, err := mh.ToString(); err != nil {
		return err
	} else {
		fmt.Printf("MANIFEST:\n%s\nMANIFEST DIGEST: %s\nIMAGE URL: %s\n", manifest, mh.Digest, mh.ImageUrl)
	}
	return nil
}

func pullTar(puller imgpull.Puller, tarFile string) error {
	start := time.Now()
	if err := puller.PullTar(tarFile); err != nil {
		return err
	} else {
		fmt.Printf("image %q saved to %q in %s\n", puller.GetUrl(), tarFile, time.Since(start))
	}
	return nil
}
