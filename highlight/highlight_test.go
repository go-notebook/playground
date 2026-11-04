package highlight

import (
	"regexp"
	"strings"
	"testing"

	"github.com/go-rouge/rouge"
)

func TestHTML(t *testing.T) {
	got, ok := HTML("go", "func main() { return 1 < 2 && \"a\" != `b` }\n")
	if !ok {
		t.Fatal("a Go cell was refused")
	}
	for _, want := range []string{`<span class="k">func</span>`, `<span class="k">return</span>`, `<span class="s">"a"</span>`, `&lt;`, `&amp;&amp;`} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML lacks %s:\n%s", want, got)
		}
	}
	if _, ok := HTML("ruby", "puts :sym # <b>\n"); !ok {
		t.Error("a Ruby cell was refused")
	}
	if got, ok := HTML("go", ""); !ok || got != "" {
		t.Errorf("an empty cell = %q, %v; want empty and accepted", got, ok)
	}
}

// A language without a lexer, an unknown lexer, a source over the cap and a
// source rouge cannot hand back as it was are all refused: plain text.
func TestRefusals(t *testing.T) {
	for _, tc := range []struct{ name, lexer, src string }{
		{"no lexer (Julia, R, markdown)", "", "x = 1\n"},
		{"unknown lexer", "no-such-lexer", "x = 1\n"},
		{"over the cap", "go", strings.Repeat("a", MaxSource+1)},
		// rouge's formatter drops \r: the text would not be the source
		{"a carriage return", "go", "x := 1\r\ny := 2\n"},
	} {
		if got, ok := HTML(tc.lexer, tc.src); ok || got != "" {
			t.Errorf("%s: HTML = %q, %v; want a refusal", tc.name, got, ok)
		}
	}
	// exactly at the cap is highlighted
	if _, ok := HTML("go", strings.Repeat("a", MaxSource)); !ok {
		t.Error("a cell of exactly MaxSource bytes was refused")
	}
}

// hostile is text that would run as markup if it were inserted unescaped.
var hostile = []string{
	"<script>alert(1)</script>",
	"<img src=x onerror=alert(1)>",
	`"><svg/onload=alert(1)>`,
	"'><iframe src=javascript:alert(1)>",
	"&lt;b&gt; &amp; &#60; &#x3c; &",
	"</span><script>x</span>",
	"a < b > c && d || \"q\" 'r' `s`",
	"x = '<a href=\"javascript:1\">'",
	"// <script>\n/* </span> */\n# <b>\n",
	"<!-- --> <![CDATA[ ]]> <?php ?>",
}

// rouge escapes <, > and & in every token it writes, and writes a class only
// from its own token table: the spans are the only markup, whatever the
// lexer and the text. A quote is left as it is, and is harmless where it is,
// in text (never in an attribute: a class is letters and digits).
func TestRougeEscapesEveryToken(t *testing.T) {
	tag := regexp.MustCompile(`<[^<>]*>`)
	span := regexp.MustCompile(`^(<span class="[a-z0-9]{1,3}">|</span>)$`)
	entity := regexp.MustCompile(`&(amp|lt|gt);`)
	for _, lexer := range []string{"go", "ruby", "c", "cpp", "python", "javascript", "html", "xml", "php", "sql", "shell", "markdown", "yaml", "text"} {
		if rouge.FindLexer(lexer) == nil {
			continue // not in this go-rouge
		}
		for _, src := range hostile {
			out, err := rouge.Highlight(src, lexer, "html")
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range tag.FindAllString(out, -1) {
				if !span.MatchString(m) {
					t.Errorf("%s %q: markup %q is not a span", lexer, src, m)
				}
			}
			bare := entity.ReplaceAllString(tag.ReplaceAllString(out, ""), "")
			if strings.ContainsAny(bare, "<>&") {
				t.Errorf("%s %q: an unescaped character in the text: %q", lexer, src, bare)
			}
			// and what the page would insert is the source, checked
			if got, ok := HTML(lexer, src); ok && !safe(got, src) {
				t.Errorf("%s %q: HTML returned what safe refuses", lexer, src)
			}
		}
	}
}

func TestSafe(t *testing.T) {
	for _, tc := range []struct {
		name, html, src string
		want            bool
	}{
		{"text", "a b", "a b", true},
		{"a span", `<span class="k">go</span> x`, "go x", true},
		{"two spans", `<span class="k">a</span><span class="s1">b</span>`, "ab", true},
		{"entities", `&lt;&gt;&amp;<span class="x">&amp;</span>`, "<>&&", true},
		{"a quote in text", `<span class="s">"'</span>`, `"'`, true},
		{"empty", "", "", true},
		{"text lost", `<span class="k">a</span>`, "ab", false},
		{"text added", `<span class="k">ab</span>`, "a", false},
		{"unclosed span", `<span class="k">a`, "a", false},
		{"nested span", `<span class="k"><span class="s">a</span></span>`, "a", false},
		{"a closing tag with nothing open", `a</span>`, "a", false},
		{"another tag", `<b>a</b>`, "a", false},
		{"another tag in a span", `<span class="k"><b></span>`, "", false},
		{"a script", `<script>x</script>`, "x", false},
		{"an attribute", `<span class="k" onclick="x">a</span>`, "a", false},
		{"another attribute", `<span id="k">a</span>`, "a", false},
		{"a quote in the class", `<span class="k&quot;">a</span>`, "a", false},
		{"an empty class", `<span class="">a</span>`, "a", false},
		{"a long class", `<span class="` + strings.Repeat("a", 17) + `">a</span>`, "a", false},
		{"the longest class", `<span class="` + strings.Repeat("a", 16) + `">a</span>`, "a", true},
		{"an unterminated tag", `<span class="k"`, "", false},
		{"a bare >", `a > b`, "a > b", false},
		{"a bare <", `a < b`, "a < b", false},
		{"a numeric entity", `&#60;`, "<", false},
		{"a bare &", `a & b`, "a & b", false},
		{"a named entity", `&quot;`, `"`, false},
	} {
		if got := safe(tc.html, tc.src); got != tc.want {
			t.Errorf("%s: safe(%q, %q) = %v, want %v", tc.name, tc.html, tc.src, got, tc.want)
		}
	}
}

// FuzzHTML: whatever the lexer and the text, what HTML accepts passes the
// check, and is made of spans only.
func FuzzHTML(f *testing.F) {
	for _, src := range hostile {
		f.Add("go", src)
		f.Add("ruby", src)
	}
	f.Add("c", "#include <stdio.h>\nint main(void) { return 0; }\n")
	f.Add("", "x")
	f.Add("nope", "x")
	f.Fuzz(func(t *testing.T, lexer, src string) {
		out, ok := HTML(lexer, src)
		if !ok {
			if out != "" {
				t.Fatalf("a refusal returned %q", out)
			}
			return
		}
		if !safe(out, src) {
			t.Fatalf("HTML(%q, %q) = %q: not safe", lexer, src, out)
		}
		if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(out, "<span class=\"", ""), "</span>", ""), "<") {
			t.Fatalf("markup other than spans in %q", out)
		}
	})
}
