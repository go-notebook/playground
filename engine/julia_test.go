//go:build !(js || wasip1 || plan9)

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-embedded-julia/julia/interp"
	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/notebook/jcells"
)

// pageLimits are the page's. The tests also run under -race, ten times slower,
// where a cell that takes a second runs over the page's 10 s: they run with a
// long timeout, and the tests of the limits set what they test.
var pageLimits = JuliaLimits

// bootsKernel skips a test that boots a kernel under -short (the emulated and
// 32-bit runs of CI): it fetches a 25 MB image and takes seconds a cell.
func bootsKernel(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("boots a Julia kernel from a 25 MB image")
	}
}

func init() { JuliaLimits.Timeout = 10 * time.Minute }

// The tests that run Julia need the pinned image (25 MB): it is fetched by its
// digest on the first one, and kept in the user's cache directory. Kernels
// are booted once per test binary and shared by the sessions of the tests that
// follow (a run closes its own, so each loads: a second or so natively).

// Julia cells run in the page's third core: a notebook of Go and Julia cells
// runs in order, the Julia kernel keeps its variables from cell to cell, what
// a cell prints is its output and the value of its last expression is shown as
// the REPL shows it, and the Go cells are unaffected.
func TestJuliaCells(t *testing.T) {
	bootsKernel(t)
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
	if r := res[2]; r.Error != "" || r.Output != "" || len(r.Display) != 1 || r.Display[0]["text/plain"] != "3-element Vector{Int64}:\n  2\n  5\n 10" {
		t.Errorf("Julia cell 2 reads cell 1's variable: %+v", r)
	}
	if !JuliaSupported || !JuliaRuns {
		t.Error("JuliaSupported, JuliaRuns")
	}
}

// The REPL's soft scope: a loop at the top of a cell assigns to the global that
// exists (go-embedded-julia#54), so the sum is 55, not an error.
func TestJuliaSoftScope(t *testing.T) {
	bootsKernel(t)
	res := Run("```julia\ns = 0\nfor i in 1:10\n    s += i\nend\ns\n```\n")
	if res[0].Error != "" || len(res[0].Display) != 1 || res[0].Display[0]["text/plain"] != "55" {
		t.Errorf("%+v", res[0])
	}
}

