//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"fmt"
	"strings"

	"github.com/go-notebook/notebook/analysis"
	_ "github.com/go-notebook/notebook/analysis/parsers" // this file's build only: the default core leaves the parsers out
	"github.com/go-notebook/notebook/format"
)

// A cell of a language go-notebook registers as check-only (analysis.RunOf
// says it does not run: Julia, until go-embedded-julia has a runtime; C,
// until go-embedded-c generates code; R runs, in r.go) is parsed and checked as go-notebook does, but not run. The parsers
// are in the core that runs Ruby cells, which the page loads for any language
// but Go (noruby.go): this file imports analysis/parsers, and the default
// core, built without it, links no parser (#23). Both cores know every
// language's name, so an edit to Julia, R or C works in either.

// JuliaSupported reports whether this build checks Julia cells (and R and C
// cells: one parser set).
const JuliaSupported = true

// checker is go-notebook's analysis of a notebook's code cells, made once for
// the whole notebook: a C cell sees the declarations of the C cells before it
// (analysis.Sequential), so it cannot be checked alone.
type checker struct {
	cells []format.Cell
	g     *analysis.Graph
}

func newChecker(cells []format.Cell) *checker {
	srcs := make([]analysis.Source, len(cells))
	for i, c := range cells {
		srcs[i] = analysis.Source{Lang: c.Lang, Source: c.Source}
	}
	return &checker{cells: cells, g: analysis.AnalyzeSources(srcs)}
}

// cell is the message of code cell i, of a check-only language: its first
// syntax error, at the cell's line and column (checked is false, a failure),
// or the language's NotRun (checked is true: the cell is well formed and not
// run by design).
func (k *checker) cell(i int) (msg string, checked bool) {
	lang, src := k.cells[i].Lang, k.cells[i].Source
	errs := k.g.Cells[i].Errors
	if len(errs) == 0 {
		return analysis.RunOf(lang).NotRun, true
	}
	e := errs[0]
	pos := min(max(e.Pos, 0), len(src)) // a parser's offset is never trusted
	line := 1 + strings.Count(src[:pos], "\n")
	col := pos - strings.LastIndexByte(src[:pos], '\n')
	return fmt.Sprintf("line %d:%d: %s", line, col, e.Msg), false
}

// blocked maps each code cell that reads what a cell of a check-only language
// defines, directly or through other cells, to that cell, by go-notebook's
// dependency graph.
func (k *checker) blocked() map[int]int {
	cells, g := k.cells, k.g
	runs := func(i int) bool { return analysis.RunOf(cells[i].Lang).Runs }
	blocked := map[int]int{}
	// Until nothing changes: a cell may read one after it, and a notebook
	// with errors has no complete order to follow.
	for changed := true; changed; {
		changed = false
		for i := range cells {
			if _, done := blocked[i]; done || !runs(i) {
				continue
			}
			for _, d := range g.Deps[i] {
				j, ok := blocked[d]
				if !runs(d) {
					j, ok = d, true
				}
				if ok {
					blocked[i] = j
					changed = true
					break
				}
			}
		}
	}
	return blocked
}
