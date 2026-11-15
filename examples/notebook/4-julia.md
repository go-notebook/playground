# Julia cells run in the page, in a core of their own

A cell written in a `julia` fence **runs** here, on [go-embedded-julia](https://go-embedded-julia.github.io/)'s interpreter, in WebAssembly in this tab. There is no external `julia` and no server behind it. It is the same interpreter that `notebook run` and `notebook serve` use.

Julia is the heaviest runtime the page has, so it is loaded **only when you run a Julia cell**: Run loads a third core and Julia's Base image (about 28 MB over the wire, a dozen seconds the first time, then your browser keeps them), and says so on the cell while it does. Opening this notebook, and running the Go cells above the first Julia cell, download nothing for Julia. The Go, Ruby, R and Fortran cores never carry it.

```go
n := 10
```

The Julia cell below runs in Julia's REPL scope: the loop at the top of the cell assigns to the global `s`, as it does in the REPL and in a Jupyter kernel, and the value of the last expression is shown, as the REPL shows it.

```julia
s = 0
for i in 1:10
    s += i
end
s
```

What a cell prints is its output; what it defines stays in the one Julia kernel of the notebook, so the next Julia cell reads it.

```julia
xs = [i^2 for i in 1:3]
println("squares: ", xs)
total = sum(xs)
```

```julia
println("the sum of the squares is ", total)
```

**Julia's variables stay in Julia in the page.** In go-notebook itself (`notebook run`, `notebook serve`) a Julia variable crosses to a Go cell and back. In the page the Julia kernel and the Go interpreter are two interpreters in one worker that do not exchange values, so a Go cell that reads `total`, or a Julia cell that reads `n`, fails with the other language's own message (`undefined: total`, `UndefVarError`) and a note that says why. The Go cell below runs as usual, and does not read the Julia cells' variables:

```go
n * 2
```

A mistake in a Julia cell is caught at its line and column before anything runs, and an exception shows Julia's own text. Add a Julia cell with `x = (1,` and run the notebook to see ``line 1:8: syntax error: Expected `)` or `,` ``, or one with `sqrt(-1)` to see `ERROR: DomainError with -1.0:`. The cells after a failed cell are not run, as in every language.

A cell is bounded like an R cell: 512 MiB of arrays, 20 million statements, 10 seconds and a cap on its output. A cell that spins ends with `stopped: the Julia cell ran longer than 10s, the limit`, and the kernel, with its variables, is kept. Each run starts a kernel of its own, so a `struct` defined by a cell is not a redefinition the second time you run the notebook.

If Julia cannot start (the network, or a tab out of memory), its cells say so and the cells of the other languages run as usual.
