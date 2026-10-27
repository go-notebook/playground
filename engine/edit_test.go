package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustEdit applies ops and fails the test on an error.
func mustEdit(t *testing.T, md string, ops ...Op) Edited {
	t.Helper()
	e, err := Edit(md, ops)
	if err != nil {
		t.Fatalf("Edit(%q, %+v): %v", md, ops, err)
	}
	return e
}

// No edit is no change, byte for byte, on every example notebook.
func TestEditRoundTripExamples(t *testing.T) {
	files, _ := filepath.Glob("../examples/*/*.md")
	if len(files) < 10 {
		t.Fatalf("%d example notebooks", len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		md := string(b)
		if e := mustEdit(t, md); e.Markdown != md {
			t.Errorf("%s: no edit changed the notebook", f)
		}
		// Every cell's text set to itself changes nothing either.
		var ops []Op
		for i, c := range Cells(md) {
			ops = append(ops, Op{Op: "source", Index: i, Source: c.Source})
		}
		if e := mustEdit(t, md, ops...); e.Markdown != md {
			t.Errorf("%s: setting each cell's text to itself changed the notebook", f)
		}
	}
}

const odd = "# t\n\n~~~go {.x}\nx := 1\n~~~\n\ntext\n\n````ruby  opt\nputs 1\n````\n\n```python\nprint(1)\n```\n"

func TestEditOps(t *testing.T) {
	// a language change rewrites the fence's first word only, and back
	e := mustEdit(t, odd, Op{Op: "lang", Index: 1, Lang: "ruby"})
	if !strings.Contains(e.Markdown, "~~~ruby {.x}\nx := 1\n~~~\n") || e.Cells[1].Lang != "ruby" || e.Focus != 1 {
		t.Errorf("go -> ruby: %q %+v", e.Markdown, e)
	}
	if back := mustEdit(t, e.Markdown, Op{Op: "lang", Index: 1, Lang: "go"}); back.Markdown != odd {
		t.Errorf("go -> ruby -> go: %q", back.Markdown)
	}
	// code to prose and back: the text is kept, not translated
	e = mustEdit(t, odd, Op{Op: "lang", Index: 3, Lang: "markdown"})
	if !strings.Contains(e.Markdown, "\n\ntext\n\nputs 1\n\n```python") || len(e.Cells) != 3 {
		t.Errorf("ruby -> markdown: %q (%d cells)", e.Markdown, len(e.Cells))
	}
	e = mustEdit(t, "intro\n", Op{Op: "lang", Index: 0, Lang: "go"})
	if e.Markdown != "```go\nintro\n```\n" || e.Cells[0].Kind != "code" {
		t.Errorf("markdown -> go: %q", e.Markdown)
	}
	if e := mustEdit(t, "intro\n", Op{Op: "lang", Index: 0, Lang: "markdown"}); e.Markdown != "intro\n" {
		t.Errorf("markdown -> markdown: %q", e.Markdown)
	}
	// source: code gets its newline, a closing fence in it rebuilds the fence
	e = mustEdit(t, odd, Op{Op: "source", Index: 1, Source: "y := 2"})
	if !strings.Contains(e.Markdown, "~~~go {.x}\ny := 2\n~~~\n") {
		t.Errorf("source: %q", e.Markdown)
	}
	e = mustEdit(t, odd, Op{Op: "source", Index: 1, Source: "s := `\n~~~\n`\n"})
	if c := e.Cells[1]; c.Kind != "code" || c.Source != "s := `\n~~~\n`\n" {
		t.Errorf("a closing fence in the source: %q", e.Markdown)
	}
	// prose gets its newline; prose that opens a go fence becomes a cell
	e = mustEdit(t, "```go\n1\n```\n", Op{Op: "insert", Index: 1, Lang: "markdown", Source: "after"})
	if e.Markdown != "```go\n1\n```\nafter\n" || e.Focus != 1 {
		t.Errorf("insert prose at the end: %q %d", e.Markdown, e.Focus)
	}
	e = mustEdit(t, "a\n", Op{Op: "source", Index: 0, Source: "a\n```go\n2\n```"})
	if len(e.Cells) != 2 || e.Cells[1].Lang != "go" {
		t.Errorf("prose with a fence: %+v", e.Cells)
	}
	// insert: a new empty cell in each language, an empty prose cell is a line
	for _, lang := range []string{"go", "ruby", "julia"} {
		e = mustEdit(t, odd, Op{Op: "insert", Index: 0, Lang: lang})
		if e.Cells[0].Kind != "code" || e.Cells[0].Lang != lang || e.Cells[0].Source != "" || e.Focus != 0 {
			t.Errorf("insert %s: %q %+v", lang, e.Markdown, e.Cells[0])
		}
	}
	e = mustEdit(t, "```go\n1\n```\n```go\n2\n```\n", Op{Op: "insert", Index: 1, Lang: "markdown"})
	if e.Markdown != "```go\n1\n```\n\n```go\n2\n```\n" || e.Focus != 1 || e.Cells[1].Kind != "prose" {
		t.Errorf("insert empty prose: %q %+v", e.Markdown, e)
	}
	// a prose cell inserted next to prose joins it, and the focus follows
	e = mustEdit(t, odd, Op{Op: "insert", Index: 1, Lang: "markdown", Source: "more"})
	if e.Focus != 0 || !strings.HasPrefix(e.Markdown, "# t\n\nmore\n~~~go") {
		t.Errorf("insert prose after prose: %q focus %d", e.Markdown, e.Focus)
	}
	// delete and move
	e = mustEdit(t, odd, Op{Op: "delete", Index: 1})
	if strings.Contains(e.Markdown, "x := 1") || e.Focus != 0 {
		t.Errorf("delete: %q focus %d", e.Markdown, e.Focus)
	}
	if e := mustEdit(t, "```go\n1\n```\n", Op{Op: "delete", Index: 0}); e.Markdown != "" || len(e.Cells) != 0 || e.Focus != 0 {
		t.Errorf("delete the only cell: %+v", e)
	}
	e = mustEdit(t, "```go\n1\n```\n```ruby\n2\n```\n", Op{Op: "move", Index: 1, To: 0})
	if e.Markdown != "```ruby\n2\n```\n```go\n1\n```\n" || e.Focus != 0 {
		t.Errorf("move: %q %d", e.Markdown, e.Focus)
	}
	// an unclosed fence at the end keeps its text through a language change
	e = mustEdit(t, "```go\nx := 1", Op{Op: "lang", Index: 0, Lang: "ruby"})
	if e.Markdown != "```ruby\nx := 1" {
		t.Errorf("unclosed: %q", e.Markdown)
	}
}

func TestEditRefuses(t *testing.T) {
	big := strings.Repeat("a", MaxNotebook+1)
	for _, c := range []struct {
		md  string
		ops []Op
		err string
	}{
		{big, nil, "up to"},
		{"a\n", []Op{{Op: "source", Index: 1}}, "no cell 1"},
		{"a\n", []Op{{Op: "lang", Index: -1, Lang: "go"}}, "no cell -1"},
		{"a\n", []Op{{Op: "lang", Index: 0, Lang: "python"}}, `"python" is not a language`},
		{"```go\n1\n```\n", []Op{{Op: "lang", Index: 0, Lang: "lua"}}, `"lua" is not a language`},
		{"a\n", []Op{{Op: "insert", Index: 2, Lang: "go"}}, "cannot insert at 2"},
		{"a\n", []Op{{Op: "insert", Index: 0, Lang: "cobol"}}, `"cobol" is not a language`},
		{"a\n", []Op{{Op: "delete", Index: 3}}, "no cell 3"},
		{"a\n", []Op{{Op: "move", Index: 0, To: 1}}, "cannot move cell 0 to 1"},
		{"a\n", []Op{{Op: "rm", Index: 0}}, `unknown edit "rm"`},
		{"a\n", []Op{{Op: "source", Index: 0, Source: big}}, "would be"},
	} {
		if _, err := Edit(c.md, c.ops); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%+v: %v, want %q", c.ops, err, c.err)
		}
	}
}

