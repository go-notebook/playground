package engine

import "time"

// The Base image the Julia core boots from: the release of go-embedded-julia
// that ships it, and the SHA-256 of its julia-image-<Version>.bin asset
// (25,638,782 bytes; about 7.8 MB gzipped). The digest is pinned here as
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

// JuliaLimits bound what one Julia cell may use in the page, as RLimits bound R's:
// a tab must not run out of memory or freeze, and a runaway cell ends with a
// message and leaves the page usable. They are in every build (the core that runs
// Julia, julia.go, is the one that reads them).
//
//   - MaxAlloc 512 MiB, as R's: the kernel counts the elements of the arrays and
//     dictionaries a cell allocates, 16 bytes each (a floor of the memory, not the
//     heap: boxed values cost more and strings are not counted), and no array may
//     have more than 2^24 elements whatever the limit;
//   - MaxSteps 20 million statements: a runaway loop ends on the count, the same
//     on every machine, in a few seconds;
//   - Timeout 20 s, under the page's watchdog (CELL_LIMIT_S, 30 s unless the
//     page says otherwise), so that the cell is stopped with Julia's own message
//     and its kernel kept, before the worker has to be killed. R's is 10 s; a
//     Julia cell compiles what it uses the first time it uses it, and an
//     ordinary cell (a comprehension, a print) took over 10 s on a CI runner;
//   - MaxOutput 16 MiB to each of standard output and error, the rest dropped,
//     and the cell says so.
//
// A test may change them; the page does not.
var JuliaLimits = struct {
	MaxAlloc  int
	MaxSteps  int64
	Timeout   time.Duration
	MaxOutput int
}{MaxAlloc: 512 << 20, MaxSteps: 20_000_000, Timeout: 20 * time.Second, MaxOutput: 16 << 20}
