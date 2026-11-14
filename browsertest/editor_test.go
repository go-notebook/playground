package browsertest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// editorPage opens the page, waits for the editor, and opens the test notebook.
func editorPage(t testing.TB) context.Context {
	t.Helper()
	ctx := open(t, serve(t), 800, 600, 1)
	editorUp(t, ctx)
	openNotebook(t, ctx, testNotebook, testCells)
	until(t, ctx, "both code cells on their canvases", time.Minute,
		`[...playgroundEditor.surfaces].filter((s) => s.mounted).length === 2`)
	return ctx
}

// A code cell is one textbox in the accessibility tree, whose value is its
// code, named for its language and number; the textarea it replaced is not a
// second one.
func TestACodeCellIsOneTextbox(t *testing.T) {
	ctx := editorPage(t)
	time.Sleep(500 * time.Millisecond) // the mirror publishes at most every 200 ms
	boxes := textboxes(axTree(t, ctx))
	if len(boxes) != 2 {
		t.Fatalf("%d textboxes in the tree, want one per code cell (2): %+v", len(boxes), boxes)
	}
	for i, want := range []struct{ name, value string }{{"Go cell 2", cell1}, {"Go cell 4", cell2}} {
		if boxes[i].Name != want.name || boxes[i].Value != want.value {
			t.Errorf("textbox %d is %q = %q, want %q = %q", i, boxes[i].Name, boxes[i].Value, want.name, want.value)
		}
	}
	// the textarea is still the page's record of the cell, and out of the tree
	if got := ev[string](t, ctx, textareaOf(0)); got != cell1 {
		t.Errorf("the hidden textarea holds %q, want %q", got, cell1)
	}
	if ev[bool](t, ctx, `document.querySelector('#notebook .cell[data-lang] textarea').offsetParent !== null`) {
		t.Error("the textarea is still on show beside the canvas")
	}
	if n := ev[int](t, ctx, `document.querySelectorAll('#notebook .cell[data-lang] canvas').length`); n != 2 {
		t.Errorf("%d canvases, want 2", n)
	}
}

// Typing reaches the cell once, through the text layer, and the page's record
// of the cell follows; an IME composition lands as its commit.
func TestTypingAndIME(t *testing.T) {
	ctx := editorPage(t)
	x, y := centre(t, ctx, 0)
	if _, err := chromedp.Run(ctx, chromedp.MouseClickXY(x, y)); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "the editor to take the keyboard", 10*time.Second,
		`document.activeElement && document.activeElement.hasAttribute('data-webcanvas-layer')`)
	time.Sleep(300 * time.Millisecond)
	if _, err := chromedp.Run(ctx, chromedp.KeyEvent("Z")); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "the typed letter in the cell's record", 10*time.Second, textareaOf(0)+`.length === `+itoa(len(cell1)+1)+` && `+textareaOf(0)+`.includes('Z')`)
	if got := ev[string](t, ctx, textareaOf(1)); got != cell2 {
		t.Errorf("the other cell changed: %q", got)
	}
	before := ev[string](t, ctx, textareaOf(0))
	for _, comp := range []string{"ni", "你"} {
		n := int64(len([]rune(comp)))
		if _, err := chromedp.Call(ctx, input.ImeSetComposition, input.ImeSetCompositionParams{Text: comp, SelectionStart: n, SelectionEnd: n}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if _, err := chromedp.Call(ctx, input.InsertText, input.InsertTextParams{Text: "你"}); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "the committed text in the cell's record", 10*time.Second, textareaOf(0)+`.includes('你')`)
	got := ev[string](t, ctx, textareaOf(0))
	if strings.Count(got, "你") != 1 || strings.Contains(got, "ni") || len(got) != len(before)+len("你") {
		t.Errorf("after the composition the cell is %q (it was %q): want the commit once and no preview left", got, before)
	}
	// the tree follows
	time.Sleep(400 * time.Millisecond)
	if bs := textboxes(axTree(t, ctx)); len(bs) != 2 || bs[0].Value != got {
		t.Errorf("tree value %q, want %q", bs[0].Value, got)
	}
}

