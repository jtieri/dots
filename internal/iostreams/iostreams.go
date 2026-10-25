// Package iostreams provides the input and output streams that dot's
// commands read from and write to.
package iostreams

import (
	"bytes"
	"io"
	"os"
)

// Streams holds the standard streams for a command.
type Streams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// System returns Streams for the process's standard input, output and error.
func System() *Streams {
	return &Streams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
}

// Test returns Streams backed by buffers, along with the buffers, so a test
// can supply input and inspect output.
func Test() (s *Streams, in, out, errOut *bytes.Buffer) {
	in, out, errOut = new(bytes.Buffer), new(bytes.Buffer), new(bytes.Buffer)
	return &Streams{In: in, Out: out, ErrOut: errOut}, in, out, errOut
}
