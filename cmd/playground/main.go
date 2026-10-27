//go:build js && wasm

// Command playground is the playground's WebAssembly core. It runs in a Web
// Worker (web/worker.js), defines three functions, then calls
// playgroundReady() and waits:
//
//   - playgroundRun(markdown, last) runs a notebook, its code cells up to
//     the code cell last (every one when last is negative), and returns what
//     each did, as JSON ([]engine.Result); it calls playgroundStarted(i) as
//     code cell i starts, for the page's time limit;
//   - playgroundCells(markdown) returns the notebook's cells, as JSON
//     ([]engine.Cell), for a notebook the visitor opens;
//   - playgroundEdit(markdown, ops) applies edits ([]engine.Op, as JSON)
//     through the format package and returns the notebook written back, as
//     JSON (engine.Edited), or {"error": "..."}.
//
// The page calls them with the whole notebook: one string in, one string
// out, never a call per widget.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/go-notebook/playground/engine"
)

func main() {
	js.Global().Set("playgroundRun", js.FuncOf(func(_ js.Value, args []js.Value) any {
		last := -1
		if len(args) > 1 && args[1].Type() == js.TypeNumber {
			last = args[1].Int()
		}
		started := func(i int) {
			if f := js.Global().Get("playgroundStarted"); f.Type() == js.TypeFunction {
				f.Invoke(i)
			}
		}
		out, _ := json.Marshal(engine.RunUpTo(args[0].String(), last, started)) // strings and numbers always encode
		return string(out)
	}))
	js.Global().Set("playgroundCells", js.FuncOf(func(_ js.Value, args []js.Value) any {
		out, _ := json.Marshal(engine.Cells(args[0].String()))
		return string(out)
	}))
	js.Global().Set("playgroundEdit", js.FuncOf(func(_ js.Value, args []js.Value) any {
		var ops []engine.Op
		if err := json.Unmarshal([]byte(args[1].String()), &ops); err != nil {
			out, _ := json.Marshal(map[string]string{"error": err.Error()})
			return string(out)
		}
		e, err := engine.Edit(args[0].String(), ops)
		if err != nil {
			out, _ := json.Marshal(map[string]string{"error": err.Error()})
			return string(out)
		}
		out, _ := json.Marshal(e)
		return string(out)
	}))
	js.Global().Call("playgroundReady")
	select {}
}
