//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"fmt"
	"strings"

	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/notebook/format"
)

// Julia cells are parsed and checked as go-notebook does (analysis.
// AnalyzeJuliaCell, go-embedded-julia's parser), but not run: there is no
// Julia runtime in Go yet (go-embedded-julia's milestone M3). The parser is
// in the core that runs Ruby cells, not in the default one (noruby.go).

// JuliaSupported reports whether this build checks Julia cells.
const JuliaSupported = true

// juliaCell is a Julia cell's failure: its syntax error, at the cell's line
// and column, or analysis.JuliaNotRun.
func juliaCell(src string) string {
	c := analysis.AnalyzeJuliaCell(0, src)
	if len(c.Errors) == 0 {
		return analysis.JuliaNotRun
	}
	e := c.Errors[0]
	line := 1 + strings.Count(src[:e.Pos], "\n")
	col := e.Pos - strings.LastIndexByte(src[:e.Pos], '\n')
	return fmt.Sprintf("line %d:%d: %s", line, col, e.Msg)
}

// juliaBlocked maps each code cell that reads what a Julia cell defines,
// directly or through other cells, to that Julia cell, by go-notebook's
// dependency graph.
func juliaBlocked(cells []format.Cell) map[int]int {
	srcs := make([]analysis.Source, len(cells))
	for i, c := range cells {
		srcs[i] = analysis.Source{Lang: c.Lang, Source: c.Source}
	}
	g := analysis.AnalyzeSources(srcs)
	blocked := map[int]int{}
	// Until nothing changes: a cell may read one after it, and a notebook
	// with errors has no complete order to follow.
	for changed := true; changed; {
		changed = false
		for i := range cells {
			if _, done := blocked[i]; done || cells[i].Lang == analysis.Julia {
				continue
			}
			for _, d := range g.Deps[i] {
				j, ok := blocked[d]
				if cells[d].Lang == analysis.Julia {
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
