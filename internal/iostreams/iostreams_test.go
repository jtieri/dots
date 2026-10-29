package iostreams

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestPrompt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "answer", input: "y\n", want: "y"},
		{name: "spaces and case", input: "  Yes \n", want: "yes"},
		{name: "just enter", input: "\n", want: ""},
		{name: "last line without a newline", input: "n", want: "n"},
		{name: "no input", input: "", wantErr: ErrNoAnswer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, in, _, errOut := Test()
			in.WriteString(tt.input)
			got, err := s.Prompt(t.Context(), "Apply? ")
			if got != tt.want || !errors.Is(err, tt.wantErr) {
				t.Errorf("Prompt() = %q, %v; want %q, %v", got, err, tt.want, tt.wantErr)
			}
			if errOut.String() != "Apply? " {
				t.Errorf("Prompt() wrote %q to stderr, want the question", errOut)
			}
		})
	}
}

func TestPromptLine(t *testing.T) {
	s, in, _, errOut := Test()
	in.WriteString("  Fix Zed's Font Size \n")
	got, err := s.PromptLine(t.Context(), "Message: ")
	if want := "Fix Zed's Font Size"; got != want || err != nil {
		t.Errorf("PromptLine() = %q, %v; want %q, nil", got, err, want)
	}
	if errOut.String() != "Message: " {
		t.Errorf("PromptLine() wrote %q to stderr, want the question", errOut)
	}
}

func TestPromptReadsOneLineAtATime(t *testing.T) {
	s, in, _, _ := Test()
	in.WriteString("d\ny\n")
	for _, want := range []string{"d", "y"} {
		if got, err := s.Prompt(t.Context(), "? "); err != nil || got != want {
			t.Errorf("Prompt() = %q, %v; want %q", got, err, want)
		}
	}
}

func TestPromptCanceled(t *testing.T) {
	r, w := io.Pipe() // input that never arrives
	defer w.Close()
	s := &Streams{In: r, Out: io.Discard, ErrOut: io.Discard}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.Prompt(ctx, "? "); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Prompt() = %v, want context.DeadlineExceeded", err)
	}
}

func TestPage(t *testing.T) {
	text := []byte("diff --git a/x b/x\n")
	tests := []struct {
		name    string
		noPager bool
		tty     bool
		pager   string
		want    string
	}{
		{name: "not a terminal", tty: false, pager: "sed s/^/paged:/", want: string(text)},
		{name: "no pager", tty: true, noPager: true, pager: "sed s/^/paged:/", want: string(text)},
		{name: "pager", tty: true, pager: "sed s/^/paged:/", want: "paged:" + string(text)},
		{name: "pager sees LESS", tty: true, pager: "echo \"$LESS\"", want: "FRX\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PAGER", tt.pager)
			t.Setenv("LESS", "")
			s, _, out, _ := Test()
			s.OutIsTerminal, s.NoPager = tt.tty, tt.noPager
			if err := s.Page(t.Context(), text); err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Errorf("Page() wrote %q, want %q", out, tt.want)
			}
		})
	}
}

func TestPageFails(t *testing.T) {
	t.Setenv("PAGER", "exit 3")
	s, _, _, _ := Test()
	s.OutIsTerminal = true
	err := s.Page(t.Context(), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "--no-pager") {
		t.Errorf("Page() = %v, want an error suggesting --no-pager", err)
	}
}

func TestCanPrompt(t *testing.T) {
	for _, tt := range []struct{ in, errOut, want bool }{
		{true, true, true}, {true, false, false}, {false, true, false},
	} {
		s := &Streams{InIsTerminal: tt.in, ErrIsTerminal: tt.errOut}
		if got := s.CanPrompt(); got != tt.want {
			t.Errorf("CanPrompt() with stdin %t, stderr %t = %t, want %t", tt.in, tt.errOut, got, tt.want)
		}
	}
}
