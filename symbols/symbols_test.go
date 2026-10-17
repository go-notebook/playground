package symbols

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/go-ndarray/ndarray"
)

// TestGenerics calls every wrapper zz_generics.go defines, with arguments
// of the types its signature names, so that each one is known to reach its
// stats function: a wrapper that does not exist, or calls the wrong
// function, is found here and not by a visitor.
func TestGenerics(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "zz_generics.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		if ix, ok := n.(*ast.IndexExpr); ok {
			if id, ok := ix.X.(*ast.Ident); ok && id.Name == "m" {
				s, _ := strconv.Unquote(ix.Index.(*ast.BasicLit).Value)
				names = append(names, s)
			}
		}
		return true
	})
	if len(names) < 80 {
		t.Fatalf("%d wrappers, want the 80 or so generic functions of stats", len(names))
	}
	m := Symbols["github.com/go-statistics/stats/stats"]
	for _, name := range names {
		fn := m[name]
		if !fn.IsValid() || fn.Kind() != reflect.Func {
			t.Errorf("%s: not a function in the table", name)
			continue
		}
		args := make([]reflect.Value, fn.Type().NumIn())
		for i := range args {
			args[i] = sample(fn.Type().In(i))
		}
		func() {
			defer func() { _ = recover() }() // a bad sample may panic in stats; the wrapper ran
			if fn.Type().IsVariadic() {
				fn.CallSlice(args)
			} else {
				fn.Call(args)
			}
		}()
	}
	// One of them, checked for its value: the generic Mean on an array.
	a, _ := ndarray.FromSlice([]int64{1, 2, 3, 4}, 2, 2)
	out := m["Mean"].Call([]reflect.Value{reflect.ValueOf(any(a)), reflect.Zero(m["Mean"].Type().In(1))})
	if got := out[0].Float(); got != 2.5 {
		t.Errorf("Mean of an int array = %v, want 2.5", got)
	}
}

// sample makes an argument of type t.
func sample(t reflect.Type) reflect.Value {
	data := []float64{1.5, 2, 3.25, 4, 5.5, 7, 8.5}
	switch {
	case t.Kind() == reflect.Interface:
		v := reflect.New(t).Elem()
		v.Set(reflect.ValueOf(data))
		return v
	case t == reflect.TypeFor[[]any]():
		return reflect.ValueOf([]any{data, data})
	case t == reflect.TypeFor[[]float64]():
		return reflect.ValueOf(data)
	case t == reflect.TypeFor[[][]float64]():
		return reflect.ValueOf([][]float64{data})
	case t.Kind() == reflect.Func:
		return reflect.MakeFunc(t, func([]reflect.Value) []reflect.Value {
			out := make([]reflect.Value, t.NumOut())
			for i := range out {
				out[i] = reflect.New(t.Out(i)).Elem()
			}
			if t.NumOut() == 1 && t.Out(0).Kind() == reflect.Float64 {
				out[0] = reflect.ValueOf(0.5).Convert(t.Out(0))
			}
			return out
		})
	case t.Kind() == reflect.Float64:
		return reflect.ValueOf(0.5).Convert(t)
	case t.Kind() == reflect.Int:
		return reflect.ValueOf(2).Convert(t)
	}
	return reflect.New(t).Elem() // nil options, zero values
}

