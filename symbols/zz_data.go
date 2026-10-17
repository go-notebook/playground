package symbols

import (
	"fmt"
	"reflect"

	"github.com/go-ndarray/ndarray"
)

// Data converts what a notebook passes as observations to the []float64 a
// stats function instantiated for []float64 takes: a []float64 as it is, a
// *ndarray.Array of any shape and dtype read in row-major order as float64
// (what stats does with an array), a []int converted, nil as nil. Anything
// else panics, as a stats call with the wrong type would not compile.
func Data(x any) []float64 {
	switch v := x.(type) {
	case nil:
		return nil
	case []float64:
		return v
	case *ndarray.Array:
		if v == nil {
			return nil
		}
		if v.DType() != ndarray.Float64 {
			v = v.AsType(ndarray.Float64)
		}
		f, _ := ndarray.Data[float64](v)
		return f
	case []int:
		f := make([]float64, len(v))
		for i, e := range v {
			f[i] = float64(e)
		}
		return f
	}
	panic(fmt.Sprintf("stats: the data is a %T; want a []float64 or a *ndarray.Array", x))
}

// DataAll is Data on each of xs.
func DataAll(xs []any) [][]float64 {
	out := make([][]float64, len(xs))
	for i, x := range xs {
		out[i] = Data(x)
	}
	return out
}

// pkg returns the symbol table of key, made if it is not there yet, so that
// the order in which the init functions run does not matter. (The tables
// yaegi extract writes replace the map, so the files adding to them are
// named to initialise after them: files initialise in name order.)
func pkg(key string) map[string]reflect.Value {
	m := Symbols[key]
	if m == nil {
		m = map[string]reflect.Value{}
		Symbols[key] = m
	}
	return m
}

// The generic functions of ndarray, for float64; FromSlice takes a slice of
// any element type an array can hold, and a []int as int64.
func init() {
	m := pkg("github.com/go-ndarray/ndarray/ndarray")
	m["FromSlice"] = reflect.ValueOf(FromSlice)
	m["Data"] = reflect.ValueOf(ndarray.Data[float64])
	m["Item"] = reflect.ValueOf(ndarray.Item[float64])
}

// FromSlice is ndarray.FromSlice for a []float64, []float32, []int64,
// []int32, []complex128 or []bool, and for a []int, made an int64 array.
func FromSlice(data any, shape ...int) (*ndarray.Array, error) {
	switch d := data.(type) {
	case []float64:
		return ndarray.FromSlice(d, shape...)
	case []float32:
		return ndarray.FromSlice(d, shape...)
	case []int:
		i := make([]int64, len(d))
		for k, v := range d {
			i[k] = int64(v)
		}
		return ndarray.FromSlice(i, shape...)
	case []int64:
		return ndarray.FromSlice(d, shape...)
	case []int32:
		return ndarray.FromSlice(d, shape...)
	case []complex128:
		return ndarray.FromSlice(d, shape...)
	case []bool:
		return ndarray.FromSlice(d, shape...)
	}
	return nil, fmt.Errorf("ndarray.FromSlice: cannot make an array of a %T", data)
}
