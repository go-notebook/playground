//go:build js && wasm && julia

package main

import (
	"syscall/js"

	"github.com/go-notebook/playground/engine"
)

// The third core (-tags=julia) adds playgroundJuliaWarm() to the functions
// main defines: it boots a Julia kernel from the image the worker put in
// playgroundJuliaImage (web/worker.js), and returns "" or why it could not. The
// worker calls it before it says the core is ready, and again after each run,
// which closes its own kernel: loading takes seconds, a cell milliseconds.
func init() {
	// playgroundJuliaUsed() tells whether a Julia cell has run in this instance,
	// which then must not boot another kernel: the worker asks the page to replace it.
	js.Global().Set("playgroundJuliaUsed", js.FuncOf(func(js.Value, []js.Value) any { return engine.JuliaUsed() }))
	js.Global().Set("playgroundJuliaWarm", js.FuncOf(func(js.Value, []js.Value) any {
		if err := engine.WarmJulia(); err != nil {
			return err.Error()
		}
		return ""
	}))
}
