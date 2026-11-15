package engine

// The Base image the Julia core boots from: the release of go-embedded-julia
// that ships it, and the SHA-256 of its julia-image-<Version>.bin asset
// (25,638,782 bytes; about 6.2 MB compressed). The digest is pinned here as
// go.sum pins a module, and the kernel checks it over the bytes before it
// decodes them (interp.LoadOptions.Digest). They are go-notebook's jcells.Version
// and jcells.Digest, which a test holds equal (julia_test.go).
const (
	JuliaImageVersion = "v0.17.0"
	JuliaImageDigest  = "d243d2b748afb7d331227195d835357b6502184bedcdc4bcc296854276c2f6ec"
	// JuliaImageSize is the image's size in bytes: what the page expects to
	// decompress, so that a truncated or inflated download is refused before
	// the digest is computed.
	JuliaImageSize = 25_638_782
)
