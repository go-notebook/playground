# Fitting a curve

Noisy measurements of a damped oscillation *y* = *a* e^(−*bt*) cos(*ωt*). `optimize.CurveFit` is `scipy.optimize.curve_fit`: Levenberg–Marquardt from a starting guess, with the covariance of the parameters.

```go
import (
	"fmt"
	"math"

	"github.com/go-ndarray/ndarray"
	"github.com/go-ndarray/ndarray/random"
	"github.com/go-numerics/optimize"
	"github.com/go-plot/plot"
)

model := func(t float64, p []float64) float64 {
	return p[0] * math.Exp(-p[1]*t) * math.Cos(p[2]*t)
}

t, _ := ndarray.Linspace(0, 6, 80)
clean := t.Map(func(v float64) float64 { return model(v, []float64{2.5, 0.4, 3}) })
noise, _ := random.DefaultRNG(11).Normal(0, 0.1, 80)
y, _ := clean.Add(noise)
```

```go
fit, err := optimize.CurveFit(model, t, y, []float64{2, 0.5, 2.8}, nil)
perr := fit.Perr()
for i, name := range []string{"a", "b", "ω"} {
	fmt.Printf("%s = %.4f ± %.4f\n", name, fit.Popt[i], perr[i])
}
```

The data and the fitted curve.

```go
tt, _ := ndarray.Linspace(0, 6, 400)
yy := tt.Map(func(v float64) float64 { return model(v, fit.Popt) })

fig := plot.NewFigure(plot.FigSize(6, 3.5))
ax := fig.AddAxes(0.12, 0.15, 0.83, 0.75)
ax.Scatter(t, y, plot.Label("data"))
ax.Plot(tt, yy, plot.Label("fit"))
ax.SetXLabel("t")
ax.Legend()
fig
```
