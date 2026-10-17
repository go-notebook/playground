# Fit a distribution, then test the fit

Draw 500 observations from a normal distribution with a seeded generator — the same numbers NumPy's `default_rng(42)` gives — fit a normal distribution to them by maximum likelihood, and ask the Kolmogorov–Smirnov test whether the sample could come from the fitted law.

```go
import (
	"fmt"

	"github.com/go-ndarray/ndarray/random"
	"github.com/go-plot/plot"
	"github.com/go-statistics/stats"
)

x, _ := random.DefaultRNG(42).Normal(10, 2, 500)
stats.Describe(x, nil)
```

The maximum-likelihood fit: the sample mean, and the standard deviation with `ddof=0`.

```go
fit, err := stats.FitNormal(x, nil)
fmt.Printf("loc = %.4f, scale = %.4f\n", fit.Loc, fit.Scale)
```

The one-sample Kolmogorov–Smirnov test against the fitted CDF. A large p-value means the data give no reason to reject it (strictly, the parameters were estimated from the same data, which makes the test conservative).

```go
stats.KSTest1Samp(x, fit.CDF, nil)
```

The histogram of the sample, normalised to unit area, under the fitted density.

```go
xs := make([]float64, 200)
pdf := make([]float64, len(xs))
for i := range xs {
	xs[i] = 2 + 16*float64(i)/float64(len(xs)-1)
	pdf[i] = fit.PDF(xs[i])
}

fig := plot.NewFigure(plot.FigSize(6, 3.5))
ax := fig.AddAxes(0.12, 0.15, 0.83, 0.75)
ax.Hist(x, plot.Bins(30), plot.Density(), plot.Alpha(0.5), plot.Label("sample"))
ax.Plot(xs, pdf, plot.Label("fitted normal"))
ax.SetXLabel("x")
ax.Legend()
fig
```
