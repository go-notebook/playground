package engine

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-notebook/playground/internal/yaegi/interp"
)

var update = flag.Bool("update", false, "rewrite the golden files of the examples")

// TestExamples runs every example notebook, headlessly, and compares what
// each cell did with testdata/golden. Numbers are compared to 1e-9 relative,
// (or 1e-11 absolute), so that a last-bit difference between architectures
// (FMA on arm64, ...) is not a failure; everything else must match exactly. The tolerances map
// gives a notebook a looser one, with the reason.
var tolerances = map[string]float64{
	// An adaptive solver's step count depends on the last bits of every
	// step: arm64 (with FMA) takes 4667 RK45 steps, WebAssembly and amd64
	// 4679.
	"numerics-3-ode": 0.01,
}

func TestExamples(t *testing.T) {
	files, _ := filepath.Glob("../examples/*/*.md")
	if len(files) < 9 {
		t.Fatalf("found %d example notebooks, want at least 9", len(files))
	}
	for _, f := range files {
		name := filepath.Base(filepath.Dir(f)) + "-" + strings.TrimSuffix(filepath.Base(f), ".md")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			// A Windows checkout has CRLF line endings.
			got := transcript(Run(strings.ReplaceAll(string(src), "\r\n", "\n")))
			golden := filepath.Join("testdata", "golden", name+".txt")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test ./engine -update)", err)
			}
			tol := tolerances[name]
			if tol == 0 {
				tol = 1e-9
			}
			if msg := sameModuloRounding(got, strings.ReplaceAll(string(want), "\r\n", "\n"), tol); msg != "" {
				t.Errorf("%s differs from %s: %s\n--- got:\n%s", f, golden, msg, got)
			}
			if strings.Contains(got, "-- error") || strings.Contains(got, "-- skipped") {
				t.Errorf("%s has a failing cell:\n%s", f, got)
			}
		})
	}
}

// transcript writes what each cell did, a figure as the size of its
// drawing area.
func transcript(rs []Result) string {
	var b strings.Builder
	svgSize := regexp.MustCompile(`<svg[^>]*\bwidth="([^"]+)"[^>]*\bheight="([^"]+)"`)
	for _, r := range rs {
		fmt.Fprintf(&b, "== cell %d (line %d)\n", r.Cell, r.Line)
		if r.Skipped {
			b.WriteString("-- skipped\n")
			continue
		}
		if r.Output != "" {
			fmt.Fprintf(&b, "-- output\n%s", r.Output)
		}
		for _, d := range r.Display {
			if svg, ok := d["image/svg+xml"]; ok {
				m := svgSize.FindStringSubmatch(svg)
				if m == nil {
					fmt.Fprintf(&b, "-- figure: not an <svg>: %.80q\n", svg)
					continue
				}
				fmt.Fprintf(&b, "-- figure: <svg> %s x %s\n", m[1], m[2])
				continue
			}
			fmt.Fprintf(&b, "-- show\n%s\n", d["text/plain"])
		}
		if r.Error != "" {
			fmt.Fprintf(&b, "-- error\n%s\n", r.Error)
		}
	}
	return b.String()
}

var number = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

// sameModuloRounding compares two transcripts, numbers to tol relative or
// 1e-11 absolute: an error estimate of 1e-13 is rounding noise, and differs
// between amd64 and arm64.
func sameModuloRounding(got, want string, tol float64) string {
	gn, wn := number.FindAllString(got, -1), number.FindAllString(want, -1)
	if g, w := number.ReplaceAllString(got, "#"), number.ReplaceAllString(want, "#"); g != w {
		return "the text differs"
	}
	for i := range gn {
		g, _ := strconv.ParseFloat(gn[i], 64)
		w, _ := strconv.ParseFloat(wn[i], 64)
		if d := math.Abs(g - w); d > 1e-11 && d > tol*math.Max(math.Abs(g), math.Abs(w)) {
			return fmt.Sprintf("%s where %s was expected", gn[i], wn[i])
		}
	}
	return ""
}

