package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// examples are this repository's statistics notebooks.
func examples(t *testing.T) []string {
	files, _ := filepath.Glob("../../examples/statistics/*.md")
	if len(files) == 0 {
		t.Fatal("no example notebooks")
	}
	return files
}

// fakeGo stands for the go command: "build" writes a small file where the
// core would be, "env" answers with goroot.
func fakeGo(goroot string) func([]string, ...string) ([]byte, error) {
	return func(_ []string, args ...string) ([]byte, error) {
		if args[0] == "build" {
			for i, a := range args {
				if a == "-o" {
					return nil, os.WriteFile(args[i+1], []byte("\x00asm"), 0o644)
				}
			}
		}
		return []byte(goroot + "\n"), nil
	}
}

// fakeRoot is a GOROOT holding lib/wasm/wasm_exec.js.
func fakeRoot(t *testing.T) string {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "lib", "wasm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "wasm", "wasm_exec.js"), []byte("// go"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// quiet returns a buffer for both streams of a run, and puts the go command's
// error stream back afterwards.
func quiet(t *testing.T) *bytes.Buffer {
	o := goStderr
	t.Cleanup(func() { goStderr = o })
	return &bytes.Buffer{}
}

// runTo runs the command with both streams in b.
func runTo(b *bytes.Buffer, args ...string) error { return run(args, b, b) }

// seam replaces *p for the test.
func seam[T any](t *testing.T, p *T, v T) {
	o := *p
	*p = v
	t.Cleanup(func() { *p = o })
}

// TestBuild builds a real playground: the WebAssembly core from source, and
// the page around it.
func TestBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the WebAssembly core")
	}
	log := quiet(t)
	out := filepath.Join(t.TempDir(), "playground")
	args := append([]string{"-o", out, "--org", "go-statistics", "--home", "https://go-statistics.github.io/", "--accent", "#9e520f"}, examples(t)...)
	if err := runTo(log, args...); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	for _, f := range []string{"index.html", "playground.css", "playground.js", "worker.js", "wasm_exec.js", "playground.wasm", "notebooks/1-fit-and-test.md"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Error(err)
		}
	}
	wasm, _ := os.ReadFile(filepath.Join(out, "playground.wasm"))
	if !bytes.HasPrefix(wasm, []byte("\x00asm")) {
		t.Error("playground.wasm is not WebAssembly")
	}
	page, _ := os.ReadFile(filepath.Join(out, "index.html"))
	for _, want := range []string{
		"<title>go-statistics playground</title>",
		`<a class="home" href="https://go-statistics.github.io/">← go-statistics</a>`,
		"--accent: #9e520f;",
		`"name":"1-fit-and-test","title":"Fit a distribution, then test the fit"`,
		`"kind":"code","lang":"go","source":"import (`,
	} {
		if !bytes.Contains(page, []byte(want)) {
			t.Errorf("index.html lacks %s", want)
		}
	}
	if bytes.Contains(page, []byte("</script><")) && bytes.Count(page, []byte("</script>")) != 2 {
		t.Error("a notebook closed a <script> early")
	}
	if !strings.Contains(log.String(), "5 notebooks, playground.wasm") {
		t.Errorf("log: %s", log)
	}
}

func TestRunErrors(t *testing.T) {
	log := quiet(t)
	for _, tc := range []struct {
		args []string
		err  string
	}{
		{[]string{"--nope"}, "unknown flag: --nope"},
		{[]string{"-n", "x.md"}, "unknown shorthand flag: 'n'"},
		{nil, "no notebooks"},
		{[]string{"--accent", "red", "x.md"}, `"red" is not a #rgb or #rrggbb colour`},
		{[]string{"--accent-dark", "#12345", "x.md"}, `"#12345" is not`},
		{[]string{"--accent"}, "flag needs an argument: --accent"},
		{[]string{"-o", t.TempDir(), "does-not-exist.md"}, "does-not-exist.md"},
		{[]string{"-o"}, "flag needs an argument: 'o'"},
	} {
		log.Reset()
		err := runTo(log, tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("run(%q) = %v, want %q", tc.args, err, tc.err)
		}
		if strings.Contains(log.String(), "Usage:") {
			t.Errorf("run(%q): usage printed on an error", tc.args)
		}
	}
}

