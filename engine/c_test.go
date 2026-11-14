//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/notebook/format"
)

// A C cell is checked, not run, through go-notebook's registry alone: a later
// C cell sees the typedefs and macros of the earlier ones, the cells that read
// what it defines are blocked, with C's message, also next to Julia and R
// cells in the same notebook; the others run.
func TestCCells(t *testing.T) {
	c, j := analysis.RunOf("c"), analysis.RunOf("julia")
	nb := "```go\nn := 10\n```\n" +
		"```c\ntypedef int myint;\n#define N 10\n```\n" +
		"```c\nmyint total = N * N;\n```\n" +
		"```go\nm := total + 1\n```\n" +
		"```julia\nj = 1\n```\n" +
		"```ruby\nk = j * 2\n```\n" +
		"```r\nq <- 1\n```\n" +
		"```go\nn * 2\n```\n"
	res := Run(nb)
	want := []string{"", c.NotRun, c.NotRun, c.Blocked(2), j.NotRun, j.Blocked(4), "", ""}
	status := []string{"", StatusNotRun, StatusNotRun, StatusBlocked, StatusNotRun, StatusBlocked, "", ""} // the R cell runs
	for i, w := range want {
		if res[i].Error != w || res[i].Skipped || res[i].Status != status[i] {
			t.Errorf("cell %d: %+v, want %q (%q)", i, res[i], w, status[i])
		}
	}
	if got := res[7].Display[0]["text/plain"]; got != "20" {
		t.Errorf("independent cell: %q", got)
	}
}

// A C cell is checked in its notebook, not alone: the same cell that is fine
// after the typedef is an error without it, at its line and column, in cc/v4's
// words, and the Go cell after it still runs.
func TestCCellsAreSequential(t *testing.T) {
	res := Run("```c\nmyint v = 1;\n```\n```go\n1 + 1\n```\n")
	if res[0].Error != "line 1:7: unexpected identifier, expected external declaration" || res[0].Status != "" || res[1].Error != "" {
		t.Errorf("alone: %+v", res)
	}
	res = Run("```c\ntypedef int myint;\n```\n```c\nmyint v = 1;\n```\n")
	if res[1].Status != StatusNotRun || res[1].Error != analysis.RunOf("c").NotRun {
		t.Errorf("after the typedef: %+v", res[1])
	}
	// A syntax error, at its line and column of the cell.
	res = Run("```c\nint y = 2;\nint x = y y;\n```\n```go\n1 + 1\n```\n")
	if res[0].Error != "line 2:11: unexpected identifier, expected ';'" || res[0].Status != "" || res[1].Error != "" {
		t.Errorf("%+v", res)
	}
	// A name that is not declared is an error too, not a not-run cell.
	res = Run("```c\nint main(void) { return z; }\n```\n")
	if res[0].Status != "" || !strings.Contains(res[0].Error, "undefined: z") {
		t.Errorf("%+v", res[0])
	}
}

// Cell source is untrusted: a macro whose expansion doubles 40 times, and a
// cell over go-notebook's size bound, are refused, as errors, in well under a
// minute even under emulation, and the cell after them is not affected.
func TestCCellsAreBounded(t *testing.T) {
	var bomb strings.Builder
	bomb.WriteString("#define D0(a) (a)\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&bomb, "#define D%d(a) (D%d(a)+D%d(a))\n", i, i-1, i-1)
	}
	bomb.WriteString("int v = D40(1);\n")
	huge := strings.Repeat("int x;\n", (1<<20)/7+2)
	for name, src := range map[string]string{"macro bomb": bomb.String(), "oversized": huge} {
		start := time.Now()
		res := Run("```c\n" + src + "```\n```go\n1 + 1\n```\n")
		// 0.2 s natively; 11 s under qemu (riscv64, s390x). Unbounded expansion
		// would take hours, so this only has to tell a bound from a hang.
		if took := time.Since(start); took > 90*time.Second {
			t.Errorf("%s: took %v", name, took)
		}
		if res[0].Error == "" || res[0].Status != "" || res[1].Error != "" {
			t.Errorf("%s: %q %q, %+v", name, res[0].Error, res[0].Status, res[1])
		}
	}
}

// A parser's error offset is never trusted: a position outside the cell is
// clamped, not a panic.
func TestCheckerClampsAnErrorOffset(t *testing.T) {
	for _, pos := range []int{-5, 0, 3, 99} {
		k := &checker{
			cells: []format.Cell{{Lang: "c", Source: "ab\ncd"}},
			g:     &analysis.Graph{Cells: []*analysis.Cell{{Errors: []analysis.Error{{Pos: pos, Msg: "m"}}}}},
		}
		msg, checked := k.cell(0)
		if checked || !strings.HasPrefix(msg, "line ") || !strings.HasSuffix(msg, ": m") {
			t.Errorf("pos %d: %q %v", pos, msg, checked)
		}
	}
}
