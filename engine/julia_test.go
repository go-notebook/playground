//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-embedded-julia/julia/interp"
	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/notebook/jcells"
)

// The tests that run Julia need the pinned image (25 MB): it is fetched by its
// digest on the first one, and kept in the user's cache directory. Kernels
// are booted once per test binary and shared by the sessions of the tests that
// follow (a run closes its own, so each loads: a second or so natively).

// Julia cells run in the page's third core: a notebook of Go and Julia cells
// runs in order, the Julia kernel keeps its variables from cell to cell, what
// a cell prints is its output and the value of its last expression is shown as
// the REPL shows it, and the Go cells are unaffected.
func TestJuliaCells(t *testing.T) {
	nb := "```go\nn := 10\n```\n" +
		"```julia\nprintln(\"hi\")\nxs = [i^2 for i in 1:3]\n```\n" +
		"```julia\nys = xs .+ 1\n```\n" +
		"```go\nn * 2\n```\n"
	res := Run(nb)
	if res[0].Error != "" || res[3].Error != "" || res[3].Display[0]["text/plain"] != "20" {
		t.Errorf("the Go cells: %+v %+v", res[0], res[3])
	}
	if r := res[1]; r.Error != "" || r.Status != "" || r.Output != "hi\n" || len(r.Display) != 1 || r.Display[0]["text/plain"] != "3-element Vector{Int64}:\n 1\n 4\n 9" {
		t.Errorf("Julia cell 1: %+v", r)
	}
	if r := res[2]; r.Error != "" || r.Output != "" || len(r.Display) != 1 || r.Display[0]["text/plain"] != "3-element Vector{Int64}:\n 2\n 5\n10" {
		t.Errorf("Julia cell 2 reads cell 1's variable: %+v", r)
	}
	if !JuliaSupported || !JuliaRuns {
		t.Error("JuliaSupported, JuliaRuns")
	}
}

// The REPL's soft scope: a loop at the top of a cell assigns to the global that
// exists (go-embedded-julia#54), so the sum is 55, not an error.
func TestJuliaSoftScope(t *testing.T) {
	res := Run("```julia\ns = 0\nfor i in 1:10\n    s += i\nend\ns\n```\n")
	if res[0].Error != "" || len(res[0].Display) != 1 || res[0].Display[0]["text/plain"] != "55" {
		t.Errorf("%+v", res[0])
	}
}

// An exception fails the cell with Julia's own text, a syntax error is the
// checker's, at its line and column, and neither stops the cells that do not
// read it... a failed cell does stop the run: the cells after it are skipped.
func TestJuliaErrors(t *testing.T) {
	res := Run("```julia\nx = 1\nsqrt(-1)\n```\n```go\n1 + 1\n```\n")
	if want := "ERROR: DomainError with -1.0:\nsqrt was called with a negative real argument but will only return a complex result if called with a complex argument. Try sqrt(Complex(x))."; res[0].Error != want || res[0].Status != "" {
		t.Errorf("an exception: %q", res[0].Error)
	}
	if !res[1].Skipped {
		t.Errorf("after the failure: %+v", res[1])
	}
	res = Run("```julia\ny = 2\nx = (1,\n```\n```go\n1 + 1\n```\n")
	if res[0].Error != "line 2:8: syntax error: Expected `)` or `,`" || res[0].Status != "" || res[0].Output != "" || !res[1].Skipped {
		t.Errorf("a syntax error: %+v", res)
	}
	res = Run("```go\npanic(\"x\")\n```\n```julia\nx = 1\n```\n")
	if !strings.Contains(res[0].Error, "x") || !res[1].Skipped {
		t.Errorf("after a Go failure: %+v", res)
	}
}

// A run is a session of its own: its Main is new, so a struct a second run
// defines again is not a redefinition, and a variable of the first run is not
// there.
func TestJuliaRunsAreFresh(t *testing.T) {
	nb := "```julia\nstruct P\n    x::Int\nend\nz = P(3)\nz.x\n```\n"
	for i := range 2 {
		res := Run(nb)
		if res[0].Error != "" || res[0].Display[0]["text/plain"] != "3" {
			t.Errorf("run %d: %+v", i, res[0])
		}
	}
	res := Run("```julia\nz\n```\n")
	if !strings.Contains(res[0].Error, "UndefVarError") {
		t.Errorf("a variable of an earlier run: %+v", res[0])
	}
}

