//go:build js && wasm

// Command playground is the playground's WebAssembly core. It runs in a Web
// Worker (web/worker.js), defines two functions, then calls
// playgroundReady() and waits:
//
//   - playgroundRun(markdown) runs a notebook and returns what each code
//     cell did, as JSON ([]engine.Result);
//   - playgroundCells(markdown) returns the notebook's cells, as JSON
//     ([]engine.Cell), for a notebook the visitor opens.
//
// The page calls them once per run with the whole notebook: one string in,
// one string out, never a call per cell or per widget.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/go-notebook/playground/engine"
)

func main() {
	js.Global().Set("playgroundRun", js.FuncOf(func(_ js.Value, args []js.Value) any {
		out, _ := json.Marshal(engine.Run(args[0].String())) // strings and numbers always encode
		return string(out)
	}))
	js.Global().Set("playgroundCells", js.FuncOf(func(_ js.Value, args []js.Value) any {
		out, _ := json.Marshal(engine.Cells(args[0].String()))
		return string(out)
	}))
	js.Global().Call("playgroundReady")
	select {}
}
