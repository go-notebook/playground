# Special functions

Bessel functions of the first kind, J_ν(*x*), for a few orders, and the gamma function across its poles. Every value comes from `special`, compiled to WebAssembly; only the loops are interpreted.

```go
import (
	"fmt"

	"github.com/go-numerics/special"
	"github.com/go-plot/plot"
)

x := make([]float64, 400)
for i := range x {
	x[i] = 20 * float64(i) / float64(len(x)-1)
}

fig := plot.NewFigure(plot.FigSize(6, 3.5))
ax := fig.AddAxes(0.12, 0.15, 0.83, 0.75)
for _, nu := range []float64{0, 1, 2.5} {
	y := make([]float64, len(x))
	for i, v := range x {
		y[i] = special.Jv(nu, v)
	}
	ax.Plot(x, y, plot.Label(fmt.Sprintf(`$\nu = %g$`, nu)))
}
ax.Axhline(0, plot.Color("gray"), plot.LineWidth(0.5))
ax.SetXLabel("x")
ax.SetYLabel(`$J_\nu(x)$`)
ax.Legend()
fig
```

The first zeros of J₀:

```go
special.JnZeros(0, 5)
```

Γ(*x*) has poles at the non-positive integers.

```go
gx := make([]float64, 800)
gy := make([]float64, len(gx))
for i := range gx {
	gx[i] = -4.5 + 9*float64(i)/float64(len(gx)-1)
	gy[i] = special.Gamma(gx[i])
}
fig2 := plot.NewFigure(plot.FigSize(6, 3.5))
ax2 := fig2.AddAxes(0.14, 0.15, 0.81, 0.75)
ax2.Plot(gx, gy)
ax2.SetYLim(-10, 10)
ax2.SetXLabel("x")
ax2.SetYLabel(`$\Gamma(x)$`)
fig2
```
