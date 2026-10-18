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

func quiet(t *testing.T) *bytes.Buffer {
	var b bytes.Buffer
	o, e := stdout, stderr
	stdout, stderr = &b, &b
	t.Cleanup(func() { stdout, stderr = o, e })
	return &b
}

// TestBuild builds a real playground: the WebAssembly core from source, and
// the page around it.
func TestBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the WebAssembly core")
	}
	log := quiet(t)
	out := filepath.Join(t.TempDir(), "playground")
	args := append([]string{"-o", out, "-org", "go-statistics", "-home", "https://go-statistics.github.io/", "-accent", "#9e520f"}, examples(t)...)
	if err := run(args); err != nil {
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
	if !strings.Contains(log.String(), "4 notebooks, playground.wasm") {
		t.Errorf("log: %s", log)
	}
}

func TestRunErrors(t *testing.T) {
	quiet(t)
	for _, tc := range []struct {
		args []string
		err  string
	}{
		{[]string{"-nope"}, "flag provided but not defined: -nope"},
		{nil, "no notebooks"},
		{[]string{"-accent", "red", "x.md"}, `"red" is not a #rgb or #rrggbb colour`},
		{[]string{"-accent-dark", "#12345", "x.md"}, `"#12345" is not`},
		{[]string{"-o", t.TempDir(), "does-not-exist.md"}, "does-not-exist.md"},
	} {
		if err := run(tc.args); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("run(%q) = %v, want %q", tc.args, err, tc.err)
		}
	}
}

func TestFailingNotebook(t *testing.T) {
	log := quiet(t)
	nb := filepath.Join(t.TempDir(), "bad.md")
	if err := os.WriteFile(nb, []byte("```go\nx := 1\n```\n```go\nundefinedName\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"-o", t.TempDir(), nb})
	if err == nil || !strings.Contains(err.Error(), "bad.md does not run") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(log.String(), "bad.md:5: cell 1 failed:\nline 1: undefined: undefinedName") {
		t.Errorf("log: %s", log)
	}
}

func TestWriteErrors(t *testing.T) {
	quiet(t)
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
		err := run([]string{"-o", out, nb})
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
	if err := run([]string{"-o", filepath.Join(file, "out"), nb}); err == nil {
		t.Error("an output under a file did not fail")
	}
}

func TestMainExit(t *testing.T) {
	log := quiet(t)
	defer func(f func(int)) { exit = f }(exit)
	defer func(a []string) { os.Args = a }(os.Args)
	code := -1
	exit = func(c int) { code = c }
	os.Args = []string{"build"}
	main()
	if code != 1 || !strings.Contains(log.String(), "build: no notebooks") {
		t.Errorf("exit %d, log %q", code, log)
	}
	defer func(f func([]string, ...string) ([]byte, error)) { goCmd = f }(goCmd)
	goCmd = fakeGo(fakeRoot(t))
	code = -1
	os.Args = []string{"build", "-o", t.TempDir(), examples(t)[1]}
	main()
	if code != -1 {
		t.Errorf("a good build exited %d: %s", code, log)
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
