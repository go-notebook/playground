package notes

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRoundTrip(t *testing.T) {
	in := []Note{{Line: 3, Text: "why"}, {Line: 1, Text: "a --> b <script>alert(1)</script> & \"q\" é\nsecond"}}
	line := Line(in)
	if !strings.HasPrefix(line, "<!-- gonb:notes ") || !strings.HasSuffix(line, " -->\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("not one comment line: %q", line)
	}
	if mid := line[len(prefix) : len(line)-len(suffix)-1]; strings.ContainsAny(mid, "<>&-") {
		t.Fatalf("the text can close the comment early or carry markup: %q", mid)
	}
	got, rest, ok := Split(line + "prose\n")
	if !ok || rest != "prose\n" {
		t.Fatalf("ok=%v rest=%q", ok, rest)
	}
	want := Clean(in)
	if !reflect.DeepEqual(got, want) || want[0].Line != 1 {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNothingToWriteIsNoLine(t *testing.T) {
	if Line(nil) != "" || Line([]Note{{Line: 1, Text: "  "}, {Line: 0, Text: "x"}}) != "" {
		t.Fatal("empty notes must write no comment")
	}
}

func TestMalformedCommentsStayProse(t *testing.T) {
	for _, s := range []string{
		"<!-- gonb:notes not json -->\nx\n",
		"<!-- gonb:notes {\"v\":2,\"notes\":[]} -->\nx\n",
		"<!-- gonb:notes {\"v\":1,\"notes\":[],\"extra\":1} -->\nx\n",
		"<!-- gonb:notes {\"v\":1,\"notes\":[]}\nx\n",
		"text\n<!-- gonb:notes {\"v\":1,\"notes\":[]} -->\n",
		"<!-- gonb:notes {\"v\":1,\"notes\":[]} --> trailing\n",
		"<!-- gonb:notes {\"v\":1,\"notes\":[]} -->{}\n",
	} {
		if n, rest, ok := Split(s); ok || rest != s || n != nil {
			t.Errorf("%q was taken as notes: %+v %q", s, n, rest)
		}
	}
}

func TestBounds(t *testing.T) {
	var many []Note
	for i := 1; i <= 2*MaxNotes; i++ {
		many = append(many, Note{Line: i, Text: "x"})
	}
	if n := len(Clean(many)); n != MaxNotes {
		t.Fatalf("%d notes kept, want %d", n, MaxNotes)
	}
	long := Clean([]Note{{Line: 1, Text: strings.Repeat("é", MaxRunes+10)}})
	if utf8.RuneCountInString(long[0].Text) != MaxRunes {
		t.Fatal("text not cut")
	}
	if got := Sanitize("a\x00b\x1b[0m\tc\u0085d\xff"); got != "ab[0m cd" {
		t.Fatalf("%q", got)
	}
	// a huge comment line is refused, not parsed
	big := prefix + `{"v":1,"notes":[{"line":1,"text":"` + strings.Repeat("a", MaxComment) + `"}]}` + suffix + "\n"
	if _, _, ok := Split(big); ok {
		t.Fatal("an oversize comment was read")
	}
	// one note per line, the last wins
	c := Clean([]Note{{Line: 2, Text: "a"}, {Line: 2, Text: "b"}})
	if len(c) != 1 || c[0].Text != "b" {
		t.Fatalf("%+v", c)
	}
}

func FuzzSplit(f *testing.F) {
	f.Add(Line([]Note{{Line: 1, Text: "a"}}) + "rest")
	f.Add("<!-- gonb:notes {} -->")
	f.Add("<!-- gonb:notes {\"v\":1,\"notes\":[{\"line\":-1,\"text\":\"\\u0000\"}]} -->\n")
	f.Fuzz(func(t *testing.T, s string) {
		n, rest, ok := Split(s)
		if !ok && (rest != s || n != nil) {
			t.Fatal("a refusal must return the prose unchanged")
		}
		if len(n) > MaxNotes {
			t.Fatal("unbounded")
		}
		for _, x := range n {
			if x.Line < 1 || utf8.RuneCountInString(x.Text) > MaxRunes || !utf8.ValidString(x.Text) {
				t.Fatalf("bad note %+v", x)
			}
		}
		// what is written reads back the same
		if l := Line(n); l != "" {
			m, _, ok := Split(l)
			if !ok || !reflect.DeepEqual(m, n) {
				t.Fatalf("round trip %+v -> %+v", n, m)
			}
		}
	})
}
