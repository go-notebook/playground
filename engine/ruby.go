//go:build !js || ruby

package engine

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/go-notebook/notebook/analysis"
	"github.com/go-notebook/playground/internal/yaegi/interp"
	"github.com/go-scikit/ruby/cells"
)

// RubySupported reports whether this build runs Ruby cells. The page's
// default WebAssembly core does not: it is built without them, and a
// notebook with a Ruby cell loads the core built with the tag ruby instead,
// so that a visitor who runs only Go never downloads the Ruby. Native builds
// always have it.
const RubySupported = true

// rubyPkg is the package the session gives the interpreter for reading the
// variables Ruby cells define.
const rubyPkg = "playground.invalid/ruby"

// rubySide is the Ruby half of a session: one interpreter for every Ruby
// cell of the notebook, and the values its cells handed to Go.
type rubySide struct {
	rs      *cells.Session
	values  map[string]any
	imports map[string]string // import path -> the name the Go side imports it as
}

// rubyCell runs a Ruby cell. The Go variables it reads are set as Ruby
// locals first, and the top-level locals it assigns become Go variables of
// the cells after it, typed from their values, as in go-notebook.
func (s *session) rubyCell(src string) (output string, shown []map[string]string, errText string) {
	s.out.Reset()
	s.shown = nil
	if s.ruby == nil {
		s.ruby = &rubySide{rs: cells.NewWriting(&s.out, &s.out), values: map[string]any{}, imports: map[string]string{}}
		mustUse(s.i, interp.Exports{rubyPkg + "/ruby": {"Get": reflect.ValueOf(func(name string) any { return s.ruby.values[name] })}})
		mustEval(s.i, `import __ruby "`+rubyPkg+`"`)
	}
	a := analysis.AnalyzeRubyCell(0, src)
	if len(a.Errors) > 0 {
		return "", nil, rubyLines(a.Errors[0].Msg)
	}
	c := cells.Cell{Source: src, File: "cell", Line: 1, Display: a.RubyDisplay}
	globals := s.i.Globals()
	for _, u := range a.Uses {
		if g, ok := globals[u]; ok && s.goVars[u] {
			v := g.Interface()
			c.In = append(c.In, cells.Var{Name: u, Ptr: &v})
		}
	}
	outs := map[string]*any{}
	for _, n := range a.Defines {
		if !slices.Contains(a.Funcs, n) && !slices.Contains(a.Consts, n) {
			p := new(any)
			outs[n] = p
			c.Out = append(c.Out, cells.Var{Name: n, Ptr: p})
		}
	}
	types := map[string]string{}
	if err := s.runRuby(c, types); err != "" {
		return s.out.String(), s.shown, err
	}
	for _, v := range c.Out {
		if !analysis.GoName(v.Name) {
			continue // a Ruby local named like a Go keyword stays Ruby's
		}
		s.ruby.values[v.Name] = *outs[v.Name]
		if err := s.declare(v.Name, types[v.Name]); err != nil {
			return s.out.String(), s.shown, err.Error()
		}
	}
	return s.out.String(), s.shown, ""
}

// runRuby runs the cell, recording the Go type of each variable it assigns,
// and returns its error text.
func (s *session) runRuby(c cells.Cell, types map[string]string) (errText string) {
	defer func() {
		if r := recover(); r != nil {
			// A *cells.Error, which prints as the cell's failure.
			errText = rubyLines(fmt.Sprint(r))
		}
	}()
	s.ruby.rs.Run(c, func(vs ...any) {
		for _, v := range vs {
			s.shown = append(s.shown, Bundle(v))
		}
	}, func(name, typ string) { types[name] = typ })
	return ""
}

var (
	pkgRef   = regexp.MustCompile(`\{([^{}]+)\}`)
	mainRef  = regexp.MustCompile(`\tfrom cell:(\d+):in '<main>'`)
	frameRef = regexp.MustCompile(`\tfrom cell:(\d+):in '([^']*)'`)
)

// declare makes a Ruby variable a Go variable of the type cells.GoType gave
// it, for the cells after it. A type of a package the playground does not
// have, or any, is declared any.
func (s *session) declare(name, typ string) error {
	t := pkgRef.ReplaceAllStringFunc(typ, func(m string) string {
		path := m[1 : len(m)-1]
		if !available(path) {
			typ = "any"
			return ""
		}
		n, ok := s.ruby.imports[path]
		if !ok {
			n = fmt.Sprintf("__rb%d", len(s.ruby.imports))
			s.ruby.imports[path] = n
			mustEval(s.i, fmt.Sprintf("import %s %q", n, path))
		}
		return n
	})
	src := fmt.Sprintf("%s := __ruby.Get(%q).(%s)", name, name, t)
	if typ == "any" || s.ruby.values[name] == nil {
		src = fmt.Sprintf("%s := __ruby.Get(%q)", name, name)
	}
	if err := s.eval(src); err != nil {
		return fmt.Errorf("%s cannot be handed to Go: %v", name, err)
	}
	s.goVars[name] = false // a Ruby variable: the Ruby side holds it already
	return nil
}

// rubyLines writes a Ruby error's positions as the page writes Go's: "line
// 3: …", and "at line 3" for each frame of its backtrace in the cell.
func rubyLines(msg string) string {
	msg = strings.TrimPrefix(msg, "parse error at ")
	msg = frameRef.ReplaceAllString(msg, "\tat line $1, in $2")
	msg = mainRef.ReplaceAllString(msg, "\tat line $1")
	if strings.HasPrefix(msg, "cell:") {
		msg = "line " + strings.TrimPrefix(msg, "cell:")
	}
	return msg
}
