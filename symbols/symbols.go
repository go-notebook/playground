// Package symbols holds the yaegi symbol tables of every package a
// playground notebook may import: a subset of the standard library, and the
// go-statistics, go-numerics, go-ndarray and go-plot packages.
//
// The tables are made by `yaegi extract`, which leaves out generic functions:
// a generic function has no single value to export until it is instantiated.
// generics.go puts back the ones a notebook needs, instantiated for
// []float64 and, where a parameter is the data being described, accepting a
// *ndarray.Array as well (see Data in generics.go).
//
// Run `go generate ./symbols` after a dependency upgrade; a test fails while
// the committed tables are not the ones the generators write.
package symbols

import (
	"fmt"
	"path"
	"reflect"
	"sort"
)

//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract -name symbols bufio bytes errors fmt io math math/big math/bits math/cmplx math/rand math/rand/v2 sort strconv strings text/tabwriter time unicode unicode/utf8
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract -name symbols github.com/go-statistics/stats
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract -name symbols github.com/go-numerics/special github.com/go-numerics/optimize github.com/go-numerics/integrate
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract -name symbols github.com/go-ndarray/ndarray github.com/go-ndarray/ndarray/random github.com/go-ndarray/ndarray/fft
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract -name symbols github.com/go-plot/plot
//go:generate go run ../internal/genwrap -o zz_generics.go

// Symbols maps "import/path/name" to the package's exported symbols, as
// interp.Interpreter.Use takes them.
var Symbols = map[string]map[string]reflect.Value{}

// MapTypes tells yaegi which native functions take an interface{} whose
// value may be an interpreted type implementing fmt.Stringer or
// fmt.Formatter, so that fmt.Println(v) calls an interpreted String method.
var MapTypes = map[reflect.Value][]reflect.Type{}

func init() {
	mt := []reflect.Type{
		reflect.TypeFor[fmt.Formatter](),
		reflect.TypeFor[fmt.Stringer](),
	}
	for _, f := range []any{
		fmt.Errorf, fmt.Fprint, fmt.Fprintf, fmt.Fprintln,
		fmt.Print, fmt.Printf, fmt.Println,
		fmt.Sprint, fmt.Sprintf, fmt.Sprintln,
	} {
		MapTypes[reflect.ValueOf(f)] = mt
	}
	Symbols["."] = map[string]reflect.Value{"MapTypes": reflect.ValueOf(MapTypes)}
}

// sourcePackages are compiled by yaegi from its own copy of their source
// (github.com/traefik/yaegi/stdlib/generic), since they are all generic:
// cmp, maps and slices. A table of symbols for them, which would be empty,
// would hide that source.
var sourcePackages = []string{"cmp", "maps", "slices"}

// Packages returns the import paths a notebook may import, sorted.
func Packages() []string {
	out := append([]string(nil), sourcePackages...)
	for k := range Symbols {
		if k == "." {
			continue
		}
		out = append(out, path.Dir(k))
	}
	sort.Strings(out)
	return out
}
