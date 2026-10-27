// Package engine runs a Go notebook in an interpreter: the playground's
// core, the same in the browser (compiled to WebAssembly) and in tests.
//
// A notebook is a Markdown document whose ```go blocks are cells, as
// github.com/go-notebook/notebook/format reads it. The cells run in document
// order, in one yaegi interpreter, so that what a cell defines is visible to
// the cells after it. The packages a cell may import are those of
// github.com/go-notebook/playground/symbols; their code is compiled, not
// interpreted, so a call into go-statistics or go-plot runs at native (or
// WebAssembly) speed and only the notebook's own code is interpreted.
//
// The conventions are go-notebook's: a cell's final expression is shown; a
// final call returning (value, error) shows the value, or fails the cell
// with the error; a value that draws itself as SVG — a go-plot figure — is
// shown as a figure.
package engine

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing/fstest"
	"time"

	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/notebook/format"
	"github.com/go-notebook/playground/internal/yaegi/interp"

	"github.com/go-notebook/playground/symbols"
)

// Result is what one code cell did.
type Result struct {
	// Cell is the cell's index among the notebook's code cells.
	Cell int `json:"cell"`
	// Line is the line of the document where the cell's code starts.
	Line int `json:"line"`
	// Output is what the cell printed.
	Output string `json:"output,omitempty"`
	// Display holds what the cell showed, each value as a MIME bundle:
	// "text/plain", and "image/svg+xml" or "text/html" when the value has
	// that representation ("image/png" is base64).
	Display []map[string]string `json:"display,omitempty"`
	// Error is why the cell failed, with the line in the cell.
	Error string `json:"error,omitempty"`
	// Skipped reports a cell that did not run because one before it
	// failed.
	Skipped bool `json:"skipped,omitempty"`
	// Millis is how long the cell ran.
	Millis float64 `json:"ms"`
}

// Cell is a cell of a notebook, as the page shows it.
type Cell struct {
	// Kind is "prose" (Markdown) or "code".
	Kind string `json:"kind"`
	// Lang is a code cell's language: "go", "ruby" or "julia".
	Lang string `json:"lang,omitempty"`
	// Source is the cell's text, without the fences of a code cell.
	Source string `json:"source"`
}

// Cells splits a notebook into its cells, as go-notebook does.
func Cells(markdown string) []Cell {
	var out []Cell
	for _, c := range format.Parse(markdown).Cells {
		k := "prose"
		if c.Kind == format.Code {
			k = "code"
		}
		out = append(out, Cell{Kind: k, Lang: c.Lang, Source: c.Source})
	}
	return out
}

// Run runs every code cell of a notebook in order and returns what each one
// did. A cell that fails stops the run: the cells after it are skipped, as
// their inputs may not be there.
func Run(markdown string) []Result {
	var cells []format.Cell
	for _, c := range format.Parse(markdown).Cells {
		if c.Kind == format.Code {
			cells = append(cells, c)
		}
	}
	s := newSession()
	out := make([]Result, len(cells))
	blocked := juliaBlocked(cells)
	failed := false
	for i, c := range cells {
		out[i] = Result{Cell: i, Line: c.Line}
		if failed {
			out[i].Skipped = true
			continue
		}
		// A Julia cell is checked, never run, and so are the cells that read
		// it: each says why, and the others run.
		if c.Lang == analysis.Julia {
			out[i].Error = juliaCell(c.Source)
			continue
		}
		if j, ok := blocked[i]; ok {
			out[i].Error = analysis.JuliaBlocked(j)
			continue
		}
		start := time.Now()
		if c.Lang == analysis.Ruby {
			out[i].Output, out[i].Display, out[i].Error = s.rubyCell(c.Source)
		} else {
			out[i].Output, out[i].Display, out[i].Error = s.cell(c.Source)
			s.goDefines(c.Source)
		}
		out[i].Millis = float64(time.Since(start).Microseconds()) / 1000
		failed = out[i].Error != ""
	}
	return out
}

// session is one run of a notebook: an interpreter, and where the running
// cell's output goes.
type session struct {
	i     *interp.Interpreter
	out   bytes.Buffer // what the cell prints
	trace bytes.Buffer // where a panic went through, as yaegi prints it
	shown []map[string]string

	// goVars are the variables the Go cells run so far define, which a Ruby
	// cell can read; ruby is the Ruby side of the session (ruby.go), made
	// by the first Ruby cell.
	goVars map[string]bool
	ruby   *rubySide
}

