package engine

import (
	"github.com/go-notebook/notebook/analysis"
)

// CheckOnly reports whether the page parses and checks the cells of lang
// without running them: a language go-notebook registers as check-only (C).
// Julia is not one: its cells run in the page's third core (julia.go), and in a
// build without that runtime a Julia cell ends in jCell's error, never in the
// Go evaluator (RunUpTo's switch names the language; Julia source must never
// reach yaegi as Go). Fortran, R and Ruby run in the second core.
func CheckOnly(lang string) bool { return !analysis.RunOf(lang).Runs }

// notRunMessage is the failure of a well-formed cell of a check-only language.
func notRunMessage(lang string) string { return analysis.RunOf(lang).NotRun }

// blockedMessage is the failure of a cell that reads what cell j, of the
// check-only language lang, defines.
func blockedMessage(lang string, j int) string { return analysis.RunOf(lang).Blocked(j) }
