// Command build writes a deployable playground: a directory holding the
// page, the WebAssembly core built from source, Go's wasm_exec.js and the
// example notebooks.
//
//	go run github.com/go-notebook/playground/cmd/build -o public/playground \
//	    --org go-statistics --home https://go-statistics.github.io/ \
//	    --accent '#9e520f' --accent-dark '#f2b36b' notebooks/*.md
//
// It runs from a Go module that requires github.com/go-notebook/playground,
// so that the version it builds is the one go.mod pins. Before writing
// anything it runs every notebook, natively, with the same engine the page
// runs in WebAssembly, and fails if a cell fails: a site never publishes an
// example that does not run. A Julia cell is checked, never run, so a Julia
// cell that parses is not a failure, nor is a cell that reads its variables
// (blocked); a syntax error in a Julia cell is.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

	"github.com/spf13/cobra"

	"github.com/go-notebook/playground/engine"
	"github.com/go-notebook/playground/web"
)

// corePkg is the package of the WebAssembly core.
const corePkg = "github.com/go-notebook/playground/cmd/playground"

// config is what the flags say, and where the command writes.
type config struct {
	out, org, home, accent, accentDark, libraries string
	notebooks                                     []string
	stdout, stderr                                io.Writer
}

// Seams for the tests.
var (
	// osExit is os.Exit, replaced by tests.
	osExit = os.Exit
	// goStderr is where the go command's own messages go: the command's
	// error stream, set by each run.
	goStderr io.Writer = os.Stderr
	// goCmd runs the go command with extra environment, and returns its
	// standard output.
	goCmd = func(env []string, args ...string) ([]byte, error) {
		cmd := exec.Command("go", args...)
		cmd.Env = append(os.Environ(), env...)
		cmd.Stderr = goStderr
		return cmd.Output()
	}
	buildInfo = debug.ReadBuildInfo
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		osExit(1)
	}
}

// run is the command, on explicit streams: the arguments are read with the
// single-dash long flags of earlier releases rewritten, then executed.
func run(args []string, stdout, stderr io.Writer) error {
	root := RootCmd()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(legacy(root, args, stderr))
	return root.Execute()
}

// RootCmd returns the build command.
func RootCmd() *cobra.Command {
	var c config
	cmd := &cobra.Command{
		Use:   "build [flags] notebook.md...",
		Short: "Write a deployable playground from a set of example notebooks",
		Long: `build writes a deployable playground: a directory holding the page, the
WebAssembly cores built from source, Go's wasm_exec.js and the example
notebooks. It first runs every notebook natively, and fails if a cell fails.`,
		Example: `  go run github.com/go-notebook/playground/cmd/build -o public/playground \
      --org go-statistics --home https://go-statistics.github.io/ \
      --accent '#9e520f' --accent-dark '#f2b36b' notebooks/*.md`,
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("no notebooks: give the example notebooks as arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			c.notebooks = args
			for _, col := range []string{c.accent, c.accentDark} {
				if !colour.MatchString(col) {
					return fmt.Errorf("%q is not a #rgb or #rrggbb colour", col)
				}
			}
			c.stdout, c.stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
			goStderr = c.stderr
			return build(c)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&c.out, "out", "o", "playground", "output directory")
	f.StringVar(&c.org, "org", "go-notebook", "the organisation the page belongs to, for its title and its home link")
	f.StringVar(&c.home, "home", "../", "the URL the home link points to")
	f.StringVar(&c.accent, "accent", "#047857", "accent colour on a light background")
	f.StringVar(&c.accentDark, "accent-dark", "#6ee7b7", "accent colour on a dark background")
	f.StringVar(&c.libraries, "libraries", "go-statistics, go-numerics, go-ndarray and go-plot", "the libraries the footer names")
	return cmd
}

// legacy rewrites the single-dash long flags earlier releases accepted
// (-org x, -accent-dark=#fff), read by the standard flag package, into the
// double-dash spelling pflag requires, and says so once on warn. pflag reads
// one dash as one letter: left alone, -org would be -o with the value "rg".
//
// Only a name the command registers is rewritten; anything else is left for
// pflag to report in its own words. The value of a flag that takes one is
// copied as it is, even when it starts with a dash, and so is everything
// after a bare "--". No long name is made only of registered shorthand
// letters (a test holds that), so a rewrite never changes what a cluster
// would have meant.
func legacy(root *cobra.Command, args []string, warn io.Writer) []string {
	cmd, _, err := root.Find(args)
	if err != nil {
		return args // cobra says why
	}
	cmd.InitDefaultHelpFlag()
	cmd.InitDefaultVersionFlag()
	flags := cmd.Flags()
	out := make([]string, 0, len(args))
	var rewritten []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			out = append(out, arg)
			continue
		}
		body, double := strings.CutPrefix(arg[1:], "-")
		name, _, inline := strings.Cut(body, "=")
		f := flags.Lookup(name)
		switch {
		case double:
		case len(name) == 1:
			f = flags.ShorthandLookup(name)
		case f != nil:
			arg = "-" + arg
			rewritten = append(rewritten, name)
		}
		out = append(out, arg)
		if f != nil && !inline && f.NoOptDefVal == "" && i+1 < len(args) {
			i++
			out = append(out, args[i])
		}
	}
	if len(rewritten) > 0 {
		fmt.Fprintf(warn, "build: read -%s as --%s; flags take two dashes now\n",
			strings.Join(rewritten, ", -"), strings.Join(rewritten, ", --"))
	}
	return out
}

