package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/go-notebook/playground/notes"
)

const plain = "# T\n\n```go\nx := 1\ny := 2\nx + y\n```\n\nafter\n\n```go\nz := 3\nz\n```\n"

func withNotes(t *testing.T) string {
	t.Helper()
	e, err := Edit(plain, []Op{{Op: "notes", Index: 1, Notes: []notes.Note{{Line: 2, Text: "why --> two"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return e.Markdown
}

func TestNotesAreAHTMLCommentAfterTheFenceAndNotCode(t *testing.T) {
	md := withNotes(t)
	want := "```\n<!-- gonb:notes "
	if !strings.Contains(md, want) {
		t.Fatalf("no comment right after the fence:\n%s", md)
	}
	cells := Cells(md)
	if len(cells) != 4 {
		t.Fatalf("%d cells: %+v", len(cells), cells)
	}
	if !reflect.DeepEqual(cells[1].Notes, []notes.Note{{Line: 2, Text: "why --> two"}}) {
		t.Fatalf("notes %+v", cells[1].Notes)
	}
	if cells[2].Source != "\nafter\n\n" {
		t.Fatalf("the prose shows the comment: %q", cells[2].Source)
	}
	if cells[1].Source != "x := 1\ny := 2\nx + y\n" {
		t.Fatalf("the code changed: %q", cells[1].Source)
	}
	// it does not change what the notebook runs
	a, b := Run(plain), Run(md)
	if len(a) != len(b) {
		t.Fatal("different number of results")
	}
	for i := range a {
		if a[i].Output != b[i].Output || a[i].Error != b[i].Error || !reflect.DeepEqual(a[i].Display, b[i].Display) {
			t.Fatalf("cell %d runs differently: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestEditingThroughTheCommentIsByteForByte(t *testing.T) {
	md := withNotes(t)
	// the code cell set to its own text, the prose set to what the page shows
	cells := Cells(md)
	e, err := Edit(md, []Op{{Op: "source", Index: 1, Source: cells[1].Source}, {Op: "source", Index: 2, Source: cells[2].Source}})
	if err != nil {
		t.Fatal(err)
	}
	if e.Markdown != md {
		t.Fatalf("not byte for byte:\n%q\n%q", e.Markdown, md)
	}
	// a prose edit keeps the comment
	e, err = Edit(md, []Op{{Op: "source", Index: 2, Source: "\nchanged\n\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := Cells(e.Markdown)[1].Notes; len(got) != 1 {
		t.Fatalf("a prose edit lost the notes: %+v", got)
	}
	// clearing them gives the original notebook back
	e, err = Edit(md, []Op{{Op: "notes", Index: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if e.Markdown != plain {
		t.Fatalf("clearing left a trace:\n%q", e.Markdown)
	}
}

func TestNotesOnACellWithNoProseAfterIt(t *testing.T) {
	e, err := Edit(plain, []Op{{Op: "notes", Index: 3, Notes: []notes.Note{{Line: 1, Text: "last"}}}})
	if err != nil {
		t.Fatal(err)
	}
	cells := Cells(e.Markdown)
	if len(cells[3].Notes) != 1 {
		t.Fatalf("%+v", cells)
	}
	e2, err := Edit(e.Markdown, []Op{{Op: "notes", Index: 3}})
	if err != nil || e2.Markdown != plain {
		t.Fatalf("%v %q", err, e2.Markdown)
	}
	if _, err := Edit(plain, []Op{{Op: "notes", Index: 0}}); err == nil {
		t.Fatal("notes on a prose cell must be refused")
	}
}

func TestNotesFollowTheirCellThroughMoveAndDelete(t *testing.T) {
	md := withNotes(t)
	e, err := Edit(md, []Op{{Op: "move", Index: 1, To: 3}})
	if err != nil {
		t.Fatal(err)
	}
	cells := Cells(e.Markdown)
	moved := -1
	for i, c := range cells {
		if len(c.Notes) > 0 {
			moved = i
		}
	}
	if moved < 0 || !strings.HasPrefix(cells[moved].Source, "x := 1") {
		t.Fatalf("the notes did not move with their cell: %+v", cells)
	}
	e, err = Edit(md, []Op{{Op: "delete", Index: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.Markdown, "gonb:notes") {
		t.Fatalf("a deleted cell left its comment behind:\n%s", e.Markdown)
	}
	// a new cell inserted after a commented one does not inherit them
	e, err = Edit(md, []Op{{Op: "insert", Index: 2, Lang: "go"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range Cells(e.Markdown) {
		if len(c.Notes) > 0 && !strings.HasPrefix(c.Source, "x := 1") {
			t.Fatalf("notes on %q", c.Source)
		}
	}
}

func TestAMalformedCommentIsJustProse(t *testing.T) {
	md := "```go\nx := 1\n```\n<!-- gonb:notes {oops} -->\ntext\n"
	cells := Cells(md)
	if len(cells[0].Notes) != 0 || !strings.Contains(cells[1].Source, "gonb:notes") {
		t.Fatalf("%+v", cells)
	}
	e, err := Edit(md, []Op{{Op: "source", Index: 0, Source: "x := 1\n"}})
	if err != nil || e.Markdown != md {
		t.Fatalf("%v %q", err, e.Markdown)
	}
}

func TestACommentNeverShowsAsACellOfItsOwn(t *testing.T) {
	two := "```go\nx := 1\n```\n```go\ny := 2\n```\n"
	e, err := Edit(two, []Op{{Op: "notes", Index: 0, Notes: []notes.Note{{Line: 1, Text: "c"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Markdown, "gonb:notes") {
		t.Fatal("not stored")
	}
	cells := Cells(e.Markdown)
	if len(cells) != 2 || cells[0].Kind != "code" || cells[1].Kind != "code" || len(cells[0].Notes) != 1 {
		t.Fatalf("cells %+v", cells)
	}
	if e.Focus != 0 {
		t.Fatalf("focus %d", e.Focus)
	}
	// a delete of the next cell, and of this one, leaves no comment cell behind
	if d, _ := Edit(e.Markdown, []Op{{Op: "delete", Index: 1}}); len(Cells(d.Markdown)) != 1 || len(Cells(d.Markdown)[0].Notes) != 1 {
		t.Fatalf("%q", d.Markdown)
	}
	d, _ := Edit(e.Markdown, []Op{{Op: "delete", Index: 0}})
	if len(Cells(d.Markdown)) != 1 || strings.Contains(d.Markdown, "gonb:notes") {
		t.Fatalf("%q", d.Markdown)
	}
	// focus on a cell after the comment cell counts the cells the page shows
	f, _ := Edit(e.Markdown, []Op{{Op: "source", Index: 1, Source: "y := 3\n"}})
	if f.Focus != 1 {
		t.Fatalf("focus %d, want 1", f.Focus)
	}
}

func TestAnEmptiedProseCellIsNotWrittenAndKeepsTheFocusSane(t *testing.T) {
	md := "```go\nx := 1\n```\n\nprose\n\n```go\ny := 2\n```\n"
	e, err := Edit(md, []Op{{Op: "source", Index: 1, Source: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if len(Cells(e.Markdown)) != 2 {
		t.Fatalf("%q", e.Markdown)
	}
	if e.Focus < 0 || e.Focus > 1 {
		t.Fatalf("focus %d", e.Focus)
	}
}
