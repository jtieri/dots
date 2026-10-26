// Package testenv sets up the environment for tests that run chezmoi and git.
package testenv

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate points HOME and the XDG directories at a new temporary home, keeps
// git from reading the user's configuration, and cuts the test off from the
// desktop session, so nothing a test runs can touch the real home or reach
// running programs. It returns the temporary home.
func Isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	for _, name := range []string{
		"DISPLAY", "WAYLAND_DISPLAY", "DBUS_SESSION_BUS_ADDRESS", "I3SOCK",
		"SWAYSOCK", "SSH_AUTH_SOCK", "XDG_RUNTIME_DIR", "CHEZMOI_SOURCE_DIR",
	} {
		unsetenv(t, name)
	}
	// dot's own settings.
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
// contents. chezmoi's scripts are never run.
func NewDotfiles(t *testing.T, files map[string]string) *Dotfiles {
	t.Helper()
	home := Isolate(t)
	d := &Dotfiles{
		Home:   home,
		Repo:   filepath.Join(home, "dotfiles"),
		Remote: filepath.Join(t.TempDir(), "dotfiles.git"),
	}
	d.Source = filepath.Join(d.Repo, "home")

	WriteFile(t, filepath.Join(d.Repo, ".chezmoiroot"), "home\n")
	for name, data := range files {
		WriteFile(t, filepath.Join(d.Source, name), data)
	}
	Run(t, d.Repo, "git", "init", "--quiet", "--initial-branch=main")
	Run(t, d.Repo, "git", "add", ".")
	Run(t, d.Repo, "git", "commit", "--quiet", "--message=initial")
	Run(t, d.Repo, "git", "init", "--quiet", "--bare", "--initial-branch=main", d.Remote)
	Run(t, d.Repo, "git", "remote", "add", "origin", d.Remote)
	Run(t, d.Repo, "git", "push", "--quiet", "--set-upstream", "origin", "main")

	WriteFile(t, filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"),
		"sourceDir = \""+d.Repo+"\"\numask = 0o022\n")
	d.Chezmoi(t, "apply", "--exclude=scripts")
	return d
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

// Run runs a program in dir and returns its standard output. It fails the
// test if the program fails.
func Run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, stderr.String())
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
