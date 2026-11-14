//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"context"
	"time"

	"github.com/go-notebook/notebook/rcells"
	nbruntime "github.com/go-notebook/notebook/runtime"
)

// RSupported reports whether this build runs R cells: go-notebook's runner
// (rcells), over go-embedded-r's evaluator, which has no cgo, no process and no
// host syscall, so it compiles to WebAssembly as it is. It is in the second
// core with the parsers (a notebook with an R cell loads it, like Ruby's), never
// in the default one, which holds Go only (#23, #25, #29).
const RSupported = true

// RLimits bound what one R cell may use in the page: a tab must not run out of
// memory or freeze, and a runaway cell ends with a message and leaves the page
// usable. They are go-notebook's DefaultRLimits (512 MiB, 50 million steps, 30
// s) tightened where WebAssembly asks for it:
//
//   - MaxAlloc 512 MiB, as go-notebook: the total of vector storage a cell
//     allocates over its life (copies included), so the heap it holds at once
//     is at most that, well under a tab's 4 GiB of linear memory;
//   - MaxSteps 20 million, calls and loop iterations together: a runaway loop
//     ends on the count, the same on every machine, in a few seconds;
//   - Timeout 10 s, under the page's watchdog (CELL_LIMIT_S, 30 s unless the
//     page says otherwise), so that the cell is stopped with R's own message
//     and its session kept, before the worker has to be killed. It is also the
//     only bound on a loop that catches the step limit's error.
//
// A test may change them; the page does not.
var RLimits = nbruntime.RLimits{MaxAlloc: 512 << 20, MaxSteps: 20_000_000, Timeout: 10 * time.Second}

// RDepth bounds the nesting of R's evaluations in the page, R's "expressions"
// option. go-embedded-r's default of 5000 is more than a browser's call stack
// holds: in Chromium a recursion of 1700 calls overflows it (a RangeError that
// loses the core), whatever the other limits say, and so does a recursion
// through sapply or tryCatch at 1500 (heavier frames per evaluation). The
// nesting is counted in evaluations, about three to a call of a small function:
// 600 is a recursion of 200 calls, and measured (Chromium) to end an infinite
// recursion with R's own error for plain calls, sapply, tryCatch, do.call and
// Reduce. R's own default is 5000, for a machine with a process's stack; the
// right bound is the evaluator's to know (go-embedded-r), not the page's.
var RDepth = 600

// rSide is the R half of a session: one interpreter for every R cell of the
// notebook, whose variables persist from cell to cell and stay apart from the
// other languages' (analysis.Isolated, as in go-notebook).
type rSide struct{ rs *rcells.Session }

// rCell runs an R cell. What it prints is the output, followed by what it
// writes to standard error and its warnings, in R's form ("Warning message:",
// then the call and the message); an error fails the cell, in R's "Error in
// call : message". Nothing of the host is visible: the files are an in-memory
// tree, and the environment is empty.
func (s *session) rCell(src string) (output string, shown []map[string]string, errText string) {
	if s.r == nil {
		s.r = &rSide{rs: rcells.NewWithDepth(RLimits, RDepth)}
	}
	out, err := s.r.rs.Eval(context.Background(), rcells.Cell{Source: src})
	output = out.Stdout + out.Stderr
	if err != nil {
		errText = err.Error()
	}
	return output, nil, errText
}
