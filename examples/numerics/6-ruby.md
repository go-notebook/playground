# Optimisation and integration, in Ruby

The cells of a notebook can be Ruby too: [rbgo](https://github.com/go-embedded-ruby/ruby), a Ruby written in Go, with the [go-scikit bindings](https://github.com/go-scikit/ruby) — `Optimize`, `Integrate` and `Special` are go-numerics, called from Ruby, with the objective or the integrand as a block. The page loads the Ruby runtime only for a notebook like this one.

Rosenbrock's function, minimised with BFGS from the usual start:

```ruby
res = Optimize.minimize([-1.2, 1.0], method: :bfgs) do |x|
  (1 - x[0])**2 + 100 * (x[1] - x[0]**2)**2
end
puts "x = #{res.x.inspect}, #{res.nit} iterations, #{res.nfev} evaluations"
res.fun
```

A root, a quadrature over an infinite interval, and a special function on an array:

```ruby
root = Optimize.brentq(2, 3) { |x| x**3 - 2 * x - 5 }.root
gauss = Integrate.quad(0, Float::INFINITY) { |x| Math.exp(-x * x) }.value
puts "root #{root}, ∫exp(-x²) = #{gauss} (√π/2 = #{Math.sqrt(Math::PI) / 2})"
Special.gamma(NDArray.arange(1, 7))
```

A damped oscillator, solved in Ruby, plotted in Ruby:

```ruby
ts = NDArray.linspace(0, 20, 200)
sol = Integrate.solve_ivp([0, 20], [1.0, 0.0], t_eval: ts.to_a) { |t, y| [y[1], -0.3 * y[1] - y[0]] }
fig, ax = Plot.subplots(figsize: [6, 3.2])
ax.plot(sol.t, sol.y_array[0], label: "position")
ax.plot(sol.t, sol.y_array[1], label: "velocity")
ax.legend
fig
```

The solution reaches Go as an `*integrate.Solution`: its time grid, and the position at the end.

```go
import "fmt"

fmt.Printf("%d points, x(20) = %.4f\n", len(sol.T), sol.Y[len(sol.Y)-1][0])
```
