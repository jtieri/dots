package dotfiles

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/git"
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

func TestHomePath(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		repoPath string
		want     string
		wantOK   bool
	}{
		{"in the source directory", "/repo/home", "home/dot_config/i3/config", ".config/i3/config", true},
		{"outside the source directory", "/repo/home", "scripts/setup.sh", "", false},
		{"a folder that starts the same", "/repo/home", "homework/dot_bashrc", "", false},
		{"a script", "/repo/home", "home/.chezmoiscripts/run_once_x.sh", "", false},
		{"source directory is the repo", "/repo", "dot_bashrc", ".bashrc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Dotfiles{Home: "/home/me", Source: tt.source, Repo: &git.Repo{Dir: "/repo"}}
			got, ok := d.HomePath(tt.repoPath)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("HomePath(%q) = %q, %t; want %q, %t", tt.repoPath, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestTilde(t *testing.T) {
	for rel, want := range map[string]string{".": "~", ".bashrc": "~/.bashrc", ".config/i3/config": "~/.config/i3/config"} {
		if got := Tilde(rel); got != want {
			t.Errorf("Tilde(%q) = %q, want %q", rel, got, want)
		}
	}
}
