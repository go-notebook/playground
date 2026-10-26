package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-fleettools/landingkit"

	"github.com/go-notebook/playground/engine"
)

// card is one card of the loading carousel the page shows while the core
// downloads (the landing kit's, landingkit.LoadingCarousel): what go-notebook
// does, in words taken from its README and documentation, with a picture
// drawn here.
type card struct {
	title, text, href string
	// The picture: a Go cell run by the engine, showing its value or its
	// figure (run), or a cell or a console session shown as it is.
	lang, code string
	run        bool
}

// cards are the carousel's cards. The pictures of the Go cells are what the
// engine gives for them when the playground is built, never drawn by hand.
var cards = []card{
	{
		title: "Cells",
		text:  "A notebook is a Markdown file whose ```go blocks are cells: what a cell defines, the cells after it see, and its final expression is shown.",
		href:  "https://go-notebook.github.io/docs/latest/cells/",
		lang:  "go", run: true,
		code: "import \"strings\"\n\nfunc words(s string) []string { return strings.Fields(s) }\n\nw := words(\"to be or not to be\")\nlen(w) // the final expression is shown\n",
	},
	{
		title: "A reactive kernel",
		text:  "go-notebook's kernel compiles the notebook into a real Go program, orders the cells by what they use, and runs only what changed.",
		href:  "https://go-notebook.github.io/docs/latest/kernel/",
		lang:  "console",
		code:  "$ notebook run spectrum.md\n── cell 0 (line 4, kept)\n── cell 1 (line 12, kept)\n── cell 2 (line 19, ran in 48µs)\n   9.54929284535666e-07\n",
	},
	{
		title: "Ruby cells",
		text:  "```ruby blocks run in rbgo, a Ruby written in Go, with the go-scikit bindings, and share their variables with the Go cells.",
		href:  "https://go-notebook.github.io/docs/latest/ruby/",
		lang:  "ruby",
		code:  "fit = Stats::Gamma.fit(waits, fixed: {loc: 0})\nshape, scale = fit.a, fit.scale\nks = Stats.ks_1samp(waits) { |x| fit.cdf(x) }\nfit\n",
	},
	{
		title: "Figures",
		text:  "A go-plot figure is shown inline, as SVG. A figure is data: two sanitizers keep it from running code or fetching anything.",
		href:  "https://github.com/go-notebook/playground#figures-are-data",
		lang:  "go", run: true,
		code: "import (\n\t\"math\"\n\n\t\"github.com/go-plot/plot\"\n)\n\nx := make([]float64, 400)\ny := make([]float64, 400)\nfor i := range x {\n\tx[i] = float64(i) / 20\n\ty[i] = math.Sin(2*x[i]) * math.Exp(-x[i]/6)\n}\nfig := plot.NewFigure(plot.FigSize(6.4, 3.6))\nax := fig.AddAxes(0.1, 0.16, 0.86, 0.76)\nax.Plot(x, y)\nax.SetXLabel(\"t\")\nfig\n",
	},
	{
		title: "Export to HTML",
		text:  "notebook html writes the notebook as a page with no scripts and no external resources; notebook serve shows it in your browser.",
		href:  "https://go-notebook.github.io/docs/latest/cli/",
		lang:  "console",
		code:  "$ notebook html analysis.md\n  # writes analysis.html: no scripts,\n  # no external resources\n$ notebook serve analysis.md\n  # the same in your browser,\n  # at http://127.0.0.1:8888\n",
	},
}

// runCell is engine.Run, replaced by tests.
var runCell = engine.Run

// carousel writes the cards' pictures into dir/cards and returns the
// carousel's markup.
func carousel(dir string, accent string) (template.HTML, error) {
	if err := os.MkdirAll(filepath.Join(dir, "cards"), 0o755); err != nil {
		return "", err
	}
	sum := sha256.New()
	lk := make([]landingkit.Card, len(cards))
	for i, c := range cards {
		svg, err := picture(c, accent)
		if err != nil {
			return "", fmt.Errorf("the picture of %q: %w", c.title, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cards", fmt.Sprintf("%d.svg", i+1)), []byte(svg), 0o644); err != nil {
			return "", err
		}
		sum.Write([]byte(svg))
		lk[i] = landingkit.Card{Title: c.title, Text: c.text, Href: c.href}
	}
	return landingkit.LoadingCarousel(landingkit.Carousel{
		Cards:    lk,
		Pictures: "cards/", Ext: ".svg",
		// a new picture is a new URL: a cached one is the one this page expects
		Suffix: "?v=" + hex.EncodeToString(sum.Sum(nil))[:12],
		Label:  "What go-notebook does",
		Start:  "Open the notebook",
		Fade:   "loading",
	})
}

// picture draws a card's picture: the figure a Go cell shows, or the cell
// (and the value it shows) as text on a code panel.
func picture(c card, accent string) (string, error) {
	var shown []string
	if c.run {
		rs := runCell("```go\n" + c.code + "```\n")
		if len(rs) != 1 || rs[0].Error != "" || len(rs[0].Display) == 0 {
			return "", fmt.Errorf("the cell shows nothing: %+v", rs)
		}
		d := rs[0].Display[0]
		if svg := d["image/svg+xml"]; svg != "" {
			return svg, nil // sanitized by the engine
		}
		shown = strings.Split(d["text/plain"], "\n")
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(c.code, "\t", "    "), "\n"), "\n")
	const lineH, top, left = 26, 40, 34
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360" viewBox="0 0 640 360">`+
		`<style>.bg{fill:#f6f7f9}.t{fill:#1d1d1f}.m{fill:#5f6368}`+
		`@media (prefers-color-scheme:dark){.bg{fill:#1a1d22}.t{fill:#e6e6e6}.m{fill:#a0a4ab}}`+
		`text{font:17px ui-monospace,"SF Mono",Menlo,Consolas,monospace;white-space:pre}</style>`+
		`<rect class="bg" width="640" height="360"/>`)
	if c.lang != "console" {
		fmt.Fprintf(&b, `<rect x="16" y="14" width="5" height="%d" fill="%s"/>`, len(lines)*lineH+12, html.EscapeString(accent))
	}
	y := top
	for _, l := range lines {
		cls := "t"
		if strings.HasPrefix(strings.TrimSpace(l), "#") || strings.HasPrefix(l, "──") {
			cls = "m"
		}
		fmt.Fprintf(&b, `<text class="%s" x="%d" y="%d" xml:space="preserve">%s</text>`, cls, left, y, html.EscapeString(l))
		y += lineH
	}
	for _, l := range shown {
		fmt.Fprintf(&b, `<text class="m" x="%d" y="%d" xml:space="preserve">→ %s</text>`, left, y+10, html.EscapeString(l))
		y += lineH
	}
	b.WriteString(`</svg>`)
	return b.String(), nil
}
