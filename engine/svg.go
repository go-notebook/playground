package engine

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// SanitizeSVG rewrites a figure keeping only what draws: an allowlist of
// SVG elements and attributes, written back by this code, so that nothing
// a cell shows can run code in the page or fetch anything. A cell's figure
// is data, even when the notebook came from someone else, and a Go value's
// SaveSVG or a Ruby cell can write any text at all.
//
// What goes, by class:
//   - scripts and anything that can hold them or HTML: script, style,
//     foreignObject, iframe, and every element outside the allowlist, with
//     its content (an <a> alone is unwrapped: its content stays);
//   - event handlers and every attribute outside the allowlist;
//   - animation (set, animate, animateMotion, animateTransform), which can
//     change an attribute after this check;
//   - external references: an href that is not a "#fragment" (or, on an
//     <image>, an embedded PNG, JPEG or GIF), a url() that is not url(#…),
//     and style properties outside the presentation allowlist;
//   - comments, processing instructions and DOCTYPEs (entities).
//
// The allowlist follows DOMPurify's SVG lists (src/tags.ts), narrower:
// <use> and <style>, which DOMPurify also refuses or rewrites, are dropped.
// A document that is not well-formed XML, too large or too deep, or whose
// root is not <svg>, is refused whole, with an error. Only the first root
// element is kept.
func SanitizeSVG(in string) (out string, err error) {
	if len(in) > maxSVG {
		return "", fmt.Errorf("figure of %d bytes, more than %d", len(in), maxSVG)
	}
	d := xml.NewDecoder(strings.NewReader(in))
	d.Strict = true
	var b bytes.Buffer
	// skip counts the open elements of a subtree being dropped; open
	// records, for each element written or unwrapped, whether it was
	// written, so that its end tag is written or not.
	skip, depth := 0, 0
	var open []bool
	root := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("figure: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > maxDepth {
				return "", fmt.Errorf("figure nested more than %d deep", maxDepth)
			}
			if skip > 0 {
				skip++
				continue
			}
			name, ok := svgElement(t.Name)
			if !root {
				if !ok || name != "svg" {
					return "", errors.New("figure: the root element is not <svg>")
				}
				root = true
				b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"`)
				writeAttrs(&b, name, t.Attr)
				b.WriteByte('>')
				open = append(open, true)
				continue
			}
			switch {
			case len(open) == 0: // a second root
				skip = 1
			case name == "a" && ok:
				open = append(open, false)
			case ok && name != "svg":
				b.WriteString("<" + name)
				writeAttrs(&b, name, t.Attr)
				b.WriteByte('>')
				open = append(open, true)
			default:
				skip = 1
			}
		case xml.EndElement:
			depth--
			if skip > 0 {
				skip--
				continue
			}
			if open[len(open)-1] {
				name, _ := svgElement(t.Name)
				b.WriteString("</" + name + ">")
			}
			open = open[:len(open)-1]
		case xml.CharData:
			if skip == 0 && len(open) > 0 {
				_ = xml.EscapeText(&b, t) // a buffer does not fail
			}
		}
	}
	if !root {
		return "", errors.New("figure: no <svg> element")
	}
	return b.String(), nil
}

// Bounds on what is parsed: a figure with an embedded raster can be large,
// but not without limit, and a deep nest is not a figure.
const (
	maxSVG   = 32 << 20
	maxDepth = 256
)

const (
	nsSVG   = "http://www.w3.org/2000/svg"
	nsXLink = "http://www.w3.org/1999/xlink"
)

// svgElement returns the element's name as written back, and whether it is
// allowed: an SVG element (in the SVG namespace, or in none) of the list.
func svgElement(n xml.Name) (string, bool) {
	if n.Space != nsSVG && n.Space != "" {
		return "", false
	}
	name, ok := svgElements[n.Local]
	return name, ok
}

var svgElements = set("svg", "g", "defs", "title", "desc", "path", "rect", "circle", "ellipse", "line",
	"polyline", "polygon", "text", "tspan", "clipPath", "mask", "linearGradient", "radialGradient", "stop",
	"pattern", "symbol", "marker", "image", "a")

