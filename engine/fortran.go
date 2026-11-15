//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"context"
	"time"

	"github.com/go-notebook/notebook/fcells"
)

// FortranSupported reports whether this build runs Fortran cells: go-notebook's
// runner (fcells), over go-embedded-fortran's evaluator, which has no cgo, no
// process, no file and no environment, so it compiles to WebAssembly as it is.
// It is in the second core with the parsers and the R evaluator (a notebook
// with a Fortran cell loads it), never in the default one, which holds Go only:
// the rule R follows (r.go).
const FortranSupported = true

// FortranOptions bound what one Fortran cell may use in the page: a tab must
// not freeze, and a runaway cell ends with a message and leaves the page
// usable.
//
//   - MaxSteps 200 million DO iterations and procedure calls together: the
//     count ends a runaway loop the same on every machine (go-notebook's default
//     is 2^32, about a minute of the evaluator's fastest loops natively, too
//     long for a tab);
//   - Timeout 10 s, under the page's watchdog (CELL_LIMIT_S, 30 s unless the
//     page says otherwise), so the cell is stopped with the evaluator's own
//     message and its session kept, before the worker has to be killed;
//   - MaxAlloc 512 MiB, R's bound, a counted budget of the storage the
//     notebook's Fortran holds: every variable, array, derived-type object and
//     character component is charged before it is allocated, so a bomb of many
//     small objects is refused like one big array, and R's own 450 MiB in the
//     same core is not in the count;
//   - MaxOutput is go-notebook's: 16 MiB to standard output and error
//     together, the rest dropped, and the cell says so.
//
// The evaluator's own bounds (array elements, character length) apply as well,
// and its calls are weighed under WebAssembly (go-embedded-fortran v0.23.0): a
// browser's call stack overflows at 150 to 2500 nested calls, by the nesting of
// the statements around the recursive call, which kills the core instead of
// failing the cell. A test may change the options; the page does not.
var FortranOptions = fcells.Options{MaxSteps: 200_000_000, Timeout: 10 * time.Second, MaxAlloc: 512 << 20}

// fSide is the Fortran half of a session: one evaluator for every Fortran cell
// of the notebook, whose modules, procedures and variables persist from cell to
// cell and stay apart from the other languages' (analysis.Isolated, as in
// go-notebook).
type fSide struct{ fs *fcells.Session }

// fCell runs a Fortran cell. What it prints is the output, followed by what it
// writes to standard error; a failure of the cell, with its line and column, is
// the error. Nothing of the host is visible: the evaluator has no file or
// environment access.
func (s *session) fCell(src string) (output string, shown []map[string]string, errText string) {
	if s.f == nil {
		s.f = &fSide{fs: fcells.NewWith(FortranOptions)}
	}
	out, err := s.f.fs.Eval(context.Background(), fcells.Cell{Source: src})
	output = out.Stdout + out.Stderr
	if err != nil {
		errText = err.Error()
	}
	return output, nil, errText
}
