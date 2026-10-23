# The same tests, in Ruby

The cells of a notebook can be Ruby too: [rbgo](https://github.com/go-embedded-ruby/ruby), a Ruby written in Go, with the [go-scikit bindings](https://github.com/go-scikit/ruby) — `Stats` is go-statistics, called from Ruby, and `NDArray` is go-ndarray. The page loads the Ruby runtime only for a notebook like this one.

Go draws two samples, as in the t-test example; Ruby reads them as they are.

```go
import "github.com/go-ndarray/ndarray/random"

g := random.DefaultRNG(7)
a, _ := g.Normal(5.0, 1.0, 40)
b, _ := g.Normal(5.6, 1.0, 40)
```

Welch's t-test, the Mann-Whitney U test and a normality test, from Ruby: the results are go-statistics' own, with readers named as in SciPy.

```ruby
t = Stats.ttest_ind(a, b, welch: true)
u = Stats.mannwhitneyu(a, b)
puts format("Welch t = %.4f, p = %.4g", t.statistic, t.pvalue)
puts format("Mann-Whitney U = %.1f, p = %.4g", u.statistic, u.pvalue)
Stats.shapiro(a)
```

A normal distribution fitted to `a`, and its density over the data's histogram.

```ruby
fit = Stats::Normal.fit(a)
grid = NDArray.linspace(2, 8, 120)
fig, ax = Plot.subplots(figsize: [6, 3.2])
ax.hist(a, bins: 12, density: true, alpha: 0.5, label: "a")
ax.plot(grid, fit.pdf(grid), color: "crimson", label: format("N(%.2f, %.2f)", fit.loc, fit.scale))
ax.legend
fig
```

Back in Go, `t` is the `stats.TTestResult` itself:

```go
t.ConfidenceInterval(0.95)
```
