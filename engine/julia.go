//go:build (js && julia) || !(js || wasip1 || plan9)

package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-embedded-julia/julia/interp"
)

// JuliaRuns reports whether this build runs Julia cells: go-embedded-julia's
// interpreter, on a kernel booted from its Base image. In the page it is the
// third core (-tags=julia, on top of the second one), loaded only when a Julia
// cell is run: the default core and the second never carry it (20 MB of
// WebAssembly and a 25 MB image).
const JuliaRuns = true

// elemBytes is what the kernel's allocation count charges for one element (a Go
// `any`), as go-notebook's jcells has it.
const elemBytes = 16

// options are the budgets of a cell for the kernel.
func juliaOptions() interp.CellOptions {
	return interp.CellOptions{
		Timeout:   JuliaLimits.Timeout,
		MaxSteps:  JuliaLimits.MaxSteps,
		MaxAlloc:  max(int64(JuliaLimits.MaxAlloc/elemBytes), 1),
		MaxOutput: JuliaLimits.MaxOutput,
	}
}

// jSide is the Julia half of a session: the kernel every Julia cell of the
// notebook runs on, whose Main module keeps its variables from cell to cell and
// shares none with the other languages' (analysis.Isolated in effect: in the
// page, Julia variables do not cross to Go or Ruby cells, where go-notebook's do).
type jSide struct{ k *interp.Kernel }

// loadKernel boots a kernel from the pinned image. The digest is checked over
// the bytes before they are decoded.
func loadKernel() (*interp.Kernel, error) {
	img, err := juliaImage()
	if err != nil {
		return nil, err
	}
	return interp.LoadKernel(bytes.NewReader(img), interp.LoadOptions{Digest: &pinned}, interp.KernelOptions{})
}

// pinned is JuliaImageDigest as the kernel takes it (a test holds that it parses).
var pinned, _ = interp.ParseDigest(JuliaImageDigest)

// warm is a kernel made ahead of the run that will use it: the page boots it
// before it says the Julia core is ready (loading takes seconds, the first cell
// milliseconds), and again after each run, which closes its own.
var (
	warmMu sync.Mutex
	warm   *interp.Kernel
	// loader makes a kernel; a seam for the tests that cannot reach the image.
	loader = loadKernel
)

// WarmJulia boots a kernel for the next run, if none is waiting.
func WarmJulia() error {
	warmMu.Lock()
	defer warmMu.Unlock()
	if warm != nil {
		return nil
	}
	k, err := loader()
	if err != nil {
		return err
	}
	warm = k
	return nil
}

// takeKernel is the waiting kernel, or a new one.
func takeKernel() (*interp.Kernel, error) {
	warmMu.Lock()
	k := warm
	warm = nil
	warmMu.Unlock()
	if k != nil {
		return k, nil
	}
	return loader()
}

// used is set once a Julia cell has run in this process: it can boot no other
// kernel for free: each boot keeps memory in the instance (about 250 MiB, and
// the twelfth boot ran out of the 4 GiB), so the page replaces the worker.
var used atomic.Bool

// JuliaUsed reports whether a Julia cell has run in this process.
func JuliaUsed() bool { return used.Load() }

// closeJulia ends the session's kernel: each run is a session of its own, with
// a Main of its own (a struct redefined by a second run would be an error in
// the first one's).
func (s *session) closeJulia() {
	if s.j != nil {
		s.j.k.Close()
		s.j = nil
	}
}

// jCell runs a Julia cell. What it prints is the output; the value of its last
// expression, as the Julia REPL shows it, is shown (nothing when it is
// nothing, or when the cell ends in a semicolon); what it writes to standard
// error follows the output; an exception fails the cell, with Julia's text of
// it after "ERROR: ". Nothing of the host is visible: the kernel's files are
// Julia's own tree and its environment is empty.
func (s *session) jCell(src string) (output string, shown []map[string]string, errText string) {
	if s.j == nil {
		k, err := takeKernel()
		if err != nil {
			return "", nil, "the Julia kernel could not start: " + err.Error()
		}
		s.j = &jSide{k: k}
		used.Store(true)
	}
	res := s.j.k.EvalWith(context.Background(), src, juliaOptions())
	output = res.Stdout + res.Stderr
	if res.Truncated {
		output += fmt.Sprintf("[output truncated: more than %d bytes]\n", JuliaLimits.MaxOutput)
	}
	if res.Err != nil {
		return output, nil, juliaFailure(res.Err)
	}
	if showValue(res.Value, src) {
		shown = []map[string]string{{"text/plain": res.Value}}
	}
	return output, shown, ""
}

// showValue reports whether the REPL shows the value of a cell: not nothing,
// and not when the cell's last statement ends in a semicolon.
func showValue(value, src string) bool {
	return value != "" && value != "nothing" && !strings.HasSuffix(strings.TrimSpace(src), ";")
}

// juliaFailure is the text of a cell's failure, in go-notebook's words
// (jcells): Julia's own message for an exception, and the bound that stopped
// the cell otherwise.
func juliaFailure(err error) string {
	var je *interp.JuliaError
	switch {
	case errors.As(err, &je):
		return "ERROR: " + je.Message
	case errors.Is(err, interp.ErrCellTimeout) && JuliaLimits.Timeout > 0:
		return fmt.Sprintf("stopped: the Julia cell ran longer than %s, the limit", JuliaLimits.Timeout)
	case errors.Is(err, interp.ErrCellTimeout):
		return "stopped: the Julia cell's deadline passed"
	case errors.Is(err, interp.ErrCellCanceled):
		return "stopped: the Julia cell was cancelled"
	case errors.Is(err, interp.ErrStepLimit):
		return fmt.Sprintf("stopped: the Julia cell executed more than %d statements, the limit", JuliaLimits.MaxSteps)
	case errors.Is(err, interp.ErrMemoryLimit):
		return "stopped: the Julia cell went over its memory limit: " + strings.TrimPrefix(err.Error(), "interp: memory limit exceeded: ")
	}
	return "the Julia cell failed: " + err.Error()
}
