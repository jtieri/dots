package untrack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dots/internal/chezmoi"
	"github.com/jtieri/dots/internal/dotfiles"
	"github.com/jtieri/dots/internal/testenv"
)

var now = time.Date(2026, 10, 29, 14, 0, 0, 0, time.Local)

type fixture struct {
	*testenv.Dotfiles
	t *testing.T
	d *dotfiles.Dotfiles
}

func newFixture(t *testing.T, files map[string]string) *fixture {
	t.Helper()
	td := testenv.NewDotfiles(t, files)
	d, err := dotfiles.Open(t.Context(), td.Home, &chezmoi.Client{})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{Dotfiles: td, t: t, d: d}
}

func (f *fixture) home(rel string) string { return filepath.Join(f.Home, rel) }

func (f *fixture) untrack(rel string, mode Mode, opts Options) (*Plan, string) {
	f.t.Helper()
	p, err := Preview(f.t.Context(), f.d, f.home(rel), mode)
	if err != nil {
		f.t.Fatal(err)
	}
	saved, err := p.Untrack(f.t.Context(), f.d, opts)
	if err != nil {
		f.t.Fatal(err)
	}
	return p, saved
}

func (f *fixture) exists(rel string) bool {
	_, err := os.Lstat(f.home(rel))
	return err == nil
}

func (f *fixture) status() string {
	f.t.Helper()
	return f.Chezmoi(f.t, "status")
}

func TestRemoveFile(t *testing.T) {
	f := newFixture(t, map[string]string{"dot_a": "a\n", "dot_b": "b\n"})
	p, saved := f.untrack(".a", Remove, Options{BackupDir: filepath.Join(t.TempDir(), "saved"), Now: now})
	if diff := cmp.Diff([]string{".a"}, p.Files); diff != "" {
		t.Errorf("Files mismatch (-want +got):\n%s", diff)
	}
	if got := testenv.ReadFile(t, filepath.Join(saved, ".a")); got != "a\n" {
		t.Errorf("saved copy = %q, want this machine's file", got)
	}
	if f.exists(".a") {
		t.Error("~/.a is still here")
	}
	if _, err := os.Stat(filepath.Join(f.Source, "dot_a")); err == nil {
		t.Error("the repo still has dot_a")
	}
	want := "# Removed from the repo by dots rm; every machine deletes these.\n.a\n"
	if got := testenv.ReadFile(t, filepath.Join(f.Source, ".chezmoiremove")); got != want {
		t.Errorf(".chezmoiremove = %q, want %q", got, want)
	}
	if got := f.status(); got != "" {
		t.Errorf("chezmoi status = %q, want nothing", got)
	}
	// A machine that still has the file deletes it on apply. Put back
	// here, it counts as added since chezmoi deleted it, hence the A.
	testenv.WriteFile(t, f.home(".a"), "a\n")
	if got := f.status(); got != "AD .a\n" {
		t.Errorf("chezmoi status with the file back = %q, want .a deleted", got)
	}
}

func TestRemoveFolder(t *testing.T) {
	f := newFixture(t, map[string]string{"dot_d/x": "x\n", "dot_d/sub/y": "y\n", "dot_e/z": "z\n", "dot_g/sub/deep/w": "w\n"})
	testenv.WriteFile(t, f.home(".d/mine"), "mine\n")
	testenv.WriteFile(t, f.home(".e/sub/mine"), "mine\n")

	p, _ := f.untrack(".d", Remove, Options{})
	if diff := cmp.Diff([]string{".d/sub/y", ".d/x"}, p.Files); diff != "" {
		t.Errorf("Files mismatch (-want +got):\n%s", diff)
	}
	if f.exists(".d/x") || f.exists(".d/sub") || !f.exists(".d/mine") {
		t.Error("want the tracked files and the emptied folder gone, and the untracked file kept")
	}
	f.untrack(".g", Remove, Options{})
	if f.exists(".g") {
		t.Error("~/.g is still here, though nothing was left in it")
	}
	f.untrack(".e", Remove, Options{})
	if f.exists(".e/z") || !f.exists(".e/sub/mine") {
		t.Error("want ~/.e/z gone and ~/.e/sub/mine kept")
	}
	if got := testenv.ReadFile(t, filepath.Join(f.Source, ".chezmoiremove")); !strings.HasSuffix(got, "\n.d/sub/y\n.d/x\n.g/sub/deep/w\n.e/z\n") {
		t.Errorf(".chezmoiremove = %q, want each removed file listed once", got)
	}

	// A machine with the folder keeps it and its own files in it.
	testenv.WriteFile(t, f.home(".d/x"), "x\n")
	testenv.WriteFile(t, f.home(".d/theirs"), "theirs\n")
	if got := f.status(); got != "AD .d/x\n" {
		t.Errorf("chezmoi status with the file back = %q, want only .d/x deleted", got)
	}
	f.Chezmoi(t, "apply", "--force")
	if f.exists(".d/x") || !f.exists(".d/theirs") || !f.exists(".d/mine") {
		t.Error("apply elsewhere: want .d/x gone and the other files kept")
	}
}

