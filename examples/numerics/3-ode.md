# Solving an ODE

The Van der Pol oscillator, *x*″ = *μ*(1 − *x*²)*x*′ − *x*, written as a first-order system. With *μ* = 50 it is stiff: the explicit RK45 needs many small steps where the implicit Radau IIA method takes few.

```go
import (
	"fmt"

	"github.com/go-numerics/integrate"
	"github.com/go-plot/plot"
)

const mu = 50.0

vdp := func(t float64, y, dydt []float64) {
	dydt[0] = y[1]
	dydt[1] = mu*(1-y[0]*y[0])*y[1] - y[0]
}

span := [2]float64{0, 150}
y0 := []float64{2, 0}

rk, err := integrate.SolveIVP(vdp, span, y0, &integrate.IVPOptions{RTol: 1e-6, ATol: 1e-9})
fmt.Println("RK45: ", rk.NSteps, "steps,", rk.NFev, "evaluations")
```

```go
radau, err := integrate.SolveIVP(vdp, span, y0, &integrate.IVPOptions{Method: integrate.Radau, RTol: 1e-6, ATol: 1e-9})
fmt.Println("Radau:", radau.NSteps, "steps,", radau.NFev, "evaluations,", radau.NLU, "LU decompositions")
```

Both solutions, *x*(*t*):

```go
first := func(s *integrate.Solution) []float64 {
	out := make([]float64, len(s.Y))
	for i, y := range s.Y {
		out[i] = y[0]
	}
	return out
}

fig := plot.NewFigure(plot.FigSize(6, 3.5))
ax := fig.AddAxes(0.12, 0.15, 0.83, 0.75)
ax.Plot(rk.T, first(rk), plot.Label("RK45"))
ax.Plot(radau.T, first(radau), plot.Label("Radau"), plot.LineStyle("--"))
ax.SetXLabel("t")
ax.SetYLabel("x(t)")
ax.Legend()
fig
```
