package cli

import (
	"strings"
	"testing"

	"github.com/jtieri/dots/internal/iostreams"
)

func TestRoot(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantOut    string
		wantErrOut string
	}{
		{
			name:     "no arguments prints help",
			wantCode: exitOK,
			wantOut:  "Usage:",
		},
		{
			name:     "version flag",
			args:     []string{"--version"},
			wantCode: exitOK,
			wantOut:  "dots ",
		},
		{
			name:       "unknown flag",
			args:       []string{"--bogus"},
			wantCode:   exitError,
			wantErrOut: "dots: unknown flag: --bogus",
		},
		{
			name:       "unknown subcommand",
			args:       []string{"bogus"},
			wantCode:   exitError,
			wantErrOut: `dots: unknown command "bogus"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streams, _, out, errOut := iostreams.Test()
			if got := Main(tt.args, streams); got != tt.wantCode {
				t.Errorf("Main(%q) = %d, want %d", tt.args, got, tt.wantCode)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("Main(%q) stdout = %q, want it to contain %q", tt.args, out, tt.wantOut)
			}
			if !strings.Contains(errOut.String(), tt.wantErrOut) {
				t.Errorf("Main(%q) stderr = %q, want it to contain %q", tt.args, errOut, tt.wantErrOut)
			}
		})
	}
}