func TestSameModuloRounding(t *testing.T) {
	if msg := sameModuloRounding("x 1.0000000000001", "x 1.0000000000002", 1e-9); msg != "" {
		t.Error(msg)
	}
	if msg := sameModuloRounding("x 1.1", "x 1.2", 1e-9); msg == "" {
		t.Error("1.1 and 1.2 compare equal")
	}
	if msg := sameModuloRounding("x 1", "y 1", 1e-9); msg == "" {
		t.Error("different text compares equal")
	}
}

func TestTranscript(t *testing.T) {
	got := transcript([]Result{
		{Cell: 0, Line: 2, Display: []map[string]string{{"image/svg+xml": "<g/>", "text/plain": "<x>"}}},
		{Cell: 1, Line: 5, Skipped: true},
	})
	want := "== cell 0 (line 2)\n-- figure: not an <svg>: \"<g/>\"\n== cell 1 (line 5)\n-- skipped\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// run runs cells (joined into a notebook) and returns the last one's
// result.
func run(t *testing.T, cells ...string) Result {
	t.Helper()
	var md strings.Builder
	for _, c := range cells {
		fmt.Fprintf(&md, "Some prose.\n\n```go\n%s\n```\n\n", c)
	}
	rs := Run(md.String())
	if len(rs) != len(cells) {
		t.Fatalf("%d results for %d cells", len(rs), len(cells))
	}
	return rs[len(rs)-1]
}

func shown(r Result) []string {
	var out []string
	for _, d := range r.Display {
		out = append(out, d["text/plain"])
	}
	return out
}

func TestRun(t *testing.T) {
	const imports = "import (\n\t\"fmt\"\n\t\"math\"\n\t\"strconv\"\n\t\"errors\"\n\n\t\"github.com/go-plot/plot\"\n\t\"github.com/go-statistics/stats\"\n)"
	for _, tc := range []struct {
		name   string
		cells  []string
		show   []string
		output string
		err    string // a regexp
	}{
		{name: "final expression", cells: []string{"x := 2\nx * 21"}, show: []string{"42"}},
		{name: "state is shared", cells: []string{"x := 2", "y := x + 1", "x * y"}, show: []string{"6"}},
		{name: "a comment after the final expression", cells: []string{"x := 2\nx // two"}, show: []string{"2"}},
		{name: "two statements on a line", cells: []string{imports, "x := 2.0; math.Pi * x"}, show: []string{"6.283185307179586"}},
		{name: "declarations and statements", cells: []string{imports, "const c = 3\nfunc sq(v int) int { return v * v }\ntype P struct{ X, y int }\nfunc (p P) Sum() int { return p.X + p.y }\nv := sq(c)\nvar w = 2\nP{v, w}.Sum()"}, show: []string{"11"}},
		{name: "a function literal is a statement", cells: []string{imports, "func() { fmt.Println(\"hi\") }()"}, output: "hi\n"},
		{name: "printed output", cells: []string{imports, "fmt.Println(1)\nfmt.Print(\"a\")"}, output: "1\na"},
		{name: "fmt.Println's results are not shown", cells: []string{imports, "fmt.Println(1)"}, output: "1\n"},
		{name: "(int, error) is not shown", cells: []string{imports, "strconv.Atoi(\"3\")"}},
		{name: "(value, error) shows the value", cells: []string{imports, "strconv.ParseFloat(\"2.5\", 64)"}, show: []string{"2.5"}},
		{name: "(value, error) fails the cell", cells: []string{imports, "strconv.ParseFloat(\"x\", 64)"}, err: `^error: strconv.ParseFloat: parsing "x": invalid syntax$`},
		{name: "other multiple results are not shown", cells: []string{imports, "math.Modf(2.5)"}},
		{name: "a nil is not shown", cells: []string{imports, "var e error\ne"}},
		{name: "a void call runs", cells: []string{imports, "func f() { fmt.Println(\"f\") }\nf()"}, output: "f\n"},
		{name: "a struct names its exported fields", cells: []string{imports, "stats.TTestInd([]float64{1, 2, 3, 4}, []float64{2, 3, 4, 5, 6}, nil)"}, show: []string{"{Statistic:-1.5275252316519465 PValue:0.17047066078705386 DF:7}"}},
		{name: "a pointer to a struct too (yaegi exports b as Xb)", cells: []string{"type T struct{ A int; b int }", "&T{1, 2}"}, show: []string{"{A:1 Xb:2}"}},
		{name: "an undefined name in a definition", cells: []string{"x := 1", "y := x + math.Sqrt(undefinedName)"}, err: `^line 1: undefined: math$`},
		{name: "an undefined name in a call", cells: []string{imports, "y := func(v int) int { return v }(1) + math.MaxInt8 + nope(2)"}, err: `^line 1: undefined: nope$`},
		{name: "an interpreted String method", cells: []string{"type T struct{ A int }\nfunc (t T) String() string { return \"T!\" }", "T{1}"}, show: []string{"T!"}},
		{name: "an error value", cells: []string{imports, "errors.New(\"e\")"}, show: []string{"e"}},
		{name: "a figure", cells: []string{imports, "fig := plot.NewFigure()\nfig"}, show: []string{"<*plot.Figure>"}},
		{name: "subplots, indexed twice (a patched yaegi defect)", cells: []string{imports, "fig, axs := plot.Subplots(2, 1)\ntop, bottom := axs[0][0], axs[1][0]\ntop.SetTitle(\"a\")\nfig.Axes()[1] == bottom"}, show: []string{"true"}},
		{name: "v, err := f() in a loop (a patched yaegi defect)", cells: []string{imports, "s := 0.0\nfor _, x := range []float64{1, 2} {\n\tq, _ := math.Frexp(x)\n\ts += q\n}\ns"}, show: []string{"1"}},
		{name: "math.Pi with an untyped float (the float constants are float64)", cells: []string{imports, "const f = 50.0\n2 * f * math.Pi"}, show: []string{"314.1592653589793"}},
		{name: "stats takes a []float64", cells: []string{imports, "stats.Mean([]float64{1, 2, 3}, nil)"}, show: []string{"2"}},
		{name: "a compile error, at its line", cells: []string{"x := 1\n\ny := undefinedName"}, err: `^line 3: undefined: undefinedName$`},
		{name: "a panic, with its calls", cells: []string{"func bad() []int { var a []int; return a[1:5] }", "x := 1\n_ = x\nbad()"}, err: `(?s)^panic: .*\n\tat line 1, in bad\n\tat line 3$`},
		{name: "an import the playground does not have", cells: []string{"import (\n\t\"fmt\"\n\t\"os\"\n)"}, err: `^line 3: package os is not in the playground; it has bufio, .*github.com/go-statistics/stats.*$`},
		{name: "an import that does not parse", cells: []string{"import \"fmt"}, err: `^line 1: `},
		{name: "a syntax error", cells: []string{"x := (1"}, err: `.`},
		{name: "an empty cell", cells: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, tc.cells...)
			if !reflect.DeepEqual(shown(r), tc.show) {
				t.Errorf("showed %q, want %q", shown(r), tc.show)
			}
			if r.Output != tc.output {
				t.Errorf("printed %q, want %q", r.Output, tc.output)
			}
			if tc.err == "" && r.Error != "" {
				t.Errorf("error %q", r.Error)
			}
			if tc.err != "" && !regexp.MustCompile(tc.err).MatchString(r.Error) {
				t.Errorf("error %q, want one matching %q", r.Error, tc.err)
			}
		})
	}
}

