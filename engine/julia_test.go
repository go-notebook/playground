//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"strings"
	"testing"

	"github.com/go-notebook/notebook/analysis"
)

// A Julia cell is checked, never run, whatever go-notebook's registry says
// about Julia (it runs there since go-notebook's jcells): it never reaches the
// Go evaluator, says so in the page's own words, and the other cells run. This
// builds and passes against go-notebook before and after Julia runs there.
func TestJuliaCells(t *testing.T) {
	nb := "```go\nn := 10\n```\n" +
		"```julia\nprintln(\"hi\")\nxs = [i^2 for i in 1:3]\n```\n" +
		"```julia\nys = xs .+ 1\n```\n" +
		"```go\nn * 2\n```\n"
	res := Run(nb)
	for _, i := range []int{1, 2} {
		r := res[i]
		if r.Error != JuliaNotRun || r.Status != StatusNotRun || r.Output != "" || len(r.Display) != 0 || r.Skipped {
			t.Errorf("Julia cell %d: %+v", i, r)
		}
	}
	if res[0].Error != "" || res[3].Error != "" || res[3].Display[0]["text/plain"] != "20" {
		t.Errorf("the Go cells: %+v %+v", res[0], res[3])
	}
	if !strings.Contains(JuliaNotRun, "not yet in the browser") || !strings.Contains(JuliaBlocked(2), "cell 2") {
		t.Error("the messages")
	}
	// A syntax error, at its line and column in the cell; it does not stop
	// the cells that do not read it.
	res = Run("```julia\ny = 2\nx = (1,\n```\n```go\n1 + 1\n```\n")
	if res[0].Error != "line 2:8: syntax error: Expected `)` or `,`" || res[0].Status != "" || res[1].Error != "" || res[1].Display[0]["text/plain"] != "2" {
		t.Errorf("%+v", res)
	}
	if !JuliaSupported {
		t.Error("JuliaSupported")
	}
}

// runsJulia is go-notebook's Julia as the release that runs it registers it:
// the same language, whose Run says Runs. go.mod may pin a release in which it
// does not, so the test makes it so.
type runsJulia struct{ analysis.Language }

func (runsJulia) Run() analysis.Run { return analysis.Run{Runs: true} }

// Whatever the registry says about Julia, a Julia cell never reaches the Go
// evaluator (it would fail there as Go source). With the language clause of
// CheckOnly deleted this fails on any go-notebook release.
func TestJuliaNeverReachesGoWhenRegisteredAsRunning(t *testing.T) {
	orig := analysis.Lookup(analysis.Julia)
	analysis.Unregister(analysis.Julia)
	analysis.Register(runsJulia{orig})
	t.Cleanup(func() {
		analysis.Unregister(analysis.Julia)
		analysis.Register(orig)
	})
	if !analysis.RunOf(analysis.Julia).Runs {
		t.Fatal("the stub does not run")
	}
	res := Run("```go\nn := 2\n```\n```julia\nprintln(\"hi\")\nxs = [i^2 for i in 1:3]\n```\n```go\nn * 21\n```\n")
	if res[1].Error != JuliaNotRun || res[1].Status != StatusNotRun || res[1].Output != "" || len(res[1].Display) != 0 {
		t.Errorf("the Julia cell: %+v", res[1])
	}
	if res[2].Error != "" || res[2].Skipped || res[2].Display[0]["text/plain"] != "42" {
		t.Errorf("the Go cell after it: %+v", res[2])
	}
	if !CheckOnly(analysis.Julia) {
		t.Error("CheckOnly(julia)")
	}
}

// Julia is check-only by language, not through the registry's Runs.
func TestCheckOnly(t *testing.T) {
	for lang, want := range map[string]bool{"julia": true, "c": true, "go": false, "ruby": false, "r": false} {
		if CheckOnly(lang) != want {
			t.Errorf("CheckOnly(%q) = %v", lang, !want)
		}
	}
	if blockedMessage("julia", 3) != JuliaBlocked(3) || notRunMessage("julia") != JuliaNotRun {
		t.Error("Julia's messages are the page's own")
	}
	if blockedMessage("c", 3) == "" || notRunMessage("c") == "" {
		t.Error("C's messages are go-notebook's")
	}
}

// A Go cell that fails still stops the run, Julia cells included after it.
func TestJuliaAfterFailure(t *testing.T) {
	res := Run("```go\npanic(\"x\")\n```\n```julia\nx = 1\n```\n")
	if !strings.Contains(res[0].Error, "x") || !res[1].Skipped {
		t.Errorf("%+v", res)
	}
}

// Fortran cells run in go-notebook (v0.17.0) and are registered as running, but
// this page links no Fortran runtime: they are checked, never sent to the Go
// evaluator, like Julia's.
func TestFortranIsCheckOnlyInThePage(t *testing.T) {
	res := Run("```go\nn := 2\n```\n```fortran\ninteger :: k = 3\nprint *, k\n```\n```go\nn * 21\n```\n")
	if res[1].Error != FortranNotRun || res[1].Status != StatusNotRun || res[1].Output != "" || len(res[1].Display) != 0 {
		t.Errorf("the Fortran cell: %+v", res[1])
	}
	if res[2].Error != "" || res[2].Display[0]["text/plain"] != "42" {
		t.Errorf("the Go cell after it: %+v", res[2])
	}
	if !CheckOnly("fortran") || notRunMessage("fortran") != FortranNotRun || !strings.Contains(blockedMessage("fortran", 4), "cell 4, a Fortran cell") {
		t.Error("Fortran's messages")
	}
}
