package symbols

import (
	"go/constant"
	"reflect"
)

// yaegi keeps a float constant of a compiled package (math.Pi) as an
// untyped constant.Value, and then refuses to mix it with an untyped float of
// the notebook's own: math.Pi * 50.0, or 2 * f with const f = 50.0, are
// "mismatched types untyped float64 and untyped float". A float64 value
// mixes with everything a notebook writes — math.Pi * 50.0, const tau = 2 *
// math.Pi, float32(math.Pi) — so the float constants are given as float64,
// which is what Go makes of them in any expression without another float
// type. This init runs after the tables are filled (files initialise in name
// order).
func init() {
	for _, m := range Symbols {
		for name, v := range m {
			if c, ok := v.Interface().(constant.Value); ok && c.Kind() == constant.Float {
				f, _ := constant.Float64Val(c)
				m[name] = reflect.ValueOf(f)
			}
		}
	}
}
