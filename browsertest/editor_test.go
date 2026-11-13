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
