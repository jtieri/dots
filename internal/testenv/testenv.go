// Package testenv sets up the environment for tests that run chezmoi and git.
package testenv

import (
	"os"
	"path/filepath"
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
