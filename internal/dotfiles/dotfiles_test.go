package dotfiles

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/testenv"
)

func TestOpen(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_bashrc": "bashrc\n"})
	got, err := Open(t.Context(), d.Home, &chezmoi.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Home != d.Home || got.Source != d.Source || got.Repo.Dir != d.Repo {
		t.Errorf("Open() = home %q, source %q, repo %q; want %q, %q, %q",
			got.Home, got.Source, got.Repo.Dir, d.Home, d.Source, d.Repo)
	}
}

func TestOpenNotSetUp(t *testing.T) {
	home := testenv.Isolate(t)
	if _, err := Open(t.Context(), home, &chezmoi.Client{}); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("Open() error = %v, want ErrNotSetUp", err)
	}
}

func TestOpenSourceNotInRepo(t *testing.T) {
	home := testenv.Isolate(t)
	source := filepath.Join(home, "plain")
	testenv.WriteFile(t, filepath.Join(source, "dot_bashrc"), "x")
	testenv.WriteFile(t, filepath.Join(home, ".config/chezmoi/chezmoi.toml"), "sourceDir = \""+source+"\"\n")
	_, err := Open(t.Context(), home, &chezmoi.Client{})
	if err == nil || !strings.Contains(err.Error(), "is not in a git repository") {
		t.Errorf("Open() error = %v, want one saying it is not in a git repository", err)
	}
}

func TestTilde(t *testing.T) {
	for rel, want := range map[string]string{".": "~", ".bashrc": "~/.bashrc", ".config/i3/config": "~/.config/i3/config"} {
		if got := Tilde(rel); got != want {
			t.Errorf("Tilde(%q) = %q, want %q", rel, got, want)
		}
	}
}
