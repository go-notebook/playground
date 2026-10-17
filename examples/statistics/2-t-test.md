# Two samples, one question: are the means equal?

Two groups of 40 measurements, drawn with means 5.0 and 5.6 and the same spread. Welch's t-test does not assume equal variances.

```go
import (
	"fmt"

	"github.com/go-ndarray/ndarray/random"
	"github.com/go-statistics/stats"
)

g := random.DefaultRNG(7)
a, _ := g.Normal(5.0, 1.0, 40)
b, _ := g.Normal(5.6, 1.0, 40)

ma, _ := stats.Mean(a, nil)
mb, _ := stats.Mean(b, nil)
fmt.Printf("mean a = %.3f, mean b = %.3f\n", ma, mb)
```

```go
res, err := stats.TTestInd(a, b, &stats.TTestIndOptions{Welch: true})
fmt.Printf("t = %.4f, df = %.2f, p = %.4g\n", res.Statistic, res.DF, res.PValue)
```

A 95% confidence interval for the difference of the means:

```go
res.ConfidenceInterval(0.95)
```

The same question without assuming normality: the Mann–Whitney U test.

```go
stats.MannWhitneyU(a, b, nil)
```
