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

// JuliaSupported reports whether this build checks Julia cells: the default
// core leaves go-embedded-julia's parser out (2 MB), and the page loads the
// core built with the tag ruby, which has it, for a notebook with a Julia
// cell.
const JuliaSupported = false

// juliaCell is what the default core says of a Julia cell; the page never
// sends it one.
func juliaCell(string) (string, bool) {
	return "this build checks no Julia cells (in the page, the core with the Julia parser is loaded for a notebook with Julia cells)", false
}

// juliaBlocked finds nothing to block without the Julia analysis.
func juliaBlocked([]format.Cell) map[int]int { return nil }
