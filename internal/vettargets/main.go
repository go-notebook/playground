// Command vettargets drives the "compile on every target" CI job.
//
// The target list always comes from `go tool dist list`; this program only
// groups it so CI can fan out one job per OS (one per GOARCH for linux).
//
//	vettargets matrix               print matrix={"include":[...]} ($GITHUB_OUTPUT line)
//	vettargets vet <goos> [goarch]  go vet ./... for every GOARCH of <goos>, or one
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// skipped lists the GOOS values that link only through cgo, so a CGO_ENABLED=0
// vet cannot type-check them.
var skipped = map[string]bool{"android": true, "ios": true}

// perArch lists the GOOS values with so many GOARCHes that one job per GOOS
// would be the critical path: they get one matrix entry per GOARCH.
var perArch = map[string]bool{"linux": true}

// entry is one matrix row; Goarch is empty for a whole-GOOS job.
type entry struct {
	Goos   string `json:"goos"`
	Goarch string `json:"goarch"`
}

var osExit = os.Exit

// vetPattern is the package pattern handed to go vet (a variable for tests).
var vetPattern = "./..."

// distList runs `go tool dist list`.
var distList = func() (string, error) {
	out, err := exec.Command("go", "tool", "dist", "list").Output()
	return string(out), err
}

// vetOne runs `go vet ./...` for one target and returns its combined output.
var vetOne = func(goos, goarch string) (string, error) {
	cmd := exec.Command("go", "vet", vetPattern)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// parse turns `go tool dist list` output into goos -> sorted goarch list,
// dropping skipped systems. Malformed lines are an error, not ignored.
func parse(list string) (map[string][]string, error) {
	m := map[string][]string{}
	for _, line := range strings.Fields(list) {
		os, arch, ok := strings.Cut(line, "/")
		if !ok || os == "" || arch == "" {
			return nil, fmt.Errorf("malformed dist target %q", line)
		}
		if skipped[os] {
			continue
		}
		m[os] = append(m[os], arch)
	}
	for _, a := range m {
		sort.Strings(a)
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("go tool dist list returned no targets")
	}
	return m, nil
}

func matrix(w io.Writer) error {
	list, err := distList()
	if err != nil {
		return err
	}
	m, err := parse(list)
	if err != nil {
		return err
	}
	oses := make([]string, 0, len(m))
	for o := range m {
		oses = append(oses, o)
	}
	sort.Strings(oses)
	var inc []entry
	for _, o := range oses {
		if !perArch[o] {
			inc = append(inc, entry{Goos: o})
			continue
		}
		for _, a := range m[o] {
			inc = append(inc, entry{Goos: o, Goarch: a})
		}
	}
	// Marshalling plain structs cannot fail.
	b, _ := json.Marshal(map[string][]entry{"include": inc})
	// "matrix=<json>" is the line format of $GITHUB_OUTPUT.
	_, err = fmt.Fprintf(w, "matrix=%s\n", b)
	return err
}

// vet vets every GOARCH of goos, or only goarch when it is not empty.
func vet(w io.Writer, goos, goarch string) error {
	list, err := distList()
	if err != nil {
		return err
	}
	m, err := parse(list)
	if err != nil {
		return err
	}
	arches, ok := m[goos]
	if !ok {
		return fmt.Errorf("no targets for GOOS=%q", goos)
	}
	if goarch != "" {
		if !slices.Contains(arches, goarch) {
			return fmt.Errorf("%s/%s is not a target", goos, goarch)
		}
		arches = []string{goarch}
	}
	failed := 0
	for _, arch := range arches {
		out, err := vetOne(goos, arch)
		if err != nil {
			failed++
			fmt.Fprintf(w, "::error::%s/%s\n%s", goos, arch, out)
			continue
		}
		fmt.Fprintf(w, "ok %s/%s\n", goos, arch)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d targets of %s failed", failed, len(arches), goos)
	}
	return nil
}

// RootCmd builds the command tree.
func RootCmd() *cobra.Command {
	root := &cobra.Command{Use: "vettargets", SilenceUsage: true}
	root.AddCommand(&cobra.Command{
		Use: "matrix", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return matrix(c.OutOrStdout()) },
	})
	root.AddCommand(&cobra.Command{
		Use: "vet <goos> [goarch]", Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, a []string) error {
			arch := ""
			if len(a) == 2 {
				arch = a[1]
			}
			return vet(c.OutOrStdout(), a[0], arch)
		},
	})
	return root
}

func main() {
	if err := RootCmd().Execute(); err != nil {
		osExit(1)
	}
}
