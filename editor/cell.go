// Package editor is the code editor of a playground cell: a go-widgets
// toolkit.CodeEditor, highlighted by go-rouge, drawn on a go-widgets/webcanvas
// surface. It has no build tag, so everything that decides something (what a
// key does, how tall the cell is, when a change is reported) is tested
// natively; cmd/editor is the thin js/wasm shell that mounts a Cell on a
// <canvas> and offers it to the page's JavaScript.
//
// The accessibility tree, the IME text layer and find-in-page all come from
// webcanvas's mirror through go-widgets/window/webmirror, the one conversion
// from a toolkit tree to webcanvas nodes: a Cell publishes nothing of its own.
//
// The text of a notebook is untrusted. It reaches the editor as a Go string,
// the canvas as pixels and the DOM as the mirror's textContent; no markup is
// ever built from it.
package editor

import (
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/textinput"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/rougelex"
	"github.com/go-widgets/webcanvas"
	"github.com/go-widgets/window/webmirror"
)

// MaxText bounds the text one cell holds, in bytes. The page edits notebooks up
// to engine.MaxNotebook; a cell is never larger than its notebook.
const MaxText = 1 << 20

// The surface a Cell starts with, in CSS pixels, before the page lays it out.
const (
	startW = 600
	startH = 60
)

// Cell is one code cell's editor, as a webcanvas scene. Its model (text, caret,
// undo history) lives as long as the Cell: the surface that shows it may be
// mounted and unmounted as the cell scrolls out of view and back.
type Cell struct {
	ed    *toolkit.CodeEditor
	theme *toolkit.Theme

	w, h  int     // surface, in device pixels
	ratio float64 // device pixels per CSS pixel

	mods       webcanvas.Modifiers
	buttonHeld bool
	replay     bool // the pointer events in flight were made by the mirror, not by a person

	last     string // the text OnChange last heard
	onChange func(text string)
	onHeight func(css float64)
	height   int // preferred height, device pixels, as of the last report

	request atomic.Pointer[func()]
}

var (
	_ webcanvas.App             = (*Cell)(nil)
	_ webcanvas.Resizer         = (*Cell)(nil)
	_ webcanvas.PixelRatioAware = (*Cell)(nil)
	_ webcanvas.ScrollConsumer  = (*Cell)(nil)
	_ webcanvas.ModifierAware   = (*Cell)(nil)
	_ webcanvas.RepaintAware    = (*Cell)(nil)
	_ webcanvas.A11yAware       = (*Cell)(nil)
	_ webcanvas.TextInputHost   = (*Cell)(nil)
)

// Config is what a Cell starts from.
type Config struct {
	Text string
	// Lang is the fence word (go, ruby, julia, r): rouge's lexer name.
	Lang string
	// Name is the accessible name of the textbox ("Go cell 3").
	Name string
	Dark bool
	// OnChange hears the whole text after every edit (typing, IME, paste, undo).
	OnChange func(text string)
	// OnHeight hears the height the cell wants, in CSS pixels, when it changes:
	// the page sets the canvas's CSS height from it.
	OnHeight func(css float64)
}

// New is a Cell with cfg. A text over MaxText is cut on a rune boundary.
func New(cfg Config) *Cell {
	c := &Cell{ratio: 1, onChange: cfg.OnChange, onHeight: cfg.OnHeight}
	c.w, c.h = startW, startH
	c.ed = toolkit.NewCodeEditor(Bound(cfg.Text))
	c.ed.NoVerticalScroll = true
	c.ed.ShowScrollbars = true // the horizontal one: no soft wrap yet
	c.ed.Syntax = rougelex.New()
	c.ed.Language = cfg.Lang
	c.ed.AccessibleName = cfg.Name
	// A new editor holds its caret at the end of its text; a cell opens with it
	// at the start, as a text area does, so that a long first line shows its
	// first column.
	c.ed.SetInputSelection(0, 0)
	c.SetDark(cfg.Dark)
	c.last = c.ed.InputText()
	c.height = c.ed.PreferredHeight().Get()
	c.ed.PreferredHeight().Subscribe(func(int) { c.reportHeight() })
	return c
}

// Bound cuts s to MaxText bytes on a rune boundary.
func Bound(s string) string {
	if len(s) <= MaxText {
		return s
	}
	cut := MaxText
	for cut > 0 && !utf8.RuneStart(s[cut]) { // s[cut] is the first byte left out
		cut--
	}
	return s[:cut]
}