var colour = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// example is a notebook as the page receives it.
type example struct {
	Name  string        `json:"name"`
	Title string        `json:"title"`
	Cells []engine.Cell `json:"cells"`
	// Markdown is the notebook as written: what the page edits through the
	// format package, and what Download gives back byte for byte.
	Markdown string `json:"markdown"`
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
		var langs []string // of the code cells, as Run numbers them
		for _, c := range engine.Cells(string(src)) {
			if c.Kind == "code" {
				langs = append(langs, c.Lang)
			}
		}
		for _, r := range engine.Run(string(src)) {
			if cellFailed(r, langs[r.Cell]) {
				fmt.Fprintf(c.stderr, "%s:%d: cell %d failed:\n%s\n", path, r.Line, r.Cell, r.Error)
				failed = true
			}
		}
		if failed {
			return fmt.Errorf("%s does not run", path)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		if len(src) > engine.MaxNotebook {
			return fmt.Errorf("%s is %d bytes; the page edits notebooks up to %d", path, len(src), engine.MaxNotebook)
		}
		exs = append(exs, example{Name: name, Title: title(string(src), name), Cells: engine.Cells(string(src)), Markdown: string(src)})
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
	// The core with Ruby cells, which the page loads only for a notebook
	// that has one: rbgo's core build (the language and its numeric stack)
	// and the go-scikit bindings, on top of everything the Go core has.
	rubyWasm := filepath.Join(c.out, "playground-ruby.wasm")
	if _, err := goCmd([]string{"GOOS=js", "GOARCH=wasm", "GOWORK=off"},
		"build", "-trimpath", "-tags=ruby,rbgo_core", "-ldflags=-s -w", "-o", rubyWasm, corePkg); err != nil {
		return fmt.Errorf("building the Ruby WebAssembly core: %w", err)
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
	// What go-notebook does, while the core downloads: the landing kit's
	// loading carousel, and its pictures under cards/.
	car, err := carousel(c.out, c.accent)
	if err != nil {
		return err
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
		"Carousel":    string(car), // html/template's output
	})
	if err := os.WriteFile(filepath.Join(c.out, "index.html"), page.Bytes(), 0o644); err != nil {
		return err
	}

	st, err := os.Stat(wasm)
	if err != nil {
		return err
	}
	rst, err := os.Stat(rubyWasm)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "wrote %s: %d notebooks, playground.wasm %.1f MB, playground-ruby.wasm %.1f MB\n",
		c.out, len(exs), float64(st.Size())/1e6, float64(rst.Size())/1e6)
	return nil
}

// cellFailed reports whether a cell of the language lang failed. Two outcomes
// are not failures, by their typed status and not by their text: a Julia cell
// that parsed and checked and is not run by design (go-embedded-julia has no
// runtime yet), and a cell of another language that reads what one defines.
// Each is accepted only where it can happen: anything else with an error, a
// syntax error in a Julia cell included, fails the build.
func cellFailed(r engine.Result, lang string) bool {
	if r.Error == "" {
		return false
	}
	switch r.Status {
	case engine.StatusNotRun:
		return lang != "julia"
	case engine.StatusBlocked:
		return lang == "julia"
	}
	return true
}

var heading =regexp.MustCompile(`(?m)^#\s+(.+?)\s*#*\s*$`)

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
