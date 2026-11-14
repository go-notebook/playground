//go:build !((js && ruby) || !(js || wasip1 || plan9))

package engine

import "github.com/go-notebook/notebook/format"

// RubySupported reports whether this build runs Ruby cells: the page's
// default WebAssembly core does not, nor a build where rbgo does not
// compile (see ruby.go).
const RubySupported = false

type rubySide struct{}

// rubyCell is what the default core says of a Ruby cell. The page never
// sends it one: it loads the Ruby core for a notebook that has any.
func (s *session) rubyCell(string) (string, []map[string]string, string) {
	return "", nil, "this build runs Go cells only (in the page, the Ruby core is loaded for a notebook with Ruby cells)"
}

// RSupported reports whether this build runs R cells: the default core does
// not, the second one does (r.go).
const RSupported = false

type rSide struct{}

// rCell is what the default core says of an R cell; the page never sends it
// one.
func (s *session) rCell(string) (string, []map[string]string, string) {
	return "", nil, "this build runs Go cells only (in the page, the second core is loaded for a notebook with R cells)"
}

// JuliaSupported reports whether this build checks Julia cells: the default
// core leaves go-embedded-julia's parser out (2 MB), and the page loads the
// core built with the tag ruby, which has it, for a notebook with a Julia
// cell.
const JuliaSupported = false

// checker is absent from the default core: it makes no analysis.
type checker struct{ cells []format.Cell }

func newChecker(cells []format.Cell) *checker { return &checker{cells} }

// cell is what the default core says of a cell of a check-only language
// (Julia, R, C); the page never sends it one.
func (k *checker) cell(i int) (string, bool) {
	lang := k.cells[i].Lang
	return "this build checks no " + lang + " cells (in the page, the core with the parsers is loaded for a notebook with " + lang + " cells)", false
}

// blocked finds nothing to block without the analysis.
func (k *checker) blocked() map[int]int { return nil }
