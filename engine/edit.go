package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-notebook/notebook/format"
)

// The page edits a notebook through the format package: the Markdown is the
// only source of truth. Each edit takes the document, applies operations to
// its parsed cells, and writes it back with format's Notebook.String(), which
// reproduces every cell it did not touch byte for byte (~~~ fences and
// info-string options included). The page never writes Markdown itself.

// MaxNotebook is the largest notebook the page opens or edits, in bytes.
const MaxNotebook = 1 << 20

// Langs are the languages a code cell may be in, as its fence names them
// (a Julia cell is parsed and checked, not run).
var Langs = []string{"go", "ruby", "julia"}

// Op is one edit. Index counts every cell, prose included, as Cells returns
// them.
type Op struct {
	// Op is "source" (set Source), "lang" (set Lang: a language, or
	// "markdown" to make the cell prose), "insert" (a new cell before Index,
	// or at the end when Index is the number of cells), "delete", or "move"
	// (to the position To).
	Op     string `json:"op"`
	Index  int    `json:"index"`
	Source string `json:"source,omitempty"`
	Lang   string `json:"lang,omitempty"`
	To     int    `json:"to,omitempty"`
}

// Edited is a notebook after edits.
type Edited struct {
	Markdown string `json:"markdown"`
	Cells    []Cell `json:"cells"`
	// Focus is the cell the last operation leaves the reader on: the edited
	// or inserted cell, as the document now splits (a prose cell inserted
	// next to prose joins it), or the one after a deleted cell.
	Focus int `json:"focus"`
}

// Edit applies ops, in order, to the cells of markdown as it parses, and
// writes the notebook back. The indices of every op refer to those cells: a
// prose edit that makes a new code fence splits the document only when it is
// parsed again, after the last op.
func Edit(markdown string, ops []Op) (Edited, error) {
	if len(markdown) > MaxNotebook {
		return Edited{}, fmt.Errorf("the notebook is %d bytes; the playground edits notebooks up to %d", len(markdown), MaxNotebook)
	}
	cells := format.Parse(markdown).Cells
	focus := 0
	for _, op := range ops {
		n := len(cells)
		in := op.Index >= 0 && op.Index < n
		switch op.Op {
		case "source":
			if !in {
				return Edited{}, fmt.Errorf("no cell %d", op.Index)
			}
			setSource(&cells[op.Index], op.Source)
			focus = op.Index
		case "lang":
			if !in {
				return Edited{}, fmt.Errorf("no cell %d", op.Index)
			}
			c, err := withLang(cells[op.Index], op.Lang)
			if err != nil {
				return Edited{}, err
			}
			cells[op.Index] = c
			focus = op.Index
		case "insert":
			if op.Index < 0 || op.Index > n {
				return Edited{}, fmt.Errorf("cannot insert at %d of %d cells", op.Index, n)
			}
			c, err := newCell(op.Lang, op.Source)
			if err != nil {
				return Edited{}, err
			}
			cells = slices.Insert(cells, op.Index, c)
			focus = op.Index
		case "delete":
			if !in {
				return Edited{}, fmt.Errorf("no cell %d", op.Index)
			}
			cells = slices.Delete(cells, op.Index, op.Index+1)
			focus = min(op.Index, len(cells)-1)
		case "move":
			if !in || op.To < 0 || op.To >= n {
				return Edited{}, fmt.Errorf("cannot move cell %d to %d of %d", op.Index, op.To, n)
			}
			c := cells[op.Index]
			cells = slices.Insert(slices.Delete(cells, op.Index, op.Index+1), op.To, c)
			focus = op.To
		default:
			return Edited{}, fmt.Errorf("unknown edit %q", op.Op)
		}
	}
	// A prose cell followed by another cell ends its last line.
	for i := range cells {
		if cells[i].Kind == format.Prose && i < len(cells)-1 {
			cells[i].Source = proseText(cells[i].Source)
		}
	}
	// Where the focused cell starts, as a line of the new document, to find
	// it again once the document is parsed.
	nb := &format.Notebook{Cells: cells}
	line := 1
	if focus >= 0 && focus < len(cells) {
		line += strings.Count((&format.Notebook{Cells: cells[:focus]}).String(), "\n")
	}
	out := nb.String()
	if len(out) > MaxNotebook {
		return Edited{}, fmt.Errorf("the notebook would be %d bytes; the playground edits notebooks up to %d", len(out), MaxNotebook)
	}
	e := Edited{Markdown: out, Cells: Cells(out)}
	for i, c := range format.Parse(out).Cells {
		start := c.Line
		if c.Kind == format.Code {
			start-- // its opening fence
		}
		if start <= line {
			e.Focus = i
		}
	}
	return e, nil
}

// setSource sets a cell's text. A code cell's fence is rebuilt by format when
// the new text would close it, and format ends the text with a newline before
// a closing fence.
func setSource(c *format.Cell, src string) {
	if c.Kind == format.Code {
		c.SetSource(src)
		return
	}
	c.Source = src
}

// proseText ends a prose text with a newline, so that a fence after it
// starts a line; the last cell of a document may end without one.
func proseText(src string) string {
	if src != "" && !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	return src
}

// withLang returns c in the language lang, or as prose for "markdown". The
// text is kept as it is: nothing translates code. A code cell that stays code
// keeps its fence; only the first word of the info string changes.
func withLang(c format.Cell, lang string) (format.Cell, error) {
	if lang == "markdown" {
		if c.Kind == format.Prose {
			return c, nil
		}
		return format.Cell{Kind: format.Prose, Source: proseText(c.Source)}, nil
	}
	if !slices.Contains(Langs, lang) {
		return c, fmt.Errorf("%q is not a language a cell can be in here", lang)
	}
	if c.Kind == format.Prose {
		return format.NewCodeIn(lang, c.Source), nil
	}
	// The cell alone, written and parsed again with its fence's first word
	// replaced: the fence character, its length, the indent and the options
	// stay as they were.
	text := (&format.Notebook{Cells: []format.Cell{c}}).String()
	i := strings.Index(text, c.Lang) // the fence is made of ` or ~ and spaces before it
	return format.Parse(text[:i] + lang + text[i+len(c.Lang):]).Cells[0], nil
}

// newCell is a new, empty cell: code in lang, or prose for "markdown".
func newCell(lang, src string) (format.Cell, error) {
	if lang == "markdown" {
		if src == "" {
			src = "\n" // an empty prose cell would not be written at all
		}
		return format.Cell{Kind: format.Prose, Source: proseText(src)}, nil
	}
	if !slices.Contains(Langs, lang) {
		return format.Cell{}, fmt.Errorf("%q is not a language a cell can be in here", lang)
	}
	return format.NewCodeIn(lang, src), nil
}