func TestFailingNotebook(t *testing.T) {
	log := quiet(t)
	nb := filepath.Join(t.TempDir(), "bad.md")
	if err := os.WriteFile(nb, []byte("```go\nx := 1\n```\n```go\nundefinedName\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runTo(log, "-o", t.TempDir(), nb)
	if err == nil || !strings.Contains(err.Error(), "bad.md does not run") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(log.String(), "bad.md:5: cell 1 failed:\nline 1: undefined: undefinedName") {
		t.Errorf("log: %s", log)
	}
}

func TestWriteErrors(t *testing.T) {
	log := quiet(t)
	defer func(f func([]string, ...string) ([]byte, error)) { goCmd = f }(goCmd)
	root := fakeRoot(t)
	nb := examples(t)[1] // 2-t-test: no figure, quick

	// Each case makes one path a directory, or makes the go command fail.
	for _, tc := range []struct {
		block string // a directory made where a file goes
		gocmd func([]string, ...string) ([]byte, error)
		err   string
	}{
		{block: "", err: ""},
		{block: "notebooks/2-t-test.md", err: "2-t-test.md"},
		{block: "wasm_exec.js", err: "wasm_exec.js"},
		{block: "playground.css", err: "playground.css"},
		{block: "index.html", err: "index.html"},
		{gocmd: func([]string, ...string) ([]byte, error) { return nil, errors.New("no compiler") }, err: "building the WebAssembly core: no compiler"},
		{gocmd: func(_ []string, args ...string) ([]byte, error) {
			if args[0] == "env" {
				return nil, errors.New("no env")
			}
			return nil, nil
		}, err: "go env GOROOT: no env"},
		{gocmd: func([]string, ...string) ([]byte, error) { return []byte(t.TempDir()), nil }, err: "wasm_exec.js"},
		{gocmd: func(_ []string, args ...string) ([]byte, error) { return []byte(root), nil }, err: "playground.wasm"}, // built nothing
		{gocmd: func(env []string, args ...string) ([]byte, error) {
			if slices.Contains(args, "-tags=ruby,rbgo_core") {
				return nil, errors.New("no rbgo")
			}
			return fakeGo(root)(env, args...)
		}, err: "building the Ruby WebAssembly core: no rbgo"},
		{gocmd: func(env []string, args ...string) ([]byte, error) {
			if slices.Contains(args, "-tags=ruby,rbgo_core") {
				return nil, nil // built nothing
			}
			return fakeGo(root)(env, args...)
		}, err: "playground-ruby.wasm"},
	} {
		out := t.TempDir()
		if tc.block != "" {
			if err := os.MkdirAll(filepath.Join(out, tc.block), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		goCmd = fakeGo(root)
		if tc.gocmd != nil {
			goCmd = tc.gocmd
		}
		err := runTo(log, "-o", out, nb)
		if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("blocking %q: err = %v, want %q", tc.block, err, tc.err)
		}
	}

	// The output directory cannot be made.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	goCmd = fakeGo(root)
	if err := runTo(log, "-o", filepath.Join(file, "out"), nb); err == nil {
		t.Error("an output under a file did not fail")
	}
}

// main reports an error on the real standard error and exits 1 through
// osExit; a good build exits nowhere.
func TestMainExit(t *testing.T) {
	quiet(t)
	// Standard error is a file for the length of the call.
	errFile, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer errFile.Close()
	oe := os.Stderr
	os.Stderr = errFile
	defer func() { os.Stderr = oe }()
	seam(t, &os.Args, []string{"build"})
	code := -1
	seam(t, &osExit, func(c int) { code = c })
	main()
	got, _ := os.ReadFile(errFile.Name())
	if code != 1 || !strings.Contains(string(got), "build: no notebooks") {
		t.Errorf("exit %d, stderr %q", code, got)
	}
	seam(t, &goCmd, fakeGo(fakeRoot(t)))
	code = -1
	os.Args = []string{"build", "-o", t.TempDir(), examples(t)[1]}
	main()
	if code != -1 {
		t.Errorf("a good build exited %d", code)
	}
}

// The exit status goes through osExit for every failure, not only a missing
// notebook list.
func TestExitStatuses(t *testing.T) {
	quiet(t)
	oe := os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	os.Stderr = devnull
	defer func() { os.Stderr = oe }()
	seam(t, &goCmd, fakeGo(fakeRoot(t)))
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"--nope"}, 1},
		{[]string{"--accent", "red", "x.md"}, 1},
		{[]string{"-o", t.TempDir(), "missing.md"}, 1},
		{[]string{"--help"}, -1},
		{[]string{"--version"}, -1},
		{[]string{"-o", t.TempDir(), examples(t)[1]}, -1},
	} {
		code := -1
		seam(t, &osExit, func(c int) { code = c })
		seam(t, &os.Args, append([]string{"build"}, tc.args...))
		devOut, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		oo := os.Stdout
		os.Stdout = devOut
		main()
		os.Stdout = oo
		devOut.Close()
		if code != tc.want {
			t.Errorf("%q: exit %d, want %d", tc.args, code, tc.want)
		}
	}
}

