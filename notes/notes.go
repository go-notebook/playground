// Package notes is the storage of a code cell's line annotations (comments on
// lines of code) inside a notebook's Markdown.
//
// The format is one HTML comment on the line right after the cell's closing
// fence:
//
//	```go
//	x := 1
//	y := 2
//	```
//	<!-- gonb:notes {"v":1,"notes":[{"line":2,"text":"why two"}]} -->
//
// It is invisible in every Markdown viewer and on GitHub; go-notebook's format
// package keeps it, byte for byte, as the first line of the prose that follows
// the cell, so it survives a parse and a write; and it is not part of any code
// cell, so what a notebook runs is the same with or without it. "line" is
// 1-based, in the cell's own source.
//
// The comment is read back from an untrusted file: it is bounded in size,
// count and text length, the text is plain (control characters dropped), a
// line outside the cell is dropped by the caller, and a comment that does not
// parse is left alone as prose rather than guessed at. The JSON is written
// with "<", ">" and "-" escaped, so no text can close the comment early.
package notes

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Bounds. MaxNotes and MaxRunes are the toolkit's own (toolkit.MaxAnnotations,
// toolkit.MaxAnnotationRunes); MaxComment bounds the whole comment line.
const (
	MaxNotes   = 256
	MaxRunes   = 2000
	MaxComment = 1 << 20
)

const (
	prefix = "<!-- gonb:notes "
	suffix = " -->"
)

// Note is a comment on one line of a code cell.
type Note struct {
	// Line is the 1-based line of the cell's source.
	Line int `json:"line"`
	// Text is the comment, plain text.
	Text string `json:"text"`
}

type payload struct {
	V     int    `json:"v"`
	Notes []Note `json:"notes"`
}

// Clean returns notes bounded and plain: control characters dropped (a tab
// becomes a space), text cut to MaxRunes, a note with no text or a line below 1
// dropped, one note per line (notes on the same line are joined, in order), at most MaxNotes, ordered by line.
func Clean(notes []Note) []Note {
	last := map[int]string{}
	for _, n := range notes {
		if n.Line < 1 {
			continue
		}
		if t := Sanitize(n.Text); strings.TrimSpace(t) != "" {
			if prev, ok := last[n.Line]; ok {
				t = Sanitize(prev + "\n" + t) // two on one line: neither is lost
			}
			last[n.Line] = t
		}
	}
	out := make([]Note, 0, len(last))
	for l, t := range last {
		out = append(out, Note{Line: l, Text: t})
	}
	for i := 1; i < len(out); i++ { // insertion sort: at most MaxNotes
		for j := i; j > 0 && out[j].Line < out[j-1].Line; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > MaxNotes {
		out = out[:MaxNotes]
	}
	return out
}

// Sanitize is the plain, bounded text of a comment (the same rule as
// toolkit.SanitizeAnnotation, which the editor applies to what it shows).
func Sanitize(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= MaxRunes {
			break
		}
		switch {
		case r == utf8.RuneError:
			continue
		case r == '\t':
			r = ' '
		case r == '\r':
			continue
		case r < 0x20 && r != '\n', r == 0x7f, r >= 0x80 && r < 0xa0:
			continue
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// Line is the comment line for notes, newline included, or "" when there are
// none.
func Line(notes []Note) string {
	notes = Clean(notes)
	if len(notes) == 0 {
		return ""
	}
	b, _ := json.Marshal(payload{V: 1, Notes: notes}) // a struct of ints and strings always encodes; < > & become \u00XX
	js := string(b)
	js = strings.ReplaceAll(js, "-", "\\"+"u002d") // no "--" can end the comment: "-" only occurs inside strings
	return prefix + js + suffix + "\n"
}

// Split reads the comment at the start of the prose that follows a code cell.
// It returns the notes and the prose without that line; ok is false, and rest
// is prose unchanged, when the prose does not start with a well-formed comment.
func Split(prose string) (found []Note, rest string, ok bool) {
	if !strings.HasPrefix(prose, prefix) {
		return nil, prose, false
	}
	line, after, _ := strings.Cut(prose, "\n")
	if len(line) > MaxComment || len(line) < len(prefix)+len(suffix) || !strings.HasSuffix(line, suffix) {
		return nil, prose, false
	}
	var p payload
	dec := json.NewDecoder(strings.NewReader(line[len(prefix) : len(line)-len(suffix)]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil || p.V != 1 || dec.More() {
		return nil, prose, false
	}
	if len(p.Notes) > MaxNotes {
		p.Notes = p.Notes[:MaxNotes]
	}
	return Clean(p.Notes), after, true
}
