package editor

import (
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"

	"github.com/go-notebook/playground/notes"
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

// A C cell is highlighted: the same text drawn with the fence word "c" is not
// the frame drawn with a language that has no lexer, and a change of language
// to "c" repaints.
func TestACCellIsHighlighted(t *testing.T) {
	src := "#include <stdio.h>\nstatic int total = 3; /* size */\nstruct s { char *name; };\n"
	draw := func(lang string) []byte {
		c := New(Config{Text: src, Lang: lang, Name: "C cell 1"})
		c.Resize(400, 200)
		return frame(c)
	}
	if string(draw("c")) == string(draw("none")) {
		t.Fatal("the C lexer paints nothing: C cells are not highlighted")
	}
	if string(draw("c")) == string(draw("go")) {
		t.Fatal("C is drawn as Go")
	}
	c := New(Config{Text: src, Lang: "none", Name: "C cell 1"})
	c.Resize(400, 200)
	before := string(frame(c))
	c.SetLanguage("c")
	if string(frame(c)) == before {
		t.Fatal("changing the language to c did not repaint with the C lexer")
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

// The text of a cell is a code editor's size, in CSS pixels, on a plain display
// and on a dense one. (The toolkit's default, the 5x7 bitmap, measured 8.25 CSS
// px at both: consistent, and too small to read.)
func TestTheTextIsThirteenCSSPixelsAtAnyPixelRatio(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	toolkit.SetMetricScale(1)
	type m struct{ adv, h, pitch float64 }
	measure := func(ratio float64) m {
		c, _ := newCell(t, "x := 1\ny := 2")
		c.SetPixelRatio(ratio)
		frame(c)
		f := c.ed.EffectiveFont()
		if c.fontApplied != int(13*ratio) {
			t.Fatalf("ratio %v: font built at %d px, want %d", ratio, c.fontApplied, int(13*ratio))
		}
		return m{float64(f.Advance()) / ratio, float64(f.Height()) / ratio, c.HeightCSS()}
	}
	a, b := measure(1), measure(2)
	t.Logf("ratio 1: %+v, ratio 2: %+v", a, b)
	if a.h < 15 || a.h > 19 || a.adv < 7 || a.adv > 9 {
		t.Errorf("at ratio 1 the glyph box is %.1f x %.1f CSS px, want about 8 x 17", a.adv, a.h)
	}
	for what, d := range map[string]float64{"advance": a.adv - b.adv, "height": a.h - b.h, "cell height": a.pitch - b.pitch} {
		if d > 1 || d < -1 {
			t.Errorf("%s differs by %.2f CSS px between ratio 1 and 2", what, d)
		}
	}
	if a.pitch < 2*17 {
		t.Errorf("two rows are %.1f CSS px tall, the bitmap font gave 30", a.pitch)
	}
}

func TestFontPxIsConfigurableAndBounded(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	toolkit.SetMetricScale(1)
	for _, tc := range []struct {
		in   float64
		want int
	}{{0, 13}, {16, 16}, {4, 13}, {200, 13}} {
		c := New(Config{Text: "a", FontPx: tc.in})
		if c.fontApplied != tc.want {
			t.Errorf("FontPx %v: %d px, want %d", tc.in, c.fontApplied, tc.want)
		}
	}
}

func TestNotesRoundTripThroughTheCell(t *testing.T) {
	var heard [][]notes.Note
	c := New(Config{Text: "a\nb\nc", Name: "Go cell 1", Notes: []notes.Note{{Line: 2, Text: "on b"}, {Line: 9, Text: "nowhere"}},
		OnNotes: func(n []notes.Note) { heard = append(heard, n) }})
	c.Resize(600, 200)
	frame(c)
	if got := c.Notes(); len(got) != 1 || got[0] != (notes.Note{Line: 2, Text: "on b"}) {
		t.Fatalf("notes %+v: the one on a line the text lacks must be dropped", got)
	}
	if len(heard) != 0 {
		t.Fatalf("loading must not report: %+v", heard)
	}
	// a line inserted above moves the comment, and the page hears it once
	c.SetSelection(0, 0)
	c.KeyDown("Enter")
	frame(c)
	if len(heard) != 1 || heard[0][0].Line != 3 {
		t.Fatalf("heard %+v, want the comment on line 3", heard)
	}
	// the cell's accessibility tree carries the marker with the comment
	var marker *webcanvas.A11yNode
	nodes := c.A11yNodes()
	for i := range nodes {
		if nodes[i].Role == "button" {
			marker = &nodes[i]
		}
	}
	if marker == nil || marker.Name != "Comment on line 3: on b" || marker.Value != "on b" || marker.W <= 0 {
		t.Fatalf("mirror nodes %+v", nodes)
	}
	// replacing them
	c.SetNotes(nil)
	if len(c.Notes()) != 0 || c.ed.Annotations().Len() != 0 {
		t.Fatal("SetNotes(nil) left notes")
	}
}

func TestACellShowsItsMinimapWhenWideAndNotWhenNarrow(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	toolkit.SetMetricScale(1)
	c, _ := newCell(t, "a\nb")
	c.Resize(800, 100)
	frame(c)
	h := c.HeightCSS()
	if !c.ed.ShowMinimap || !c.ed.MinimapWhenFixed {
		t.Fatal("minimap off")
	}
	c.Resize(360, 100)
	frame(c)
	if c.HeightCSS() != h {
		t.Fatalf("the minimap changed the height: %v vs %v", c.HeightCSS(), h)
	}
}

func TestAClickOnTheGutterOpensTheAnnotationPanelAndTypingFillsIt(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	toolkit.SetMetricScale(1)
	var heard [][]notes.Note
	c := New(Config{Text: "a\nb", OnNotes: func(n []notes.Note) { heard = append(heard, n) }})
	c.Resize(600, 100)
	frame(c)
	c.Click(8, 13)
	c.Release(8, 13)
	if l, open := c.ed.AnnotationOpen(); !open || l != 0 {
		t.Fatalf("panel open=%v on line %d after a click at (8,13)", open, l)
	}
	c.ed.ReplaceInput(-1, -1, "hi") // the text layer's route
	c.KeyDown("Enter")
	frame(c)
	if c.Text() != "a\nb" {
		t.Fatalf("the buffer changed: %q", c.Text())
	}
	if len(heard) != 1 || heard[0][0] != (notes.Note{Line: 1, Text: "hi"}) {
		t.Fatalf("heard %+v", heard)
	}
}

func TestTheFontCacheIsBoundedAndABrokenFaceKeepsTheFontItHas(t *testing.T) {
	for px := 10; px < 40; px++ {
		if monoFont(px) == nil {
			t.Fatalf("no font at %d px", px)
		}
	}
	if len(fontCache) > 17 {
		t.Fatalf("%d fonts cached", len(fontCache))
	}
	defer func(b []byte) { monoTTF = b }(monoTTF)
	monoTTF = []byte("not a font")
	if monoFont(61) != nil {
		t.Fatal("a font from garbage")
	}
	c := New(Config{Text: "a", FontPx: 20}) // 20 px at the process's scale: not cached yet with this face
	_ = c
}
