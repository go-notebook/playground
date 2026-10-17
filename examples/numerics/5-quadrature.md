# Quadrature

`integrate.Quad` is QUADPACK, as `scipy.integrate.quad` calls it: adaptive Gauss–Kronrod on finite intervals, a change of variables on infinite ones.

∫₀^∞ e^(−*x*²) d*x* = √π / 2:

```go
import (
	"fmt"
	"math"

	"github.com/go-numerics/integrate"
	"github.com/go-numerics/special"
)

r, err := integrate.Quad(func(x float64) float64 { return math.Exp(-x * x) }, 0, math.Inf(1), nil)
fmt.Printf("%.15f  (error estimate %.1e, %d evaluations)\n", r.Value, r.AbsErr, r.NEval)
fmt.Printf("%.15f  exact\n", math.Sqrt(math.Pi)/2)
```

An integrable singularity at the origin: ∫₀¹ ln(*x*) / √*x* d*x* = −4.

```go
integrate.Quad(func(x float64) float64 { return math.Log(x) / math.Sqrt(x) }, 0, 1, nil)
```

The Bessel function against its integral representation, J₀(*x*) = (1/π) ∫₀^π cos(*x* sin θ) dθ:

```go
for _, x := range []float64{0.5, 2, 10} {
	q, _ := integrate.Quad(func(th float64) float64 { return math.Cos(x * math.Sin(th)) }, 0, math.Pi, nil)
	fmt.Printf("x = %4.1f   quad %.15f   special.J0 %.15f\n", x, q.Value/math.Pi, special.J0(x))
}
```
