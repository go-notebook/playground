package engine

import (
	"fmt"

	"github.com/go-notebook/notebook/analysis"
)

// pageR is go-notebook's R language as this page has to see it. Since
// go-notebook v0.10.0 the registry says R cells run, in a worker process with
// R variables kept apart from the other languages (analysis.Isolated). A
// browser starts no process, so here an R cell is still only parsed and
// checked, and a cell that reads what it defines is blocked, as before that
// version (playground#24). Nothing else of the registry's R changes: its
// parser, its names and its highlighting lexer are the registry's.
type pageR struct{ analysis.Language }

func (pageR) Run() analysis.Run {
	return analysis.Run{
		NotRun: "R cells are parsed and checked, but not run yet: execution arrives with go-embedded-r's evaluator (milestones M2 and M3)",
		Blocked: func(c int) string {
			return fmt.Sprintf("blocked: it depends on cell %d, an R cell, and R cells are not run yet", c)
		},
	}
}

func (p pageR) AnalyzeCell(index int, src string) *analysis.Cell {
	return p.Language.(analysis.CellAnalyzer).AnalyzeCell(index, src)
}

func (pageR) Lexer() string { return "r" }

// Crosses: a cell of another language that reads what an R cell assigns
// depends on it (the registry's R keeps its variables out of reach).
func (pageR) Crosses(_, _ *analysis.Cell, _ string) bool { return true }

func init() {
	if r := analysis.Lookup("r"); r != nil {
		if _, ok := r.(analysis.CellAnalyzer); ok {
			analysis.Unregister("r")
			analysis.Register(pageR{r})
		}
	}
}
