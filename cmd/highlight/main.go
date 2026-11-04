//go:build js && wasm

// Command highlight is the editors' highlighter as WebAssembly: go-rouge and
// nothing else, so that colouring a keystroke is never queued behind a
// running notebook in the playground's own core. It runs in a Web Worker
// (web/highlight-worker.js), defines playgroundHighlight(lexer, source),
// which returns the source as HTML (highlight.HTML) or "" when it is to be
// shown as plain text, then calls playgroundReady() and waits.
package main

import (
	"syscall/js"

	"github.com/go-notebook/playground/highlight"
)

func main() {
	js.Global().Set("playgroundHighlight", js.FuncOf(func(_ js.Value, args []js.Value) any {
		out, _ := highlight.HTML(args[0].String(), args[1].String())
		return out
	}))
	js.Global().Call("playgroundReady")
	select {}
}
