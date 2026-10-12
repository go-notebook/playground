# A kernel density estimate

A sample from a mixture of two normal laws. A histogram depends on where the bins fall; a Gaussian kernel density estimate (`scipy.stats.gaussian_kde`, Scott's bandwidth) gives a smooth curve.

```go
import (
	"github.com/go-ndarray/ndarray"
	"github.com/go-ndarray/ndarray/random"
	"github.com/go-plot/plot"
	"github.com/go-statistics/stats"
)

g := random.DefaultRNG(3)
left, _ := g.Normal(-2, 0.7, 300)
right, _ := g.Normal(1.5, 1.2, 500)
x, _ := ndarray.Concatenate([]*ndarray.Array{left, right}, 0)
x.Size()
```

```go
kde, err := stats.NewGaussianKDE(x, nil)
kde.Factor()
```

```go
grid := make([]float64, 300)
for i := range grid {
	grid[i] = -5 + 10*float64(i)/float64(len(grid)-1)
}
density, err := kde.EvaluateSlice(grid)

fig := plot.NewFigure(plot.FigSize(6, 3.5))
ax := fig.AddAxes(0.12, 0.15, 0.83, 0.75)
ax.Hist(x, plot.Bins(40), plot.Density(), plot.Alpha(0.35), plot.Label("histogram"))
ax.Plot(grid, density, plot.Label("Gaussian KDE"))
ax.SetXLabel("x")
ax.Legend()
fig
```

The probability mass between the two modes, from the density itself:

```go
kde.IntegrateBox1D(-1, 0)
```