func TestData(t *testing.T) {
	ints, _ := ndarray.FromSlice([]int64{1, 2}, 2)
	floats, _ := ndarray.FromSlice([]float64{1, 2}, 2)
	for _, tc := range []struct {
		in   any
		want []float64
	}{
		{nil, nil},
		{[]float64{1, 2}, []float64{1, 2}},
		{(*ndarray.Array)(nil), nil},
		{ints, []float64{1, 2}},
		{floats, []float64{1, 2}},
		{[]int{1, 2}, []float64{1, 2}},
	} {
		if got := Data(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("Data(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if got := DataAll([]any{[]float64{1}, []int{2}}); len(got) != 2 || got[1][0] != 2 {
		t.Errorf("DataAll = %v", got)
	}
	defer func() {
		if r := recover(); r != "stats: the data is a string; want a []float64 or a *ndarray.Array" {
			t.Errorf("Data(string) panicked with %v", r)
		}
	}()
	Data("x")
}

func TestFromSlice(t *testing.T) {
	for _, in := range []any{
		[]float64{1, 2}, []float32{1, 2}, []int{1, 2}, []int64{1, 2},
		[]int32{1, 2}, []complex128{1, 2}, []bool{true, false},
	} {
		a, err := FromSlice(in, 2)
		if err != nil || a.Size() != 2 {
			t.Errorf("FromSlice(%T) = %v, %v", in, a, err)
		}
	}
	if _, err := FromSlice([]string{"a"}, 1); err == nil {
		t.Error("FromSlice([]string) did not fail")
	}
	a, _ := FromSlice([]float64{1, 2.5}, 2)
	if got := Symbols["github.com/go-ndarray/ndarray/ndarray"]["Item"].Interface().(func(*ndarray.Array, ...int) float64)(a, 1); got != 2.5 {
		t.Errorf("Item = %v", got)
	}
}

func TestFloatConstants(t *testing.T) {
	if v := Symbols["math/math"]["Pi"]; v.Kind() != reflect.Float64 || v.Float() != math.Pi {
		t.Errorf("math.Pi is a %v, %v", v.Type(), v)
	}
	if v := Symbols["math/math"]["MaxInt8"]; v.Kind() == reflect.Float64 {
		t.Error("an integer constant became a float64")
	}
}

func TestPackages(t *testing.T) {
	ps := Packages()
	for _, want := range []string{"fmt", "math", "github.com/go-statistics/stats", "github.com/go-plot/plot", "github.com/go-ndarray/ndarray/random"} {
		if !slices.Contains(ps, want) {
			t.Errorf("Packages() lacks %s: %v", want, ps)
		}
	}
	if slices.Contains(ps, ".") || !slices.IsSorted(ps) {
		t.Errorf("Packages() = %v", ps)
	}
}

func TestPkg(t *testing.T) {
	m := pkg("example.invalid/x/x")
	defer delete(Symbols, "example.invalid/x/x")
	if m == nil || Symbols["example.invalid/x/x"] == nil {
		t.Error("pkg did not make the table")
	}
}

// TestInterfaceWrappers calls each method of the interface wrappers yaegi
// extract writes (_fmt_Stringer, ...), which a type the notebook declares
// goes through when compiled code calls it: each must reach the function
// the interpreter puts in its W field.
func TestInterfaceWrappers(t *testing.T) {
	n := 0
	for path, m := range Symbols {
		for name, v := range m {
			if len(name) == 0 || name[0] != '_' || v.Kind() != reflect.Pointer || v.Type().Elem().Kind() != reflect.Struct {
				continue
			}
			w := reflect.New(v.Type().Elem()).Elem()
			called := 0
			for i := 0; i < w.NumField(); i++ {
				f := w.Field(i)
				if f.Kind() != reflect.Func {
					continue
				}
				ft := f.Type()
				f.Set(reflect.MakeFunc(ft, func([]reflect.Value) []reflect.Value {
					called++
					out := make([]reflect.Value, ft.NumOut())
					for k := range out {
						out[k] = reflect.New(ft.Out(k)).Elem()
					}
					return out
				}))
			}
			for i := 0; i < w.NumMethod(); i++ {
				meth := w.Method(i)
				args := make([]reflect.Value, meth.Type().NumIn())
				for k := range args {
					args[k] = reflect.New(meth.Type().In(k)).Elem()
				}
				before := called
				if meth.Type().IsVariadic() {
					meth.CallSlice(args)
				} else {
					meth.Call(args)
				}
				if called != before+1 {
					t.Errorf("%s.%s: %s did not call its function", path, name, w.Type().Method(i).Name)
				}
				n++
			}
		}
	}
	if n < 100 {
		t.Errorf("called %d wrapper methods, want the hundred or so yaegi extract writes", n)
	}
}

// An interpreted type without its String method still prints, as "".
func TestStringerWithoutMethod(t *testing.T) {
	var s _fmt_Stringer
	if got := s.String(); got != "" {
		t.Errorf("String() = %q", got)
	}
}
