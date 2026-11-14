//go:build js && wasm

// Command editor is the playground's code editor, a WebAssembly module that
// runs on the page's main thread (the language cores stay in their Worker). It
// defines globalThis.gonbEditor and calls gonbEditorReady() if the page defined
// it:
//
//	const h = gonbEditor.create({text, lang, name, dark, fontPx, notes, onChange(text), onHeight(cssPx), onNotes(notes)})
//	h.mount(canvas)      // draw on this <canvas>; keyboard, IME, find and the AX tree follow
//	h.resize()           // lay it out again at its canvas's CSS box (this surface only)
//	h.unmount()          // free its frame; the text, caret and undo stay in h
//	h.focus()
//	h.text()  h.setText(s)  h.insert(s)  h.setLang(l)  h.setName(n)  h.setDark(b)
//	h.selection() -> [anchor, focus]   h.setSelection(anchor, focus)   (UTF-16 units)
//	h.replay(b)          // pointer events in flight come from the mirror taking focus
//	h.height()           // the CSS height the text wants
//	h.notes() -> [{line, text}]   h.setNotes([{line, text}])   (line annotations, 1-based lines)
//	h.destroy()
//
// What the editor does is in package editor, tested natively; this file only
// moves values between it and JavaScript. The text is handed over as a string
// and never becomes markup.
package main

import (
	"syscall/js"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"

	"github.com/go-notebook/playground/editor"
	"github.com/go-notebook/playground/notes"
)

func main() {
	toolkit.SetClipboard(webcanvas.NewClipboard())
	api := js.Global().Get("Object").New()
	api.Set("create", js.FuncOf(create))
	js.Global().Set("gonbEditor", api)
	if f := js.Global().Get("gonbEditorReady"); f.Type() == js.TypeFunction {
		f.Invoke()
	}
	select {}
}

// create makes a Cell from the page's options and returns its handle.
func create(_ js.Value, args []js.Value) any {
	o := args[0]
	str := func(k string) string {
		if v := o.Get(k); v.Type() == js.TypeString {
			return v.String()
		}
		return ""
	}
	cfg := editor.Config{Text: str("text"), Lang: str("lang"), Name: str("name"), Dark: o.Get("dark").Truthy()}
	if v := o.Get("fontPx"); v.Type() == js.TypeNumber {
		cfg.FontPx = v.Float()
	}
	cfg.Notes = notesFromJS(o.Get("notes"))
	if f := o.Get("onNotes"); f.Type() == js.TypeFunction {
		cfg.OnNotes = func(n []notes.Note) { f.Invoke(notesToJS(n)) }
	}
	if f := o.Get("onChange"); f.Type() == js.TypeFunction {
		cfg.OnChange = func(t string) { f.Invoke(t) }
	}
	if f := o.Get("onHeight"); f.Type() == js.TypeFunction {
		cfg.OnHeight = func(h float64) { f.Invoke(h) }
	}
	cell := editor.New(cfg)
	var surf *webcanvas.Surface
	var canvas js.Value
	var funcs []js.Func
	h := js.Global().Get("Object").New()
	def := func(name string, f func(a []js.Value) any) {
		fn := js.FuncOf(func(_ js.Value, a []js.Value) any { return f(a) })
		funcs = append(funcs, fn)
		h.Set(name, fn)
	}
	unmount := func() {
		if surf == nil {
			return
		}
		surf.Close()
		surf = nil
		canvas.Set("width", 0) // frees the backing store
		canvas.Set("height", 0)
	}
	def("mount", func(a []js.Value) any {
		unmount()
		canvas = a[0]
		surf = webcanvas.Mount(canvas, cell)
		return nil
	})
	def("resize", func([]js.Value) any {
		if surf != nil {
			surf.Resize()
		}
		return nil
	})
	def("unmount", func([]js.Value) any { unmount(); return nil })
	def("focus", func([]js.Value) any {
		cell.SetFocused(true)
		if surf != nil {
			surf.Refresh()
			surf.Focus()
		}
		return nil
	})
	def("text", func([]js.Value) any { return cell.Text() })
	def("setText", func(a []js.Value) any { cell.SetText(a[0].String()); return nil })
	def("insert", func(a []js.Value) any { cell.Insert(a[0].String()); return nil })
	def("setLang", func(a []js.Value) any { cell.SetLanguage(a[0].String()); return nil })
	def("setName", func(a []js.Value) any { cell.SetName(a[0].String()); return nil })
	def("setDark", func(a []js.Value) any { cell.SetDark(a[0].Truthy()); return nil })
	def("selection", func([]js.Value) any {
		x, y := cell.Selection()
		return []any{x, y}
	})
	def("setSelection", func(a []js.Value) any { cell.SetSelection(a[0].Int(), a[1].Int()); return nil })
	def("replay", func(a []js.Value) any { cell.ReplayPointer(a[0].Truthy()); return nil })
	def("height", func([]js.Value) any { return cell.HeightCSS() })
	def("notes", func([]js.Value) any { return notesToJS(cell.Notes()) })
	def("setNotes", func(a []js.Value) any { cell.SetNotes(notesFromJS(a[0])); return nil })
	def("focused", func([]js.Value) any { return cell.Focused() })
	def("blur", func([]js.Value) any { cell.SetFocused(false); return nil })
	def("destroy", func([]js.Value) any {
		unmount()
		for _, f := range funcs {
			f.Release()
		}
		return nil
	})
	return h
}

// notesFromJS reads an array of {line, text}; anything else is no notes. The
// page's values are untrusted: a bad element is skipped, and the cell bounds
// the rest.
func notesFromJS(v js.Value) []notes.Note {
	if v.Type() != js.TypeObject || !js.Global().Get("Array").Call("isArray", v).Bool() {
		return nil
	}
	var out []notes.Note
	for i := 0; i < v.Length() && i < notes.MaxNotes*2; i++ {
		e := v.Index(i)
		if e.Type() != js.TypeObject || e.Get("line").Type() != js.TypeNumber || e.Get("text").Type() != js.TypeString {
			continue
		}
		out = append(out, notes.Note{Line: e.Get("line").Int(), Text: e.Get("text").String()})
	}
	return out
}

// notesToJS is notes as an array of {line, text}.
func notesToJS(ns []notes.Note) any {
	arr := make([]any, 0, len(ns))
	for _, n := range ns {
		arr = append(arr, map[string]any{"line": n.Line, "text": n.Text})
	}
	return arr
}
