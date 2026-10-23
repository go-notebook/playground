//go:build js && !ruby

package engine

// RubySupported reports whether this build runs Ruby cells: the page's
// default WebAssembly core does not (see ruby.go).
const RubySupported = false

type rubySide struct{}

// rubyCell is what the default core says of a Ruby cell. The page never
// sends it one: it loads the Ruby core for a notebook that has any.
func (s *session) rubyCell(string) (string, []map[string]string, string) {
	return "", nil, "this core runs Go cells only; the page loads the Ruby core for a notebook with Ruby cells"
}
