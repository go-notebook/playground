package editor

import (
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"
)

type heard struct {
	texts   []string
	heights []float64
}

func newCell(t *testing.T, text string) (*Cell, *heard) {
	t.Helper()
	h := &heard{}
	c := New(Config{
		Text: text, Lang: "go", Name: "Go cell 1",
		OnChange: func(s string) { h.texts = append(h.texts, s) },
		OnHeight: func(f float64) { h.heights = append(h.heights, f) },
	})
	c.Resize(400, 200)
	return c, h
}

func frame(c *Cell) []byte {
	w, h := c.Size()
	buf := make([]byte, w*h*4)
	c.Draw(buf)
	return buf
}

func TestNewHoldsTheTextAndNamesTheTextbox(t *testing.T) {
	c, _ := newCell(t, "x := 1\ny := 2")
	if c.Text() != "x := 1\ny := 2" {
		t.Fatalf("text %q", c.Text())
	}
	frame(c)
	nodes := c.A11yNodes()
	if len(nodes) != 1 {
		t.Fatalf("%d nodes, want one textbox: %+v", len(nodes), nodes)
	}
	n := nodes[0]
	if n.Role != "textbox" || n.Name != "Go cell 1" || n.Value != "x := 1\ny := 2" || !n.Multiline {
		t.Fatalf("node %+v", n)
	}
	if c.TextInputFor(n.Key) == nil {
		t.Fatal("the textbox has no text layer")
	}
	if c.TextInputFor("nope") != nil {
		t.Fatal("an unknown key has a text layer")
	}
}

func TestACellOpensWithItsCaretAtTheStart(t *testing.T) {
	c, _ := newCell(t, "first\nsecond")
	if a, f := c.Selection(); a != 0 || f != 0 {
		t.Fatalf("caret %d,%d, want 0,0", a, f)
	}
	// and the view shows the first column, however long the first line is
	long, _ := newCell(t, strings.Repeat("long line ", 100))
	frame(long)
	short, _ := newCell(t, "x")
	frame(short)
	if lx, _ := long.ed.CaretPixel(0, 0); func() bool { sx, _ := short.ed.CaretPixel(0, 0); return lx != sx }() {
		t.Fatalf("a long first line opens scrolled: first column at x=%d", lx)
	}
}

func TestBoundCutsOnARuneBoundary(t *testing.T) {
	if Bound("short") != "short" {
		t.Fatal("a short text changed")
	}
	// a 3-byte rune straddling the limit goes whole
	s := strings.Repeat("a", MaxText-1) + "€€"
	got := Bound(s)
	if len(got) != MaxText-1 {
		t.Fatalf("len %d, want %d", len(got), MaxText-1)
	}
	for _, r := range got {
		if r == 0xFFFD {
			t.Fatal("cut inside a rune")
		}
	}
	if got := Bound(strings.Repeat("b", MaxText+5)); len(got) != MaxText {
		t.Fatalf("ASCII over the limit: len %d", len(got))
	}
	c := New(Config{Text: strings.Repeat("c", MaxText+10)})
	if len(c.Text()) != MaxText {
		t.Fatalf("New did not bound: %d", len(c.Text()))
	}
}

func TestTypingIsReportedOncePerChange(t *testing.T) {
	c, h := newCell(t, "")
	c.SetFocused(true)
	c.Char("a")
	c.Char("b")
	if c.Text() != "ab" || len(h.texts) != 2 || h.texts[1] != "ab" {
		t.Fatalf("text %q, heard %q", c.Text(), h.texts)
	}
	c.KeyDown("ArrowLeft") // a caret move changes no text
	if len(h.texts) != 2 {
		t.Fatalf("a caret move was reported: %q", h.texts)
	}
	c.KeyDown("Backspace")
	if c.Text() != "b" || len(h.texts) != 3 {
		t.Fatalf("text %q, heard %q", c.Text(), h.texts)
	}
}