// Any document reads back byte for byte with no edit; a language change and
// its reverse give the document back; setting every cell's text to itself
// changes nothing.
func FuzzEdit(f *testing.F) {
	for _, s := range []string{odd, "", "```go\n", "~~~ruby x\n1\n~~~", "a\n```go\n```\n```\n", "   ```go\n1\n   ```\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, md string) {
		if len(md) > MaxNotebook {
			return
		}
		e, err := Edit(md, nil)
		if err != nil || e.Markdown != md {
			t.Fatalf("no edit: %q -> %q, %v", md, e.Markdown, err)
		}
		var ops []Op
		for i, c := range e.Cells {
			ops = append(ops, Op{Op: "source", Index: i, Source: c.Source})
		}
		if s, err := Edit(md, ops); err != nil || s.Markdown != md {
			t.Fatalf("each text to itself: %q -> %q, %v", md, s.Markdown, err)
		}
		for i, c := range e.Cells {
			if c.Kind != "code" {
				continue
			}
			other := map[string]string{"go": "ruby", "ruby": "julia", "julia": "go"}[c.Lang]
			there, err := Edit(md, []Op{{Op: "lang", Index: i, Lang: other}})
			if err != nil {
				t.Fatal(err)
			}
			back, err := Edit(there.Markdown, []Op{{Op: "lang", Index: i, Lang: c.Lang}})
			if err != nil || back.Markdown != md {
				t.Fatalf("cell %d %s -> %s -> %s: %q -> %q -> %q, %v", i, c.Lang, other, c.Lang, md, there.Markdown, back.Markdown, err)
			}
		}
	})
}

func TestRunUpTo(t *testing.T) {
	md := "```go\nx := 1\n```\n```go\ny := x + 1\ny\n```\n```go\ny * 10\n```\n"
	var started []int
	rs := RunUpTo(md, 1, func(i int) { started = append(started, i) })
	if len(rs) != 2 || rs[1].Error != "" || len(rs[1].Display) == 0 || rs[1].Display[0]["text/plain"] != "2" {
		t.Errorf("up to cell 1: %+v", rs)
	}
	if len(started) != 2 || started[1] != 1 {
		t.Errorf("started %v", started)
	}
	if rs := RunUpTo(md, 9, nil); len(rs) != 3 {
		t.Errorf("past the end runs every cell: %d", len(rs))
	}
}
