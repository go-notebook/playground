//go:build !((js && ruby) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (darwin && (amd64 || arm64)) || (windows && (386 || amd64 || arm64)))

package engine

// RubySupported reports whether this build runs Ruby cells: the page's
// default WebAssembly core does not, nor a build where rbgo does not
// compile (see ruby.go).
const RubySupported = false

type rubySide struct{}

// rubyCell is what the default core says of a Ruby cell. The page never
// sends it one: it loads the Ruby core for a notebook that has any.
func (s *session) rubyCell(string) (string, []map[string]string, string) {
	return "", nil, "this build runs Go cells only (in the page, the Ruby core is loaded for a notebook with Ruby cells)"
}