// Find-in-page's engine reaches code, for a cell on its canvas.
func TestFindReachesTheCode(t *testing.T) {
	ctx := editorPage(t)
	if !ev[bool](t, ctx, `(() => { getSelection().removeAllRanges();
  return window.find('y * 3') && !!getSelection().anchorNode &&
    !!(getSelection().anchorNode.parentElement || getSelection().anchorNode).closest('[data-webcanvas-layer]'); })()`) {
		t.Error("window.find does not hit the code of a cell")
	}
}

// A selection from prose into code, and from code into prose, copies both.
func TestASelectionFromProseIntoCodeCopies(t *testing.T) {
	ctx := editorPage(t)
	// the copy event reads the selection the browser would put on the clipboard
	copy := func(js string) string {
		ev[any](t, ctx, `(() => {
  const l = document.querySelector('#notebook .cell[data-lang] [data-webcanvas-layer]');
  const prose = document.querySelectorAll('#notebook .prose')[0];
  const s = getSelection();
  `+js+`
  window.__copied = null;
  document.addEventListener('copy', () => { window.__copied = getSelection().toString(); }, { once: true });
})()`)
		// the browser's own copy command, as Ctrl+C runs it (a script's
		// execCommand has no user activation in headless Chrome)
		if _, err := chromedp.Call(ctx, input.DispatchKeyEvent, input.DispatchKeyEventParams{Type: input.DispatchKeyEventTypeRawKeyDown, Key: "c", Modifiers: 2 /* Ctrl */, Commands: []string{"copy"}}); err != nil {
			t.Fatal(err)
		}
		until(t, ctx, "the copy event", 5*time.Second, `window.__copied !== null`)
		return ev[string](t, ctx, `window.__copied`)
	}
	// prose into code: from "before" in the first paragraph to the second line of cell 1
	got := copy(`s.setBaseAndExtent(prose.querySelector('p').firstChild, 6, l.children[1].firstChild.firstChild, 3);`)
	if !strings.HasPrefix(got, "before") || !strings.Contains(got, "x := 21") || !strings.Contains(got, "x *") {
		t.Errorf("prose into code copied %q", got)
	}
	// code into the prose after it
	got = copy(`const after = document.querySelectorAll('#notebook .prose')[1].querySelector('p').firstChild;
  s.setBaseAndExtent(l.children[0].firstChild.firstChild, 3, after, 5);`)
	if !strings.HasPrefix(got, "= 21") || !strings.Contains(got, "x * 2") || !strings.HasSuffix(got, "Prose") {
		t.Errorf("code into prose copied %q", got)
	}
}

// The wheel over a cell scrolls the page: the editor is as tall as its text and
// keeps no wheel of its own.
func TestTheWheelOverACellScrollsThePage(t *testing.T) {
	ctx := open(t, serve(t), 800, 360, 1)
	editorUp(t, ctx)
	var md strings.Builder
	md.WriteString("# Wheel\n\n")
	for i := 0; i < 12; i++ {
		md.WriteString("Prose.\n\n```go\nx := 1\nx\n```\n\n")
	}
	openNotebook(t, ctx, md.String(), 25)
	until(t, ctx, "a cell on its canvas", time.Minute, `[...playgroundEditor.surfaces].some((s) => s.mounted)`)
	ev[any](t, ctx, `window.scrollTo(0, 0)`)
	time.Sleep(200 * time.Millisecond)
	x, y := centre(t, ctx, 0)
	// centre() scrolled the cell into the middle of the viewport; start from there
	before := ev[float64](t, ctx, `window.scrollY`)
	if _, err := chromedp.Call(ctx, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: input.DispatchMouseEventTypeMouseWheel, X: x, Y: y, DeltaX: 1, DeltaY: 250}); err != nil { // DeltaX 0 is left out, which CDP refuses
		t.Fatal(err)
	}
	until(t, ctx, "the page to scroll", 5*time.Second, `window.scrollY > `+ftoa(before))
	if got := ev[float64](t, ctx, `window.scrollY`); got-before < 100 {
		t.Errorf("the wheel moved the page %.0f px, want about 250", got-before)
	}
	if ev[bool](t, ctx, `document.querySelectorAll('#notebook .cell[data-lang] canvas').length === 0`) {
		t.Error("no canvas: the test did not run over the editor")
	}
}