// goDefines records the variables a Go cell defines: what the notebook
// analysis says it defines, but its funcs and types.
func (s *session) goDefines(src string) {
	c := analysis.AnalyzeCell(0, src)
	for _, n := range c.Defines {
		if !slices.Contains(c.Funcs, n) && !slices.Contains(c.Types, n) {
			s.goVars[n] = true
		}
	}
}

// showPkg is the import path of the package the session gives the
// interpreter for showing the final expression; nothing else imports it.
const showPkg = "playground.invalid/show"

func newSession() *session {
	s := &session{goVars: map[string]bool{}}
	s.i = interp.New(interp.Options{
		Stdout: &s.out,
		Stderr: &s.trace,
		// No source on disk: an import is a package of symbols or nothing.
		SourcecodeFilesystem: fstest.MapFS{},
	})
	mustUse(s.i, symbols.Symbols)
	// MapTypes makes the interpreter hand Show a value of a type the
	// notebook declares with its String method callable from Go.
	show := reflect.ValueOf(s.show)
	mustUse(s.i, interp.Exports{
		showPkg + "/show": {"Show": show},
		".": {"MapTypes": reflect.ValueOf(map[reflect.Value][]reflect.Type{
			show: {reflect.TypeFor[fmt.Stringer]()},
		})},
	})
	mustEval(s.i, `import __show "`+showPkg+`"`)
	return s
}

// mustUse and mustEval set up an interpreter, which cannot fail with the
// symbols and the source this package gives it.
func mustUse(i *interp.Interpreter, e interp.Exports) {
	if err := i.Use(e); err != nil {
		panic(err)
	}
}

func mustEval(i *interp.Interpreter, src string) {
	if _, err := i.Eval(src); err != nil {
		panic(err)
	}
}

// show records the cell's final expression: one value, or the results of a
// call. (value, error), with a value that is not an int, shows the value or
// fails the cell with the error; other calls with several results —
// fmt.Println's (n, err) — are run for their effect. A nil shows nothing.
func (s *session) show(vs ...any) {
	if len(vs) == 2 {
		err, isErr := vs[1].(error)
		if isErr || vs[1] == nil {
			if _, isInt := vs[0].(int); !isInt {
				if err != nil {
					panic(cellError{err})
				}
				vs = vs[:1]
			}
		}
	}
	if len(vs) != 1 || vs[0] == nil {
		return
	}
	s.shown = append(s.shown, Bundle(vs[0]))
}

// cellError is the error of a final (value, error) call, raised as a panic
// and reported as the cell's error.
type cellError struct{ err error }

// cell runs one cell's source.
func (s *session) cell(src string) (output string, shown []map[string]string, errText string) {
	s.out.Reset()
	s.shown = nil
	for _, p := range split(src) {
		if err := s.piece(p); err != nil {
			return s.out.String(), s.shown, err.Error()
		}
	}
	return s.out.String(), s.shown, ""
}

// piece runs one piece of a cell. Blank lines before the source put it on
// its own lines of the cell, so that the interpreter's positions are the
// cell's.
func (s *session) piece(p piece) error {
	pad := strings.Repeat("\n", p.line-1)
	if p.kind == decls {
		if err := checkImports(p.src, p.line); err != nil {
			return err
		}
	}
	if p.kind == final {
		// The final expression, shown: Show takes a value or the results
		// of a call. A call with no result cannot be an argument; it runs
		// as a statement.
		err := s.eval(pad + "__show.Show(" + p.src + ")")
		var panicked interp.Panic
		if err == nil || errors.As(err, &panicked) {
			return s.describe(err)
		}
		// It did not compile as an argument: a call with no result (or
		// another mistake, which compiling it alone reports). Nothing ran.
	}
	return s.describe(s.eval(pad + p.src))
}

// eval is the interpreter's Eval, which also reports the interpreter's own
// panics: yaegi has defects that crash it on some valid programs, and a
// crash would take the page's worker down with it.
func (s *session) eval(src string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the interpreter failed on this code (a yaegi defect): %v", r)
		}
	}()
	s.trace.Reset()
	_, err = evaluate(s.i, src)
	return err
}

// evaluate is the interpreter's Eval; a test makes it crash.
var evaluate = (*interp.Interpreter).Eval

