# yaegi, patched

This is the interpreter of [traefik/yaegi](https://github.com/traefik/yaegi) **v0.16.1**
(`interp/`, and `internal/unsafe2/` which it imports), copied here because the
playground needs fixes that are not in any yaegi release. It is Apache-2.0
licensed ([LICENSE](LICENSE)); the rest of this repository is BSD-3-Clause.

`stdlib/generic` (the source of `cmp`, `maps` and `slices` it compiles) and
`cmd/yaegi` (the `extract` command that writes `symbols/`) are used from the
yaegi module itself, at the version go.mod pins, which must stay v0.16.1.

## What was changed

Every change is marked `go-notebook/playground` in the code.

| File | Change | Why |
|---|---|---|
| `interp/*.go` | The import path of `unsafe2`; a `go:generate` line removed; gofmt. | It lives here now. |
| `interp/cfg.go` | A `v, err := f()` is skipped by the CFG pass only where the global type analysis (GTA) has defined it, `reachedByGTA`, not wherever the scope has no function. | GTA does not enter nested blocks, so such a definition inside a loop or an `if` of a top-level statement list was defined by nobody: `undefined: v`. In a notebook every cell is top-level code. |
| `interp/type.go` | The type of an index expression on a compiled slice, array or map (`valueT`). | It was nil, and `axs[0][0]` on the `[][]*plot.Axes` of `plot.Subplots` crashed the interpreter with a nil dereference. |
| `interp/gta.go` | When GTA gives up on a definition, it names the undefined identifier that kept its type unknown (`undefinedIdent`, `defined`). | It said `constant definition loop` for `y := undefinedName`. |

Each change is exercised by `engine/engine_test.go` (the cases that name a
patched defect).
