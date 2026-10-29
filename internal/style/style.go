// Package style marks up text for a terminal with ANSI escape codes.
package style

import (
	"fmt"
	"strings"
)

// A Style marks up text, or leaves it plain. The zero Style is plain.
type Style struct {
	on bool
}

// New returns a Style that marks up text when on is set.
func New(on bool) Style { return Style{on: on} }

func (s Style) wrap(code, text string) string {
	if !s.on || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// Heading marks a heading, in bold.
func (s Style) Heading(text string) string { return s.wrap("1", text) }

// Good marks good news, in green.
func (s Style) Good(text string) string { return s.wrap("32", text) }

// Bad marks an error, in bold red.
func (s Style) Bad(text string) string { return s.wrap("1;31", text) }

// Mode is the --color setting: when to mark up text.
type Mode string

// Modes of marking up text.
const (
	Auto   Mode = "auto"
	Always Mode = "always"
	Never  Mode = "never"
)

// ParseMode reads a --color value.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case Auto, Always, Never:
		return m, nil
	}
	return "", fmt.Errorf("--color %q must be auto, always or never", s)
}

// Enabled reports whether text written to a stream should be marked up.
// Auto marks it up only for a terminal, and not when NO_COLOR is set to
// anything or TERM is dumb; getenv reads the environment.
func (m Mode) Enabled(terminal bool, getenv func(string) string) bool {
	switch m {
	case Always:
		return true
	case Never:
		return false
	}
	return terminal && getenv("NO_COLOR") == "" && !strings.EqualFold(getenv("TERM"), "dumb")
}
