//go:build js && julia

package engine

import (
	"errors"
	"syscall/js"
)

// juliaImage reads the image the worker downloaded, decompressed and put in
// the global playgroundJuliaImage (a Uint8Array, outside this module's linear
// memory until now), and copies it for one load. The digest is checked over
// these bytes by the kernel, not trusted from the page.
func juliaImage() ([]byte, error) {
	v := js.Global().Get("playgroundJuliaImage")
	if v.IsUndefined() || v.IsNull() {
		return nil, errors.New("the page did not give the Julia image")
	}
	n := v.Get("length").Int()
	if n != JuliaImageSize {
		return nil, errors.New("the Julia image the page gave is not the pinned size")
	}
	b := make([]byte, n)
	js.CopyBytesToGo(b, v)
	return b, nil
}
