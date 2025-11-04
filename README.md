# Simple Container Image Puller

This project consists of a library and a small CLI that **only** pulls OCI container images. The project goal was to create a feature-capable and robust image puller with a minimalist code base.

* [Quick Start - CLI](#quick-start---cli)
  + [Usage](#usage)
  + [Options](#options)
  + [More about namespaces](#more-about-namespaces)
  + [CLI Examples](#cli-examples)
* [Quick Start - Library](#quick-start---library)
  + [The `Puller` interface](#the-puller-interface)
---

## Quick Start - CLI

After git cloning the project, build the CLI:
```shell
make imgpull
```

This creates the binary `imgpull` in the `bin` directory. Verify:
```shell
bin/imgpull --version
```

Success looks like (date and version will differ):
```shell
imgpull version: 1.2.0 build date: 2025-03-01T01:56:38.18Z
```

### Usage

**Two** positional parameters are required to pull an image tarball: 1) an image reference and, 2) a tar file:
```shell
bin/imgpull [image ref] [tar file]
```

Example:
```shell
bin/imgpull docker.io/hello-world:latest hello-world-latest.tar
```

That's the simplest use case! Several options are supported:

### Options

**`-o|--os [operating system]`**

Specifies the operating system of the image to pull. Most images in public registries are multi-platform images. Usually you want an image for your operating system. Therefore if you omit this, the CLI uses `runtime.GOOS` to determine your OS and will pull the image for that OS. Generally, this will be used in conjunction with the `--arch` param.

Example:
```shell
bin/imgpull docker.io/hello-world:latest hello-world-latest.tar --os linux --arch amd64
```
---
**`-a|--arch [architecture]`**

Same idea as `--os` except specifies the architecture. If omitted, the code uses `runtime.GOARCH` to determine your architecture and will pull the image for that architecture. Generally, this will be used in conjunction with the `--os` param.

Example:
```shell
bin/imgpull docker.io/hello-world:latest hello-world-latest.tar --os linux --arch amd64
```

---
**`-n|--ns [namespace]`**

Specifies a namespace. The word _namespace_ in this context refers to a registry host name like `docker.io`, `registry.k8s.io`, `quay.io`, etc. This supports pulling from mirroring / pull-through registries and is intended to pull the same way `containerd` does when you've configured `containerd` mirroring.

Example:
```shell
bin/imgpull localhost:8080/curl/curl:8.10.1 quay.io-curl-8.1.0.1.tar --ns quay.io
```

In this scenario, you have a pull-through or mirroring registry running on `localhost:8080`. When the CLI interacts with the registry via the V2 OCI Distribution Server REST API, it appends the namespace to the various REST API calls as a query param. In other words, internally the CLI makes calls like:
```shell
https://localhost:8080/v2/curl/curl/manifests/8.10.1?ns=quay.io
```

Note the query param: `?ns=quay.io`. The pull-through mirroring registry running on `localhost:8080` will check to see if `quay.io/curl/curl:8.10.1` is cached and if not, will pull and cache from the upstream `quay.io` registry. (Or the mirror will fail it only supports mirroring, and the desired image is not already cached.)

For a highly configurable, performant, and feature-rich caching pull-through OCI distribution server that supports this functionality, see: my distribution server: https://github.com/yo-l1982/ociregistry.

---
**`-u|--user [username]` `-p|--password [password]`**

Specifies the username and password for basic auth.

Example:
```shell
bin/imgpull docker.io/hello-world:latest hello-world-latest.tar\
  --user jqpubli --password mypass
```
---
**`-s|--scheme [scheme]`**

Specifies the scheme. The CLI defaults to `https`. Valid values are `http` and `https`.

Example:
```shell
bin/imgpull my.inhouse.http.registry/hello-world:latest hello-world-latest.tar\
  --scheme http
```

The example pulls from a registry over HTTP, rather than HTTPS.

---
**`-c|--cert [tls cert]` `-k|--key [tls key]`**

Configures mTLS. By default, the CLI performs 1-way TLS using the OS trust store. You use these options to configure mTLS. The option values are paths to a PEM-encoded cert and key.

Example:
```shell
bin/imgpull my.mtls.registry/hello-world:latest hello-world-latest.tar\
  --cert /path/to/cert.pem --key /path/to/key.pem
```

---
**`-x|--cacert [tls ca cert]`**

Configures verification of the server cert using a specified CA. By default, server certs are validated using the OS trust store. This parameter supports cases where the server presents certs that are **not** signed by the OS trust store. The option value is a path to a PEM-encoded CA or CA bundle file.

Example:
```shell
bin/imgpull my.private.registry/hello-world:latest hello-world-latest.tar\
  --ca /path/to/ca.pem
```

---
**`-i|--insecure`**

Does not verify the server certs presented by the OCI distribution server if the connection to the server is over HTTPS (the default.) Use this option when the registry presents a server cert that is not signed by the OS trust store, and you are unable to provide a CA using the `--cacert` option.

Example:
```shell
bin/imgpull my.private.registry/hello-world:latest hello-world-latest.tar --insecure
```

---
**`-m|--manifest [type]`**

Displays the manifest to the console rather than downloading the image tarball. Valid values are `list` for the image list manifest, and `image` for the image manifest. If  you supply this param then the tarball positional param is ignored and can be omitted.

Example:
```shell
bin/imgpull docker.io/hello-world:latest --manifest list
```

> Not every image repository provides an image list manifest. If the image is not multi-platform then an image list manifest won't be available. In that case if you ask for an image list manifest (and it's not provided by the server) the CLI will display an error message to this effect.

---
**`-v|--version`**

Prints the version and exits.

Example:
```shell
bin/imgpull --version
```

---
**`-h|--help`**

Prints the help and exits.

Example:
```shell
bin/imgpull --help
```

---
**`--parsed`**

This is a developer troubleshooting option that prints the parsed command line without acting on the params, and then exits.

Example:
```shell
bin/imgpull my.inhouse.http.registry/hello-world:latest hello-world-latest.tar --cacert /path/to/ca.pem --parsed
```

### More about namespaces

Above, you saw that the following form of the CLI pulls _through_ a pull-through registry:
```shell
bin/imgpull localhost:8080/curl/curl:8.10.1 quay.io-curl-8.1.0.1.tar --ns quay.io
```

To further reiterate the information presented above, the result is that internally, the CLI makes a call like:
```shell
https://localhost:8080/v2/curl/curl/manifests/8.10.1?ns=quay.io
```

The CLI supports an *alternate* method of providing the namespace - directly in the path, as follows:
```shell
bin/imgpull localhost:8080/quay.io/curl/curl:8.10.1 quay.io-curl-8.1.0.1.tar
```

Notice that in this case, the namespace is embedded in the path of the image URL. The parsing rule for this is that the namespace must be the first path segment (`quay.io`) immediately after the upstream registry URL (`localhost:8080`). When using this form, the `--ns` arg is ignored. In this form, the CLI passes the URL directly through to the pull-through registry. E.g:
```shell
https://localhost:8080/v2/quay.io/curl/curl/manifests/8.10.1
```

### CLI Examples

1. Pull the image for `linux/amd64` to `hello-world.tar` in the working directory:
   ```shell
   bin/imgpull docker.io/hello-world:latest ./hello-world.tar -o linux -a amd64
   ```
1. Show the image manifest for the system OS and architecture
   ```shell
   bin/imgpull docker.io/hello-world:latest --manifest image
   ```
1. Pull with mTLS
   ```shell
   bin/imgpull docker.io/hello-world:latest --cert ~/mycert.pem --key ~/mykey.pem 
   ```
1. Pull with a custom CA
   ```shell
   bin/imgpull docker.io/hello-world:latest --cacert ~/myca.pem
   ```
1. Pull without server cert verification
   ```shell
   bin/imgpull docker.io/hello-world:latest --insecure
   ```
1. Pull with basic auth
   ```shell
   bin/imgpull docker.io/hello-world:latest --user foobar --password frobozz
   ```
1. Pull using http
   ```shell
   bin/imgpull localhost:8080/hello-world:latest --scheme http
   ```
1. Pull from a mirror
   ```shell
   bin/imgpull localhost:8080/hello-world:latest --ns docker.io
   ```
1. Pull from a mirror with a path-based namespace
   ```shell
   bin/imgpull localhost:8080/docker.io/hello-world:latest
   ```

## Quick Start - Library

The project was designed for the code to be easily used as a library. The two primary abstractions are:

1. `PullerOpts` - Configures how you'll interact with the upstream distribution server.
2. `Puller` - An interface comprising the API of the library for pulling images and manifests.

Example:

```go
package main

import (
    "fmt"
    "imgpull/pkg/imgpull"
)

func main() {
    image := "docker.io/hello-world:latest"
    puller, err := imgpull.NewPullerWith(imgpull.NewPullerOpts(image))
    if err != nil {
        fmt.Println(err)
        return
    } else if err := puller.PullTar("./hello-world.tar"); err != nil {
        fmt.Println(err)
    }
}
```

The `NewPullerOpts` function in the example above creates a`PullerOpts` struct as follows:

1. Uses the image URL you provide.
2. Defaults the scheme to `https`.
3. Validates the upstream distribution server cert using the host OS trust store.
4. Tries to pull an image matching the OS and architecture for your system.

> This is the most common use case.

Or, suppose you're pulling from a private registry that presents a cert signed by a CA that isn't included in the OS trust store. You can initialize a `PullerOpts` struct directly:
```go
    ...
    opts := imgpull.PullerOpts{
        Url:      "docker.io/hello-world:latest",
        Scheme:   "https",
        OStype:   "linux",
        ArchType: "amd64",
        CaCert:   "/path/to/ca.pem",
    }
    p, err := imgpull.NewPullerWith(opts)
```

You can see that the `PullerOpts` struct is the key to configuring the puller to interface with the upstream registry. In fact the CLI options directly map to the fields in the `PullerOpts` struct as shown by the table below.

> See the [Examples](examples) directory for examples of how to use the project as a library.

| Struct Member | Command line option | Setting via the struct | Setting via the CLI |
|-|-|-|-|
| `Url` | Positional param one | `Url: "docker.io/hello-world:latest"` | `imgpull docker.io/hello-world:latest` |
| `Scheme` | `-s\|--scheme [scheme]` | `Scheme: "http"` | `--scheme http` |
| `OStype` | `-o\|--os [operating system]` | `OStype: "linux"` | `--os linux` |
| `ArchType` | `-a\|--arch [architecture]` | `ArchType: "amd64"` | `--arch amd64` |
| `Username` | `-u\|--user [username]` | `Username: "foo"` | `--user foo` |
| `Password` | `-p\|--password [password]` | `Password: "bar"` | `--password bar` |
| `TlsCert` | `-c\|--cert [tls cert]` | `TlsCert: "/path/to/client-cert.pem"` | `--cert /path/to/client-cert.pem` |
| `TlsKey` | `-k\|--key [tls key]` | `TlsKey: "/path/to/client-key.pem"` | `--key /path/to/client-key.pem` |
| `CaCert` | `-x\|--cacert [tls ca cert]` | `CaCert: "/path/to/ca-cert.pem"` | `--cacert /path/to/ca-cert.pem` |
| `Insecure` | `-i\|--insecure` | `Insecure: true` | `--insecure` |
| `Namespace` | `-n\|--ns [namespace]` | `Namespace: "docker.io"` | `--ns docker.io` |

### The `Puller` interface

The `Puller` interface is how you interact with the library. The functions in the interface are shown in the table below. Each function is defined along with documentation in the `pkg/imgpull/dopull.go` file. All interface functions are obviously called via a receiver. E.g.:
```go
puller, _ := imgpull.NewPuller("docker.io/hello-world:latest")
puller.PullTar("/tmp/docker.io.hello-world.latest.tar")
```
> Once you create the puller, the `PullerOpts` contained within the puller govern the puller's behavior.

| Interface function | Purpose |
|-|-|
| `PullTar(dest string) error` | Pulls an image tarball using the `PullerOpts` in the receiver, and saves the tarball to the filesystem at the path and file name provided in the `dest` arg. |
| `PullManifest(mpt ManifestPullType) (ManifestHolder, error)` | Pulls an image list manifest or an image manifest depending on the passed `ManifestPullType` arg. |
| `PullBlobs(mh ManifestHolder, blobDir string) error` | Pulls all the blobs for the image in the passed `ManifestHolder` and writes them to the filesystem at the `blobDir` path. |
| `HeadManifest() (types.ManifestDescriptor, error)` | Performs a manifest HEAD request for the image in the receiver. Returns the manifest digest, media type, and size in the returned `ManifestDescriptor`. |
| `GetManifest() (ManifestHolder, error)` | Gets a manifest for the image in the receiver. The type of manifest returned is determined by the upstream. For example, if the receiver specifies a tag, and the upstream has a manifest list for that tag, then a manifest list is returned from the function. This would typically be the case when the upstream is a [multi-platform](https://docs.docker.com/build/building/multi-platform/) image. But if the upstream image is **not** multi-platform, then get by tag will return an image manifest, not an image list manifest. |
| `GetUrl() string` | Gets the image URL in the receiver. E.g.: `docker.io/hello-world:latest`. |
| `GetOpts() PullerOpts` | Gets the options in the receiver. |
