//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"fmt"
	"strings"

	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/notebook/format"
)

// A cell of a language go-notebook registers as check-only (analysis.RunOf
// says it does not run: Julia, until go-embedded-julia has a runtime; R,
// until go-embedded-r has an evaluator) is parsed and checked as go-notebook
// does, but not run. The parsers are in the core that runs Ruby cells, which
// the page loads for any language but Go (noruby.go).

// JuliaSupported reports whether this build checks Julia cells.
const JuliaSupported = true

// checkedCell is the message of a cell of check-only language lang: its
// syntax error, at the cell's line and column (checked is false, a failure),
// or the language's NotRun (checked is true: the cell is well formed and not
// run by design).
func checkedCell(lang, src string) (msg string, checked bool) {
	c := analysis.AnalyzeSources([]analysis.Source{{Lang: lang, Source: src}}).Cells[0]
	if len(c.Errors) == 0 {
		return analysis.RunOf(lang).NotRun, true
	}
	e := c.Errors[0]
	line := 1 + strings.Count(src[:e.Pos], "\n")
	col := e.Pos - strings.LastIndexByte(src[:e.Pos], '\n')
	return fmt.Sprintf("line %d:%d: %s", line, col, e.Msg), false
}

// checkedBlocked maps each code cell that reads what a cell of a check-only
// language defines, directly or through other cells, to that cell, by
// go-notebook's dependency graph.
func checkedBlocked(cells []format.Cell) map[int]int {
	srcs := make([]analysis.Source, len(cells))
	for i, c := range cells {
		srcs[i] = analysis.Source{Lang: c.Lang, Source: c.Source}
	}
	g := analysis.AnalyzeSources(srcs)
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