// position is a position yaegi prints: "_.go:3:5: " or "3:5: ".
var position = regexp.MustCompile(`(?:_\.go:)?(\d+):(\d+): `)

// frame is a line yaegi prints for each call a panic unwinds: "3:5: panic:
// main.f(...)".
var frame = regexp.MustCompile(`(?m)^(?:_\.go:)?(\d+):\d+: panic: (?:main\.)?(\S+)\(\.\.\.\)$`)

// describe turns an interpreter error into the cell's error text: "line
// 3: undefined: x". A panic gives its value and the lines of the calls it
// went through, innermost first.
func (s *session) describe(err error) error {
	if err == nil {
		return nil
	}
	var p interp.Panic
	if errors.As(err, &p) {
		if ce, ok := p.Value.(cellError); ok {
			return fmt.Errorf("error: %v", ce.err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "panic: %v", p.Value)
		for _, m := range frame.FindAllStringSubmatch(s.trace.String(), -1) {
			if m[2] == "main" {
				fmt.Fprintf(&b, "\n\tat line %s", m[1])
			} else {
				fmt.Fprintf(&b, "\n\tat line %s, in %s", m[1], m[2])
			}
		}
		return errors.New(b.String())
	}
	msg := err.Error()
	if m := position.FindStringSubmatchIndex(msg); m != nil && m[0] == 0 {
		msg = "line " + msg[m[2]:m[3]] + ": " + msg[m[1]:]
	}
	return errors.New(msg)
}

// checkImports reports, at its line, an import of a package the playground
// does not have, which yaegi would only say it cannot find.
func checkImports(src string, line int) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", "package p\n"+src, parser.ImportsOnly)
	if err != nil {
		return nil // not valid: the interpreter says why
	}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		if !available(path) {
			return fmt.Errorf("line %d: package %s is not in the playground; it has %s",
				line+fset.Position(im.Pos()).Line-2, path, strings.Join(symbols.Packages(), ", "))
		}
	}
	return nil
}

// available reports whether the playground has the package.
func available(path string) bool {
	i := sort.SearchStrings(packages, path)
	return i < len(packages) && packages[i] == path
}

var packages = symbols.Packages()

// svgWriter is what a go-plot figure is: something that writes itself as
// SVG.
type svgWriter interface {
	SaveSVG(w io.Writer) error
}

// displayer is go-notebook's display contract.
type displayer interface {
	Display() map[string][]byte
}

// Bundle returns the MIME bundle of a value. A figure is drawn as SVG only
// (Display would also rasterise it to PNG, which the page does not need);
// a displayer gives its own bundle, PNG in base64; anything else is its
// text. text/plain is always there.
func Bundle(v any) map[string]string {
	b := map[string]string{}
	switch d := v.(type) {
	case svgWriter:
		var buf bytes.Buffer
		if err := d.SaveSVG(&buf); err != nil {
			b["text/plain"] = fmt.Sprintf("<%T: %v>", v, err)
			return b
		}
		b["image/svg+xml"] = buf.String()
	case displayer:
		for mime, data := range d.Display() {
			if mime == "image/png" {
				b[mime] = base64.StdEncoding.EncodeToString(data)
			} else {
				b[mime] = string(data)
			}
		}
	case fmt.Stringer, error:
		b["text/plain"] = fmt.Sprintf("%v", v)
	default:
		b["text/plain"] = text(v)
	}
	if _, ok := b["text/plain"]; !ok {
		b["text/plain"] = fmt.Sprintf("<%T>", v)
	}
	// A figure reaches the page only as SanitizeSVG writes it back.
	if svg, ok := b["image/svg+xml"]; ok {
		clean, err := SanitizeSVG(svg)
		if err != nil {
			delete(b, "image/svg+xml")
			b["text/plain"] = fmt.Sprintf("<%T: %v>", v, err)
		} else {
			b["image/svg+xml"] = clean
		}
	}
	return b
}

// text is the %v of a value, but for a struct, or a pointer to one, without
// a String method: its exported fields, named, as {Statistic:2.1
// PValue:0.03}. A test result shows what it holds and not its internals.
func text(v any) string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Sprintf("%v", v)
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		if b.Len() > 1 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s:%v", f.Name, rv.Field(i))
	}
	b.WriteByte('}')
	return b.String()
}
