package engine

import (
	"fmt"

	"github.com/go-notebook/notebook/analysis"
)

// JuliaNotRun is the failure of every Julia cell in the page. Julia cells run in
// go-notebook (go-embedded-julia's interpreter, in the worker of `notebook
// run`), but their kernel boots from a 100 MB image fetched at run time, which a
// page cannot do, so here they are parsed and checked only. The text is this
// package's, not go-notebook's: analysis.JuliaNotRun is gone from go-notebook
// once Julia runs, and this must build against the release before and the one
// after.
const JuliaNotRun = "Julia cells run in go-notebook, not yet in the browser"

// JuliaBlocked is the failure of a cell that reads what Julia cell julia
// defines, where go-notebook's graph still has such an edge (the releases in
// which Julia variables cross).
func JuliaBlocked(julia int) string {
	return fmt.Sprintf("blocked: it depends on cell %d, a Julia cell, and Julia cells do not run in the browser yet", julia)
}

// CheckOnly reports whether the page parses and checks the cells of lang
// without running them: Julia, whatever go-notebook says about it (RunOf(julia)
// answers Runs once go-notebook runs it), and any language go-notebook
// registers as check-only (C). It is the only test of that: a cell that is not
// check-only goes to an evaluator, and Julia source must never reach yaegi as
// Go.
func CheckOnly(lang string) bool {
	return lang == analysis.Julia || !analysis.RunOf(lang).Runs
}

// notRunMessage is the failure of a well-formed cell of a check-only language.
func notRunMessage(lang string) string {
	if lang == analysis.Julia {
		return JuliaNotRun
	}
	return analysis.RunOf(lang).NotRun
}

// blockedMessage is the failure of a cell that reads what cell j, of the
// check-only language lang, defines.
func blockedMessage(lang string, j int) string {
	if lang == analysis.Julia {
		return JuliaBlocked(j)
	}
	return analysis.RunOf(lang).Blocked(j)
}