func TestHelp(t *testing.T) {
	var out bytes.Buffer
	if err := runTo(&out, "--help"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"build [flags] notebook.md...",
		"-o, --out string",
		"--org string",
		"--home string",
		"--accent string",
		"--accent-dark string",
		"--libraries string",
		"--version",
		"--org go-statistics",
	} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("build --help lacks %q:\n%s", s, out.String())
		}
	}
	out.Reset()
	if err := runTo(&out, "-h"); err != nil || !strings.Contains(out.String(), "Usage:") {
		t.Errorf("-h: %v\n%s", err, out.String())
	}
}

func TestVersionFlag(t *testing.T) {
	seam(t, &buildInfo, func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{GoVersion: "go1.27.2", Main: debug.Module{Path: "github.com/go-notebook/playground", Version: "v0.2.1"}}, true
	})
	var out bytes.Buffer
	if err := runTo(&out, "--version"); err != nil {
		t.Fatal(err)
	}
	if want := "build version go-notebook/playground v0.2.1, go1.27.2\n"; out.String() != want {
		t.Errorf("--version = %q, want %q", out.String(), want)
	}
}

// legacy rewrites the single-dash long flags of earlier releases and nothing
// else: every case names the arguments, what pflag must receive, and the
// note on stderr ("" for none).
func TestLegacy(t *testing.T) {
	for _, c := range []struct {
		name       string
		args, want []string
		note       string
	}{
		{"long flag", []string{"-org", "x", "a.md"},
			[]string{"--org", "x", "a.md"}, "read -org as --org"},
		{"long flag with =", []string{"-accent=#fff", "a.md"},
			[]string{"--accent=#fff", "a.md"}, "read -accent as --accent"},
		{"hyphenated name", []string{"-accent-dark", "#000", "a.md"},
			[]string{"--accent-dark", "#000", "a.md"}, "read -accent-dark as --accent-dark"},
		{"several, one note", []string{"-o", "out", "-org", "x", "-home=h", "-libraries", "l", "a.md"},
			[]string{"-o", "out", "--org", "x", "--home=h", "--libraries", "l", "a.md"},
			"read -org, -home, -libraries as --org, --home, --libraries"},
		{"a value is not a flag", []string{"-o", "-org", "a.md"},
			[]string{"-o", "-org", "a.md"}, ""},
		{"a value after a long flag is not a flag", []string{"--home", "-org", "a.md"},
			[]string{"--home", "-org", "a.md"}, ""},
		{"a value after a rewritten flag is not a flag", []string{"-home", "-org", "a.md"},
			[]string{"--home", "-org", "a.md"}, "read -home as --home"},
		{"inline value: the next word is an argument", []string{"--home=h", "-org", "x", "a.md"},
			[]string{"--home=h", "--org", "x", "a.md"}, "read -org as --org"},
		{"shorthand with an attached value", []string{"-oout", "-org", "x", "a.md"},
			[]string{"-oout", "--org", "x", "a.md"}, "read -org as --org"},
		{"unknown flag left to pflag", []string{"-nope", "a.md"},
			[]string{"-nope", "a.md"}, ""},
		{"after --", []string{"-org", "x", "--", "-org"},
			[]string{"--org", "x", "--", "-org"}, "read -org as --org"},
		{"a lone dash", []string{"-", "-org", "x"},
			[]string{"-", "--org", "x"}, "read -org as --org"},
		{"trailing flag without its value", []string{"a.md", "-org"},
			[]string{"a.md", "--org"}, "read -org as --org"},
		{"help", []string{"-help"}, []string{"--help"}, "read -help as --help"},
		{"version", []string{"-version"}, []string{"--version"}, "read -version as --version"},
		{"nothing", nil, []string{}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			var warn bytes.Buffer
			got := legacy(RootCmd(), c.args, &warn)
			if !slices.Equal(got, c.want) {
				t.Errorf("legacy(%q) = %q, want %q", c.args, got, c.want)
			}
			if c.note == "" && warn.Len() != 0 {
				t.Errorf("unexpected note: %q", warn.String())
			}
			if c.note != "" && !strings.Contains(warn.String(), c.note) {
				t.Errorf("note %q, want %q", warn.String(), c.note)
			}
		})
	}
}

