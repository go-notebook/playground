//go:build js || wasip1 || plan9

package engine

import "errors"

// JuliaImage is for cmd/build, which runs natively: the page's worker is given
// the image (julia_js.go), and these targets only compile the command.
func JuliaImage() ([]byte, error) {
	return nil, errors.New("the Julia image is fetched by a native build")
}
