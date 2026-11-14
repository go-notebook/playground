//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/go-notebook/notebook/analysis"
	nbruntime "github.com/go-notebook/notebook/runtime"
)

// With the evaluator in the core, the page's R is go-notebook's R as the
// registry has it: it runs, it keeps its variables apart from the other
// languages' (analysis.Isolated) and it is highlighted with the r lexer. The
// page no longer wraps it (#29).
func TestRIsTheRegistrys(t *testing.T) {
	r := analysis.Lookup("r")
	if h, ok := r.(analysis.Highlighter); !ok || h.Lexer() != "r" {
		t.Errorf("R is not highlighted with the r lexer: %T", r)
	}
	if i, ok := r.(analysis.Isolated); !ok || !i.Isolated() {
		t.Errorf("R is not isolated: %T", r)
	}
	if !analysis.RunOf("r").Runs || !RSupported {
		t.Error("the page does not run R cells")
	}
}

// An R cell runs: its value is printed as R prints it, what it cat()s and
// message()s is there, the warnings come in R's form, the variables persist
// from one R cell to the next, and a failure is R's own "Error in call :
// message", with the output before it.
func TestRCellRuns(t *testing.T) {
	nb := "```r\nxs <- c(1.5, 2.5, 4)\nf <- function(x) { warning('careful'); x }\n```\n" +
		"```r\nsummary_total <- sum(xs)\nsummary_total\ncat('done\\n')\nmessage('said')\nf(2)\n```\n" +
		"```r\ncat('before\\n')\ng <- function(a) stop('boom ', a)\ng(xs[1])\n```\n" +
		"```r\nxs\n```\n"
	res := Run(nb)
	want := []struct{ out, err string }{
		{"", ""},
		{"[1] 8\ndone\n[1] 2\nsaid\nWarning message:\nIn f(2) : careful\n", ""},
		{"before\n", "Error in g(xs[1]) : boom 1.5"},
	}
	for i, w := range want {
		if res[i].Output != w.out || res[i].Error != w.err || res[i].Status != "" {
			t.Errorf("cell %d: %+v, want output %q, error %q", i, res[i], w.out, w.err)
		}
	}
	// The failure stops the run, as in any language: the last cell is skipped.
	if !res[3].Skipped {
		t.Errorf("a cell after a failed one ran: %+v", res[3])
	}
	// The session kept xs, and the failed cell's own output was kept too.
	res = Run("```r\nxs <- 1:3\n```\n```r\nxs * 2\n```\n")
	if res[1].Output != "[1] 2 4 6\n" || res[1].Error != "" {
		t.Errorf("variables do not persist: %+v", res[1])
	}
	// The error the 5-r example quotes is what the page shows.
	res = Run("```r\nsqrt(\"a\")\n```\n")
	if res[0].Error != `Error in sqrt("a") : non-numeric argument to mathematical function` {
		t.Errorf("the 5-r example quotes a message the page does not give: %q", res[0].Error)
	}
	// A syntax error is a failure, in R's words, and says where.
	res = Run("```r\ny <- 2\nx <- y y\n```\n")
	if !strings.HasPrefix(res[0].Error, "syntax error: unexpected symbol") || !strings.Contains(res[0].Error, "(line 2)") || res[0].Status != "" {
		t.Errorf("%+v", res[0])
	}
}

// R's variables stay R's, as in go-notebook: a Go cell cannot read what an R
// cell assigns, and an R cell cannot read what a Go cell defines. Neither is
// blocked; each says what it does not find. The Go cell that reads nothing
// from R runs.
func TestRCellsAreIsolated(t *testing.T) {
	nb := "```go\nn := 10\n```\n" +
		"```r\ntotal <- 5\n```\n" +
		"```go\nm := total + 1\n```\n"
	res := Run(nb)
	if res[2].Error != "line 1: undefined: total" || res[2].Status != "" {
		t.Errorf("a Go cell read an R name: %+v", res[2])
	}
	res = Run("```go\nn := 10\n```\n```r\nn\n```\n")
	if !strings.Contains(res[1].Error, "'n' not found") || res[1].Status != "" {
		t.Errorf("an R cell read a Go name: %+v", res[1])
	}
	// A Go cell and an R cell of the same name keep two variables.
	res = Run("```go\nv := 1\n```\n```r\nv <- 100\nv\n```\n```go\nv\n```\n")
	if res[1].Output != "[1] 100\n" || res[2].Display[0]["text/plain"] != "1" {
		t.Errorf("%+v", res)
	}
	// Ruby and R, two of the second core's languages, are as apart.
	res = Run("```ruby\nr = 7\n```\n```r\nexists('r')\n```\n")
	if res[1].Output != "[1] FALSE\n" {
		t.Errorf("an R cell saw a Ruby local: %+v", res[1])
	}
}