// SetDark chooses the theme.
func (c *Cell) SetDark(dark bool) {
	if dark {
		c.theme = toolkit.DefaultDark()
	} else {
		c.theme = toolkit.DefaultLight()
	}
	c.repaint()
}

// Text is the cell's text, with "\n" between lines.
func (c *Cell) Text() string { return c.ed.InputText() }

// SetText replaces the text (the page changed it from outside, say a Markdown
// cell was split again). The change is not reported back.
func (c *Cell) SetText(s string) {
	s = Bound(s)
	if s == c.ed.InputText() {
		return
	}
	c.ed.SetText(s)
	c.last = c.ed.InputText()
	c.repaint()
}

// SetLanguage changes the lexer; the next frame highlights with it.
func (c *Cell) SetLanguage(l string) {
	c.ed.Language = l
	c.repaint()
}

// SetName changes the accessible name.
func (c *Cell) SetName(n string) {
	c.ed.AccessibleName = n
	c.repaint()
}

// Selection is the selection in UTF-16 code units, the unit of the DOM's
// selectionStart and of the text-input contract, as (anchor, focus).
func (c *Cell) Selection() (anchor, focus int) { return c.ed.InputSelection() }

// SetSelection puts the selection at (anchor, focus), in UTF-16 code units.
func (c *Cell) SetSelection(anchor, focus int) {
	c.ed.SetInputSelection(anchor, focus)
	c.repaint()
}

// Focused reports whether the editor has the keyboard.
func (c *Cell) Focused() bool { return c.ed.Focused() }

// SetFocused gives or takes the keyboard.
func (c *Cell) SetFocused(f bool) {
	c.ed.SetFocused(f)
	c.repaint()
}

// HeightCSS is the height the cell wants, in CSS pixels.
func (c *Cell) HeightCSS() float64 { return float64(c.height) / c.ratio }

// reportHeight tells the page the preferred height; the Observable it is
// subscribed to fires only when the height changed.
func (c *Cell) reportHeight() {
	c.height = c.ed.PreferredHeight().Get()
	if c.onHeight != nil {
		c.onHeight(c.HeightCSS())
	}
}

// changed tells the page the text, once per change. It runs after every input
// the surface delivers, so a caret move reports nothing.
func (c *Cell) changed() {
	t := c.ed.InputText()
	if t == c.last {
		return
	}
	c.last = t
	if c.onChange != nil {
		c.onChange(t)
	}
}

// ReplayPointer says that the pointer events that follow, until it is called
// with false, were made by the accessibility mirror when it took the keyboard
// (it replays a click at the centre of a control it focuses), not by a person:
// they must not move the caret.
func (c *Cell) ReplayPointer(on bool) { c.replay = on }

// --- webcanvas.App ---

// Size is the surface a mount starts at, in device pixels.
func (c *Cell) Size() (int, int) { return c.w, c.h }

// Draw paints the editor into buf.
func (c *Cell) Draw(buf []byte) {
	p := painter.NewPixelPainter(buf, c.w, c.h)
	full := toolkit.Rect{W: c.w, H: c.h}
	c.ed.SetBounds(full)
	p.FillRect(full, c.theme.Background)
	c.ed.Draw(p, c.theme)
	// Typing and IME reach the editor through the text layer, which calls its
	// text-input client (the editor itself) and then asks for a frame: this is
	// where a change that did not come through a Char or a KeyDown is heard.
	c.changed()
}

// Insert replaces the selection with s, as a paste does (the Tab key, which
// indents).
func (c *Cell) Insert(s string) {
	c.ed.ReplaceInput(-1, -1, Bound(s))
	c.changed()
	c.repaint()
}

func (c *Cell) event(kind toolkit.EventKind, x, y int) toolkit.Event {
	return toolkit.Event{Kind: kind, X: x, Y: y,
		Ctrl: c.mods.Ctrl, Shift: c.mods.Shift, Alt: c.mods.Alt, Meta: c.mods.Meta}
}

// deliver hands ev to the editor and reports after it. A frame is always due.
func (c *Cell) deliver(ev toolkit.Event) bool {
	c.ed.OnEvent(ev)
	c.changed()
	return true
}

