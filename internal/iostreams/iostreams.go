// Package iostreams provides the input and output streams that the dots
// commands read from and write to, and the prompts and pager built on them.
package iostreams

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"

	"github.com/jtieri/dots/internal/style"
)

// Streams holds the standard streams for a command.
type Streams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	// InIsTerminal, OutIsTerminal and ErrIsTerminal report whether each
	// stream is a terminal, where someone is reading and typing.
	InIsTerminal  bool
	OutIsTerminal bool
	ErrIsTerminal bool

	// NoPager makes Page write straight to Out.
	NoPager bool

	// OutStyle and ErrStyle mark up text written to Out and ErrOut. They
	// are plain until dots decides, from --color and the environment, to
	// color its output.
	OutStyle, ErrStyle style.Style
}

// System returns Streams for the process's standard input, output and error.
func System() *Streams {
	return &Streams{
		In:            os.Stdin,
		Out:           os.Stdout,
		ErrOut:        os.Stderr,
		InIsTerminal:  term.IsTerminal(int(os.Stdin.Fd())),
		OutIsTerminal: term.IsTerminal(int(os.Stdout.Fd())),
		ErrIsTerminal: term.IsTerminal(int(os.Stderr.Fd())),
	}
}

// Test returns Streams backed by buffers, along with the buffers, so a test
// can supply input and inspect output.
func Test() (s *Streams, in, out, errOut *bytes.Buffer) {
	in, out, errOut = new(bytes.Buffer), new(bytes.Buffer), new(bytes.Buffer)
	return &Streams{In: in, Out: out, ErrOut: errOut}, in, out, errOut
}

// CanPrompt reports whether someone is there to answer a question: both
// standard input and standard error are terminals.
func (s *Streams) CanPrompt() bool { return s.InIsTerminal && s.ErrIsTerminal }

// ErrNoAnswer is returned by Prompt when input ends before an answer.
var ErrNoAnswer = errors.New("no answer")

// Prompt writes question to ErrOut and returns the line typed in reply, with
// surrounding space removed and in lower case, for answers such as y or n.
// If ctx is done first, as after Ctrl-C, Prompt returns ctx's error without
// waiting for the line.
func (s *Streams) Prompt(ctx context.Context, question string) (string, error) {
	line, err := s.PromptLine(ctx, question)
	return strings.ToLower(line), err
}

// PromptLine is like Prompt but keeps the line's case, for answers that are
// text, such as a commit message.
func (s *Streams) PromptLine(ctx context.Context, question string) (string, error) {
	if _, err := io.WriteString(s.ErrOut, question); err != nil {
		return "", err
	}
	type reply struct {
		line string
		err  error
	}
	replies := make(chan reply, 1)
	go func() {
		line, err := readLine(s.In)
		replies <- reply{line, err}
	}()
	select {
	case <-ctx.Done():
		_, _ = io.WriteString(s.ErrOut, "\n")
		return "", ctx.Err()
	case r := <-replies:
		return strings.TrimSpace(r.line), r.err
	}
}

// readLine reads up to and including the next newline, a byte at a time so
// nothing after the line is consumed.
func readLine(r io.Reader) (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return string(line), nil
			}
			line = append(line, b[0])
		}
		if errors.Is(err, io.EOF) {
			if len(line) > 0 {
				return string(line), nil
			}
			return "", ErrNoAnswer
		}
		if err != nil {
			return "", err
		}
	}
}

// Page shows text through the pager, $PAGER or less, when Out is a terminal
// and NoPager is unset; otherwise it writes text to Out. Like git, it sets
// LESS to FRX when unset, so less exits when the text fits on one screen and
// leaves it there.
func (s *Streams) Page(ctx context.Context, text []byte) error {
	if s.NoPager || !s.OutIsTerminal {
		_, err := s.Out.Write(text)
		return err
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", pager)
	cmd.Stdin = bytes.NewReader(text)
	cmd.Stdout, cmd.Stderr = s.Out, s.ErrOut
	if os.Getenv("LESS") == "" {
		cmd.Env = append(os.Environ(), "LESS=FRX")
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pager %s: %w (--no-pager prints instead)", pager, err)
	}
	return nil
}