// withRLimits runs f with other limits on the page's R.
func withRLimits(t *testing.T, l nbruntime.RLimits, f func()) {
	t.Helper()
	old := RLimits
	RLimits = l
	defer func() { RLimits = old }()
	f()
}

// A runaway cell is stopped, with a message that says why, and the cells after
// it are skipped; the next run is a new session that works. Each of R's bounds
// ends one kind of runaway.
func TestRLimitsStopARunawayCell(t *testing.T) {
	for _, tc := range []struct {
		name string
		l    nbruntime.RLimits
		src  string
		want string
	}{
		{"steps", nbruntime.RLimits{MaxSteps: 100_000, Timeout: time.Minute}, "while(TRUE) 0", "evaluation step limit reached"},
		{"allocation", nbruntime.RLimits{MaxAlloc: 32 << 20, Timeout: time.Minute}, "x <- NULL\nfor (i in 1:100000) x <- c(x, i)", "memory limit exceeded: more than 33554432 bytes of vectors allocated"},
		{"time", nbruntime.RLimits{MaxSteps: 1 << 40, Timeout: 100 * time.Millisecond}, "repeat tryCatch(0, error = function(e) NULL)", "ran longer than 100ms, the limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withRLimits(t, tc.l, func() {
				start := time.Now()
				res := Run("```r\n" + tc.src + "\n```\n```r\n1\n```\n")
				if !strings.Contains(res[0].Error, tc.want) || res[0].Status != "" || !res[1].Skipped {
					t.Errorf("%+v, want an error with %q and the next cell skipped", res, tc.want)
				}
				if d := time.Since(start); d > 20*time.Second {
					t.Errorf("stopped after %v", d)
				}
			})
			if res := Run("```r\n1 + 1\n```\n"); res[0].Output != "[1] 2\n" || res[0].Error != "" {
				t.Errorf("the next run: %+v", res[0])
			}
		})
	}
}

// The limits the page ships with are bounded, as go-notebook's defaults are:
// none is zero or unbounded, and the time is under the page's watchdog.
func TestPageRLimits(t *testing.T) {
	if RLimits.MaxAlloc <= 0 || RLimits.MaxAlloc > 512<<20 || RLimits.MaxSteps <= 0 || RLimits.MaxSteps > nbruntime.DefaultRLimits.MaxSteps {
		t.Errorf("limits %+v are not bounded as the default's", RLimits)
	}
	// The nesting bound is the page's, under the browser's call stack, and a
	// recursion the bound allows still runs.
	if RDepth <= 0 || RDepth > 600 {
		t.Errorf("RDepth %d: the browser's call stack overflows well before go-embedded-r's 5000", RDepth)
	}
	res := Run("```r\nf <- function(n) if (n == 0) 0 else 1 + f(n - 1)\nf(150)\n```\n```r\ng <- function(n) sapply(1, function(i) g(n + 1))\ng(1)\n```\n")
	if res[0].Output != "[1] 150\n" || res[0].Error != "" || !strings.HasPrefix(res[1].Error, "Error: evaluation nested too deeply") {
		t.Errorf("recursion: %+v", res)
	}
	if RLimits.Timeout <= 0 || RLimits.Timeout >= 30*time.Second {
		t.Errorf("timeout %v is not under the page's 30 s watchdog", RLimits.Timeout)
	}
}

// No file or variable of the host is visible: the files are a private in-memory
// tree, and the environment is empty. (go-notebook's rcells has the same test
// of its own, TestFilesAreSealedFromTheHost; this one is over the page's cell.)
func TestRCellSeesNothingOfTheHost(t *testing.T) {
	t.Setenv("PLAYGROUND_SECRET", "hunter2")
	res := Run("```r\nSys.getenv('PLAYGROUND_SECRET')\nSys.getenv('HOME')\nlength(Sys.getenv())\n```\n" +
		"```r\nfile.exists('/etc/passwd')\n```\n" +
		"```r\nreadLines('/etc/passwd')\n```\n")
	if res[0].Error != "" || strings.Contains(res[0].Output, "hunter2") || res[0].Output != "[1] \"\"\n[1] \"\"\n[1] 0\n" {
		t.Errorf("the environment is visible: %+v", res[0])
	}
	if res[1].Output != "[1] FALSE\n" {
		t.Errorf("a host file exists for R: %+v", res[1])
	}
	if res[2].Error == "" || strings.Contains(res[2].Output, "root:") {
		t.Errorf("a host file was read: %+v", res[2])
	}
}