// When the editor cannot load, the page is what it was: text areas, typing,
// running.
func TestAFailedEditorLoadLeavesTheTextareaPage(t *testing.T) {
	ctx := open(t, serve(t, "editor.wasm"), 800, 600, 1)
	until(t, ctx, "the editor's load to fail", 3*time.Minute, `window.playgroundTimings && !!window.playgroundTimings.editorError`)
	openNotebook(t, ctx, testNotebook, testCells)
	if n := ev[int](t, ctx, `document.querySelectorAll('#notebook canvas').length`); n != 0 {
		t.Fatalf("%d canvases after a failed load", n)
	}
	if !ev[bool](t, ctx, `[...document.querySelectorAll('#notebook .cell[data-lang] textarea')].every((t) => t.offsetParent !== null)`) {
		t.Fatal("a code cell's textarea is not on show")
	}
	// typing into the textarea changes the cell, and the cell runs
	if _, err := chromedp.Run(ctx, chromedp.Click(`#notebook .cell[data-lang] textarea`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.KeyEvent("Z")); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "the typed letter in the textarea", 10*time.Second, textareaOf(0)+`.includes('Z')`)
	if _, err := chromedp.Run(ctx, chromedp.Click(`#run`)); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "the second cell's result", time.Minute,
		`document.querySelectorAll('#notebook .cell[data-lang]')[1].querySelector('.outputs').textContent.includes('66')`)
}

// A cell is the same size in CSS pixels on a plain display and on a dense one:
// the same glyphs, the same row spacing, the same height. (The toolkit's metric
// scale is global, and the surface is drawn at devicePixelRatio: they must
// move together.)
func TestACellHasTheSameCSSMetricsAtDPR1AndDPR2(t *testing.T) {
	type metrics struct{ Font, Pitch, Height, Left float64 }
	measure := func(dpr float64) metrics {
		ctx := open(t, serve(t), 800, 600, dpr)
		editorUp(t, ctx)
		openNotebook(t, ctx, testNotebook, testCells)
		until(t, ctx, "both code cells on their canvases", time.Minute,
			`[...playgroundEditor.surfaces].filter((s) => s.mounted).length === 2`)
		time.Sleep(800 * time.Millisecond) // the layer follows the first frames
		r := ev[[]float64](t, ctx, `(() => {
  const cell = document.querySelectorAll('#notebook .cell[data-lang]')[0];
  const l = cell.querySelector('[data-webcanvas-layer]');
  const a = l.children[0].getBoundingClientRect(), b = l.children[1].getBoundingClientRect();
  return [parseFloat(getComputedStyle(l.children[0]).fontSize), b.top - a.top,
          cell.querySelector('canvas').getBoundingClientRect().height, a.left - cell.querySelector('canvas').getBoundingClientRect().left];
})()`)
		return metrics{r[0], r[1], r[2], r[3]}
	}
	m1, m2 := measure(1), measure(2)
	t.Logf("dpr 1: %+v", m1)
	t.Logf("dpr 2: %+v", m2)
	near := func(what string, a, b float64) {
		if d := a - b; d > 1 || d < -1 {
			t.Errorf("%s is %.2f CSS px at dpr 1 and %.2f at dpr 2", what, a, b)
		}
	}
	near("the glyph size", m1.Font, m2.Font)
	near("the row pitch", m1.Pitch, m2.Pitch)
	near("the cell height", m1.Height, m2.Height)
	near("the text's left edge", m1.Left, m2.Left)
	// ...and it is the size of code: the toolkit's own 5x7 bitmap, which the cells
	// used before, measured a font of 8.25 CSS px in the text layer and a row of
	// 11. A 13 px editor font has a 15 px glyph box and a 19 px row, so the layer
	// (which sizes itself at 3/4 of the row) is 14.25.
	for dpr, m := range map[float64]metrics{1: m1, 2: m2} {
		if m.Font < 13 || m.Font > 16 || m.Pitch < 18 || m.Pitch > 22 {
			t.Errorf("at dpr %v the layer font is %.2f CSS px and the row pitch %.2f; want about 14.25 and 19 (the 5x7 bitmap gave 8.25 and 11)", dpr, m.Font, m.Pitch)
		}
	}
}

