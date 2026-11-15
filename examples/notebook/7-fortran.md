# Fortran cells, run in your browser

A cell written in a `fortran` fence **runs** here, with [go-embedded-fortran](https://go-embedded-fortran.github.io/)'s evaluator compiled to WebAssembly. There is no `gfortran` behind it, and no server. The badge on the cell says **Fortran**, and the output is what the program prints.

A Fortran cell is a piece of a program, and what it declares stays: variables, procedures, modules and derived types persist from one Fortran cell to the next. This cell declares some data and a function:

```fortran
integer :: i, total
integer :: squares(10)

do i = 1, 10
  squares(i) = i * i
end do
```

The next Fortran cell reads them. `print *` writes where Fortran's list-directed output goes:

```fortran
total = sum(squares)
print *, total
print *, 'mean of the squares:', real(total) / 10.0
```

Fortran's variables **stay Fortran's**, as in go-notebook: they do not cross to the Go cells, and the Go cells' variables do not cross to Fortran. This Go cell has its own `total`, and the Fortran cell above is not touched by it:

```go
total := 42
total
```

## Limits

A browser tab must not freeze, so a Fortran cell is bounded: 200 million loop iterations and procedure calls, and 10 seconds. A cell that goes past one is stopped with a message that says which, and the page stays usable: `do` with no exit ends on the step limit. The evaluator has no access to a file or an environment variable of your machine.

Follow the progress of the evaluator at [go-embedded-fortran.github.io](https://go-embedded-fortran.github.io/).
