package browsertest

import (
	"strings"
	"testing"
	"time"
)

// At DPR 2 a cell that is off screen (its textarea on show) is as tall as it is
// once it is mounted: the toolkit's metric scale is the process's, set by
// whichever cell mounted first, and a cell made after that must not size itself
// at twice its height (the page would swell, and jump as the cells mount).
func TestOffScreenCellsAreAsTallAsMountedOnesAtDPR2(t *testing.T) {
	ctx := open(t, serve(t), 800, 500, 2)
	editorUp(t, ctx)
	var md strings.Builder
	md.WriteString("# Tall\n\n")
	for i := 0; i < 30; i++ {
		md.WriteString("Prose.\n\n```go\nx := 1\ny := 2\nz := 3\n```\n\n")
	}
	openNotebook(t, ctx, md.String(), -30)
	until(t, ctx, "30 surfaces", time.Minute, `playgroundEditor.surfaces.size === 30`)
	time.Sleep(800 * time.Millisecond)
	// what each cell is now: a textarea's height for a cell that is off screen
	// (it is on show), the canvas's for a cell that is mounted
	heights := `[...document.querySelectorAll('#notebook .cell[data-lang]')].map((c) => {
  const ta = c.querySelector('textarea'), cv = c.querySelector('canvas');
  return ta.offsetParent !== null ? ta.getBoundingClientRect().height : cv.getBoundingClientRect().height; })`
	before := ev[[]float64](t, ctx, heights)
	mounted := ev[int](t, ctx, `[...playgroundEditor.surfaces].filter((s) => s.mounted).length`)
	if mounted == 0 || mounted == 30 {
		t.Fatalf("%d of 30 cells are mounted: the test needs both kinds", mounted)
	}
	// mount them all, by scrolling through
	h := ev[int](t, ctx, `document.documentElement.scrollHeight`)
	for y := 0; y < h; y += 300 {
		ev[any](t, ctx, `window.scrollTo(0, `+itoa(y)+`)`)
		time.Sleep(80 * time.Millisecond)
	}
	ev[any](t, ctx, `window.scrollTo(0, 0)`)
	time.Sleep(500 * time.Millisecond)
	after := ev[[]float64](t, ctx, `[...document.querySelectorAll('#notebook .cell[data-lang]')].map((c) => parseFloat(c.querySelector('canvas').style.height))`) // the CSS height the page set; a canvas that is not mounted is hidden
	for i := range before {
		if d := before[i] - after[i]; d > 1.5 || d < -1.5 { // a textarea has a border of its own
			t.Errorf("cell %d was %.1f CSS px tall off screen and is %.1f mounted", i, before[i], after[i])
		}
	}
	// and they are the heights of a plain display, not double
	plain := open(t, serve(t), 800, 500, 1)
	editorUp(t, plain)
	openNotebook(t, plain, md.String(), -30)
	until(t, plain, "30 surfaces", time.Minute, `playgroundEditor.surfaces.size === 30`)
	time.Sleep(800 * time.Millisecond)
	want := ev[[]float64](t, plain, `[...document.querySelectorAll('#notebook .cell[data-lang]')].map((c) => parseFloat(c.querySelector('canvas').style.height))`)
	for i := range after {
		if d := after[i] - want[i]; d > 1.5 || d < -1.5 {
			t.Errorf("cell %d is %.1f CSS px tall at DPR 2 and %.1f at DPR 1", i, after[i], want[i])
		}
	}
	t.Logf("%d cells, %d mounted at the start: first %.1f / %.1f, last %.1f / %.1f CSS px (before / after)", len(before), mounted, before[0], after[0], before[len(before)-1], after[len(after)-1])
}