// Sideways: a horizontal wheel over a cell with a long line moves the text
// (the editor uses it), and a vertical one moves the page (it does not).
func TestTheWheelScrollsALongLineSidewaysAndTheEditorKeepsNoVerticalWheel(t *testing.T) {
	ctx := open(t, serve(t), 700, 360, 1)
	editorUp(t, ctx)
	// not periodic: a scroll by a whole number of periods would look like none
	var digits strings.Builder
	for i := 0; i < 150; i++ {
		digits.WriteString(itoa(i * 7))
	}
	long := "x := \"" + digits.String() + "\""
	var md strings.Builder
	md.WriteString("# Sideways\n\n```go\n" + long + "\n```\n\n")
	for i := 0; i < 10; i++ {
		md.WriteString("Prose.\n\n```go\nx := 1\n```\n\n")
	}
	openNotebook(t, ctx, md.String(), -11)
	until(t, ctx, "the cell on its canvas", time.Minute, `[...playgroundEditor.surfaces].some((s) => s.mounted)`)
	time.Sleep(800 * time.Millisecond)
	x, y := centre(t, ctx, 0)
	frame := func() string {
		return ev[string](t, ctx, `(() => { const c = document.querySelector('#notebook .cell[data-lang] canvas');
  const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data; let h = 0;
  for (let i = 0; i < d.length; i += 4) h = (h * 31 + d[i] + d[i + 1] * 3 + d[i + 2] * 7) | 0; return '' + h; })()`)
	}
	before, y0 := frame(), ev[float64](t, ctx, `window.scrollY`)
	// sideways: dx leads
	if _, err := chromedp.Call(ctx, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: input.DispatchMouseEventTypeMouseWheel, X: x, Y: y, DeltaX: 400, DeltaY: 1}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if frame() == before {
		t.Error("a horizontal wheel did not move the text of a long line")
	}
	if got := ev[float64](t, ctx, `window.scrollY`); got != y0 {
		t.Errorf("a horizontal wheel moved the page (%v -> %v)", y0, got)
	}
	// vertical: the page moves, the text does not
	moved := frame()
	if _, err := chromedp.Call(ctx, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: input.DispatchMouseEventTypeMouseWheel, X: x, Y: y, DeltaX: 1, DeltaY: 200}); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "the page to scroll", 5*time.Second, `window.scrollY > `+ftoa(y0))
	if frame() != moved {
		t.Error("a vertical wheel moved the text of the cell")
	}
}

