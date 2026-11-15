//go:build !(js || wasip1 || plan9)

package engine

import (
	"context"
	"sync"
	"time"

	"github.com/go-embedded-julia/julia/interp"
)

// The image, natively: fetched from go-embedded-julia's release by its pinned
// digest on the first use, and kept in the user's cache directory (hashed again
// each time), as go-notebook's does. The page does not fetch it this way: its
// worker downloads the compressed copy that cmd/build wrote (julia_js.go).
var (
	imageOnce  sync.Once
	imageBytes []byte
	imageErr   error
	// fetchImage is interp.FetchImage; a seam for the test of its failure.
	fetchImage = interp.FetchImage
	// juliaImage is the verified image the kernel boots from; a seam for the
	// test of a kernel that cannot start.
	juliaImage = fetchedImage
)

func fetchedImage() ([]byte, error) {
	imageOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		imageBytes, imageErr = fetchImage(ctx, JuliaImageVersion, pinned, interp.FetchOptions{})
	})
	return imageBytes, imageErr
}

// JuliaImage is the verified image, for cmd/build, which compresses it for the page.
func JuliaImage() ([]byte, error) { return fetchedImage() }
