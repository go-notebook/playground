package engine

import (
	"strings"
	"testing"
)

// FuzzSplit checks that split never panics, and that its pieces are pieces
// of the cell, in order, each at the line it says.
func FuzzSplit(f *testing.F) {
	for _, s := range []string{
		"x := 1\nx\n", "import \"fmt\"\nfmt.Println(1) // c\n", "func f() {}\nfunc (t T) M() {}\nfunc() {}()\n",
		"x := []int{\n\t1,\n}\nx", "x := 1; x", "type T int\nvar v T\nv = 2\n", "func (", "`raw\nstring`\n", "/* c\n */ x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		at := 0
		for _, p := range split(src) {
			// From the start of its line, or the end of the piece before.
			from := at
			if ls := lineStart(src, p.line); ls > from {
				from = ls
			}
			i := strings.Index(src[from:], p.src)
			if i < 0 {
				t.Fatalf("piece %q is not in %q after %d", p.src, src, from)
			}
			start := from + i
			if line := 1 + strings.Count(src[:start], "\n"); line != p.line {
				t.Fatalf("piece %q of %q at line %d, says %d", p.src, src, line, p.line)
			}
			at = start + len(p.src)
		}
	})
}

// lineStart is the offset of the 1-based line, or len(src).
func lineStart(src string, line int) int {
	off := 0
	for ; line > 1; line-- {
		i := strings.IndexByte(src[off:], '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
	}
	return off
}
