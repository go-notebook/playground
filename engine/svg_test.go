package engine

import (
	"bytes"
	"encoding/xml"
	"io"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/go-plot/plot"
)

const svgOpen = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">`

// One case per hostile input class: what goes in, and what must not come
// out (each a regexp, matched case-insensitively), and what must (if any).
var hostileSVG = []struct {
	class, in string
	never     []string
	keeps     string
}{
	// Scripts, and what can carry them.
	{"script element", svgOpen + `<script>alert(1)</script><rect width="1"/></svg>`, []string{`script`, `alert`}, `<rect width="1"></rect>`},
	{"script, CDATA", svgOpen + `<script><![CDATA[alert(1)]]></script></svg>`, []string{`script`, `alert`}, ""},
	{"script in the HTML namespace", `<svg xmlns="http://www.w3.org/2000/svg" xmlns:h="http://www.w3.org/1999/xhtml"><h:script>alert(1)</h:script></svg>`, []string{`script`, `alert`}, ""},
	{"script, upper case", svgOpen + `<SCRIPT>alert(1)</SCRIPT></svg>`, []string{`script`, `alert`}, ""},
	{"script nested in a kept element", svgOpen + `<g><script>alert(1)</script><path d="M0 0"/></g></svg>`, []string{`script`, `alert`}, `<g><path d="M0 0"></path></g>`},
	{"style element", svgOpen + `<style>body{display:none}@import url(https://evil.example/x.css)</style></svg>`, []string{`style`, `evil`, `display`}, ""},
	{"iframe", svgOpen + `<iframe src="https://evil.example"/></svg>`, []string{`iframe`, `evil`}, ""},
	{"embed and object", svgOpen + `<embed src="x.swf"/><object data="x"/></svg>`, []string{`embed`, `object`, `x\.swf`}, ""},
	{"nested svg", svgOpen + `<svg onload="alert(1)"><rect/></svg></svg>`, []string{`onload`, `alert`, `<svg.*<svg`}, ""},
	{"a second root", svgOpen + `</svg><script>alert(1)</script><g onclick="x"/>`, []string{`script`, `alert`, `onclick`, `</svg>.`}, ""},

	// Event handlers.
	{"onload on the root", `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`, []string{`onload`, `alert`}, "<svg "},
	{"onclick on a shape", svgOpen + `<rect onclick="alert(1)" width="2"/></svg>`, []string{`onclick`, `alert`}, `<rect width="2">`},
	{"handler, upper case", svgOpen + `<rect ONMOUSEOVER="alert(1)"/></svg>`, []string{`onmouseover`, `alert`}, ""},
	{"handler in a namespace", svgOpen + `<rect xlink:onload="alert(1)"/></svg>`, []string{`onload`, `alert`}, ""},
	{"handler and listener elements", svgOpen + `<handler>alert(1)</handler><listener event="load"/></svg>`, []string{`handler`, `listener`, `alert`}, ""},

	// Animation, which changes an attribute after any check.
	{"set an href", svgOpen + `<a><set attributeName="href" to="javascript:alert(1)"/><text>x</text></a></svg>`, []string{`<set`, `javascript`}, `<text>x</text>`},
	{"animate a handler", svgOpen + `<animate attributeName="onclick" values="alert(1)"/></svg>`, []string{`animate`, `alert`}, ""},
	{"animateMotion and animateTransform", svgOpen + `<animateMotion path="M0 0"/><animateTransform type="scale"/></svg>`, []string{`animate`}, ""},

	// Foreign objects.
	{"foreignObject with HTML", svgOpen + `<foreignObject><div xmlns="http://www.w3.org/1999/xhtml"><img src="x" onerror="alert(1)"/></div></foreignObject></svg>`, []string{`foreignobject`, `img`, `onerror`, `alert`, `div`}, ""},
	{"foreignObject, lower case", svgOpen + `<foreignobject><b>x</b></foreignobject></svg>`, []string{`foreignobject`, `<b>`}, ""},
	{"element in another namespace", `<svg xmlns="http://www.w3.org/2000/svg" xmlns:m="http://www.w3.org/1998/Math/MathML"><m:math><m:mi>x</m:mi></m:math></svg>`, []string{`math`, `mi`}, ""},

	// External references.
	{"javascript: link", svgOpen + `<a href="javascript:alert(1)"><text>x</text></a></svg>`, []string{`javascript`, `href`}, `<text>x</text>`},
	{"javascript: behind a tab", svgOpen + `<a xlink:href="java&#9;script:alert(1)"><text>x</text></a></svg>`, []string{`script`, `href`}, ""},
	{"data:text/html link", svgOpen + `<a href=" data:text/html,x"><text>x</text></a></svg>`, []string{`data:`, `href`}, ""},
	{"remote image", svgOpen + `<image href="https://evil.example/t.png"/></svg>`, []string{`evil`, `href`}, "<image></image>"},
	{"remote image, xlink", svgOpen + `<image xlink:href="//evil.example/t.png"/></svg>`, []string{`evil`, `href`}, ""},
	{"SVG as an embedded image", svgOpen + `<image href="data:image/svg+xml;base64,PHN2Zy8+"/></svg>`, []string{`svg\+xml`, `href`}, ""},
	{"use of a remote document", svgOpen + `<use href="https://evil.example/s.svg#x"/></svg>`, []string{`<use`, `evil`}, ""},
	{"use of a fragment", svgOpen + `<use href="#x"/></svg>`, []string{`<use`}, ""},
	{"remote gradient", svgOpen + `<linearGradient id="g" href="https://evil.example/g.svg#g"/></svg>`, []string{`evil`, `href`}, `<linearGradient id="g">`},
	{"remote fill", svgOpen + `<rect fill="url(https://evil.example/p.svg#p)"/></svg>`, []string{`evil`, `fill`}, ""},
	{"remote fill, quoted, upper case", svgOpen + `<rect fill="URL( 'https://evil.example/p')"/></svg>`, []string{`evil`, `fill`}, ""},
	{"remote filter in style", svgOpen + `<rect style="fill:red;filter:url(https://evil.example/f)"/></svg>`, []string{`evil`, `filter`}, `style="fill:red"`},
	{"background in style", `<svg xmlns="http://www.w3.org/2000/svg" style="background:url(https://evil.example/b.png)"/>`, []string{`evil`, `background`}, ""},
	{"image-set in style", svgOpen + `<rect style="fill:image-set('https://evil.example/i.png' 1x)"/></svg>`, []string{`evil`, `image-set`}, ""},
	{"CSS escape hiding url(", svgOpen + `<rect style="fill:u\72l(https://evil.example/x)"/></svg>`, []string{`evil`}, ""},
	{"CSS comment in a value", svgOpen + `<rect fill="ur/**/l(x)"/></svg>`, []string{`fill`}, ""},
	{"remote clip-path", svgOpen + `<g clip-path="url(https://evil.example/c#c)"/></svg>`, []string{`evil`, `clip-path`}, "<g></g>"},
	{"DOCTYPE with an external entity", `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]>` + svgOpen + `<text>&x;</text></svg>`, []string{`passwd`, `doctype`, `entity`}, ""},
	{"processing instruction and comment", `<?xml-stylesheet href="https://evil.example/s.css"?>` + svgOpen + `<!-- <script>alert(1)</script> --></svg>`, []string{`evil`, `script`, `stylesheet`}, ""},
}

