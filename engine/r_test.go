//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"testing"

	"github.com/go-notebook/notebook/analysis"
)

// An R cell is checked, not run, through go-notebook's registry alone: the
// cells that read it are blocked, with R's message, also when a Julia cell
// blocks others in the same notebook; the others run.
func TestRCells(t *testing.T) {
	nb := "```go\nn := 10\n```\n" +
		"```r\nxs <- seq_len(n)^2\ntotal <- sum(xs)\n```\n" +
		"```go\nm := total + 1\n```\n" +
		"```julia\nj = 1\n```\n" +
		"```ruby\nk = j * 2\n```\n" +
		"```go\nn * 2\n```\n"
	res := Run(nb)
	want := []string{"", analysis.RNotRun, analysis.RBlocked(1), analysis.JuliaNotRun, analysis.JuliaBlocked(3), ""}
	status := []string{"", StatusNotRun, StatusBlocked, StatusNotRun, StatusBlocked, ""}
	for i, w := range want {
		if res[i].Error != w || res[i].Skipped || res[i].Status != status[i] {
			t.Errorf("cell %d: %+v, want %q (%q)", i, res[i], w, status[i])
		}
	}
	if got := res[5].Display[0]["text/plain"]; got != "20" {
		t.Errorf("independent cell: %q", got)
	}
	// A syntax error, at its line and column, in R's words.
	res = Run("```r\ny <- 2\nx <- y y\n```\n```go\n1 + 1\n```\n")
	if res[0].Error != "line 2:8: syntax error: unexpected symbol" || res[0].Status != "" || res[1].Error != "" {
		t.Errorf("%+v", res)
	}
}
