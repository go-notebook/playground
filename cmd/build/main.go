// Command build writes a deployable playground: a directory holding the
// page, the WebAssembly core built from source, Go's wasm_exec.js and the
// example notebooks.
//
//	go run github.com/go-notebook/playground/cmd/build -o public/playground \
//	    -org go-statistics -home https://go-statistics.github.io/ \
//	    -accent '#9e520f' -accent-dark '#f2b36b' notebooks/*.md
//
// It runs from a Go module that requires github.com/go-notebook/playground,
// so that the version it builds is the one go.mod pins. Before writing
// anything it runs every notebook, natively, with the same engine the page
// runs in WebAssembly, and fails if a cell fails: a site never publishes an
// example that does not run.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"text/template"

	"github.com/go-notebook/playground/engine"
	"github.com/go-notebook/playground/web"
)

// corePkg is the package of the WebAssembly core.
const corePkg = "github.com/go-notebook/playground/cmd/playground"

// config is what the flags say.
type config struct {
	out, org, home, accent, accentDark, libraries string
	notebooks                                     []string
}

// Seams for the tests.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
	exit             = os.Exit
	// goCmd runs the go command with extra environment, and returns its
	// standard output.
	goCmd = func(env []string, args ...string) ([]byte, error) {
		cmd := exec.Command("go", args...)
		cmd.Env = append(os.Environ(), env...)
		cmd.Stderr = stderr
		return cmd.Output()
	}
	buildInfo = debug.ReadBuildInfo
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(stderr, "build:", err)
		exit(1)
	}
}

func run(args []string) error {
	var c config
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.out, "o", "playground", "output `directory`")
	fs.StringVar(&c.org, "org", "go-notebook", "the organisation the page belongs to, for its title and its home link")
	fs.StringVar(&c.home, "home", "../", "the `URL` the home link points to")
	fs.StringVar(&c.accent, "accent", "#047857", "accent `colour` on a light background")
	fs.StringVar(&c.accentDark, "accent-dark", "#6ee7b7", "accent `colour` on a dark background")
	fs.StringVar(&c.libraries, "libraries", "go-statistics, go-numerics, go-ndarray and go-plot", "the libraries the footer names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c.notebooks = fs.Args()
	if len(c.notebooks) == 0 {
		return errors.New("no notebooks: give the example notebooks as arguments")
	}
	for _, col := range []string{c.accent, c.accentDark} {
		if !colour.MatchString(col) {
			return fmt.Errorf("%q is not a #rgb or #rrggbb colour", col)
		}
	}
	return build(c)
}

var colour = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// example is a notebook as the page receives it.
type example struct {
	Name  string        `json:"name"`
	Title string        `json:"title"`
	Cells []engine.Cell `json:"cells"`
}

func build(c config) error {
	// Every example runs, or nothing is written.
	var exs []example
	sort.Strings(c.notebooks)
	for _, path := range c.notebooks {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		failed := false
		for _, r := range engine.Run(string(src)) {
			if r.Error != "" {
				fmt.Fprintf(stderr, "%s:%d: cell %d failed:\n%s\n", path, r.Line, r.Cell, r.Error)
				failed = true
			}
		}
		if failed {
			return fmt.Errorf("%s does not run", path)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		exs = append(exs, example{Name: name, Title: title(string(src), name), Cells: engine.Cells(string(src))})
	}

	if err := os.MkdirAll(filepath.Join(c.out, "notebooks"), 0o755); err != nil {
		return err
	}
	for i, path := range c.notebooks {
		src, _ := os.ReadFile(path) // read above
		if err := os.WriteFile(filepath.Join(c.out, "notebooks", exs[i].Name+".md"), src, 0o644); err != nil {
			return err
		}
	}

	// The core, built here from the version go.mod pins.
	wasm := filepath.Join(c.out, "playground.wasm")
	if _, err := goCmd([]string{"GOOS=js", "GOARCH=wasm", "GOWORK=off"},
		"build", "-trimpath", "-ldflags=-s -w", "-o", wasm, corePkg); err != nil {
		return fmt.Errorf("building the WebAssembly core: %w", err)
	}
	root, err := goCmd(nil, "env", "GOROOT")
	if err != nil {
		return fmt.Errorf("go env GOROOT: %w", err)
	}
	execJS, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(root)), "lib", "wasm", "wasm_exec.js"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.out, "wasm_exec.js"), execJS, 0o644); err != nil {
		return err
	}

	// The page.
	for _, name := range []string{"playground.css", "playground.js", "worker.js"} {
		b, _ := web.Files.ReadFile(name) // embedded
		if err := os.WriteFile(filepath.Join(c.out, name), b, 0o644); err != nil {
			return err
		}
	}
	data, _ := json.Marshal(exs) // escapes <, > and &: safe inside <script>
	tmpl := template.Must(template.ParseFS(web.Files, "index.html"))
	var page bytes.Buffer
	_ = tmpl.Execute(&page, map[string]string{ // into memory: cannot fail
		"Title":       html.EscapeString(c.org + " playground"),
		"Description": html.EscapeString("Run Go notebooks using " + c.libraries + " in your browser: the cells are interpreted in WebAssembly, no server."),
		"Org":         html.EscapeString(c.org),
		"Home":        html.EscapeString(c.home),
		"Accent":      c.accent,
		"AccentDark":  c.accentDark,
		"Libraries":   html.EscapeString(c.libraries),
		"Version":     html.EscapeString(version()),
		"Examples":    string(data),
	})
	if err := os.WriteFile(filepath.Join(c.out, "index.html"), page.Bytes(), 0o644); err != nil {
		return err
	}

	st, err := os.Stat(wasm)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: %d notebooks, playground.wasm %.1f MB\n", c.out, len(exs), float64(st.Size())/1e6)
	return nil
}

var heading = regexp.MustCompile(`(?m)^#\s+(.+?)\s*#*\s*$`)

// title is the notebook's first level-one heading, or its name.
func title(src, name string) string {
	if m := heading.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	return name
}

// version says which playground and which Go built the page.
func version() string {
	v := "(devel)"
	gover := ""
	if bi, ok := buildInfo(); ok {
		gover = bi.GoVersion
		if bi.Main.Path == "github.com/go-notebook/playground" {
			v = bi.Main.Version
		}
		for _, d := range bi.Deps {
			if d.Path == "github.com/go-notebook/playground" {
				v = d.Version
			}
		}
	}
	if gover == "" {
		return "go-notebook/playground " + v
	}
	return "go-notebook/playground " + v + ", " + gover
}