// Julia variables stay in Julia in the page, in both directions, and the cell
// that reads one says so, besides the failure itself.
func TestJuliaStaysIsolated(t *testing.T) {
	res := Run("```julia\ntotal = 55\n```\n```go\ntotal + 1\n```\n")
	if !strings.Contains(res[1].Error, "undefined: total") || !strings.Contains(res[1].Error, "cell 0, a Julia cell") || !strings.Contains(res[1].Error, "do not cross") {
		t.Errorf("a Go cell reading Julia's: %q", res[1].Error)
	}
	res = Run("```go\nn := 10\n```\n```julia\nn + 1\n```\n")
	if !strings.Contains(res[1].Error, "UndefVarError") || !strings.Contains(res[1].Error, "cell 0, a go cell") {
		t.Errorf("a Julia cell reading Go's: %q", res[1].Error)
	}
}

// The limits end a runaway cell with the kernel's own bound named and keep the
// kernel: the next cell of the notebook (here, skipped; in a new run, run)
// works. The output cap drops the rest and says so.
func TestJuliaLimits(t *testing.T) {
	saved := JuliaLimits
	t.Cleanup(func() { JuliaLimits = saved })

	JuliaLimits.Timeout = time.Second
	JuliaLimits.MaxSteps = -1
	res := Run("```julia\nwhile true\nend\n```\n")
	if want := "stopped: the Julia cell ran longer than 1s, the limit"; res[0].Error != want {
		t.Errorf("a spinning cell: %q", res[0].Error)
	}
	JuliaLimits = saved

	JuliaLimits.MaxSteps = 1000
	res = Run("```julia\ns = 0\nfor i in 1:1_000_000\n    s += i\nend\n```\n")
	if want := "stopped: the Julia cell executed more than 1000 statements, the limit"; res[0].Error != want {
		t.Errorf("the step limit: %q", res[0].Error)
	}
	JuliaLimits = saved

	JuliaLimits.MaxAlloc = 1 << 20 // 64k elements
	res = Run("```julia\nzeros(1_000_000)\n```\n")
	if !strings.HasPrefix(res[0].Error, "stopped: the Julia cell went over its memory limit: ") {
		t.Errorf("the memory limit: %q", res[0].Error)
	}
	JuliaLimits = saved

	JuliaLimits.MaxOutput = 100
	res = Run("```julia\nfor i in 1:100\n    println(\"line \", i)\nend\n```\n")
	if !strings.Contains(res[0].Output, "[output truncated: more than 100 bytes]") || len(res[0].Output) > 200 {
		t.Errorf("the output cap: %q", res[0].Output)
	}
	JuliaLimits = saved

	if res := Run("```julia\n1 + 1\n```\n"); res[0].Error != "" || res[0].Display[0]["text/plain"] != "2" {
		t.Errorf("after the limits: %+v", res[0])
	}
}

// The page's limits are R's where R has them: 512 MiB, 20 million steps, 10 s.
func TestJuliaLimitsAreThePages(t *testing.T) {
	if JuliaLimits.MaxAlloc != RLimits.MaxAlloc || JuliaLimits.MaxSteps != int64(RLimits.MaxSteps) || JuliaLimits.Timeout != RLimits.Timeout {
		t.Errorf("%+v against R's %+v", JuliaLimits, RLimits)
	}
	if o := juliaOptions(); o.MaxAlloc != 32<<20 || o.Timeout != 10*time.Second || o.MaxSteps != 20_000_000 || o.MaxOutput != 16<<20 {
		t.Errorf("%+v", o)
	}
}

