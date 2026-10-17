# A bootstrap confidence interval

How precise is the median of a skewed sample? There is no simple formula, so resample: draw the sample again with replacement thousands of times, compute the median each time, and read the interval off the spread. This is `scipy.stats.bootstrap`, BCa interval by default.

```go
import (
	"fmt"

	"github.com/go-ndarray/ndarray"
	"github.com/go-ndarray/ndarray/random"
	"github.com/go-plot/plot"
	"github.com/go-statistics/stats"
)

g := random.DefaultRNG(2026)
arr, _ := g.Exponential(3.0, 200)
sample, _ := ndarray.Data(arr)
stats.Median(sample, nil)
```

The statistic is a plain Go function of the resampled data.

```go
median := func(s ...[]float64) float64 {
	m, _ := stats.Median(s[0], nil)
	return m
}

boot, err := stats.Bootstrap([][]float64{sample}, median, random.DefaultRNG(1), &stats.BootstrapOptions{NResamples: 2000})
fmt.Printf("95%% CI for the median: [%.3f, %.3f], standard error %.3f\n",
	boot.ConfidenceInterval.Low, boot.ConfidenceInterval.High, boot.StandardError)
```

The bootstrap distribution, with the interval marked.

```go
fig := plot.NewFigure(plot.FigSize(6, 3.5))
ax := fig.AddAxes(0.12, 0.15, 0.83, 0.75)
ax.Hist(boot.BootstrapDistribution, plot.Bins(40), plot.Alpha(0.6))
ax.Axvline(boot.ConfidenceInterval.Low, plot.LineStyle("--"))
ax.Axvline(boot.ConfidenceInterval.High, plot.LineStyle("--"))
ax.SetXLabel("median of a resample")
fig
```