// Find fails when the command takes no positional arguments at all and is
// given one: the arguments go through untouched and cobra reports it.
func TestLegacyFindError(t *testing.T) {
	root := RootCmd()
	root.Args = nil
	root.AddCommand(&cobra.Command{Use: "sub", Run: func(*cobra.Command, []string) {}})
	args := []string{"dance", "-org"}
	if got := legacy(root, args, &bytes.Buffer{}); !slices.Equal(got, args) {
		t.Errorf("got %q", got)
	}
}

// The old spellings still build, with a note.
func TestLegacyEndToEnd(t *testing.T) {
	log := quiet(t)
	seam(t, &goCmd, fakeGo(fakeRoot(t)))
	out := t.TempDir()
	if err := runTo(log, "-o", out, "-org", "legacy-org", "-accent", "#123", "-accent-dark=#456", examples(t)[1]); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	page, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !bytes.Contains(page, []byte("<title>legacy-org playground</title>")) || !bytes.Contains(page, []byte("--accent: #123;")) {
		t.Error("the single-dash flags were not applied")
	}
	if !strings.Contains(log.String(), "read -org, -accent, -accent-dark as --org, --accent, --accent-dark") {
		t.Errorf("no note: %s", log)
	}
}

// No long flag is spelled only with registered shorthand letters: if one
// were, -name would mean a cluster of shorthands to pflag and the long flag
// to earlier releases, and the rewrite would silently pick one.
func TestNoLongFlagIsACluster(t *testing.T) {
	cmd := RootCmd()
	cmd.InitDefaultHelpFlag()
	cmd.InitDefaultVersionFlag()
	flags := cmd.Flags()
	flags.VisitAll(func(f *pflag.Flag) {
		cluster := true
		for _, r := range f.Name {
			if flags.ShorthandLookup(string(r)) == nil {
				cluster = false
			}
		}
		if cluster {
			t.Errorf("--%s is also a cluster of shorthands", f.Name)
		}
	})
}

// The command takes exactly these flags, with POSIX long and short spellings.
func TestFlags(t *testing.T) {
	want := []string{"accent/", "accent-dark/", "home/", "libraries/", "org/", "out/o"}
	var got []string
	RootCmd().Flags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name+"/"+f.Shorthand) })
	if !slices.Equal(got, want) {
		t.Errorf("flags %q, want %q", got, want)
	}
}

func TestGoCmd(t *testing.T) {
	quiet(t)
	out, err := goCmd([]string{"GOFLAGS="}, "env", "GOOS")
	if err != nil {
		t.Skipf("no go command: %v", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		t.Error("go env GOOS said nothing")
	}
}

func TestTitle(t *testing.T) {
	if got := title("text\n# The title #\n# Second\n", "n"); got != "The title" {
		t.Errorf("title = %q", got)
	}
	if got := title("no heading", "n"); got != "n" {
		t.Errorf("title = %q", got)
	}
}

func TestVersion(t *testing.T) {
	defer func(f func() (*debug.BuildInfo, bool)) { buildInfo = f }(buildInfo)
	for _, tc := range []struct {
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		{nil, false, "go-notebook/playground (devel)"},
		{&debug.BuildInfo{GoVersion: "go1.27.2", Main: debug.Module{Path: "github.com/go-notebook/playground", Version: "v0.1.0"}}, true, "go-notebook/playground v0.1.0, go1.27.2"},
		{&debug.BuildInfo{GoVersion: "go1.27.2", Main: debug.Module{Path: "site"}, Deps: []*debug.Module{{Path: "x"}, {Path: "github.com/go-notebook/playground", Version: "v0.2.0"}}}, true, "go-notebook/playground v0.2.0, go1.27.2"},
	} {
		buildInfo = func() (*debug.BuildInfo, bool) { return tc.bi, tc.ok }
		if got := version(); got != tc.want {
			t.Errorf("version() = %q, want %q", got, tc.want)
		}
	}
}
