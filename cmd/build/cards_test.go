package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-notebook/playground/engine"
)

// wellFormed reports whether s parses as XML to its end.
func wellFormed(s string) error {
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		if _, err := d.Token(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
}

// The carousel's pictures: the Go cells' are what the engine gives for them
// (a value under the cell, or the figure itself), the others the text as it
// is; every one is well-formed SVG, and a card's text cannot become markup.
func TestCarousel(t *testing.T) {
	dir := t.TempDir()
	html, err := carousel(dir, "#047857")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		`aria-roledescription="carousel" aria-label="What go-notebook does"`,
		`data-fade="loading"`,
		`id="lk-carousel-start" class="btn btn-primary lk-car-start" hidden>Open the notebook</button>`,
		`aria-label="1 of 5: Cells"`,
		`whose ` + "```go" + ` blocks are cells`,
		`<a href="https://go-notebook.github.io/docs/latest/kernel/" target="_blank" rel="noopener">Read more`,
		`window.landingkitCarousel=api`,
	} {
		if !strings.Contains(string(html), w) {
			t.Errorf("the carousel lacks %q", w)
		}
	}
	var pics []string
	for i := range cards {
		b, err := os.ReadFile(filepath.Join(dir, "cards", fmt.Sprintf("%d.svg", i+1)))
		if err != nil {
			t.Fatal(err)
		}
		if err := wellFormed(string(b)); err != nil {
			t.Errorf("cards/%d.svg: %v", i+1, err)
		}
		if !strings.Contains(string(html), fmt.Sprintf(`src="cards/%d.svg?v=`, i+1)) {
			t.Errorf("the carousel does not show cards/%d.svg", i+1)
		}
		pics = append(pics, string(b))
	}
	if !strings.Contains(pics[0], `→ 6</text>`) {
		t.Error("the Cells picture does not show the value the engine gives")
	}
	if !strings.Contains(pics[0], `fill="#047857"`) || strings.Contains(pics[1], `fill="#047857"`) {
		t.Error("a cell has the accent bar, a console session does not")
	}
	if !strings.Contains(pics[1], `9.54929284535666e-07`) || !strings.Contains(pics[2], `Stats::Gamma.fit`) {
		t.Error("the console and Ruby pictures are not their text")
	}
	if strings.Contains(pics[3], "<text class=") || !strings.Contains(pics[3], "<path") {
		t.Error("the Figures picture is not the figure")
	}
	if !strings.Contains(pics[2], `{ |x| fit.cdf(x) }`) || !strings.Contains(pics[0], `&#34;strings&#34;`) {
		t.Error("the code is not escaped as text")
	}
}

func TestCarouselErrors(t *testing.T) {
	// the cards directory cannot be made
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := carousel(file, "#000"); err == nil {
		t.Error("cards under a file")
	}
	// a picture cannot be written
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cards", "2.svg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := carousel(dir, "#000"); err == nil || !strings.Contains(err.Error(), "2.svg") {
		t.Errorf("a picture over a directory: %v", err)
	}
	// a cell that shows nothing, or fails
	for _, rs := range [][]engine.Result{
		nil,
		{{Error: "boom"}},
		{{}},
	} {
		seam(t, &runCell, func(string) []engine.Result { return rs })
		if _, err := carousel(t.TempDir(), "#000"); err == nil || !strings.Contains(err.Error(), `the picture of "Cells": the cell shows nothing`) {
			t.Errorf("%+v: %v", rs, err)
		}
	}
}

// A notebook larger than the page edits is refused before anything is
// written; the page carries each example's Markdown, as written.
func TestNotebookSize(t *testing.T) {
	log := quiet(t)
	seam(t, &goCmd, fakeGo(fakeRoot(t)))
	big := filepath.Join(t.TempDir(), "big.md")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", engine.MaxNotebook+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := runTo(log, "-o", out, big); err == nil || !strings.Contains(err.Error(), "the page edits notebooks up to") {
		t.Errorf("err = %v", err)
	}
	nb := examples(t)[1]
	if err := runTo(log, "-o", out, nb); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(nb)
	page, _ := os.ReadFile(filepath.Join(out, "index.html"))
	want, _ := json.Marshal(string(src))
	if !strings.Contains(string(page), `"markdown":`+string(want)) {
		t.Error("the page does not carry the notebook's Markdown as written")
	}
}

// The build fails, and writes no page, when the carousel cannot be made.
func TestBuildCarouselError(t *testing.T) {
	log := quiet(t)
	seam(t, &goCmd, fakeGo(fakeRoot(t)))
	seam(t, &runCell, func(string) []engine.Result { return nil })
	out := t.TempDir()
	err := runTo(log, "-o", out, examples(t)[1])
	if err == nil || !strings.Contains(err.Error(), "the picture of") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err == nil {
		t.Error("a page was written without its carousel")
	}
}