func TestATextLayerEditIsHeardAtTheNextFrame(t *testing.T) {
	c, h := newCell(t, "ab")
	frame(c)
	// the text layer calls the editor itself, not the cell: IME and typing
	c.ed.SetInputSelection(2, 2)
	c.ed.ReplaceInput(-1, -1, "か")
	if len(h.texts) != 0 {
		t.Fatalf("heard %q before a frame", h.texts)
	}
	frame(c)
	frame(c) // the second frame has nothing new
	if len(h.texts) != 1 || h.texts[0] != "abか" {
		t.Fatalf("heard %q", h.texts)
	}
}

func TestInsertReplacesTheSelection(t *testing.T) {
	c, h := newCell(t, "ab")
	n := 0
	c.RepaintWith(func() { n++ })
	c.SetSelection(1, 1)
	c.Insert("\t")
	if c.Text() != "a\tb" || len(h.texts) != 1 || n != 2 {
		t.Fatalf("text %q, heard %q, %d frames", c.Text(), h.texts, n)
	}
	c.Insert(strings.Repeat("x", MaxText+1))
	if len(c.Text()) > MaxText+3 {
		t.Fatalf("an unbounded insert: %d", len(c.Text()))
	}
}

func TestSetTextFromOutsideIsNotReportedBack(t *testing.T) {
	c, h := newCell(t, "one")
	c.SetText("one") // same text: nothing to do
	c.SetText("two\nlines")
	if c.Text() != "two\nlines" || len(h.texts) != 0 {
		t.Fatalf("text %q, heard %q", c.Text(), h.texts)
	}
	c.Char("!") // and the next edit is reported from the new text
	if len(h.texts) != 1 || !strings.Contains(h.texts[0], "two") {
		t.Fatalf("heard %q", h.texts)
	}
}

func TestTheHeightFollowsTheLines(t *testing.T) {
	c, h := newCell(t, "a")
	frame(c)
	one := c.HeightCSS()
	if one <= 0 {
		t.Fatalf("height %v", one)
	}
	c.SetFocused(true)
	c.SetText("a\nb\nc\nd\ne")
	frame(c)
	five := c.HeightCSS()
	if five <= one || len(h.heights) == 0 || h.heights[len(h.heights)-1] != five {
		t.Fatalf("one line %v, five lines %v, heard %v", one, five, h.heights)
	}
	// an edit that adds a line reports the new height; one that does not, none
	n := len(h.heights)
	c.Char("x")
	if len(h.heights) != n {
		t.Fatalf("a character reported a height: %v", h.heights)
	}
	c.KeyDown("Enter")
	if len(h.heights) != n+1 || h.heights[n] <= five {
		t.Fatalf("Enter: heard %v after %d", h.heights, n)
	}
}

func TestHeightWithoutAListener(t *testing.T) {
	c := New(Config{Text: "a"})
	c.Resize(400, 200)
	c.SetText("a\nb\nc")
	frame(c)
	c.SetPixelRatio(2) // must not call a nil listener
	c.Char("x")        // nor must a change
	if c.Text() == "" {
		t.Fatal("lost the text")
	}
}

// The toolkit's metric scale is global, so what a cell says its height is, in
// CSS pixels, must not depend on which cell set the scale or when: a cell made
// before the scale moved, one made after, and one that has not drawn since all
// say the same.
func TestTheCSSHeightDoesNotDependOnWhoSetTheScale(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	toolkit.SetMetricScale(1)
	a, _ := newCell(t, "a\nb\nc")
	frame(a)
	want := a.HeightCSS()
	if want <= 0 {
		t.Fatalf("height %v", want)
	}
	a.SetPixelRatio(2) // a is mounted at ratio 2: the scale is the process's now
	if got := a.HeightCSS(); got != want {
		t.Errorf("a, before it drew again, says %v CSS px; at ratio 1 it said %v", got, want)
	}
	b, _ := newCell(t, "a\nb\nc") // made after the scale moved
	if got := b.HeightCSS(); got != want {
		t.Errorf("a cell created at ratio 2 says %v CSS px, want %v", got, want)
	}
	frame(a)
	frame(b)
	if ga, gb := a.HeightCSS(), b.HeightCSS(); ga != want || gb != want {
		t.Errorf("after a frame: a %v, b %v CSS px, want %v", ga, gb, want)
	}
	// and the page hears the same number from either when the text grows
	var heard []float64
	b.onHeight = func(f float64) { heard = append(heard, f) }
	b.SetText("a\nb\nc\nd")
	frame(b)
	if len(heard) == 0 || heard[len(heard)-1] <= want {
		t.Errorf("a longer text reported %v after %v", heard, want)
	}
	c, _ := newCell(t, "a\nb\nc\nd")
	toolkit.SetMetricScale(1) // the scale moves back; c was laid out at 2
	if got, w := c.HeightCSS(), heard[len(heard)-1]; got != w {
		t.Errorf("a cell laid out at ratio 2 says %v CSS px after the scale went back to 1, want %v", got, w)
	}
}

