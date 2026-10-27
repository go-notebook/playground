//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"strings"
	"testing"

	"github.com/go-notebook/notebook/analysis"
)

// A Julia cell is checked, not run; the cells that read it are blocked, in
// any language and through any chain, even one that reads a cell after it;
// the others run.
func TestJuliaCells(t *testing.T) {
	nb := "```go\nn := 10\n```\n" +
		"```julia\nxs = [i^2 for i in 1:n]\ntotal = sum(xs)\n```\n" +
		"```go\nm := total + 1\n```\n" +
		"```ruby\nk = m * 2\n```\n" +
		"```go\nlater + 1\n```\n" +
		"```julia\nlater = 2\n```\n" +
		"```go\nn * 2\n```\n"
	res := Run(nb)
	want := []string{
		"",
		analysis.JuliaNotRun,
		analysis.JuliaBlocked(1),
		analysis.JuliaBlocked(1),
		analysis.JuliaBlocked(5),
		analysis.JuliaNotRun,
		"",
	}
	for i, w := range want {
		if res[i].Error != w || res[i].Skipped {
			t.Errorf("cell %d: %+v, want %q", i, res[i], w)
		}
	}
	if got := res[6].Display[0]["text/plain"]; got != "20" {
		t.Errorf("independent cell: %q", got)
	}
	// A syntax error, at its line and column in the cell; it does not stop
	// the cells that do not read it.
	res = Run("```julia\ny = 2\nx = (1,\n```\n```go\n1 + 1\n```\n")
	if res[0].Error != "line 2:8: syntax error: Expected `)` or `,`" || res[1].Error != "" || res[1].Display[0]["text/plain"] != "2" {
		t.Errorf("%+v", res)
	}
	if !JuliaSupported {
		t.Error("JuliaSupported")
	}
}

// A Go cell that fails still stops the run, Julia cells included after it.
func TestJuliaAfterFailure(t *testing.T) {
	res := Run("```go\npanic(\"x\")\n```\n```julia\nx = 1\n```\n")
	if !strings.Contains(res[0].Error, "x") || !res[1].Skipped {
		t.Errorf("%+v", res)
	}
}