func TestRemoveDeletedHere(t *testing.T) {
	f := newFixture(t, map[string]string{"dot_a": "a\n"})
	if err := os.Remove(f.home(".a")); err != nil {
		t.Fatal(err)
	}
	if _, saved := f.untrack(".a", Remove, Options{BackupDir: t.TempDir(), Now: now}); saved != "" {
		t.Errorf("saved = %q, want nothing to save", saved)
	}
	if got := f.status(); got != "" {
		t.Errorf("chezmoi status = %q, want nothing", got)
	}
}

func TestForget(t *testing.T) {
	f := newFixture(t, map[string]string{"dot_a": "a\n", "dot_d/x": "x\n", "dot_d/y": "y\n"})
	testenv.WriteFile(t, filepath.Join(f.Source, ".chezmoiignore"), "# Caches.\n**/*.cache")
	f.untrack(".a", Forget, Options{BackupDir: t.TempDir()})
	p, _ := f.untrack(".d", Forget, Options{})
	if diff := cmp.Diff([]string{".d/x", ".d/y"}, p.Files); diff != "" {
		t.Errorf("Files mismatch (-want +got):\n%s", diff)
	}
	if !f.exists(".a") || !f.exists(".d/x") {
		t.Error("want every file kept")
	}
	want := "# Caches.\n**/*.cache\n\n# No longer tracked, by dots forget; each machine keeps its own copy.\n.a\n.d\n"
	if got := testenv.ReadFile(t, filepath.Join(f.Source, ".chezmoiignore")); got != want {
		t.Errorf(".chezmoiignore = %q, want %q", got, want)
	}
	testenv.WriteFile(t, f.home(".d/new"), "new\n")
	if got := f.status(); got != "" {
		t.Errorf("chezmoi status = %q, want nothing", got)
	}
	if got := f.Chezmoi(t, "managed"); got != "" {
		t.Errorf("chezmoi managed = %q, want nothing", got)
	}
	if got := f.Chezmoi(t, "unmanaged", f.home(".d")); got != "" {
		t.Errorf("chezmoi unmanaged .d = %q, want the forgotten folder ignored", got)
	}
}

// TestEscaping checks every awkward name against chezmoi itself: each line
// written matches its own file, and not a decoy that the name would match
// as a pattern.
func TestEscaping(t *testing.T) {
	names := map[string]string{ // the name and a decoy it matches unescaped
		"st*r":       "stxr",
		"q?x":        "qax",
		"b[1]":       "b1",
		"cur{a,b}":   "cura",
		`back\slash`: "backslash",
		"#hash":      "",
		"!bang":      "bang",
		"d/#nested":  "",
		"d/!nested":  "d/nested",
		"sp ace/f":   "",
		"üñï/ćödé":   "",
		"x]y":        "",
		"tail\\":     "",
	}
	for _, mode := range []Mode{Remove, Forget} {
		files := make(map[string]string)
		for name, decoy := range names {
			files[name] = "x\n"
			if decoy != "" {
				files[decoy] = "decoy\n"
			}
		}
		f := newFixture(t, files)
		for name := range names {
			f.untrack(name, mode, Options{})
		}
		managed := f.Chezmoi(t, "managed", "--include=files")
		for name, decoy := range names {
			if strings.Contains("\n"+managed, "\n"+name+"\n") {
				t.Errorf("mode %d: chezmoi still manages %q", mode, name)
			}
			if decoy != "" && !strings.Contains("\n"+managed, "\n"+decoy+"\n") {
				t.Errorf("mode %d: chezmoi no longer manages the decoy %q for %q", mode, decoy, name)
			}
			if mode == Remove {
				testenv.WriteFile(t, f.home(name), "back\n")
			}
		}
		status := f.status()
		for name := range names {
			want := "AD " + name + "\n"
			if got := strings.Contains(status, want); got != (mode == Remove) {
				t.Errorf("mode %d: chezmoi status = %q; want deleting %q %t", mode, status, name, mode == Remove)
			}
		}
		if mode == Remove && strings.Count(status, "\n") != len(names) {
			t.Errorf("chezmoi status = %q, want only the removed files deleted", status)
		}
	}
}

func TestPreviewRefuses(t *testing.T) {
	f := newFixture(t, map[string]string{"dot_a": "a\n", "dot_{{x}}": "x\n"})
	tests := []struct {
		target  string
		wantErr string
	}{
		{f.home(".nope"), "the repo does not track ~/.nope"},
		{"/etc/hostname", "/etc/hostname is not in the home directory"},
		{f.Home, "the home directory itself"},
		{f.Repo, "is the dotfiles repo or holds it"},
		{f.home(".{{x}}"), ".{{x}} has {{ in its name, which cannot be written in .chezmoiremove"},
	}
	for _, tt := range tests {
		if _, err := Preview(t.Context(), f.d, tt.target, Remove); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("Preview(%q) error = %v, want one containing %q", tt.target, err, tt.wantErr)
		}
	}
}

func TestAppendLines(t *testing.T) {
	name := filepath.Join(t.TempDir(), "list")
	for _, lines := range [][]string{{"a", "b"}, {"b", "c"}} {
		if err := appendLines(name, "# heading", lines); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := testenv.ReadFile(t, name), "# heading\na\nb\nc\n"; got != want {
		t.Errorf("list = %q, want %q", got, want)
	}
}
