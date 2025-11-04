package imgpull

import "github.com/yo-l1982/imgpull/internal/blobsync"

// SetConcurrentBlobs exposes the ability to configure blob download concurrency
// at the package level since this function is encapsulated within the 'blobsync'
// internal package. The 'timeoutSec' arg indicates how long a blob pull will
// run before timing out, and is intended to accommodate slow or degraded network
// connectivity to the upstream.
//
// If enabled, then if multiple goroutines pull the same blob concurrently, only
// one goroutine will actually pull and the others will wait. This conserves
// network bandwidth.
func SetConcurrentBlobs(timeoutSec int) {
	blobsync.SetConcurrentBlobs(timeoutSec)
}