// A Julia cell never reaches the Go evaluator, whatever happens to the kernel:
// when it cannot start, the cell fails with that, and the Go cell after it is
// skipped; nothing of Julia's source is Go's to parse (it would not: `xs = [i^2
// for i in 1:3]` is not Go). With the Julia case of RunUpTo's switch deleted
// this fails: the cell would fall to the default, and yaegi would say what it
// says of Julia source.
func TestJuliaNeverReachesGo(t *testing.T) {
	saved := loader
	t.Cleanup(func() { loader = saved })
	loader = func() (*interp.Kernel, error) { return nil, errors.New("no network") }

	res := Run("```go\nn := 2\n```\n```julia\nprintln(\"hi\")\nxs = [i^2 for i in 1:3]\n```\n```go\nn * 21\n```\n")
	if res[1].Error != "the Julia kernel could not start: no network" || res[1].Output != "" || len(res[1].Display) != 0 || res[1].Status != "" {
		t.Errorf("the Julia cell: %+v", res[1])
	}
	if !res[2].Skipped {
		t.Errorf("the Go cell after it: %+v", res[2])
	}
	if CheckOnly(analysis.Julia) {
		t.Error("CheckOnly(julia): Julia is run, not check-only")
	}
	// A registry that says Julia does not run changes nothing: Julia is named
	// in the switch, not asked of the registry.
	orig := analysis.Lookup(analysis.Julia)
	analysis.Unregister(analysis.Julia)
	analysis.Register(notRunningJulia{orig})
	t.Cleanup(func() {
		analysis.Unregister(analysis.Julia)
		analysis.Register(orig)
	})
	res = Run("```go\nn := 2\n```\n```julia\nxs = [i^2 for i in 1:3]\n```\n")
	if res[1].Error != "the Julia kernel could not start: no network" && res[1].Status != StatusNotRun {
		t.Errorf("with a registry that does not run Julia: %+v", res[1])
	}
	if strings.Contains(res[1].Error, "expected") || strings.Contains(res[1].Error, "undefined") {
		t.Errorf("the Julia cell reached the Go evaluator: %+v", res[1])
	}
}

// notRunningJulia is Julia as a go-notebook that does not run it registers it.
type notRunningJulia struct{ analysis.Language }

func (notRunningJulia) Run() analysis.Run { return analysis.Run{NotRun: "not run"} }

// Only C is check-only: Julia, Ruby, R and Fortran run (in the cores that have
// their runtimes).
func TestCheckOnly(t *testing.T) {
	for lang, want := range map[string]bool{"julia": false, "c": true, "go": false, "ruby": false, "r": false, "fortran": false} {
		if CheckOnly(lang) != want {
			t.Errorf("CheckOnly(%q) = %v", lang, !want)
		}
	}
	if blockedMessage("c", 3) == "" || notRunMessage("c") == "" {
		t.Error("C's messages are go-notebook's")
	}
}

// The pinned image is the one go-notebook's jcells pins: a release moves both
// or neither.
func TestImageIsTheOneGoNotebookPins(t *testing.T) {
	if JuliaImageVersion != jcells.Version || JuliaImageDigest != jcells.Digest {
		t.Errorf("the page pins %s %s, go-notebook %s %s", JuliaImageVersion, JuliaImageDigest, jcells.Version, jcells.Digest)
	}
	img, err := JuliaImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(img) != JuliaImageSize {
		t.Errorf("the image is %d bytes, JuliaImageSize says %d", len(img), JuliaImageSize)
	}
}

// A kernel booted ahead (WarmJulia, as the page does before it says the core is
// ready) is the one the next run takes, once.
func TestJuliaWarmKernel(t *testing.T) {
	saved := loader
	t.Cleanup(func() { loader = saved })
	n := 0
	loader = func() (*interp.Kernel, error) { n++; return saved() }
	if err := WarmJulia(); err != nil {
		t.Fatal(err)
	}
	if err := WarmJulia(); err != nil || n != 1 {
		t.Fatalf("a second warm-up loaded again: %d, %v", n, err)
	}
	if res := Run("```julia\n1 + 1\n```\n"); res[0].Error != "" || n != 1 {
		t.Errorf("the run did not take the warm kernel: %d loads, %+v", n, res[0])
	}
	if res := Run("```julia\n2 + 2\n```\n"); res[0].Error != "" || n != 2 {
		t.Errorf("the next run loads its own: %d loads, %+v", n, res[0])
	}
}
