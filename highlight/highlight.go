// Package highlight colours a cell's source with go-rouge, for the page's
// editors: the same lexers and the same token classes as go-notebook's HTML
// export, so there is one highlighter and one palette.
//
// The result is inserted into the page as HTML, so it is never trusted
// because rouge produced it: HTML checks it before returning it, and
// refuses (the editor then shows plain text) unless it is made only of
// <span class="..."> elements whose text, with the three entities rouge
// writes decoded, is the source byte for byte. The page's overlay depends on
// that equality: a character the highlight lost or added would put every
// colour after it out of line with the caret.
package highlight

import (
	"strings"

	"github.com/go-rouge/rouge"
)

// MaxSource is the largest cell, in bytes, that is highlighted. A bigger one
// is shown as plain text: lexing is linear, but the page's latency budget is
// per keystroke, and a notebook is capped at 1 MiB (engine.MaxNotebook).
const MaxSource = 256 << 10

// HTML returns src coloured with the go-rouge lexer named lexer, as HTML
// made only of <span class="..."> elements, and whether it did. It does not
// when lexer is empty or unknown (a language without a go-rouge lexer is
// shown as text), when src is over MaxSource, or when the output fails the
// check above; the caller shows src as plain text.
func HTML(lexer, src string) (string, bool) {
	if lexer == "" || len(src) > MaxSource {
		return "", false
	}
	out, err := rouge.Highlight(src, lexer, "html")
	if err != nil || !safe(out, src) {
		return "", false
	}
	return out, true
}

// safe reports whether h is only text and <span class="..."> elements, with
// text that decodes to src.
//
// The grammar is the one rouge's HTML formatter writes and nothing wider:
// text has no '<' or '>' and no '&' but as &amp; &lt; &gt;; a span opens
// with exactly `<span class="` and one to sixteen letters and digits, and is
// closed before the next opens (rouge's spans do not nest). A quote in text
// is allowed: it is harmless outside an attribute, and rouge leaves it as it
// is.
func safe(h, src string) bool {
	var text strings.Builder
	text.Grow(len(src))
	open := false
	for i := 0; i < len(h); {
		switch c := h[i]; c {
		case '<':
			if open {
				if !strings.HasPrefix(h[i:], "</span>") {
					return false
				}
				open, i = false, i+len("</span>")
				continue
			}
			const head = `<span class="`
			if !strings.HasPrefix(h[i:], head) {
				return false
			}
			i += len(head)
			n := 0
			for i < len(h) && isClass(h[i]) {
				i, n = i+1, n+1
			}
			if n == 0 || n > 16 || !strings.HasPrefix(h[i:], `">`) {
				return false
			}
			open, i = true, i+2
		case '>':
			return false
		case '&':
			switch {
			case strings.HasPrefix(h[i:], "&amp;"):
				text.WriteByte('&')
				i += len("&amp;")
			case strings.HasPrefix(h[i:], "&lt;"):
				text.WriteByte('<')
				i += len("&lt;")
			case strings.HasPrefix(h[i:], "&gt;"):
				text.WriteByte('>')
				i += len("&gt;")
			default:
				return false
			}
		default:
			text.WriteByte(c)
			i++
		}
	}
	return !open && text.String() == src
}

// isClass reports whether c may be in a token class: rouge's are one to
// three letters, and the check allows letters and digits.
func isClass(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
