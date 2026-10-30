# Julia cells, checked but not run yet

A cell written in a `julia` fence is **parsed and checked** with [go-embedded-julia](https://go-embedded-julia.github.io/)'s parser, and it is **not run**: go-embedded-julia has no runtime yet (its milestone M3). Until that lands, a Julia cell is checked, not run. There is no external `julia` behind it, and there never will be one. The badge on the cell says so: *Julia · not runnable yet*.

What works today is the checking, and it is worth having: the notebook knows what each Julia cell defines and what it reads, and it knows which Go cells depend on it. Go draws the size; the Julia cell reads `n` and defines `xs` and `total`.

```go
n := 10
```

The Julia cell below is well formed, so it is not an error. It does not run either: its output says why, and no number comes back.

```julia
xs = [i^2 for i in 1:n]
total = sum(xs)
```

This Go cell reads `total`, which the Julia cell defines. The name analysis sees that, so the cell is **blocked**: it waits for the Julia cell, and says so, instead of failing on a name that never arrives.

```go
m := total + 1
```

A cell that does not read anything from Julia is not held back. It runs as usual, whatever the Julia cells above it do:

```go
n * 2
```

A mistake in a Julia cell is caught at the right line, before anything runs, and the cells that do not read it still run. This notebook would stop being published if one of its cells had one, so here is one, quoted. With a cell that reads

> y = 2
> x = (1,

the page shows ``line 2:8: syntax error: Expected `)` or `,` ``, at line 2, column 8, where the closing parenthesis was expected. Try it: add a Julia cell with `x = (1,` and run the notebook.

When go-embedded-julia's runtime lands, these cells will run and the blocked cell will compute `total + 1`. Follow the progress at [go-embedded-julia.github.io](https://go-embedded-julia.github.io/).
