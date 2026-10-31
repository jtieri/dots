package backup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 25, 14, 2, 5, 0, time.Local)

func write(t *testing.T, name, data string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, perm); err != nil { // the umask may have cut it
		t.Fatal(err)
	}
}

func TestSave(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".bashrc"), "mine\n", 0o644)
	write(t, filepath.Join(home, ".config/weechat/irc.conf"), "secret\n", 0o600)
	write(t, filepath.Join(home, ".local/bin/tool"), "#!/bin/sh\n", 0o755)
	if err := os.Symlink("../elsewhere", filepath.Join(home, ".config/link")); err != nil {
		t.Fatal(err)
	}

	paths := []string{".bashrc", ".config/weechat/irc.conf", ".local/bin/tool", ".config/link", ".config", ".deleted"}
	folder, err := Save(dir, home, paths, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "2026-10-25T14-02-05"); folder != want {
		t.Errorf("Save() folder = %q, want %q", folder, want)
	}

	for _, tt := range []struct {
		path string
		data string
		perm os.FileMode
	}{
		{".bashrc", "mine\n", 0o644},
		{".config/weechat/irc.conf", "secret\n", 0o600},
		{".local/bin/tool", "#!/bin/sh\n", 0o755},
	} {
		name := filepath.Join(folder, tt.path)
		data, err := os.ReadFile(name)
		if err != nil || string(data) != tt.data {
			t.Errorf("copy of %s = %q, %v; want %q", tt.path, data, err, tt.data)
			continue
		}
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != tt.perm {
			t.Errorf("copy of %s has mode %v, want %v", tt.path, info.Mode().Perm(), tt.perm)
		}
	}
	if target, err := os.Readlink(filepath.Join(folder, ".config/link")); err != nil || target != "../elsewhere" {
		t.Errorf("copy of .config/link = %q, %v; want a link to ../elsewhere", target, err)
	}
	if _, err := os.Lstat(filepath.Join(folder, ".deleted")); !os.IsNotExist(err) {
		t.Errorf("a missing file was copied: %v", err)
	}
	info, err := os.Stat(folder)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("backup folder mode = %v, want 0700, since it may hold private files", info.Mode().Perm())
	}
}

func TestSaveNothingToCopy(t *testing.T) {
	home, dir := t.TempDir(), filepath.Join(t.TempDir(), "overwritten")
	folder, err := Save(dir, home, []string{".deleted"}, now)
	if err != nil || folder != "" {
		t.Errorf("Save() = %q, %v; want no folder", folder, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Save() created %s with nothing to copy", dir)
	}
}

func TestSaveSameSecond(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".bashrc"), "first\n", 0o644)
	first, err := Save(dir, home, []string{".bashrc"}, now)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, ".bashrc"), "second\n", 0o644)
	second, err := Save(dir, home, []string{".bashrc"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if second != first+"-2" {
		t.Errorf("second folder = %q, want %q", second, first+"-2")
	}
	for folder, want := range map[string]string{first: "first\n", second: "second\n"} {
		if data, _ := os.ReadFile(filepath.Join(folder, ".bashrc")); string(data) != want {
			t.Errorf("%s holds %q, want %q", folder, data, want)
		}
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"2026-09-24T14-02-04",   // more than 30 days old: deleted
		"2026-09-24T14-02-04-2", // a numbered folder from the same second
		"2026-09-25T14-02-05",   // exactly 30 days: kept
		"2026-10-25T14-00-00",   // today
		"notes",                 // not a backup folder
		"2020-01-01T00-00-00x",  // not a backup folder either
	}
	for _, name := range names {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "2026-09-24T14-02-04", ".bashrc"), "old\n", 0o644)

	if err := Prune(dir, 0, now); err != nil {
		t.Fatal(err)
	}
	if got := list(t, dir); len(got) != len(names) {
		t.Errorf("Prune() with keepDays 0 left %v, want everything", got)
	}

	if err := Prune(dir, 30, now); err != nil {
		t.Fatal(err)
	}
	want := []string{"2020-01-01T00-00-00x", "2026-09-25T14-02-05", "2026-10-25T14-00-00", "notes"}
	if got := list(t, dir); !slices.Equal(got, want) {
		t.Errorf("Prune() left %v, want %v", got, want)
	}

	if err := Prune(filepath.Join(dir, "missing"), 30, now); err != nil {
		t.Errorf("Prune() of a missing folder = %v, want nil", err)
	}
}

func list(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got := DefaultDir("/home/me"); got != "/state/dots/overwritten" {
		t.Errorf("DefaultDir() = %q, want /state/dots/overwritten", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	if got := DefaultDir("/home/me"); got != "/home/me/.local/state/dots/overwritten" {
		t.Errorf("DefaultDir() without XDG_STATE_HOME = %q", got)
	}
}
