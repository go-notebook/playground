# Julia cells, checked in the page and run in go-notebook

A cell written in a `julia` fence is **parsed and checked** with [go-embedded-julia](https://go-embedded-julia.github.io/)'s parser. In go-notebook itself (`notebook run`, `notebook serve`) it also **runs**, on go-embedded-julia's interpreter in one kernel. In this page it does **not** run yet: the interpreter's Base image is fetched at run time, which a page cannot do. There is no external `julia` behind it. The badge on the cell says so: *Julia · not runnable yet*.

In the page a Julia cell is checked, not run. What works here is the checking: syntax errors come back at the right line, and the cell never reaches the Go evaluator.

```go
n := 10
```

The Julia cell below is well formed, so it is not an error. It does not run in the page: its output says why, and no number comes back.

```julia
xs = [i^2 for i in 1:3]
total = sum(xs)
```

Julia variables stay in Julia (they are not shared with Go cells, in either direction), so a cell of another language neither reads `total` nor waits for it. It runs as usual, whatever the Julia cells above it do:

```go
n * 2
```

A mistake in a Julia cell is caught at the right line, before anything runs, and the cells that do not read it still run. This notebook would stop being published if one of its cells had one, so here is one, quoted. With a cell that reads

> y = 2
> x = (1,

the page shows ``line 2:8: syntax error: Expected `)` or `,` ``, at line 2, column 8, where the closing parenthesis was expected. Try it: add a Julia cell with `x = (1,` and run the notebook.

Running them in the browser is a separate question, pending with go-embedded-julia. Follow the progress at [go-embedded-julia.github.io](https://go-embedded-julia.github.io/).
