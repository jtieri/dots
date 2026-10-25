// Package run runs the external programs dot is built on, such as chezmoi
// and git.
package run

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// Error reports a command that could not be started or exited unsuccessfully.
type Error struct {
	// Args is the command line, starting with the program name.
	Args []string
	// Stderr is what the command wrote to standard error.
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return strings.Join(e.Args, " ") + ": " + msg
}

func (e *Error) Unwrap() error { return e.Err }

// Output runs cmd and returns its standard output. If the command fails, the
// error is an *Error that includes what it wrote to standard error.
func Output(cmd *exec.Cmd) ([]byte, error) {
	if cmd.Stdout != nil || cmd.Stderr != nil {
		return nil, errors.New("run: Stdout or Stderr already set")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &Error{Args: cmd.Args, Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}
