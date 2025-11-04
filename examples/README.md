# Examples

This directory contains a couple of simple examples that show how to use the package as a library.

To get the package:

```
go get github.com/yo-l1982/imgpull@v1.2.0
```

## The Examples

To run each example, first `cd` into the example directory, then `go run main.go`.

| Directory | What it does |
|-|-|
| `pullblobs` | Pulls blobs and the image manifest for an image, writing them to the current working directory. |
| `pulltarball` | Pulls a tarball for an image, exactly like you would get with `docker save some-image:v123 -o some-image.tar` |
