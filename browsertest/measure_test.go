package browsertest

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/heapprofiler"
	"github.com/chromedp/cdproto/systeminfo"
	"github.com/chromedp/chromedp"
)

// The measurements behind the PR, run only when asked (PLAYGROUND_MEASURE=1),
// with the machine's load beside them: times taken at a 1-minute load of 8 or
// more are indicative. Sizes and canvas bytes are exact.
func measuring(t *testing.T) {
	t.Helper()
	if os.Getenv("PLAYGROUND_MEASURE") == "" {
		t.Skip("set PLAYGROUND_MEASURE=1 for the measurements")
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		t.Logf("load %s", strings.TrimSpace(string(b)))
	} else if out, err := exec.Command("uptime").Output(); err == nil {
		t.Logf("load: %s", strings.TrimSpace(string(out)))
	}
}

func compressed(t *testing.T, name string) (raw, gz, br int) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(siteDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	var g bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&g, gzip.BestCompression)
	zw.Write(b)
	zw.Close()
	var r bytes.Buffer
	bw := brotli.NewWriterLevel(&r, brotli.BestCompression)
	bw.Write(b)
	bw.Close()
	return len(b), g.Len(), r.Len()
}

// What the editor adds to the page's download.
func TestMeasureWasmSize(t *testing.T) {
	measuring(t)
	for _, n := range []string{"editor.wasm", "playground.wasm", "playground-ruby.wasm"} {
		raw, gz, br := compressed(t, n)
		t.Logf("%-22s raw %10d  gzip -9 %9d  brotli -11 %9d", n, raw, gz, br)
	}
}

// From the start of the page to the editor being usable, and what one keystroke
// costs from the browser's key event to the page's record of the cell.
func TestMeasureFirstInteraction(t *testing.T) {
	measuring(t)
	var ready, editor []float64
	var keys []float64
	for round := 0; round < 5; round++ {
		ctx := open(t, serve(t), 800, 600, 2)
		editorUp(t, ctx)
		ready = append(ready, ev[float64](t, ctx, `playgroundTimings.runtimeReady`))
		editor = append(editor, ev[float64](t, ctx, `playgroundTimings.editorReady`))
		if round == 0 {
			openNotebook(t, ctx, testNotebook, testCells)
			until(t, ctx, "both cells on canvas", time.Minute, `[...playgroundEditor.surfaces].filter((s) => s.mounted).length === 2`)
			x, y := centre(t, ctx, 0)
			if _, err := chromedp.Run(ctx, chromedp.MouseClickXY(x, y)); err != nil {
				t.Fatal(err)
			}
			until(t, ctx, "the keyboard", 10*time.Second, `document.activeElement.hasAttribute('data-webcanvas-layer')`)
			time.Sleep(500 * time.Millisecond)
			ev[any](t, ctx, `window.__lat = []; document.addEventListener('keydown', () => { window.__t0 = performance.now(); }, true);
document.addEventListener('input', () => { window.__lat.push(performance.now() - window.__t0); }, true);`)
			for i := 0; i < 40; i++ {
				if _, err := chromedp.Run(ctx, chromedp.KeyEvent("a")); err != nil {
					t.Fatal(err)
				}
				time.Sleep(60 * time.Millisecond)
			}
			keys = ev[[]float64](t, ctx, `window.__lat`)
		}
	}
	slices.Sort(ready)
	slices.Sort(editor)
	slices.Sort(keys)
	t.Logf("runtime ready (ms from page start), 5 loads: %v", ready)
	t.Logf("editor ready (ms from page start), 5 loads: %v  (both warm-cache, localhost)", editor)
	if len(keys) > 0 {
		t.Logf("keystroke, browser key event -> page's record of the cell, %d presses: median %.1f ms, min %.1f, max %.1f", len(keys), keys[len(keys)/2], keys[0], keys[len(keys)-1])
	}
}

// residentKiB is the resident set of a process, as ps says.
func residentKiB(t *testing.T, pid int64) int {
	t.Helper()
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.FormatInt(pid, 10)).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// fiftyCells is a notebook of one heading and 50 code cells of 15 lines.
func fiftyCells() string {
	var b strings.Builder
	b.WriteString("# Fifty cells\n\n")
	for i := 0; i < 50; i++ {
		b.WriteString("```go\n")
		for l := 0; l < 15; l++ {
			fmt.Fprintf(&b, "v%d_%d := compute(%d, %d) // line %d of cell %d\n", i, l, i, l, l, i)
		}
		b.WriteString("```\n\n")
	}
	return b.String()
}

// Memory on a 50-cell notebook, with and without the IntersectionObserver that
// gives the canvases of far-off cells back. Canvas bytes are exact (width x
// height x 4, over the canvases that have a backing store); the processes'
// resident set is what the OS says, after a scroll through the whole notebook
// and a collection, and moves with everything else the machine does.
func TestMeasureMemoryOf50Cells(t *testing.T) {
	measuring(t)
	for _, tc := range []struct {
		name   string
		before []string
	}{
		{"with the IntersectionObserver", nil},
		{"without it", []string{`window.gonbNoIO = true`}},
		{"textarea page (no editor)", []string{`window.gonbNoEditor = true`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := open(t, serve(t), 1000, 800, 2, tc.before...)
			if tc.name != "textarea page (no editor)" {
				editorUp(t, ctx)
			} else {
				until(t, ctx, "the runtime", 3*time.Minute, `window.playgroundTimings && window.playgroundTimings.runtimeReady != null`)
			}
			openNotebook(t, ctx, fiftyCells(), -50)
			if tc.name != "textarea page (no editor)" {
				until(t, ctx, "50 surfaces", time.Minute, `playgroundEditor.surfaces.size === 50`)
			}
			// a scroll through the whole notebook, and back to the top
			h := ev[int](t, ctx, `document.documentElement.scrollHeight`)
			for y := 0; y < h; y += 500 {
				ev[any](t, ctx, fmt.Sprintf(`window.scrollTo(0, %d)`, y))
				time.Sleep(60 * time.Millisecond)
			}
			ev[any](t, ctx, `window.scrollTo(0, 0)`)
			time.Sleep(1500 * time.Millisecond)
			if _, err := chromedp.Call(ctx, heapprofiler.CollectGarbage, cdp.Empty{}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(500 * time.Millisecond)
			canvases := ev[[]float64](t, ctx, `(() => { let b = 0, n = 0, live = 0;
  for (const c of document.querySelectorAll('#notebook canvas')) { n++; if (c.width * c.height) live++; b += c.width * c.height * 4; }
  return [n, live, b]; })()`)
			info, err := chromedp.CallBrowser(ctx, systeminfo.GetProcessInfo, cdp.Empty{})
			if err != nil {
				t.Fatal(err)
			}
			var renderer, gpu int
			for _, p := range info.ProcessInfo {
				switch p.Type {
				case "renderer":
					renderer += residentKiB(t, p.ID)
				case "GPU":
					gpu += residentKiB(t, p.ID)
				}
			}
			t.Logf("%-30s canvases %2.0f, with a backing store %2.0f, canvas bytes %6.1f MB; resident: renderers %4d MB, GPU %4d MB",
				tc.name, canvases[0], canvases[1], canvases[2]/1e6, renderer/1024, gpu/1024)
		})
	}
}
