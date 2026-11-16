//go:build (js && julia) || !(js || wasip1 || plan9)

package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

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

// The process has one kernel, booted ahead of the first run (the page boots it
// before it says the Julia core is ready: loading takes seconds, a cell
// milliseconds) and kept: each Julia run starts with Kernel.Reset (go-embedded-julia
// v0.19.0), which puts it back as it was booted, in milliseconds and without
// the memory a second boot keeps in the instance (about 250 MiB each, and the
// twelfth boot ran out of the 4 GiB).
var (
	kernMu sync.Mutex
	kern   *interp.Kernel
	// dirty: a cell has run in kern since it was booted or last reset.
	dirty bool
	// replace: the worker owes the page a replacement, because a Reset had to
	// load the image again (ResetInfo.Reloaded: about 250 MiB are kept) or failed
	// and the kernel was booted anew.
	replace bool
	// loader makes a kernel; a seam for the tests that cannot reach the image.
	loader = loadKernel
)

// WarmJulia boots the kernel, if there is none.
func WarmJulia() error {
	kernMu.Lock()
	defer kernMu.Unlock()
	if kern != nil {
		return nil
	}
	k, err := loader()
	if err != nil {
		return err
	}
	kern = k
	return nil
}

// takeKernel is the kernel for a run: the one booted, reset if a cell has run in
// it, or a new one when there is none or its Reset failed.
func takeKernel() (*interp.Kernel, error) {
	kernMu.Lock()
	defer kernMu.Unlock()
	if kern == nil {
		k, err := loader()
		if err != nil {
			return nil, err
		}
		kern = k
	} else if dirty {
		info, err := kern.Reset()
		if err != nil {
			kern.Close()
			kern = nil
			k, lerr := loader()
			if lerr != nil {
				return nil, lerr
			}
			kern, replace = k, true
		} else if info.Reloaded {
			replace = true
		}
	}
	dirty = false
	return kern, nil
}

// JuliaReplace reports whether the page should replace this worker: a Reset
// reloaded the image or failed, and the instance holds the memory of a boot more.
func JuliaReplace() bool {
	kernMu.Lock()
	defer kernMu.Unlock()
	return replace
}

// BreakJulia closes the kernel under the process, as a fault the next run must
// survive: its Reset fails, a kernel is booted anew and the worker's replacement
// is owed. It is for the page's test of that path (a Reset that reloads the image
// needs over 260,000 logged writes, which no cell can make within the page's
// 20 million statements).
func BreakJulia() {
	kernMu.Lock()
	defer kernMu.Unlock()
	if kern != nil {
		kern.Close()
		dirty = true
	}
}

// closeJulia ends the run's use of the kernel, which stays for the next run,
// whose Reset gives it a Main of its own (a struct redefined by a second run
// is not an error in it).
func (s *session) closeJulia() {
	if s.j != nil {
		kernMu.Lock()
		dirty = true
		kernMu.Unlock()
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