// An exception fails the cell with Julia's own text, a syntax error is the
// checker's, at its line and column, and neither stops the cells that do not
// read it... a failed cell does stop the run: the cells after it are skipped.
func TestJuliaErrors(t *testing.T) {
	bootsKernel(t)
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
	bootsKernel(t)
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
	bootsKernel(t)
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
	bootsKernel(t)
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

// The page's limits are R's (512 MiB, 20 million steps) but for the time: 20 s,
// under the page's 30 s watchdog (see JuliaLimits).
func TestJuliaLimitsAreThePages(t *testing.T) {
	saved := JuliaLimits
	t.Cleanup(func() { JuliaLimits = saved })
	JuliaLimits = pageLimits
	if JuliaLimits.MaxAlloc != RLimits.MaxAlloc || JuliaLimits.MaxSteps != int64(RLimits.MaxSteps) || JuliaLimits.Timeout != 2*RLimits.Timeout {
		t.Errorf("%+v against R's %+v", JuliaLimits, RLimits)
	}
	if o := juliaOptions(); o.MaxAlloc != 32<<20 || o.Timeout != 20*time.Second || o.MaxSteps != 20_000_000 || o.MaxOutput != 16<<20 {
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
	forgetKernel(t)
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
	bootsKernel(t)
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

// The failures that need no kernel: the pinned digest parses, an image that
// cannot be had stops the kernel and the warm-up with its reason, a fetch that
// fails is kept, and every error of a cell has Julia's or the bound's words.
func TestJuliaFailures(t *testing.T) {
	if _, err := interp.ParseDigest(JuliaImageDigest); err != nil || pinned == [32]byte{} {
		t.Fatalf("the pinned digest: %v", err)
	}
	forgetKernel(t)
	saved := juliaImage
	t.Cleanup(func() { juliaImage = saved })
	juliaImage = func() ([]byte, error) { return nil, errors.New("no image") }
	if _, err := loadKernel(); err == nil || err.Error() != "no image" {
		t.Errorf("loadKernel: %v", err)
	}
	if err := WarmJulia(); err == nil || err.Error() != "no image" {
		t.Errorf("WarmJulia: %v", err)
	}
	// bytes that are not the image: the kernel refuses them by digest
	juliaImage = func() ([]byte, error) { return []byte("not an image"), nil }
	if _, err := loadKernel(); !errors.Is(err, interp.ErrImageDigest) {
		t.Errorf("a wrong image: %v", err)
	}

	// the fetch of the image, once, and its failure kept
	savedFetch, savedBytes, savedErr := fetchImage, imageBytes, imageErr
	t.Cleanup(func() { fetchImage, imageBytes, imageErr, imageOnce = savedFetch, savedBytes, savedErr, sync.Once{} })
	imageOnce = sync.Once{}
	calls := 0
	fetchImage = func(context.Context, string, [32]byte, interp.FetchOptions) ([]byte, error) {
		calls++
		return nil, errors.New("offline")
	}
	for range 2 {
		if _, err := fetchedImage(); err == nil || err.Error() != "offline" {
			t.Errorf("fetchedImage: %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("the image was fetched %d times", calls)
	}

	saved2 := JuliaLimits
	t.Cleanup(func() { JuliaLimits = saved2 })
	JuliaLimits = pageLimits
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w (x)", interp.ErrCellCanceled), "stopped: the Julia cell was cancelled"},
		{interp.ErrCellTimeout, "stopped: the Julia cell ran longer than 20s, the limit"},
		{errors.New("boom"), "the Julia cell failed: boom"},
		{&interp.JuliaError{Message: "m"}, "ERROR: m"},
	} {
		if got := juliaFailure(tc.err); got != tc.want {
			t.Errorf("juliaFailure(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	JuliaLimits.Timeout = 0
	if got := juliaFailure(interp.ErrCellTimeout); got != "stopped: the Julia cell's deadline passed" {
		t.Errorf("no timeout: %q", got)
	}
	if showValue("nothing", "x") || showValue("", "x") || showValue("1", "x;\n") || !showValue("1", "x") {
		t.Error("showValue")
	}
	var s session
	s.closeJulia() // nothing to close
}

// BreakJulia closes the kernel under the process, as a fault the next run must
// survive: its Reset fails, a kernel is booted anew and the worker's replacement
// is owed. It is a test's: a Reset that reloads the image needs over 260,000
// logged writes, which no cell makes within the page's 20 million statements, so
// the page cannot reach this path without a hook, and the shipped core has none.
func BreakJulia() {
	kernMu.Lock()
	defer kernMu.Unlock()
	if kern != nil {
		kern.Close()
		dirty = true
	}
}

// forgetKernel closes the process's kernel, now and at the end of the test: a
// test that changes the loader must not meet the kernel of another, nor leave
// its own.
func forgetKernel(t *testing.T) {
	t.Helper()
	reset := func() {
		kernMu.Lock()
		defer kernMu.Unlock()
		if kern != nil {
			kern.Close()
		}
		kern, dirty, replace, lastReset = nil, false, false, interp.ResetInfo{}
	}
	reset()
	t.Cleanup(reset)
}

// noKernel forgets the process's kernel and what is owed, and counts the loads
// until the end of the test.
func noKernel(t *testing.T) *int {
	t.Helper()
	bootsKernel(t)
	saved := loader
	forgetKernel(t)
	t.Cleanup(func() { loader = saved })
	n := new(int)
	loader = func() (*interp.Kernel, error) { *n++; return saved() }
	return n
}

// A kernel booted ahead (WarmJulia, as the page does before it says the core is
// ready) is the one every run takes: no run boots another, each is reset.
func TestJuliaWarmKernel(t *testing.T) {
	n := noKernel(t)
	if err := WarmJulia(); err != nil {
		t.Fatal(err)
	}
	if err := WarmJulia(); err != nil || *n != 1 {
		t.Fatalf("a second warm-up loaded again: %d, %v", *n, err)
	}
	if res := Run("```go\n1 + 1\n```\n"); res[0].Error != "" || *n != 1 {
		t.Errorf("a Go-only run took the warm kernel: %d loads, %+v", *n, res[0])
	}
	for i := range 3 {
		if res := Run("```julia\n1 + 1\n```\n"); res[0].Error != "" || *n != 1 || JuliaReplace() {
			t.Errorf("run %d did not take the kernel: %d loads, %+v", i, *n, res[0])
		}
	}
}

// Each run starts as the first of a kernel just booted: a global of the run
// before is gone, a struct and a constant are defined again, a seeded run does
// not change the first draw of the next, and no worker replacement is owed.
func TestJuliaRunsAreReset(t *testing.T) {
	n := noKernel(t)
	run := func(src string) string {
		res := Run("```julia\n" + src + "\n```\n")
		if res[0].Error != "" {
			return "ERROR " + res[0].Error
		}
		if len(res[0].Display) > 0 {
			return res[0].Output + res[0].Display[0]["text/plain"]
		}
		return res[0].Output
	}
	first := run("rand()")
	if first == "" || strings.HasPrefix(first, "ERROR") {
		t.Fatalf("first draw: %q", first)
	}
	if seeded := run("using Random\nRandom.seed!(7)\nrand()"); seeded == first {
		t.Fatalf("a seeded draw %q is the first draw", seeded)
	}
	// the next run's first draw is the kernel's first draw again, as if just booted
	if got := run("rand()"); got != first {
		t.Errorf("first draw %q, after a seeded run %q", first, got)
	}
	run("g1 = 5\nstruct P\n    x::Int\nend\nconst K = 1\nnothing")
	if got := run("g1"); !strings.HasPrefix(got, "ERROR") {
		t.Errorf("a global of an earlier run is still there: %q", got)
	}
	// a struct and a const defined again do not fail
	for range 2 {
		if got := run("struct P\n    x::Int\nend\nconst K = 2\nP(21).x * K"); got != "42" {
			t.Errorf("redefinition: %q", got)
		}
	}
	if *n != 1 || JuliaReplace() {
		t.Errorf("%d loads, replacement owed %v: a plain run reloads nothing", *n, JuliaReplace())
	}
}

// A Reset that has to load the image again (the cells changed more than the undo
// log holds) leaves a replacement owed, and so does a Reset that fails, which
// boots a kernel anew; a kernel that cannot be booted again is the run's error.
func TestJuliaResetReloadsOrFails(t *testing.T) {
	n := noKernel(t)
	savedLimits := JuliaLimits
	t.Cleanup(func() { JuliaLimits = savedLimits })
	JuliaLimits.MaxSteps, JuliaLimits.Timeout = 2_000_000_000, 5*time.Minute // the page's bound is for a runaway; this cell writes 300000 elements
	// (writes to a vector of Base, which is older than the kernel's cut: a write
	// to what the cell allocated itself is not logged)
	big := "for i in 1:300000\n    push!(Base.DEPOT_PATH, \"x\")\nend\nnothing"
	if res := Run("```julia\n" + big + "\n```\n"); res[0].Error != "" {
		t.Fatal(res[0].Error)
	}
	if JuliaReplace() {
		t.Fatal("a replacement is owed before any Reset")
	}
	if res := Run("```julia\n1 + 1\n```\n"); res[0].Error != "" || res[0].Display[0]["text/plain"] != "2" {
		t.Fatalf("the run after a big one: %+v", res[0])
	}
	if !JuliaReplace() {
		t.Error("a Reset that reloaded the image owes no replacement (ResetInfo.Reloaded)")
	}

	// a Reset that fails: the kernel is closed under it
	kernMu.Lock()
	replace = false
	kernMu.Unlock()
	BreakJulia()
	before := *n
	if res := Run("```julia\n3 + 3\n```\n"); res[0].Error != "" || res[0].Display[0]["text/plain"] != "6" || *n != before+1 || !JuliaReplace() {
		t.Errorf("a failed Reset: %d loads (was %d), owed %v, %+v", *n, before, JuliaReplace(), res[0])
	}

	// ... and the kernel cannot be booted again
	kernMu.Lock()
	replace = false
	kernMu.Unlock()
	BreakJulia()
	working := loader
	loader = func() (*interp.Kernel, error) { return nil, errors.New("no memory") }
	if res := Run("```julia\n4 + 4\n```\n"); !strings.Contains(res[0].Error, "the Julia kernel could not start: no memory") {
		t.Errorf("a Reset that fails and a boot that fails: %+v", res[0])
	}
	// The next run boots through kern == nil; the replacement is still owed,
	// because the instance holds the memory of the failed kernel's boot.
	loader = working
	if res := Run("```julia\n5 + 5\n```\n"); res[0].Error != "" || res[0].Display[0]["text/plain"] != "10" || !JuliaReplace() {
		t.Errorf("the boot after a failed Reset and a failed boot: owed %v, %+v", JuliaReplace(), res[0])
	}
}

// A task or a timer an earlier run started does not reach the next one
// (go-embedded-julia v0.20.0: Reset stops them before the rollback; on v0.19.0 a
// task parked on its timer woke in run 2, which failed with "not ported yet:
// ccall(:jl_get_backtrace)" or "val already in a list", and left the kernel
// broken). Run 1 starts a task that sleeps 0.5 s and yields once, so it is parked
// on its timer, and a periodic Timer; run 2 sleeps 1 s and must print exactly
// "done", with no "ghost" and no "tick"; runs 3 and 4 work. The Reset that began
// run 2 says what it stopped: a task and a timer at least.
func TestJuliaTaskOfAnEarlierRunDoesNotReachTheNext(t *testing.T) {
	noKernel(t)
	out := func(src string) (string, string) {
		res := Run("```julia\n" + src + "\n```\n")
		text := res[0].Output
		if len(res[0].Display) > 0 {
			text += res[0].Display[0]["text/plain"]
		}
		return text, res[0].Error
	}
	if _, e := out("@async (sleep(0.5); println(\"ghost\"))\nsleep(0.01)\nnothing"); e != "" {
		t.Fatal(e)
	}
	if got, e := out("sleep(1)\nprintln(\"done\")"); e != "" || got != "done\n" {
		t.Errorf("run 2: output %q, error %q; want exactly \"done\"", got, e)
	}
	if lastReset.Tasks < 1 {
		t.Errorf("the Reset that began run 2 stopped %d tasks, want at least 1: %+v", lastReset.Tasks, lastReset)
	}
	for i, want := range []string{"2", "4"} {
		if got, e := out(fmt.Sprintf("%d + %d", i+1, i+1)); e != "" || got != want {
			t.Errorf("run %d: %q, error %q; want %q", i+3, got, e, want)
		}
	}
}

// A periodic Timer a run started does not fire in the next run.
func TestJuliaTimerOfAnEarlierRunDoesNotFireInTheNext(t *testing.T) {
	noKernel(t)
	out := func(src string) (string, string) {
		res := Run("```julia\n" + src + "\n```\n")
		return res[0].Output, res[0].Error
	}
	if _, e := out("t = Timer(0.2; interval=0.2) do _\n    println(\"tick\")\nend\nsleep(0.01)\nnothing"); e != "" {
		t.Fatal(e)
	}
	if got, e := out("sleep(1)\nprintln(\"done\")"); e != "" || got != "done\n" {
		t.Errorf("run 2: output %q, error %q; want exactly \"done\", no tick", got, e)
	}
	if lastReset.Timers < 1 {
		t.Errorf("the Reset that began run 2 closed %d timers, want at least 1: %+v", lastReset.Timers, lastReset)
	}
	if got, e := out("println(\"after\")"); e != "" || got != "after\n" {
		t.Errorf("run 3: %q, error %q", got, e)
	}
}