// A comment on a line of code is stored in the notebook, in an HTML comment
// after the cell's fence: it is read when the notebook opens, shows as a button
// in the accessibility tree, can be added from the cell, and is in the Markdown
// the page downloads, with the code and the prose as they were.
func TestLineAnnotationsLoadShowAndSave(t *testing.T) {
	const stored = `<!-- gonb:notes {"v":1,"notes":[{"line":2,"text":"doubled"}]} -->`
	md := strings.Replace(testNotebook, "```\n\nProse between", "```\n"+stored+"\n\nProse between", 1)
	ctx := open(t, serve(t), 800, 600, 1)
	editorUp(t, ctx)
	openNotebook(t, ctx, md, testCells)
	until(t, ctx, "both code cells on their canvases", time.Minute,
		`[...playgroundEditor.surfaces].filter((s) => s.mounted).length === 2`)
	time.Sleep(600 * time.Millisecond)
	// the stored comment is on the cell's line 2, as a button, and the prose
	// after the cell does not show the comment's line
	has := func(name string) bool {
		for _, n := range axTree(t, ctx) {
			if !n.Ignored && n.Role == "button" && n.Name == name {
				return true
			}
		}
		return false
	}
	if !has("Comment on line 2: doubled") {
		t.Fatal("no button 'Comment on line 2: doubled' in the accessibility tree")
	}
	if ev[bool](t, ctx, `document.querySelector('#notebook').textContent.includes('gonb:notes')`) {
		t.Error("the storage comment shows in the prose")
	}
	// add one on line 1 from the cell: click its gutter, type, Enter
	r := ev[[]float64](t, ctx, `(() => {
  const c = document.querySelectorAll('#notebook .cell[data-lang]')[0].querySelector('canvas');
  c.scrollIntoView({ block: 'center' });
  const b = c.getBoundingClientRect();
  return [b.left, b.top];
})()`)
	if _, err := chromedp.Run(ctx, chromedp.MouseClickXY(r[0]+8, r[1]+4+9)); err != nil {
		t.Fatal(err)
	}
	until(t, ctx, "the editor to take the keyboard", 10*time.Second,
		`document.activeElement && document.activeElement.hasAttribute('data-webcanvas-layer')`)
	time.Sleep(300 * time.Millisecond)
	for _, k := range []string{"o", "n", "e"} {
		if _, err := chromedp.Run(ctx, chromedp.KeyEvent(k)); err != nil {
			t.Fatal(err)
		}
	}
	// Enter as a keyboard sends it: one keydown, then the key up
	for _, typ := range []input.DispatchKeyEventType{input.DispatchKeyEventTypeRawKeyDown, input.DispatchKeyEventTypeKeyUp} {
		if _, err := chromedp.Call(ctx, input.DispatchKeyEvent, input.DispatchKeyEventParams{Type: typ, Key: "Enter", Code: "Enter", WindowsVirtualKeyCode: 13}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for !has("Comment on line 1: one") {
		if time.Now().After(deadline) {
			t.Fatalf("no button 'Comment on line 1: one'; the cells hold %s", ev[string](t, ctx, `JSON.stringify([...playgroundEditor.surfaces].map((s) => [s.handle.text(), s.handle.notes()]))`))
		}
		time.Sleep(200 * time.Millisecond)
	}
	// the downloaded Markdown
	ev[any](t, ctx, `(() => {
  const o = URL.createObjectURL;
  URL.createObjectURL = (b) => { b.text().then((s) => (window.__md = s)); return o.call(URL, b); };
  document.getElementById('download').click();
})()`)
	until(t, ctx, "the downloaded notebook", 10*time.Second, `typeof window.__md === 'string'`)
	got := ev[string](t, ctx, `window.__md`)
	if !strings.Contains(got, "\n```\n<!-- gonb:notes ") || !strings.Contains(got, `"line":1,"text":"one"`) || !strings.Contains(got, `"line":2,"text":"doubled"`) {
		t.Fatalf("the downloaded notebook does not hold both comments:\n%s", got)
	}
	if !strings.Contains(got, "```go\n"+cell1+"\n```\n") || !strings.Contains(got, "Prose between the cells.") {
		t.Errorf("the code or the prose changed:\n%s", got)
	}
}

// The minimap on the right of a cell, like the annotation column, is the
// canvas's: the text layer, which takes the pointer wherever its element is,
// does not cover it (a click on it moves the caret to that line).
func TestTheMinimapAndTheGutterAreNotUnderTheTextLayer(t *testing.T) {
	ctx := editorPage(t)
	time.Sleep(600 * time.Millisecond)
	for name, at := range map[string]string{"minimap": "b.right - 20", "annotation column": "b.left + 6", "text": "b.left + 200"} {
		got := ev[string](t, ctx, `(() => {
  const c = document.querySelectorAll('#notebook .cell[data-lang]')[0].querySelector('canvas');
  c.scrollIntoView({ block: 'center' });
  const b = c.getBoundingClientRect();
  return document.elementFromPoint(`+at+`, b.top + 12).tagName;
})()`)
		want := "CANVAS"
		if name == "text" {
			want = "DIV" // the text layer, as ever
		}
		if got != want {
			t.Errorf("at the %s the pointer reaches a %s, want a %s", name, got, want)
		}
	}
}
