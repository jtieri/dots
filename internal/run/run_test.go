package run

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestOutput(t *testing.T) {
	out, err := Output(exec.CommandContext(t.Context(), "sh", "-c", "echo hello"))
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if got, want := string(out), "hello\n"; got != want {
		t.Errorf("Output() = %q, want %q", got, want)
	}
}

func TestOutputError(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "stderr is the message",
			args:    []string{"sh", "-c", "echo partial; echo 'it broke' >&2; exit 3"},
			wantMsg: "sh -c echo partial; echo 'it broke' >&2; exit 3: it broke",
		},
		{
			name:    "exit status when stderr is empty",
			args:    []string{"sh", "-c", "exit 3"},
			wantMsg: "sh -c exit 3: exit status 3",
		},
		{
			name:    "program not found",
			args:    []string{"dot-no-such-program"},
			wantMsg: `dot-no-such-program: exec: "dot-no-such-program": executable file not found in $PATH`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Output(exec.CommandContext(t.Context(), tt.args[0], tt.args[1:]...))
			if _, ok := errors.AsType[*Error](err); !ok {
				t.Fatalf("Output() error = %v, want an *Error", err)
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("Output() error = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestOutputExitCode(t *testing.T) {
	_, err := Output(exec.CommandContext(t.Context(), "sh", "-c", "exit 3"))
	if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok || exitErr.ExitCode() != 3 {
		t.Errorf("Output() error = %v, want exit code 3", err)
	}
}

func TestOutputRefusesSetStreams(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "true")
	cmd.Stdout = new(bytes.Buffer)
	if _, err := Output(cmd); err == nil {
		t.Error("Output() with Stdout set: error = nil, want an error")
	}
}

func TestReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "first line of stderr",
			err:  &Error{Stderr: "\n  fatal: it broke  \nhint: try again\n", Err: errors.New("exit status 128")},
			want: "fatal: it broke",
		},
		{
			name: "empty stderr",
			err:  &Error{Args: []string{"git", "fetch"}, Err: errors.New("exit status 1")},
			want: "git fetch: exit status 1",
		},
		{
			name: "wrapped",
			err:  fmt.Errorf("fetching: %w", &Error{Stderr: "fatal: it broke\n"}),
			want: "fatal: it broke",
		},
		{
			name: "other error",
			err:  errors.New("no answer"),
			want: "no answer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Reason(tt.err); got != tt.want {
				t.Errorf("Reason() = %q, want %q", got, tt.want)
			}
		})
	}
}
