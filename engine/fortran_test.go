//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/notebook/fcells"
)

// The page's Fortran is go-notebook's Fortran as the registry has it: it runs
// (the page links fcells since this release), it keeps its variables apart
// from the other languages' (analysis.Isolated), and it is no longer check-only.
func TestFortranIsTheRegistrys(t *testing.T) {
	f := analysis.Lookup(analysis.Fortran)
	if i, ok := f.(analysis.Isolated); !ok || !i.Isolated() {
		t.Errorf("Fortran is not isolated: %T", f)
	}
	if !analysis.RunOf(analysis.Fortran).Runs || !FortranSupported || CheckOnly(analysis.Fortran) {
		t.Error("the page does not run Fortran cells")
	}
}

// A Fortran cell runs: what it prints is the output, the variables and the
// procedures persist from one Fortran cell to the next, the Go cells around it
// run on their own variables, and a failure is the evaluator's own, with its
// line and column, and stops the run.
func TestFortranCellRuns(t *testing.T) {
	res := Run("```go\nn := 2\n```\n" +
		"```fortran\ninteger :: k = 3\nprint *, k\n```\n" +
		"```fortran\nk = k * 14\nprint *, k\n```\n" +
		"```go\nn * 21\n```\n")
	if strings.TrimSpace(res[1].Output) != "3" || res[1].Error != "" || res[1].Status != "" {
		t.Errorf("the first Fortran cell: %+v", res[1])
	}
	if strings.TrimSpace(res[2].Output) != "42" || res[2].Error != "" {
		t.Errorf("the variable did not persist: %+v", res[2])
	}
	if res[3].Error != "" || res[3].Skipped || res[3].Display[0]["text/plain"] != "42" {
		t.Errorf("the Go cell after them: %+v", res[3])
	}

	res = Run("```fortran\nprint *, 1\nprint *, undefined_name\n```\n```fortran\nprint *, 2\n```\n")
	if res[0].Error == "" || res[0].Status != "" || !res[1].Skipped {
		t.Errorf("a failed Fortran cell: %+v", res)
	}
	if !strings.HasPrefix(res[0].Error, "line 2") {
		t.Errorf("the failure does not say where: %q", res[0].Error)
	}
}

// Fortran's variables stay Fortran's, as in go-notebook: a Go cell cannot read
// what a Fortran cell declares, and neither is blocked.
func TestFortranCellsAreIsolated(t *testing.T) {
	res := Run("```fortran\ninteger :: total = 5\n```\n```go\nm := total + 1\n```\n")
	if res[1].Error != "line 1: undefined: total" || res[1].Status != "" {
		t.Errorf("a Go cell read a Fortran name: %+v", res[1])
	}
	res = Run("```go\nv := 1\n```\n```fortran\ninteger :: v = 100\nprint *, v\n```\n```go\nv\n```\n")
	if strings.TrimSpace(res[1].Output) != "100" || res[2].Display[0]["text/plain"] != "1" {
		t.Errorf("%+v", res)
	}
}

// withFortranOptions runs f with other options on the page's Fortran.
func withFortranOptions(t *testing.T, o fcells.Options, f func()) {
	t.Helper()
	old := FortranOptions
	FortranOptions = o
	defer func() { FortranOptions = old }()
	f()
}

// A runaway cell is stopped, with a message that says why, and the cells after
// it are skipped; the next run is a new session that works. Each bound ends one
// kind of runaway: the step count a loop that never ends, the time a cell that
// the count would let run for long, the output cap a cell that prints forever.
func TestFortranLimitsStopARunawayCell(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    fcells.Options
		src  string
		want string
	}{
		{"steps", fcells.Options{MaxSteps: 100_000, Timeout: time.Minute}, "integer :: i\ndo\n  i = 1\nend do", "step"},
		{"time", fcells.Options{MaxSteps: -1, Timeout: 100 * time.Millisecond}, "integer :: i\ndo\n  i = 1\nend do", "ran longer than 100ms, the limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withFortranOptions(t, tc.o, func() {
				start := time.Now()
				res := Run("```fortran\n" + tc.src + "\n```\n```fortran\nprint *, 1\n```\n")
				if !strings.Contains(res[0].Error, tc.want) || res[0].Status != "" || !res[1].Skipped {
					t.Errorf("%+v, want an error with %q and the next cell skipped", res, tc.want)
				}
				if d := time.Since(start); d > 20*time.Second {
					t.Errorf("stopped after %v", d)
				}
			})
			if res := Run("```fortran\nprint *, 1 + 1\n```\n"); strings.TrimSpace(res[0].Output) != "2" || res[0].Error != "" {
				t.Errorf("the next run: %+v", res[0])
			}
		})
	}
}

// The options the page ships with are bounded: the steps are far under the
// default's 2^32, and the time is under the page's 30 s watchdog.
func TestPageFortranOptions(t *testing.T) {
	if FortranOptions.MaxSteps <= 0 || FortranOptions.MaxSteps >= fcells.DefaultMaxSteps {
		t.Errorf("options %+v are not bounded tighter than the default's", FortranOptions)
	}
	if FortranOptions.Timeout <= 0 || FortranOptions.Timeout >= 30*time.Second {
		t.Errorf("timeout %v is not under the page's 30 s watchdog", FortranOptions.Timeout)
	}
}

// No file or variable of the host is visible: the evaluator has no access to
// either.
func TestFortranCellSeesNothingOfTheHost(t *testing.T) {
	t.Setenv("PLAYGROUND_SECRET", "hunter2")
	res := Run("```fortran\ncharacter(len=64) :: v\ncall get_environment_variable('PLAYGROUND_SECRET', v)\nprint *, trim(v)\n```\n")
	if strings.Contains(res[0].Output, "hunter2") {
		t.Errorf("the environment is visible: %+v", res[0])
	}
	res = Run("```fortran\nopen(10, file='/etc/passwd')\n```\n")
	if res[0].Error == "" || strings.Contains(res[0].Output, "root:") {
		t.Errorf("a host file was opened: %+v", res[0])
	}
}