func TestSanitizeSVGHostile(t *testing.T) {
	for _, tc := range hostileSVG {
		out, err := SanitizeSVG(tc.in)
		for _, never := range tc.never {
			if regexp.MustCompile(`(?i)` + never).MatchString(out) {
				t.Errorf("%s: %q survived: %s", tc.class, never, out)
			}
		}
		if tc.keeps != "" && !strings.Contains(out, tc.keeps) {
			t.Errorf("%s: lost %q: %s (%v)", tc.class, tc.keeps, out, err)
		}
		checkClean(t, tc.class, out)
	}
}

// What a figure needs survives: go-plot's own output, an embedded PNG,
// references inside the document, presentation styles, text.
func TestSanitizeSVGKeeps(t *testing.T) {
	in := `<?xml version="1.0" encoding="utf-8" standalone="no"?>` + "\n" + svgOpen +
		`<defs><clipPath id="c1"><rect x="0" y="0" width="10" height="10"/></clipPath>` +
		`<linearGradient id="g"><stop offset="0" stop-color="#fff"/></linearGradient></defs>` +
		`<g clip-path="url(#c1)"><path d="M0 0L1 1" style="fill:none;stroke:#1f77b4;stroke-width:1.5"/></g>` +
		`<rect fill="url( '#g')" width="1"/>` +
		`<image x="0" y="0" width="2" height="2" preserveAspectRatio="none" style="image-rendering:pixelated" xlink:href="data:image/png;base64,iVBORw0KGgo="/>` +
		`<a href="https://example.com"><text x="1" y="2">a &lt; b &amp; c</text></a><title>T</title></svg>` + "\n"
	out, err := SanitizeSVG(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<clipPath id="c1"><rect x="0" y="0" width="10" height="10"></rect></clipPath>`,
		`<stop offset="0" stop-color="#fff"></stop>`,
		`<g clip-path="url(#c1)">`,
		`style="fill:none;stroke:#1f77b4;stroke-width:1.5"`,
		`fill="url( &#39;#g&#39;)"`,
		`xlink:href="data:image/png;base64,iVBORw0KGgo="`,
		`style="image-rendering:pixelated"`,
		`<text x="1" y="2">a &lt; b &amp; c</text>`,
		`<title>T</title>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "example.com") || strings.Contains(out, "<a") {
		t.Errorf("the link survived: %s", out)
	}
	checkClean(t, "keeps", out)
}