// Click is a left-button press: it takes the keyboard and puts the caret down.
func (c *Cell) Click(x, y int) bool {
	if c.replay {
		c.ed.SetFocused(true)
		return true
	}
	c.buttonHeld = true
	c.ed.SetFocused(true)
	return c.deliver(c.event(toolkit.EventClick, x, y))
}

// Move is a pointer move: a drag while the button is held.
func (c *Cell) Move(x, y int) bool {
	kind := toolkit.EventMouseMove
	if c.buttonHeld {
		kind = toolkit.EventMouseDrag
	}
	return c.deliver(c.event(kind, x, y))
}

// Release ends a press.
func (c *Cell) Release(x, y int) bool {
	if c.replay {
		return false
	}
	c.buttonHeld = false
	return c.deliver(c.event(toolkit.EventMouseUp, x, y))
}

// Context is a right-button press; the editor has no menu of its own, and the
// browser's own menu is the one that stays.
func (c *Cell) Context(int, int) bool { return false }

// Char is a printable character typed with no Ctrl, Meta or Alt.
func (c *Cell) Char(s string) bool {
	ev := c.event(toolkit.EventChar, 0, 0)
	ev.Code = s
	return c.deliver(ev)
}

// KeyDown is a named key, or a modified one.
func (c *Cell) KeyDown(s string) bool {
	ev := c.event(toolkit.EventKeyDown, 0, 0)
	ev.Code = c.code(s)
	return c.deliver(ev)
}

// code is the toolkit's name for the key: the key as the browser names it, or,
// with Ctrl (or Cmd, which the toolkit treats the same), "Ctrl+Z" and
// "Ctrl+Shift+Z", the spelling TextView's shortcuts are written in.
func (c *Cell) code(key string) string {
	if !c.mods.Ctrl && !c.mods.Meta {
		return key
	}
	if len(key) == 1 {
		key = strings.ToUpper(key)
	}
	if c.mods.Shift {
		return "Ctrl+Shift+" + key
	}
	return "Ctrl+" + key
}

// Modifiers records the modifier keys of the event about to be delivered.
func (c *Cell) Modifiers(m webcanvas.Modifiers) { c.mods = m }

// Scroll is the wheel. The cell has no vertical scroll (it is as tall as its
// text), so the wheel is the page's; only a horizontal gesture over a line that
// overflows the width is the editor's. A wheel with both a vertical and a
// horizontal part belongs to the page unless the horizontal part leads.
func (c *Cell) Scroll(x, y, dx, dy int) (repaint, consumed bool) {
	if dx == 0 || abs(dx) < abs(dy) {
		return false, false
	}
	ev := c.event(toolkit.EventScroll, x, y)
	ev.DeltaX = dx
	c.ed.OnEvent(ev)
	return true, true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Resize follows the canvas to the size the page lays it out at, in device
// pixels. The editor draws at that size; the height the text wants is reported
// through OnHeight, and the page's CSS height follows.
func (c *Cell) Resize(w, h int) (int, int) {
	if w > 0 && h > 0 {
		c.w, c.h = w, h
	}
	return c.w, c.h
}

// SetPixelRatio is the device pixel ratio. The toolkit's metrics (font, line
// pitch, gutter) scale with it, so the text is crisp on a dense display.
func (c *Cell) SetPixelRatio(r float64) {
	if r == c.ratio {
		return
	}
	c.ratio = r
	toolkit.SetMetricScale(r)
	c.height = c.ed.PreferredHeight().Get()
	if c.onHeight != nil {
		c.onHeight(c.HeightCSS())
	}
}

// RepaintWith keeps webcanvas's way to ask for a frame.
func (c *Cell) RepaintWith(request func()) { c.request.Store(&request) }

func (c *Cell) repaint() {
	if r := c.request.Load(); r != nil {
		(*r)()
	}
}

// --- accessibility and the text layer ---

func (c *Cell) host() webmirror.Host {
	return webmirror.Host{Sources: func() []webmirror.Source { return webmirror.Single(c.ed) }}
}

// A11yNodes is the editor as the accessibility mirror shows it: one multi-line
// textbox named Config.Name whose value is the text.
func (c *Cell) A11yNodes() []webcanvas.A11yNode { return c.host().A11yNodes() }

// TextInputFor gives the editor its text layer, the element that takes IME
// input, is found by find-in-page and selects across cells.
func (c *Cell) TextInputFor(key string) textinput.Client { return c.host().TextInputFor(key) }