func TestFailureSkipsTheRest(t *testing.T) {
	rs := Run("```go\nx := 1\n```\n```go\nundefinedName\n```\n```go\nx\n```\n")
	if rs[0].Error != "" || rs[1].Error == "" || !rs[2].Skipped || rs[2].Error != "" {
		t.Errorf("%+v", rs)
	}
	if rs[1].Line != 5 {
		t.Errorf("cell 1 at line %d, want 5", rs[1].Line)
	}
}

func TestCells(t *testing.T) {
	got := Cells("# T\n\n```go\nx := 1\n```\nend\n")
	want := []Cell{{"prose", "# T\n\n"}, {"code", "x := 1\n"}, {"prose", "end\n"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterpreterCrash(t *testing.T) {
	defer func(e func(*interp.Interpreter, string) (reflect.Value, error)) { evaluate = e }(evaluate)
	evaluate = func(*interp.Interpreter, string) (reflect.Value, error) { panic("boom") }
	r := run(t, "1")
	if r.Error != "the interpreter failed on this code (a yaegi defect): boom" {
		t.Errorf("error %q", r.Error)
	}
}

func TestMust(t *testing.T) {
	i := interp.New(interp.Options{})
	for name, f := range map[string]func(){
		"use":  func() { mustUse(i, interp.Exports{"nopackage": nil}) },
		"eval": func() { mustEval(i, "x :=") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("must%s did not panic", name)
				}
			}()
			f()
		}()
	}
}

// svgFailer is a figure that cannot draw itself.
type svgFailer struct{}

func (svgFailer) SaveSVG(io.Writer) error { return errors.New("no ink") }

type both struct{}

func (both) Display() map[string][]byte {
	return map[string][]byte{"text/html": []byte("<b>x</b>"), "image/png": {1, 2}}
}

type withText struct{}

func (withText) Display() map[string][]byte { return map[string][]byte{"text/plain": []byte("own")} }

func TestBundle(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want map[string]string
	}{
		{svgFailer{}, map[string]string{"text/plain": "<engine.svgFailer: no ink>"}},
		{both{}, map[string]string{"text/html": "<b>x</b>", "image/png": "AQI=", "text/plain": "<engine.both>"}},
		{withText{}, map[string]string{"text/plain": "own"}},
		{(*struct{ A int })(nil), map[string]string{"text/plain": "<nil>"}},
		{[]int{1}, map[string]string{"text/plain": "[1]"}},
	} {
		if got := Bundle(tc.v); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Bundle(%#v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestSplit(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want []piece
	}{
		{"", nil},
		{"// only a comment\n", nil},
		{"x := 1\ny := 2\n", []piece{{stmts, "x := 1\ny := 2\n", 1}}},
		{"x := 1\nx\n", []piece{{stmts, "x := 1\n", 1}, {final, "x", 2}}},
		{"x // note\n", []piece{{final, "x", 1}}},
		{"import \"fmt\"\nconst c = 1\nfmt.Println(c)\n", []piece{{decls, "import \"fmt\"\nconst c = 1\n", 1}, {final, "fmt.Println(c)", 3}}},
		{"func f() {}\nfunc (t T) M() {}\nfunc() {}()\n", []piece{{decls, "func f() {}\nfunc (t T) M() {}\n", 1}, {final, "func() {}()", 3}}},
		{"func(x int) { _ = x }(1); y := 2\n", []piece{{stmts, "func(x int) { _ = x }(1); y := 2\n", 1}}},
		{"x := []int{\n\t1,\n}\nx", []piece{{stmts, "x := []int{\n\t1,\n}\n", 1}, {final, "x", 4}}},
		{"x := 1; x", []piece{{stmts, "x := 1;", 1}, {final, " x", 1}}},
		{"type T int\nvar v T\nv = 2\n", []piece{{decls, "type T int\nvar v T\n", 1}, {stmts, "v = 2\n", 3}}},
		{"func (", []piece{{stmts, "func (", 1}}},
		{"func", []piece{{stmts, "func", 1}}},
	} {
		if got := split(tc.src); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("split(%q) = %+v, want %+v", tc.src, got, tc.want)
		}
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}
