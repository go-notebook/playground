# Finding roots

Where does *cos x* cross *x*? Brent's method needs a bracket, an interval where the function changes sign; Newton's method needs a starting point and, if given, the derivative.

```go
import (
	"fmt"
	"math"

	"github.com/go-numerics/optimize"
)

f := func(x float64) float64 { return math.Cos(x) - x }
r, err := optimize.Brentq(f, 0, 1, nil)
fmt.Printf("root %.15f after %d iterations, f(root) = %.1e\n", r.Root, r.Iterations, f(r.Root))
```

Newton's method with the derivative converges quadratically.

```go
df := func(x float64) float64 { return -math.Sin(x) - 1 }
n, err := optimize.RootScalar(f, "newton", &optimize.RootScalarOptions{Fprime: df, X0: 1, X0Set: true})
fmt.Printf("root %.15f after %d iterations\n", n.Root, n.Iterations)
```

A system: the intersection of the circle *x*² + *y*² = 4 and the hyperbola *xy* = 1, by MINPACK's hybrid method (`scipy.optimize.root`).

```go
sys := optimize.SystemProblem{
	Func: func(x, out []float64) {
		out[0] = x[0]*x[0] + x[1]*x[1] - 4
		out[1] = x[0]*x[1] - 1
	},
}
sol, err := optimize.Root(sys, []float64{2, 0.5}, nil)
sol.X
```
