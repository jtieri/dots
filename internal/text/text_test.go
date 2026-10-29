package text

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPluralize(t *testing.T) {
	for n, want := range map[int]string{0: "0 commits", 1: "1 commit", 2: "2 commits"} {
		if got := Pluralize(n, "commit"); got != want {
			t.Errorf("Pluralize(%d, %q) = %q, want %q", n, "commit", got, want)
		}
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		s     string
		width int
		want  []string
	}{
		{"", 10, nil},
		{"one", 10, []string{"one"}},
		{"one two three", 7, []string{"one two", "three"}},
		{"one two", 7, []string{"one two"}},
		{"  spaced   out  ", 20, []string{"spaced out"}},
		{"a longerthanwidth b", 5, []string{"a", "longerthanwidth", "b"}},
		{"ünï cödé", 8, []string{"ünï cödé"}},
	}
	for _, tt := range tests {
		if diff := cmp.Diff(tt.want, Wrap(tt.s, tt.width)); diff != "" {
			t.Errorf("Wrap(%q, %d) mismatch (-want +got):\n%s", tt.s, tt.width, diff)
		}
	}
}
