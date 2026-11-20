//go:build js && wasm && julia

package main

import (
	"syscall/js"

	"github.com/go-notebook/playground/engine"
)

// The third core (-tags=julia) adds playgroundJuliaWarm() to the functions
// main defines: it boots a Julia kernel from the image the worker put in
// playgroundJuliaImage (web/worker.js), and returns "" or why it could not. The
// worker calls it before it says the core is ready: loading takes seconds, a
// cell milliseconds. Each run then resets that kernel (Kernel.Reset).
func init() {
	// playgroundJuliaReplace() tells whether a Reset had to load the image again
	// (or failed): the instance then holds a boot more of memory, and the worker
	// asks the page to replace it.
	js.Global().Set("playgroundJuliaReplace", js.FuncOf(func(js.Value, []js.Value) any { return engine.JuliaReplace() }))
	js.Global().Set("playgroundJuliaWarm", js.FuncOf(func(js.Value, []js.Value) any {
		if err := engine.WarmJulia(); err != nil {
			return err.Error()
		}
		return ""
	}))
}
