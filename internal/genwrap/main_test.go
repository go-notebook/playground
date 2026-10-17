package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommitted regenerates the wrappers from the stats go.mod pins and
// compares them with symbols/zz_generics.go: an upgrade of stats that adds
// or changes a generic function fails here until `go generate ./symbols`
// is run.
func TestCommitted(t *testing.T) {
	dir, err := goList(statsPath)
	if err != nil {
		t.Skipf("go list: %v", err) // no go command next to the test (qemu)
	}
	got, skipped, err := generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../symbols/zz_generics.go")
	if err != nil {
		t.Fatal(err)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")) // a Windows checkout
	if !bytes.Equal(got, want) {
		t.Error("symbols/zz_generics.go is not what genwrap writes: run go generate ./symbols")
	}
	if len(skipped) != 0 {
		t.Errorf("left out: %v", skipped)
	}
}

const fake = `package stats

import (
	"github.com/go-ndarray/ndarray"
	rnd "github.com/go-ndarray/ndarray/random"
)

type Data interface{ []float64 | *ndarray.Array }
type Options struct{}
type hidden struct{}

func Plain(x []float64) float64 { return 0 }
func unexported[D Data](x D) {}
func (o Options) Method() {}
func Mean[D Data](x D, o *Options) (float64, error) { return 0, nil }
func Many[D, C Data](a D, b C, samples []D, opts ...int) (out D, err error) { return }
func Var[D Data](o *Options, samples ...D) {}
func Funcs[D Data](cb func(float64), f func(x float64) float64, m map[string]D, g *rnd.Generator, _ int, _ D) (D) { var d D; return d }
func Unnamed[D Data](D, int) {}
func Any[T any](x T) {}
func Hidden[D Data](x D, h hidden) {}
func Chan[D Data](x D, c chan int) {}
`

func fakeDir(t *testing.T, src string) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stats.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stats_test.go"), []byte("not go"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGenerate(t *testing.T) {
	src, skipped, err := generate(fakeDir(t, fake))
	if err != nil {
		t.Fatal(err)
	}
	got := string(src)
	for _, want := range []string{
		`rnd "github.com/go-ndarray/ndarray/random"`,
		`m["Mean"] = reflect.ValueOf(func(x any, o *stats.Options) (float64, error) { return stats.Mean[[]float64](Data(x), o) })`,
		`m["Many"] = reflect.ValueOf(func(a any, b any, samples [][]float64, opts ...int) (out []float64, err error) {`,
		`return stats.Many[[]float64, []float64](Data(a), Data(b), samples, opts...)`,
		`m["Var"] = reflect.ValueOf(func(o *stats.Options, samples ...any) { stats.Var[[]float64](o, DataAll(samples)...) })`,
		`func(cb func(float64), f func(x float64) float64, m map[string][]float64, g *rnd.Generator, p4 int, p5 any) []float64`,
		`m["Unnamed"] = reflect.ValueOf(func(p0 any, p1 int) { stats.Unnamed[[]float64](Data(p0), p1) })`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated code lacks %s\n%s", want, got)
		}
	}
	for _, absent := range []string{"Plain", "unexported", "Method", `"Any"`, `"Hidden"`, `"Chan"`} {
		if strings.Contains(got, absent) {
			t.Errorf("generated code has %s", absent)
		}
	}
	want := []string{
		"Any: a type parameter is not constrained by Data",
		"Chan: its signature has a *ast.ChanType",
		"Hidden: its signature names the unexported hidden",
	}
	if strings.Join(skipped, "\n") != strings.Join(want, "\n") {
		t.Errorf("skipped %q, want %q", skipped, want)
	}
}

func TestGenerateErrors(t *testing.T) {
	if _, _, err := generate("bad[pattern"); err == nil {
		t.Error("a bad directory did not fail")
	}
	if _, _, err := generate(fakeDir(t, "package stats\nfunc (")); err == nil {
		t.Error("a file that does not parse did not fail")
	}
	defer func(f func([]byte) ([]byte, error)) { formatSource = f }(formatSource)
	formatSource = func([]byte) ([]byte, error) { return nil, errors.New("bad code") }
	if _, _, err := generate(fakeDir(t, fake)); err == nil || !strings.Contains(err.Error(), "bad code") {
		t.Errorf("a formatting error gave %v", err)
	}
}

func TestMainAndRun(t *testing.T) {
	dir := fakeDir(t, fake)
	defer func(f func(string) (string, error)) { goList = f }(goList)
	defer func(w any) { stderr = w.(interface{ Write([]byte) (int, error) }) }(stderr)
	defer func(f func(int)) { exit = f }(exit)
	var log bytes.Buffer
	stderr = &log
	goList = func(string) (string, error) { return dir, nil }

	out := filepath.Join(t.TempDir(), "zz.go")
	os.Args = []string{"genwrap", "-o", out}
	flag.CommandLine = flag.NewFlagSet("genwrap", flag.ContinueOnError)
	main()
	if b, err := os.ReadFile(out); err != nil || !bytes.Contains(b, []byte(`m["Mean"]`)) {
		t.Errorf("main wrote %q, %v", b, err)
	}
	if !strings.Contains(log.String(), "genwrap: left out Any: a type parameter is not constrained by Data") {
		t.Errorf("stderr: %q", log.String())
	}

	code := 0
	exit = func(c int) { code = c }
	goList = func(string) (string, error) { return "", errors.New("no go") }
	flag.CommandLine = flag.NewFlagSet("genwrap", flag.ContinueOnError)
	main()
	if code != 1 || !strings.Contains(log.String(), "genwrap: go list github.com/go-statistics/stats: no go") {
		t.Errorf("exit %d, stderr %q", code, log.String())
	}

	goList = func(string) (string, error) { return "bad[", nil }
	if err := run(out); err == nil {
		t.Error("run on a bad directory did not fail")
	}
}