// A real go-plot figure comes through with all its drawing.
func TestSanitizeSVGGoPlot(t *testing.T) {
	fig, axs := plot.Subplots(1, 1)
	ax := axs[0][0]
	ax.Plot([]float64{0, 1, 2}, []float64{1, 3, 2})
	ax.SetTitle("t")
	var buf bytes.Buffer
	if err := fig.SaveSVG(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := SanitizeSVG(buf.String())
	if err != nil {
		t.Fatal(err)
	}
	if n, m := strings.Count(buf.String(), "<path"), strings.Count(out, "<path"); n == 0 || n != m {
		t.Errorf("%d paths in, %d out", n, m)
	}
	if strings.Count(buf.String(), `style="`) != strings.Count(out, `style="`) {
		t.Error("a style was dropped from a go-plot figure")
	}
	checkClean(t, "go-plot", out)
}

func TestSanitizeSVGRefused(t *testing.T) {
	for _, tc := range []struct{ name, in, err string }{
		{"empty", "", "no <svg>"},
		{"text only", "hello", "no <svg>"},
		{"not well-formed", svgOpen + "<g></svg>", "figure:"},
		{"unknown entity", svgOpen + "&nbsp;</svg>", "figure:"},
		{"root not svg", `<html><svg/></html>`, "root element is not <svg>"},
		{"root svg in another namespace", `<svg xmlns="http://www.w3.org/1999/xhtml"/>`, "root element is not <svg>"},
		{"too deep", svgOpen + strings.Repeat("<g>", maxDepth) + strings.Repeat("</g>", maxDepth) + "</svg>", "nested more than"},
		{"too large", svgOpen + strings.Repeat(" ", maxSVG) + "</svg>", "more than"},
	} {
		out, err := SanitizeSVG(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.err) || out != "" {
			t.Errorf("%s: %q, %v; want an error containing %q", tc.name, out, err, tc.err)
		}
	}
	// Just under the depth limit is a figure.
	if _, err := SanitizeSVG(svgOpen + strings.Repeat("<g>", maxDepth-1) + strings.Repeat("</g>", maxDepth-1) + "</svg>"); err != nil {
		t.Error(err)
	}
}

func TestSafeValue(t *testing.T) {
	for v, want := range map[string]bool{
		"#fff": true, "url(#a)": true, "url( \"#a\") url(#b)": true, "1 2\t3\n": true,
		"url(x)": false, "url(#a) url(x)": false, "a\\b": false, "<b>": false, "a\x00": false, "a\x7f": false,
		"expression(alert(1))": false, "JavaScript:x": false, "-webkit-image-set(x)": false, "a/*b*/": false,
	} {
		if safeValue(v) != want {
			t.Errorf("safeValue(%q) = %v", v, !want)
		}
	}
	for _, tc := range []struct {
		elem, v string
		want    bool
	}{
		{"linearGradient", "#g", true}, {"pattern", "#a\\62", false}, {"rect", "data:image/png;base64,AA==", false},
		{"image", "data:image/png;base64,AA==", true}, {"image", "data:image/png;base64,AA==\"x", false},
	} {
		if safeHref(tc.elem, tc.v) != tc.want {
			t.Errorf("safeHref(%q, %q) = %v", tc.elem, tc.v, !tc.want)
		}
	}
	if got := safeStyle("FILL : red; stroke:url(x);cursor:pointer;junk;fill-opacity:.5"); got != "fill:red;fill-opacity:.5" {
		t.Errorf("safeStyle: %q", got)
	}
}

// A figure goes through Bundle only sanitized; one that cannot be read is
// shown as text.
type svgFrom string

func (s svgFrom) SaveSVG(w io.Writer) error { _, err := io.WriteString(w, string(s)); return err }

type svgDisplay struct{}

func (svgDisplay) Display() map[string][]byte {
	return map[string][]byte{"image/svg+xml": []byte(svgOpen + `<script>alert(1)</script></svg>`)}
}

func TestBundleSanitizes(t *testing.T) {
	b := Bundle(svgFrom(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect/></svg>`))
	if svg := b["image/svg+xml"]; strings.Contains(svg, "onload") || !strings.Contains(svg, "<rect>") {
		t.Errorf("figure: %q", svg)
	}
	b = Bundle(svgFrom("<svg><unclosed></svg>"))
	if _, ok := b["image/svg+xml"]; ok || !strings.Contains(b["text/plain"], "figure:") {
		t.Errorf("malformed figure: %q", b)
	}
	b = Bundle(svgDisplay{})
	if strings.Contains(b["image/svg+xml"], "script") {
		t.Errorf("displayed figure: %q", b)
	}
}

// checkClean parses the output again, as the page will, and checks every
// element and attribute against the allowlist.
func checkClean(t *testing.T, name, out string) {
	t.Helper()
	if out == "" {
		return
	}
	d := xml.NewDecoder(strings.NewReader(out))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s: the output does not parse: %v\n%s", name, err, out)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			if _, ok := svgElements[tok.Name.Local]; !ok || tok.Name.Local == "a" || tok.Name.Space != nsSVG {
				t.Errorf("%s: element %v in the output", name, tok.Name)
			}
			for _, a := range tok.Attr {
				_, inSVG := svgAttrs[a.Name.Local]
				_, inPres := presentation[a.Name.Local]
				switch {
				case a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns":
				case a.Name.Space == nsXLink && a.Name.Local == "href":
				case a.Name.Space == "" && (inSVG || inPres):
				default:
					t.Errorf("%s: attribute %v in the output", name, a.Name)
				}
				if !safeValue(a.Value) {
					t.Errorf("%s: unsafe value %s=%q", name, a.Name.Local, a.Value)
				}
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			t.Errorf("%s: %T in the output", name, tok)
		}
	}
}

// FuzzSanitizeSVG: whatever comes in, the output parses, holds only the
// allowlist, and is a fixed point.
func FuzzSanitizeSVG(f *testing.F) {
	for _, tc := range hostileSVG {
		f.Add(tc.in)
	}
	f.Add(svgOpen + `<g clip-path="url(#c)"><path d="M0 0" style="fill:red"/></g></svg>`)
	f.Fuzz(func(t *testing.T, in string) {
		if len(in) > math.MaxUint16 {
			return
		}
		out, err := SanitizeSVG(in)
		if err != nil {
			if out != "" {
				t.Fatalf("an error and output: %v %q", err, out)
			}
			return
		}
		checkClean(t, "fuzz", out)
		again, err := SanitizeSVG(out)
		if err != nil || again != out {
			t.Fatalf("not a fixed point: %v\n%q\n%q", err, out, again)
		}
	})
}
