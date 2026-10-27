# playground — go-notebook

[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27.2%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-brightgreen)](.github/workflows/ci.yml)

**Go notebooks in the browser: cells interpreted in WebAssembly, no server.**

A [go-notebook](https://github.com/go-notebook/notebook) is a Markdown document whose ` ```go ` blocks are cells. The playground shows one as a page: the prose as text, each cell as an editor, and under it what the cell printed, the value of its last expression and its figures, inline as SVG. Ctrl+Enter (⌘+Enter) runs the notebook again, in the tab: a Go interpreter, [yaegi](https://github.com/traefik/yaegi), compiled to WebAssembly, runs the cells, and the libraries they call — [go-statistics](https://github.com/go-statistics/stats), [go-numerics](https://github.com/go-numerics) (`special`, `optimize`, `integrate`), [go-ndarray](https://github.com/go-ndarray/ndarray) (with `random` and `fft`) and [go-plot](https://github.com/go-plot/plot) — run compiled, at WebAssembly speed. Only the notebook's own code is interpreted.

It is deployed as `/playground/` on [go-notebook.github.io](https://go-notebook.github.io/playground/), [go-statistics.github.io](https://go-statistics.github.io/playground/) and [go-numerics.github.io](https://go-numerics.github.io/playground/), each with its own example notebooks.

```go
import (
	"github.com/go-ndarray/ndarray/random"
	"github.com/go-statistics/stats"
)

x, _ := random.DefaultRNG(42).Normal(10, 2, 500)
fit, err := stats.FitNormal(x, nil)
stats.KSTest1Samp(x, fit.CDF, nil) // shown: {Statistic:0.0292… PValue:0.773… …}
```

## How it works

The page follows the fleet's N3 web doctrine: HTML, CSS and about 15 KB of JavaScript of its own (plus Go's `wasm_exec.js`) own the DOM; the Go core is called rarely, on big buffers.

- **[web/](web/)** — `index.html`, `playground.css`, `playground.js` and `worker.js`. The page renders the cells, and on each run posts the **whole notebook** (as Markdown) to a Web Worker and gets back **every cell's results** (as JSON): one call in, one string out, never a call per cell or per widget. The worker keeps a long run from freezing the page, and Stop ends it.
- **[cmd/playground](cmd/playground/)** — the WebAssembly core, a few lines: `playgroundRun(markdown)` and `playgroundCells(markdown)`.
- **[engine/](engine/)** — runs a notebook in a yaegi interpreter, natively in the tests and in WebAssembly in the page. It splits the notebook with [`notebook/format`](https://github.com/go-notebook/notebook/tree/main/format) and follows go-notebook's conventions:
  - the cells run in document order and share one top-level scope: what a cell defines, the cells after it see;
  - a cell's final expression is shown; a final call returning `(value, error)` shows the value, or fails the cell with the error; other calls with several results (`fmt.Println`'s `(n, err)`) are run for their effect;
  - a value that draws itself as SVG — a go-plot `*plot.Figure` — is shown as a figure; a struct is shown with its exported fields named;
  - a failing cell stops the run, and the cells after it say they were not run; an error gives the line in the cell, and a panic the lines of the calls it went through.
- **[symbols/](symbols/)** — the packages a cell may import, as yaegi symbol tables written by `yaegi extract` (`go generate ./symbols`; CI checks that the committed tables are what it writes): a subset of the standard library (`fmt`, `math`, `math/rand`, `math/rand/v2`, `math/cmplx`, `math/bits`, `math/big`, `sort`, `strings`, `strconv`, `errors`, `time`, `bytes`, `bufio`, `io`, `text/tabwriter`, `unicode`, `unicode/utf8`; `cmp`, `maps` and `slices` from yaegi's source) and the libraries above. Nothing reaches the network or a file system.
- **[internal/yaegi](internal/yaegi/)** — yaegi v0.16.1's interpreter with three fixes the playground needs; [its README](internal/yaegi/README.md) lists them.
- **[cmd/build](cmd/build/)** — writes a deployable directory from a set of notebooks. It first **runs every notebook natively and fails if a cell fails**, then builds the WebAssembly core from source (`GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w"`), copies Go's `wasm_exec.js` and writes the page with the notebooks in it.

### Each cell says its language

Above each cell, a badge says which language runs it, **Go** or **Ruby**. The badge is text, not a colour, and it is the cell's description for a screen reader. The language comes from the fence's info string (` ```go `, ` ```ruby `), as go-notebook's `format` package reads it, never from the code. While the runtime that runs the cell is not ready, the badge says so: *Go · loading the runtime…*, *Ruby · loading the Ruby runtime…*. It says *cannot run: the runtime did not start* if the runtime failed. A fenced block in any other language (` ```python `) is not a cell. It stays prose, shown as text under a badge that says *python · not run: shown as text*. The editor is a plain `<textarea>` with no syntax highlighting, so the badge is how a cell shows its language. `web/test/page.mjs` checks every example's badges, their wording before the runtime is ready, other fences, and their fit at 360 px.

### While it loads

The Go core is 7.6 MB on the wire, the Ruby core about 12.7 MB. While the first one downloads, the page shows what go-notebook does: the [landing kit](https://github.com/go-fleettools/landingkit)'s loading carousel, the same component as the go-slides playground's, rendered by `cmd/build` with `landingkit.LoadingCarousel` (the kit executes its own Hugo partial, so nothing is copied here).

- Five cards: cells, the reactive kernel, Ruby cells, figures, export to HTML. Their words come from go-notebook's README and documentation, and each links to the page that says more. The pictures (`cards/<n>.svg`) are drawn by `cmd/build`: a Go cell's picture is what the engine gives for it when the site is built, the value it shows or its go-plot figure.
- It shows only while the core is **still downloading after 300 ms**. The worker reads a clone of the core's response to its end and tells the page, so a cached core never shows it, however long its compile takes.
- It floats in the corner, so the notebook does not move when it comes and goes, and it goes when the runtime is ready: it fades away, or, for a reader who is in it (hovering, focused, or a click in the last 6 s), waits with an **Open the notebook** button that moves focus to the notebook.
- The kit's WAI-ARIA carousel pattern: the rotation button first, labelled slides, dots as buttons; focus stops the rotation, hover pauses it. Under `prefers-reduced-motion` it neither rotates nor fades.
- It covers **the first wait only**, whichever core that is: a visitor who arrives on a Ruby notebook (`#3-ruby`) sees it during the Ruby core's download. Opening a Ruby notebook later does not bring it back: the reader is already reading a notebook, and the status line says *Loading the Ruby runtime…*.

### Ruby cells

A notebook may have ` ```ruby ` cells too, as in [go-notebook](https://github.com/go-notebook/notebook#ruby-cells). They run in [rbgo](https://github.com/go-embedded-ruby/ruby), a Ruby written in Go, with the [go-scikit bindings](https://github.com/go-scikit/ruby): `Stats`, `Special`, `Optimize`, `Integrate`, `DataFrame` and `Plot` over the same compiled libraries the Go cells import, and rbgo's `NDArray`.

- **The Ruby is loaded only when it is needed.** The Go core (`playground.wasm`) has no Ruby in it. A second core, `playground-ruby.wasm`, holds both languages: the Go core plus rbgo's `rbgo_core` build and the bindings. The page asks for it only when the open notebook has a Ruby cell, so a visitor who runs only Go never downloads it. `web/test/page.mjs` checks this.
- **Variables cross between the languages.**
  - A Go variable a Ruby cell reads is set as a Ruby local. An `*ndarray.Array` stays the same NDArray, and a `[]float64` becomes an Array.
  - A Ruby local becomes a Go variable typed from its value: `float64`, `[]float64`, `*ndarray.Array`, or the go-scikit value itself (`stats.TTestResult`, `*plot.Figure`, …). A type of a package the playground does not have is `any`.
  - A Ruby cell's methods and constants stay Ruby's, and so does a local named like a Go builtin (`len`).
- **Errors** read as for Go cells: `line 2: divided by 0 (ZeroDivisionError)`, then `at line N` for each frame in the cell.

### Figures are data

A notebook can come from anyone (a visitor opens a file), and a cell can show any SVG it likes: a Go type's `SaveSVG` or a Ruby cell writes whatever text it wants. So a figure reaches the page through two sanitizers:

- [`engine.SanitizeSVG`](engine/svg.go) rewrites it through an allowlist of SVG elements, attributes and style properties. It drops scripts and style sheets, event handlers, animation, foreign objects, and every reference outside the document: an `href` that is not a `#fragment` or an embedded PNG/JPEG/GIF, and a `url()` that is not `url(#…)`. A figure that is not well-formed XML, over 32 MiB or nested over 256 deep is shown as text. Each hostile class has a regression test, and `FuzzSanitizeSVG` (run in CI) checks that the output always parses, holds only the allowlist and comes back unchanged when sanitized again.
- The page checks the parsed DOM again before inserting it.

### Deploying it on a site

A site keeps its notebooks and a small Go module that pins the playground:

```
playground/
  go.mod          require github.com/go-notebook/playground v0.4.0
  tools.go        //go:build tools — import _ "github.com/go-notebook/playground/cmd/build"
  notebooks/*.md
```

To pin or bump the version, `go get` **the package**, not the module:

```sh
cd playground && go get github.com/go-notebook/playground/cmd/build@v0.4.0
```

`go get github.com/go-notebook/playground@v0.4.0` updates `go.mod` but records in `go.sum` only what the module's own packages need, not what `cmd/build` imports. Since v0.3.0 that includes the landing kit (`github.com/go-fleettools/landingkit`, for the loading carousel), and the deploy then fails with `missing go.sum entry for module providing package github.com/go-fleettools/landingkit`. Getting the package records the sums of everything `go run …/cmd/build` compiles. `go mod tidy` does the same, because `tools.go` imports the package.

and its deploy workflow builds the page into the site, from that pinned version:

```sh
cd playground && go run github.com/go-notebook/playground/cmd/build \
    -o ../public/playground --org go-statistics --home https://go-statistics.github.io/ \
    --accent '#9e520f' --accent-dark '#f2b36b' notebooks/*.md
```

The flags are POSIX: `-o`/`--out`, `--org`, `--home`, `--accent`, `--accent-dark`, `--libraries`, plus `-h`/`--help` and `--version`. The single-dash long spelling of earlier releases (`-org x`) is still read, with a note on stderr, so existing workflows keep working.

The WebAssembly is never committed: every deploy builds it from the version `go.mod` and `go.sum` pin.

## Size and speed

Measured on the live pages on 2026-10-17, v0.1.0 (headless Chromium from a fresh profile, so with an empty browser cache, from a workstation in Europe):

| | |
|---|---|
| `playground.wasm` on the wire | **7.56 MB** (7 561 584 bytes; GitHub Pages serves it `Content-Encoding: gzip`, `Content-Type: application/wasm`); 29.8 MB uncompressed |
| page load → runtime ready → first result | **0.73–0.80 s → 0.81–0.85 s** on the first visit (the CDN cold); 0.22–0.36 s → 0.26–0.43 s on the next ones |
| one run of an example | 7 ms (quadrature) to 220 ms (the stiff ODE, 32 000 interpreted right-hand-side calls) |

`node web/test/page.mjs https://go-statistics.github.io/playground/` measures it again.

Where the size goes, measured by building without each part: the interpreter with the standard library and go-ndarray, 3.6 MB compressed; go-plot (its DejaVu Sans faces, go-tex's math fonts, the rasteriser) 2.5 MB; go-statistics 0.6 MB; go-numerics 0.5 MB. yaegi calls methods by name through `reflect`, so the linker keeps every exported method of every type the tables reach: it cannot drop what a notebook might call.

Interpreted code is much slower than compiled code — a loop of a million float additions takes about 36 ms in the interpreter, natively, against well under a millisecond compiled — which is why the libraries are compiled in, and why a notebook should hand them whole arrays and functions rather than loop itself.

## What works

- Statements, `:=`, loops, closures, functions, methods, struct types, interfaces, goroutines and channels, `defer`, labels: the cells are Go.
- **Calling the libraries**, including with Go functions: `optimize.CurveFit(model, …)`, `integrate.SolveIVP(f, …)`, `stats.Bootstrap(…, statistic, …)`, `integrate.Quad(func(x float64) float64 {…}, …)`. The callback is interpreted; the solver is compiled.
- **go-statistics' generic functions.** yaegi cannot export a generic function (it has no value until its type parameters are fixed), so `yaegi extract` leaves out every one, and most of stats is generic (`Mean[D Data]`, `FitNormal[D Data]`, `TTestInd[D Data]`…). [`internal/genwrap`](internal/genwrap/) writes an instantiated wrapper for each of the 89 (`go generate ./symbols`; a test fails when they are not what it writes), in which:
  - a parameter that is the data (`x D`, `samples ...D`) is an `any` that takes a `[]float64`, a `*ndarray.Array` of any shape and dtype (read row-major as float64, as stats reads it) or a `[]int`: `stats.Mean(xs, nil)`, `stats.Mean(arr, nil)` and `stats.TTestInd(a, b, nil)` all work;
  - every other use of the type parameter is `[]float64`: `stats.ZScore(arr, nil)` returns a `[]float64`, not an array of `arr`'s shape, and `stats.Bootstrap` takes a `[][]float64`.
- go-ndarray's three generic functions: `ndarray.Data(a)` (a `[]float64` view; `ok` is false for another dtype), `ndarray.Item(a, i…)` (a float64), and `ndarray.FromSlice(s, shape…)` for a slice of any element type an array holds (a `[]int` becomes int64).
- A type the notebook declares with a `String` method prints through it, with `fmt` and when shown.

## What does not

These are yaegi's limits, met while writing the examples, and listed so that nobody has to find them again:

- **Generic functions declared in a cell** work only in simple cases: with two type parameters, inference goes wrong (`Map[T, U any]` called with a `func(int) string` fails with `cannot use type func(int) string as type func(int) int`).
- **`slices`, `maps` and `cmp`** come from yaegi's copy of their Go 1.22 source, and only part of it compiles: `slices.Contains`, `slices.Index`, `slices.Equal` and `cmp.Compare` work; `slices.Sort`, `slices.Max`, `slices.Reverse`, `slices.Sorted`, `maps.Clone` and `maps.Keys` do not. Use `sort.Float64s`, or the libraries.
- **The float constants of compiled packages are `float64` values**, not untyped constants. yaegi refused to mix an untyped constant of a compiled package with one of the notebook (`math.Pi * 50.0` was `mismatched types untyped float64 and untyped float`), so the tables give them as `float64`: `2 * math.Pi`, `math.Pi * 50.0`, `const tau = 2 * math.Pi` and `var x float32 = math.Pi` work, `const c float32 = math.Pi` does not (`constant definition loop`): write `float32(math.Pi)`.
- An unexported field of a struct the notebook declares shows as `X` + its name (`{A:1 Xb:2}`): yaegi exports it to build the type with `reflect`.
- No `os`, `net`, `unsafe`, `reflect`, `syscall` or `testing`. An import of a package the playground does not have says which ones it has.
- Cells run in document order, all of them, on each run. go-notebook's kernel orders cells by what they use and runs only what changed; the playground runs the whole notebook again, which takes milliseconds, so the page always shows the state the code makes. A cell may define a name again (yaegi allows it); the kernel would refuse.
- A yaegi defect that crashes the interpreter is caught and reported as `the interpreter failed on this code (a yaegi defect)`, rather than taking the page down.

## Tests

`go test ./...` runs every example notebook headlessly and compares each cell's output with [`engine/testdata/golden`](engine/testdata/golden/) (numbers to 1e-9 relative or 1e-11 absolute, so that a last-bit difference between architectures is not a failure; `-update` rewrites them); it tests the cell conventions and each patched yaegi defect, the symbol tables (every generated wrapper is called, every interface wrapper reaches its function) and `cmd/build`, which builds the real WebAssembly core. Coverage is 100% outside `internal/yaegi`. CI also builds both example sites and runs [`web/test/page.mjs`](web/test/page.mjs) in headless Chromium: the page loads, every example runs, every cell gets a result, none fails, and every figure is an `<svg>`. It drives the loading carousel first, with the core's request held back for 5.2 s: shown on a cold load, rotating and fading; never on a second load from the cache (the local server is not asked for the core again); worked with Tab, Enter and Space, the Start button waiting for a reader in it; still and unfaded under reduced motion. The same script checks a deployed playground: `node web/test/page.mjs https://go-statistics.github.io/playground/`.

## License

BSD-3-Clause. See [LICENSE](LICENSE). `internal/yaegi` is yaegi's code, under the Apache License 2.0: see [its LICENSE](internal/yaegi/LICENSE).
