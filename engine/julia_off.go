//go:build !((js && julia) || !(js || wasip1 || plan9))

package engine

import "errors"

// JuliaRuns reports whether this build runs Julia cells: the default core and
// the second one do not (go-embedded-julia's interpreter is 20 MB of
// WebAssembly), the third one does (julia.go). The page loads that core when a
// Julia cell is run, never before.
const JuliaRuns = false

// jSide is the Julia half of a session, absent from these builds.
type jSide struct{}

// errNoJulia is what these builds say of a Julia cell; the page never sends
// one: it loads the Julia core to run a notebook that has Julia cells.
var errNoJulia = errors.New("this build does not run Julia cells (in the page, the Julia core is loaded when a Julia cell is run)")

// jCell is what these builds say of a Julia cell. It is where a Julia cell
// ends here, never the Go evaluator.
func (s *session) jCell(string) (string, []map[string]string, string) {
	return "", nil, errNoJulia.Error()
}

// closeJulia has nothing to close.
func (s *session) closeJulia() {}

// BreakJulia does nothing here: there is no kernel.
func BreakJulia() {}

// JuliaReplace is false here: no Julia cell runs.
func JuliaReplace() bool { return false }

// WarmJulia is an error here: there is no Julia runtime to prepare.
func WarmJulia() error { return errNoJulia }