func TestReplayedPointerEventsDoNotMoveTheCaret(t *testing.T) {
	c, _ := newCell(t, "first line\nsecond line\nthird line")
	frame(c)
	c.SetSelection(0, 0)
	c.ReplayPointer(true)
	if !c.Click(100, 100) {
		t.Fatal("a replayed click wants no frame")
	}
	if !c.Focused() {
		t.Fatal("a replayed click did not give the keyboard")
	}
	if c.Release(100, 100) {
		t.Fatal("a replayed release was delivered")
	}
	if a, f := c.Selection(); a != 0 || f != 0 {
		t.Fatalf("the replayed click moved the caret to %d,%d", a, f)
	}
	c.ReplayPointer(false)
	x0, y, _, hh := c.ed.InputRect(0, 0)
	x4, _, _, _ := c.ed.InputRect(4, 4)
	c.Click(x0+1, y+hh/2) // a real click on the first line
	c.Move(x4, y+hh/2)    // a drag while the button is held
	c.Release(x4, y+hh/2)
	if a, f := c.Selection(); a == f {
		t.Fatalf("a real drag selected nothing: %d,%d", a, f)
	}
	if !c.Move(70, 8) { // a hover after release
		t.Fatal("a hover wants no frame")
	}
}

func TestSelectionIsInUTF16Units(t *testing.T) {
	c, _ := newCell(t, "a😀b")
	c.SetSelection(1, 3) // the emoji is two UTF-16 units
	if a, f := c.Selection(); a != 1 || f != 3 {
		t.Fatalf("selection %d,%d", a, f)
	}
}

func TestOnlyAHorizontalGestureThatMovesTheTextIsTheEditors(t *testing.T) {
	c, _ := newCell(t, strings.Repeat("long line ", 100))
	frame(c)
	for _, w := range [][2]int{{0, 3}, {0, -3}, {0, 0}, {1, 5}} { // dx, dy
		if r, used := c.Scroll(10, 10, w[0], w[1]); r || used {
			t.Fatalf("wheel dx=%d dy=%d was taken (repaint %v, consumed %v): the page must scroll", w[0], w[1], r, used)
		}
	}
	// at the start, a leftward swipe has nowhere to go: the page's (swipe back)
	if r, used := c.Scroll(10, 10, -4, 0); r || used {
		t.Fatalf("a leftward wheel at the start was taken (%v, %v)", r, used)
	}
	if r, used := c.Scroll(10, 10, 4, 0); !r || !used {
		t.Fatal("a rightward wheel over a long line was left to the page")
	}
	if r, used := c.Scroll(10, 10, -4, 1); !r || !used {
		t.Fatal("a leftward wheel after scrolling right was left to the page")
	}
	// at the far end, a rightward swipe has nowhere to go either
	for i := 0; i < 100; i++ {
		c.Scroll(10, 10, 50, 0)
	}
	if r, used := c.Scroll(10, 10, 5, 0); r || used {
		t.Fatalf("a rightward wheel at the end was taken (%v, %v)", r, used)
	}
	// lines that fit: no wheel is the editor's, however it leads
	short, _ := newCell(t, "x := 1")
	frame(short)
	for _, w := range [][2]int{{5, 0}, {-5, 0}, {9, 1}} {
		if r, used := short.Scroll(10, 10, w[0], w[1]); r || used {
			t.Fatalf("a horizontal wheel dx=%d over a line that fits was taken (%v, %v)", w[0], r, used)
		}
	}
	if c.Context(1, 1) {
		t.Fatal("the editor opened a menu of its own")
	}
}

