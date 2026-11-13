// Package browsertest runs the playground's page in a real browser, headless
// Chrome through chromedp, for what a native test cannot reach: the code editor
// on its canvas, the accessibility tree, IME, find-in-page, selection and the
// wheel. It is a module of its own so that the playground itself depends on
// nothing for it.
//
//	go run ./cmd/build -o <dir> --org go-notebook examples/notebook/*.md
//	cd browsertest && PLAYGROUND_SITE=<dir> CHROME=<chrome> go test -count=1 -v ./...
//
// Without PLAYGROUND_SITE the tests skip, and CI requires their PASS lines, so a
// skip there fails the job.
package browsertest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// siteDir is the built playground, from PLAYGROUND_SITE.
func siteDir(t testing.TB) string {
	t.Helper()
	d := os.Getenv("PLAYGROUND_SITE")
	if d == "" {
		t.Skip("PLAYGROUND_SITE is not set: build the playground with cmd/build and name its directory")
	}
	return d
}

// serve serves the built playground; the paths in refuse answer 404.
func serve(t testing.TB, refuse ...string) string {
	t.Helper()
	files := http.FileServer(http.Dir(siteDir(t)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range refuse {
			if r.URL.Path == "/"+p {
				http.NotFound(w, r)
				return
			}
		}
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/index.html"
}

func browser(t testing.TB) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("headless", true))
	if p := os.Getenv("CHROME"); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	actx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancel)
	ctx, cancel2 := chromedp.NewContext(actx)
	t.Cleanup(cancel2)
	ctx, cancel3 := context.WithTimeout(ctx, 10*time.Minute)
	t.Cleanup(cancel3)
	if _, err := chromedp.Run(ctx, chromedp.Navigate("about:blank")); err != nil {
		t.Skipf("no Chrome to run the page in: %v", err)
	}
	return ctx
}

func ev[T any](t testing.TB, ctx context.Context, js string) T {
	t.Helper()
	v, err := chromedp.Run(ctx, chromedp.Evaluate[T](js))
	if err != nil {
		t.Fatalf("%s: %v", js, err)
	}
	return v
}

// until polls js, an expression, until it is true, or fails the test.
func until(t testing.TB, ctx context.Context, what string, limit time.Duration, js string) {
	t.Helper()
	for deadline := time.Now().Add(limit); ; time.Sleep(50 * time.Millisecond) {
		if ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js)); err == nil && ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", limit, what)
		}
	}
}

// open loads the page, with a viewport of w x h at a device pixel ratio. Scripts
// run before the page's own, for a test that sets a switch.
func open(t testing.TB, url string, w, h int64, dpr float64, before ...string) context.Context {
	t.Helper()
	ctx := browser(t)
	if _, err := chromedp.Call(ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{Width: w, Height: h, DeviceScaleFactor: dpr}); err != nil {
		t.Fatal(err)
	}
	for _, js := range before {
		if _, err := chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: js}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Call(ctx, emulation.SetFocusEmulationEnabled, emulation.SetFocusEmulationEnabledParams{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// editorUp waits for the editor to have loaded (it follows the first runtime).
func editorUp(t testing.TB, ctx context.Context) {
	t.Helper()
	until(t, ctx, "the editor to load", 3*time.Minute, `window.playgroundTimings && window.playgroundTimings.editorReady != null`)
}

// openNotebook opens a notebook through the page's Open control, as a visitor
// does, and waits for its cells.
func openNotebook(t testing.TB, ctx context.Context, md string, cells int) {
	t.Helper()
	ev[any](t, ctx, `(() => {
  const dt = new DataTransfer();
  dt.items.add(new File([`+strconv.Quote(md)+`], 'editor-test.md', { type: 'text/markdown' }));
  const i = document.getElementById('open');
  i.files = dt.files;
  i.dispatchEvent(new Event('change'));
})()`)
	sel := `#notebook .cell`
	if cells < 0 { // a negative count is of code cells only
		sel, cells = `#notebook .cell[data-lang]`, -cells
	}
	until(t, ctx, "the notebook's cells", time.Minute, fmt.Sprintf(`document.querySelectorAll('%s').length === %d`, sel, cells))
}

const (
	cell1 = "x := 21\nx * 2"
	cell2 = "y := x + 1\ny * 3"
)

// testNotebook: prose, a code cell, prose, a code cell, prose.
var testNotebook = "# Editor test\n\nProse before the code.\n\n```go\n" + cell1 + "\n```\n\nProse between the cells.\n\n```go\n" + cell2 + "\n```\n\nProse after the code.\n"

const testCells = 5 // the notebook's prose and code cells

// textareaOf is the text of the n-th (0-based) code cell's textarea.
func textareaOf(n int) string {
	return fmt.Sprintf(`document.querySelectorAll('#notebook .cell[data-lang]')[%d].querySelector('textarea').value`, n)
}

// centre is the middle of the n-th code cell's canvas, in CSS pixels of the
// viewport, after scrolling it into view.
func centre(t testing.TB, ctx context.Context, n int) (x, y float64) {
	t.Helper()
	r := ev[[]float64](t, ctx, fmt.Sprintf(`(() => {
  const c = document.querySelectorAll('#notebook .cell[data-lang]')[%d].querySelector('canvas');
  c.scrollIntoView({ block: 'center' });
  const b = c.getBoundingClientRect();
  return [b.left + b.width / 2, b.top + b.height / 2];
})()`, n))
	return r[0], r[1]
}

// --- the accessibility tree ---

type ax struct {
	Role, Name, Value string
	Ignored           bool
	ID                cdp.BackendNodeID
}

func str(v *accessibility.Value) string {
	if v == nil {
		return ""
	}
	if s, err := strconv.Unquote(string(v.Value)); err == nil {
		return s
	}
	return string(v.Value)
}

func axTree(t testing.TB, ctx context.Context) []ax {
	t.Helper()
	res, err := chromedp.Call(ctx, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{})
	if err != nil {
		t.Fatal(err)
	}
	var out []ax
	for _, n := range res.Nodes {
		out = append(out, ax{Role: str(n.Role), Name: str(n.Name), Value: str(n.Value), Ignored: n.Ignored, ID: n.BackendDOMNodeID})
	}
	return out
}

func textboxes(tree []ax) []ax {
	var out []ax
	for _, n := range tree {
		if !n.Ignored && n.Role == "textbox" {
			out = append(out, n)
		}
	}
	return out
}

func itoa(n int) string           { return strconv.Itoa(n) }
func ftoa(f float64) string       { return strconv.FormatFloat(f, 'f', -1, 64) }
func contains(s, sub string) bool { return strings.Contains(s, sub) }
