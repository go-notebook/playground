//go:build (js && ruby) || !(js || wasip1 || plan9)

package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-notebook/playground/internal/yaegi/interp"
)

func TestRubyCells(t *testing.T) {
	nb := "```go\nimport \"github.com/go-ndarray/ndarray/random\"\n\nxs, _ := random.DefaultRNG(1).Normal(3, 1, 50)\nn := 50\n```\n" +
		"```ruby\nfit = Stats::Normal.fit(xs)\nloc = fit.loc\ncount = n * 2\nputs \"n #{n}\"\nfit\n```\n" +
		"```go\nimport \"fmt\"\n\nfmt.Sprintf(\"%.3f %d %T\", loc, count, fit)\n```\n" +
		"```ruby\nfig, ax = Plot.subplots\nax.plot([1, 2], [loc, loc])\nlen = 3\nfig\n```\n"
	res := Run(nb)
	for _, r := range res {
		if r.Error != "" {
			t.Fatalf("cell %d: %s", r.Cell, r.Error)
		}
	}
	if res[1].Output != "n 50\n" || !strings.HasPrefix(res[1].Display[0]["text/plain"], "#<Stats::Normal loc=") {
		t.Fatalf("ruby cell: %+v", res[1])
	}
	if got := res[2].Display[0]["text/plain"]; !strings.HasSuffix(got, " 100 stats.Normal") {
		t.Fatalf("go cell sees %q", got)
	}
	if res[3].Display[0]["image/svg+xml"] == "" {
		t.Fatalf("figure: %+v", res[3].Display)
	}
}

func TestRubyErrors(t *testing.T) {
	for src, want := range map[string]string{
		"```ruby\nx = (\n```\n":                 "line 2:",
		"```ruby\na = 1\nb = a / 0\n```\n":      "line 2: divided by 0 (ZeroDivisionError)",
		"```ruby\ndef f = raise('x')\nf\n```\n": "line 1: x (RuntimeError)\n\tat line 1, in Object#f\n\tat line 2",
		// A Ruby local named as a Go variable already is.
		// A local whose value is nil, of a type of another package, of no
		// Go type (an Object), and named like a Go builtin.
		"```ruby\nz = nil\nsym = :a\no = Object.new\nlen = 1\nc = Complex(1, 2)\n```\n```go\nimport \"fmt\"\n\nfmt.Sprint(z, sym, c)\n```\n": "",
	} {
		res := Run(src)
		last := res[len(res)-1]
		if want == "" {
			continue
		}
		if !strings.HasPrefix(last.Error, want) {
			t.Errorf("%q: error %q, want %q", src, last.Error, want)
		}
	}
	if !RubySupported {
		t.Fatal("a native build runs Ruby")
	}
}

// A variable the interpreter refuses to declare fails the cell, saying so.
func TestRubyDeclareFails(t *testing.T) {
	saved := evaluate
	defer func() { evaluate = saved }()
	evaluate = func(i *interp.Interpreter, src string) (reflect.Value, error) {
		if strings.HasPrefix(src, "y := ") {
			return reflect.Value{}, errors.New("refused")
		}
		return saved(i, src)
	}
	res := Run("```ruby\ny = 1\n```\n")
	if res[0].Error != "y cannot be handed to Go: refused" {
		t.Fatalf("error %q", res[0].Error)
	}
}
