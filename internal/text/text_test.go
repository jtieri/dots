package text

import "testing"

func TestPluralize(t *testing.T) {
	for n, want := range map[int]string{0: "0 commits", 1: "1 commit", 2: "2 commits"} {
		if got := Pluralize(n, "commit"); got != want {
			t.Errorf("Pluralize(%d, %q) = %q, want %q", n, "commit", got, want)
		}
	}
}
