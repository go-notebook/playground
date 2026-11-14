package engine

import (
	"github.com/go-notebook/notebook/format"
	"github.com/go-notebook/playground/notes"
)

// Line annotations live in the Markdown as one HTML comment after a code cell's
// fence (package notes), which format keeps as the first line of the prose that
// follows the cell. The page never sees that line in the prose: Cells takes it
// out and gives it to the code cell as Notes, and Edit takes it out of the cells
// it works on, carries each cell's notes along with it through insert, delete
// and move, and writes the comments back at the end. So the prose cell is
// edited as the reader sees it, a deleted or moved cell takes its comments with
// it, and what a notebook runs is unchanged: the comment is not in any code cell.

// detach takes the annotation comment out of the prose after each code cell of
// cells (in place) and returns the notes read, indexed like cells. A comment
// that does not parse stays in the prose.
func detach(cells []format.Cell) [][]notes.Note {
	out := make([][]notes.Note, len(cells))
	for i := 0; i+1 < len(cells); i++ {
		if cells[i].Kind != format.Code || cells[i+1].Kind != format.Prose {
			continue
		}
		if n, rest, ok := notes.Split(cells[i+1].Source); ok {
			out[i] = n
			cells[i+1].Source = rest
		}
	}
	return out
}

// attach writes each code cell's notes as a comment line at the start of the
// prose after it, making that prose cell when the cell has none, and drops the
// prose cells detach left empty. keep maps an index of cells to its index in the
// result.
func attach(cells []format.Cell, ns [][]notes.Note) (out []format.Cell, keep []int) {
	keep = make([]int, len(cells))
	for i := 0; i < len(cells); i++ {
		c := cells[i]
		if c.Kind == format.Prose && c.Source == "" {
			// A prose cell that was only the comment: nothing to write. (An empty
			// prose cell is not written by format, so it would not come back.)
			keep[i] = max(len(out)-1, 0)
			continue
		}
		keep[i] = len(out)
		out = append(out, c)
		if c.Kind != format.Code {
			continue
		}
		line := notes.Line(ns[i])
		if line == "" {
			continue
		}
		if i+1 < len(cells) && cells[i+1].Kind == format.Prose {
			next := cells[i+1]
			next.Source = line + next.Source
			cells[i+1] = next
			continue
		}
		out = append(out, format.Cell{Kind: format.Prose, Source: line})
	}
	return out, keep
}