// svgAttrs are the attributes kept, in no namespace; presentation
// attributes are also the style properties kept.
var svgAttrs = set("id", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry", "fx", "fy",
	"width", "height", "d", "points", "transform", "viewBox", "preserveAspectRatio", "version",
	"dx", "dy", "rotate", "textLength", "lengthAdjust", "offset",
	"gradientUnits", "gradientTransform", "spreadMethod", "patternUnits", "patternContentUnits",
	"patternTransform", "clipPathUnits", "maskUnits", "maskContentUnits",
	"markerWidth", "markerHeight", "markerUnits", "refX", "refY", "orient", "style", "href")

var presentation = set("fill", "fill-opacity", "fill-rule", "stroke", "stroke-width", "stroke-opacity",
	"stroke-linecap", "stroke-linejoin", "stroke-dasharray", "stroke-dashoffset", "stroke-miterlimit",
	"opacity", "clip-path", "clip-rule", "mask", "font-family", "font-size", "font-weight", "font-style",
	"text-anchor", "dominant-baseline", "letter-spacing", "stop-color", "stop-opacity",
	"marker-start", "marker-mid", "marker-end", "visibility", "display", "vector-effect",
	"shape-rendering", "image-rendering", "color")

func set(names ...string) map[string]string {
	m := make(map[string]string, len(names))
	for _, n := range names {
		m[n] = n
	}
	return m
}

// writeAttrs writes the attributes of an element that are kept.
func writeAttrs(b *bytes.Buffer, elem string, attrs []xml.Attr) {
	for _, a := range attrs {
		name, value, ok := keepAttr(elem, a)
		if !ok {
			continue
		}
		b.WriteString(" " + name + `="`)
		_ = xml.EscapeText(b, []byte(value)) // a buffer does not fail
		b.WriteByte('"')
	}
}

// keepAttr decides one attribute: the name to write, and its value.
func keepAttr(elem string, a xml.Attr) (string, string, bool) {
	switch {
	case a.Name.Space == nsXLink && a.Name.Local == "href":
		return "xlink:href", a.Value, safeHref(elem, a.Value)
	case a.Name.Space != "":
		return "", "", false // xmlns declarations, xml:*, foreign namespaces
	case a.Name.Local == "href":
		return "href", a.Value, safeHref(elem, a.Value)
	case a.Name.Local == "style":
		v := safeStyle(a.Value)
		return "style", v, v != ""
	}
	if _, ok := presentation[a.Name.Local]; ok {
		return a.Name.Local, a.Value, safeValue(a.Value)
	}
	if _, ok := svgAttrs[a.Name.Local]; ok {
		return a.Name.Local, a.Value, safeValue(a.Value)
	}
	return "", "", false
}

// embedded is an image embedded in the document, which loads nothing.
var embedded = regexp.MustCompile(`^data:image/(png|jpeg|gif);base64,[A-Za-z0-9+/=\s]*$`)

// safeHref keeps a reference inside the document, and on <image> an
// embedded raster.
func safeHref(elem, v string) bool {
	if strings.HasPrefix(v, "#") {
		return safeValue(v)
	}
	return elem == "image" && embedded.MatchString(v)
}

// safeValue refuses a value that could reach outside the document or be
// read as more than a value: a url() that is not a fragment, a CSS escape
// or comment that could hide one, markup, or a control character.
func safeValue(v string) bool {
	if strings.ContainsAny(v, "\\<>") || strings.Contains(v, "/*") {
		return false
	}
	for _, r := range v {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0x7f {
			return false
		}
	}
	lower := strings.ToLower(v)
	for i := strings.Index(lower, "url("); i >= 0; i = strings.Index(lower, "url(") {
		rest := strings.TrimLeft(lower[i+4:], " \t\n\r\"'")
		if !strings.HasPrefix(rest, "#") {
			return false
		}
		lower = rest
	}
	return !strings.Contains(lower, "image-set(") && !strings.Contains(lower, "expression(") &&
		!strings.Contains(lower, "javascript:")
}

// safeStyle keeps the declarations of a style attribute that set a
// presentation property to a safe value, and drops the rest.
func safeStyle(v string) string {
	var kept []string
	for _, decl := range strings.Split(v, ";") {
		prop, val, ok := strings.Cut(decl, ":")
		prop = strings.ToLower(strings.TrimSpace(prop))
		if _, known := presentation[prop]; !ok || !known || !safeValue(val) {
			continue
		}
		kept = append(kept, prop+":"+strings.TrimSpace(val))
	}
	return strings.Join(kept, ";")
}
