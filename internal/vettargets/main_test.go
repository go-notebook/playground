package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// The real implementations, captured before any test stubs them.
var realDist, realVet = distList, vetOne

const sample = "android/arm64\naix/ppc64\nlinux/amd64\nlinux/386\nios/arm64\nwindows/arm64\n"

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := RootCmd()
	c.SetArgs(args)
	c.SetOut(&out)
	c.SetErr(&out)
	err := c.Execute()
	return out.String(), err
}

func TestMatrixSkipsCgoOnlyAndIsSorted(t *testing.T) {
	distList = func() (string, error) { return sample, nil }
	out, err := run(t, "matrix")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != `matrix={"include":[{"goos":"aix","goarch":""},{"goos":"linux","goarch":"386"},{"goos":"linux","goarch":"amd64"},{"goos":"windows","goarch":""}]}` {
		t.Fatalf("got %q", out)
	}
}

func TestVetReportsEveryFailure(t *testing.T) {
	distList = func() (string, error) { return sample, nil }
	var seen []string
	vetOne = func(goos, arch string) (string, error) {
		seen = append(seen, goos+"/"+arch)
		if arch == "386" {
			return "boom\n", errors.New("exit 1")
		}
		return "", nil
	}
	out, err := run(t, "vet", "linux")
	if err == nil {
		t.Fatal("expected failure for linux/386")
	}
	if strings.Join(seen, ",") != "linux/386,linux/amd64" {
		t.Fatalf("seen %v", seen)
	}
	if !strings.Contains(out, "::error::linux/386") || !strings.Contains(out, "ok linux/amd64") {
		t.Fatalf("output %q", out)
	}
}

func TestRealDistListAndVet(t *testing.T) {
	// The unstubbed defaults: the real target list, and a real vet of this
	// package for the host target (pattern narrowed so it stays fast).
	distList, vetOne = realDist, realVet
	list, err := distList()
	if err != nil {
		t.Fatal(err)
	}
	m, err := parse(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(m["linux"]) == 0 || m["android"] != nil || m["ios"] != nil {
		t.Fatalf("unexpected targets: %v", m)
	}
	old := vetPattern
	vetPattern = "."
	defer func() { vetPattern = old }()
	if out, err := vetOne("linux", "amd64"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := vetOne("nosuchos", "amd64"); err == nil {
		t.Fatal("an invalid GOOS must fail")
	}
}

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestListFailuresPropagate(t *testing.T) {
	old := distList
	defer func() { distList = old }()
	distList = func() (string, error) { return "", errors.New("no go") }
	if _, err := run(t, "matrix"); err == nil {
		t.Fatal("matrix must fail when the list fails")
	}
	if _, err := run(t, "vet", "linux"); err == nil {
		t.Fatal("vet must fail when the list fails")
	}
	distList = func() (string, error) { return "linux", nil }
	if _, err := run(t, "vet", "linux"); err == nil {
		t.Fatal("vet must fail on a malformed list")
	}
	distList = func() (string, error) { return sample, nil }
	if err := matrix(badWriter{}); err == nil {
		t.Fatal("a failed write must be reported")
	}
}

func TestMainExitsNonZeroOnError(t *testing.T) {
	oldArgs, oldExit, oldList := os.Args, osExit, distList
	defer func() { os.Args, osExit, distList = oldArgs, oldExit, oldList }()
	distList = func() (string, error) { return sample, nil }
	code := 0
	osExit = func(c int) { code = c }
	os.Args = []string{"vettargets", "vet", "plan9"}
	main()
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
}

func TestVetAllPass(t *testing.T) {
	distList = func() (string, error) { return sample, nil }
	vetOne = func(string, string) (string, error) { return "", nil }
	if _, err := run(t, "vet", "windows"); err != nil {
		t.Fatal(err)
	}
}

func TestVetOneArch(t *testing.T) {
	distList = func() (string, error) { return sample, nil }
	var seen []string
	vetOne = func(goos, arch string) (string, error) {
		seen = append(seen, goos+"/"+arch)
		return "", nil
	}
	if _, err := run(t, "vet", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, ",") != "linux/amd64" {
		t.Fatalf("seen %v", seen)
	}
	if _, err := run(t, "vet", "linux", "mips"); err == nil {
		t.Fatal("an unknown GOARCH must fail")
	}
}

func TestUnknownOSAndMalformed(t *testing.T) {
	distList = func() (string, error) { return sample, nil }
	if _, err := run(t, "vet", "plan9"); err == nil {
		t.Fatal("unknown GOOS must fail")
	}
	distList = func() (string, error) { return "linux", nil }
	if _, err := run(t, "matrix"); err == nil {
		t.Fatal("malformed list must fail")
	}
	distList = func() (string, error) { return "ios/arm64\n", nil }
	if _, err := run(t, "matrix"); err == nil {
		t.Fatal("a list with no usable target must fail")
	}
}
