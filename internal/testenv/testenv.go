// Package testenv sets up the environment for tests that run chezmoi and git.
package testenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtieri/dot/internal/run"
)

// Vars returns the environment variables, as "name=value" pairs, that point
// chezmoi and git at home and keep git from reading the user's configuration.
func Vars(home string) []string {
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}
}

// sessionVars are the variables that would let a program reach the desktop
// session or the user's own setup.
var sessionVars = []string{
	"DISPLAY", "WAYLAND_DISPLAY", "DBUS_SESSION_BUS_ADDRESS", "I3SOCK",
	"SWAYSOCK", "SSH_AUTH_SOCK", "XDG_RUNTIME_DIR", "CHEZMOI_SOURCE_DIR",
}

// Isolate sets the variables from Vars for a new temporary home and unsets
// those that would reach the desktop session or carry dot's own settings, so
// nothing a test runs can touch the real home or running programs. It
// returns the temporary home.
func Isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, kv := range Vars(home) {
		name, value, _ := strings.Cut(kv, "=")
		t.Setenv(name, value)
	}
	for _, name := range sessionVars {
		unsetenv(t, name)
	}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "DOT_") {
			unsetenv(t, name)
		}
	}
	return home
}

// unsetenv unsets an environment variable for the rest of the test.
func unsetenv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "") // restores the old value when the test ends
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// A Dotfiles is a home directory set up the way dot expects: a dotfiles repo
// with a remote, and chezmoi applied from it.
type Dotfiles struct {
	Home string
	// Repo is the dotfiles repo, ~/dotfiles.
	Repo string
	// Source is chezmoi's source directory, the repo's home folder.
	Source string
	// Remote is the bare repo that is Repo's origin.
	Remote string
}

// NewDotfiles isolates the test as Isolate does, then creates a Dotfiles
// whose repo holds files, a map from paths in the source directory to their
// contents.
func NewDotfiles(t *testing.T, files map[string]string) *Dotfiles {
	t.Helper()
	home := Isolate(t)
	for name, data := range files {
		WriteFile(t, filepath.Join(home, "dotfiles", "home", name), data)
	}
	d, err := InitDotfiles(t.Context(), home, filepath.Join(t.TempDir(), "dotfiles.git"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// InitDotfiles turns home/dotfiles, with its source files already in its home
// folder, into a repo whose origin is a new bare repo at remote. It then
// configures chezmoi and applies the repo without running scripts. env is
// the environment for git and chezmoi; nil means the current one.
func InitDotfiles(ctx context.Context, home, remote string, env []string) (*Dotfiles, error) {
	d := &Dotfiles{
		Home:   home,
		Repo:   filepath.Join(home, "dotfiles"),
		Source: filepath.Join(home, "dotfiles", "home"),
		Remote: remote,
	}
	if err := os.WriteFile(filepath.Join(d.Repo, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		return nil, err
	}
	config := filepath.Join(home, ".config", "chezmoi", "chezmoi.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(config, []byte("sourceDir = \""+d.Repo+"\"\numask = 0o022\n"), 0o644); err != nil {
		return nil, err
	}
	steps := [][]string{
		{"git", "init", "--quiet", "--initial-branch=main", d.Repo},
		{"git", "-C", d.Repo, "add", "."},
		{"git", "-C", d.Repo, "commit", "--quiet", "--message=initial"},
		{"git", "init", "--quiet", "--bare", "--initial-branch=main", remote},
		{"git", "-C", d.Repo, "remote", "add", "origin", remote},
		{"git", "-C", d.Repo, "push", "--quiet", "--set-upstream", "origin", "main"},
		{"chezmoi", "--no-tty", "apply", "--exclude=scripts"},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = home
		cmd.Env = env
		if _, err := run.Output(cmd); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Chezmoi runs chezmoi with args and fails the test if it fails. It never
// runs scripts unless args ask for them.
func (d *Dotfiles) Chezmoi(t *testing.T, args ...string) string {
	t.Helper()
	return Run(t, d.Home, "chezmoi", append([]string{"--no-tty", "--no-pager", "--color=false"}, args...)...)
}

// Git runs git in the dotfiles repo and fails the test if it fails.
func (d *Dotfiles) Git(t *testing.T, args ...string) string {
	t.Helper()
	return Run(t, d.Repo, "git", args...)
}

// PushFromElsewhere commits changes to the repo in another clone of its
// remote and pushes them, as another machine would. changes maps paths in the
// repo to their new contents; an empty string deletes the file.
func (d *Dotfiles) PushFromElsewhere(t *testing.T, changes map[string]string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	Run(t, d.Home, "git", "clone", "--quiet", d.Remote, other)
	for name, data := range changes {
		if data == "" {
			Run(t, other, "git", "rm", "--quiet", name)
			continue
		}
		WriteFile(t, filepath.Join(other, name), data)
	}
	Run(t, other, "git", "add", "--all")
	Run(t, other, "git", "commit", "--quiet", "--message=elsewhere")
	Run(t, other, "git", "push", "--quiet")
}

// Run runs a program in dir and returns its standard output. It fails the
// test if the program fails.
func Run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	out, err := run.Output(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// WriteFile writes data to name, creating its parent directories.
func WriteFile(t *testing.T, name, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Running reports whether the process pid exists and has not exited.
func Running(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// The state follows the command name, which is in parentheses.
	_, after, _ := strings.Cut(string(data), ") ")
	return after != "" && after[0] != 'Z'
}

// StartProgram starts a program named name that runs until the test ends, by
// running a copy of sleep under that name. It is for tests that look for
// running programs by name.
func StartProgram(t *testing.T, name string) {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(exe, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
}