func TestModifiersReachTheEditor(t *testing.T) {
	c, _ := newCell(t, "")
	c.SetFocused(true)
	c.Char("a")
	c.Char("b")
	c.Modifiers(webcanvas.Modifiers{Ctrl: true})
	c.KeyDown("z") // Ctrl+Z: undo
	if c.Text() == "ab" {
		t.Fatalf("Ctrl+Z undid nothing: %q", c.Text())
	}
	c.Modifiers(webcanvas.Modifiers{Meta: true, Shift: true})
	c.KeyDown("z") // Cmd+Shift+Z: redo
	if c.Text() != "ab" {
		t.Fatalf("Cmd+Shift+Z redid nothing: %q", c.Text())
	}
	c.Modifiers(webcanvas.Modifiers{Alt: true})
	if got := c.code("ArrowLeft"); got != "ArrowLeft" {
		t.Fatalf("Alt alone renamed the key: %q", got)
	}
	c.Modifiers(webcanvas.Modifiers{Ctrl: true})
	if got := c.code("Enter"); got != "Ctrl+Enter" {
		t.Fatalf("a named key with Ctrl: %q", got)
	}
}

func TestResizeKeepsTheSizeOfAZeroBox(t *testing.T) {
	c, _ := newCell(t, "a")
	if w, h := c.Resize(0, 0); w != 400 || h != 200 {
		t.Fatalf("a zero box gave %dx%d", w, h)
	}
	if w, h := c.Resize(300, 90); w != 300 || h != 90 {
		t.Fatalf("Resize gave %dx%d", w, h)
	}
	if w, h := c.Size(); w != 300 || h != 90 {
		t.Fatalf("Size %dx%d", w, h)
	}
}

func TestDrawPaintsTheTextInTheThemesInk(t *testing.T) {
	c, _ := newCell(t, "func main() {}")
	light := frame(c)
	c.SetDark(true)
	dark := frame(c)
	same := true
	for i := range light {
		if light[i] != dark[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("the dark theme draws the light frame")
	}
	// the frame is not blank: more than one colour
	seen := map[[3]byte]bool{}
	for i := 0; i+3 < len(dark); i += 4 {
		seen[[3]byte{dark[i], dark[i+1], dark[i+2]}] = true
	}
	if len(seen) < 3 {
		t.Fatalf("%d colours in the frame: text was not painted", len(seen))
	}
}

func TestLanguageNameAndFocusAreTheCells(t *testing.T) {
	c, _ := newCell(t, "x")
	c.SetLanguage("ruby")
	c.SetName("Ruby cell 2")
	frame(c)
	if n := c.A11yNodes(); len(n) != 1 || n[0].Name != "Ruby cell 2" {
		t.Fatalf("nodes %+v", n)
	}
	if c.Focused() {
		t.Fatal("focused before anyone asked")
	}
	c.SetFocused(true)
	frame(c)
	if n := c.A11yNodes(); !n[0].Focused {
		t.Fatal("a focused editor is not focused in the tree")
	}
}

func TestARepaintIsRequestedWhenTheHostChangesTheCell(t *testing.T) {
	c, _ := newCell(t, "x")
	c.SetFocused(true) // no request installed yet: must not panic
	n := 0
	c.RepaintWith(func() { n++ })
	c.SetText("y")
	c.SetLanguage("go")
	c.SetName("n")
	c.SetSelection(0, 1)
	c.SetFocused(false)
	c.SetDark(true)
	if n != 6 {
		t.Fatalf("%d frames requested, want 6", n)
	}
}
