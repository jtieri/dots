package push

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/testenv"
)

type fixture struct {
	*testenv.Dotfiles
	t    *testing.T
	d    *dotfiles.Dotfiles
	host string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	td := testenv.NewDotfiles(t, map[string]string{
		"dot_a":         "a\n",
		"dot_b":         "b\n",
		"dot_x":         "x\n",
		"private_dot_p": "p\n",
		"dot_gone":      "gone\n",
	})
	d, err := dotfiles.Open(t.Context(), td.Home, &chezmoi.Client{})
	if err != nil {
		t.Fatal(err)
	}
	host, err := d.Chezmoi.Hostname(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{Dotfiles: td, t: t, d: d, host: host}
}

func (f *fixture) source(rel string) string { return filepath.Join(f.Source, rel) }

func (f *fixture) prepare(opts Options) *Plan {
	f.t.Helper()
	p, err := Prepare(f.t.Context(), f.d, opts)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) rename(from, to string) {
	f.t.Helper()
	if err := os.Rename(f.source(from), f.source(to)); err != nil {
		f.t.Fatal(err)
	}
}

// fakePacman returns a pacman that lists packages, or with none set to
// "none" lists nothing as pacman does, or with it set to "error" fails.
func fakePacman(t *testing.T, mode string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "pacman")
	testenv.WriteFile(t, name, `#!/bin/sh
case "$1:`+mode+`" in
*:error) echo "error: could not open the database" >&2; exit 1 ;;
-Qqen:*) printf 'base\nlinux\n' ;;
-Qqem:none) echo "warning: database file for 'core' does not exist" >&2; exit 1 ;;
-Qqem:*) printf 'yay\n' ;;
esac
`)
	if err := os.Chmod(name, 0o755); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestPrepareItems(t *testing.T) {
	f := newFixture(t)
	testenv.WriteFile(t, filepath.Join(f.Repo, "scripts/old.sh"), "old\n")
	f.Git(t, "add", "scripts")
	f.Git(t, "commit", "--quiet", "--message=old script")
	if err := os.Remove(filepath.Join(f.Repo, "scripts/old.sh")); err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, f.source("dot_a"), "edited\n")
	testenv.WriteFile(t, f.source("dot_new"), "new\n")
	if err := os.Remove(f.source("dot_gone")); err != nil {
		t.Fatal(err)
	}
	f.rename("dot_x", "private_dot_x")         // unstaged: a deleted and an untracked file
	f.Git(t, "mv", "home/dot_b", "home/dot_c") // staged: a rename
	f.Git(t, "mv", "home/private_dot_p", "home/dot_p")
	testenv.WriteFile(t, filepath.Join(f.Repo, "scripts/setup.sh"), "setup\n")

	p := f.prepare(Options{})
	want := []Item{
		{Kind: Changed, HomePath: ".a", RepoPath: "home/dot_a"},
		{Kind: Moved, HomePath: ".c", RepoPath: "home/dot_c", From: "home/dot_b"},
		{Kind: Deleted, HomePath: ".gone", RepoPath: "home/dot_gone"},
		{Kind: Moved, HomePath: ".p", RepoPath: "home/dot_p", From: "home/private_dot_p", Note: "no longer private"},
		{Kind: Moved, HomePath: ".x", RepoPath: "home/private_dot_x", From: "home/dot_x", Note: "now private"},
		{Kind: Deleted, RepoPath: "scripts/old.sh"},
		{Kind: Added, HomePath: ".new", RepoPath: "home/dot_new"},
		{Kind: Added, RepoPath: "scripts/setup.sh"},
	}
	if diff := cmp.Diff(want, p.Items); diff != "" {
		t.Errorf("Items mismatch (-want +got):\n%s", diff)
	}
	if p.Host != f.host {
		t.Errorf("Host = %q, want %q", p.Host, f.host)
	}

	diff, err := p.Diff(t.Context(), f.d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+edited\n", "rename from home/dot_x\nrename to home/private_dot_x\n", "+setup\n"} {
		if !strings.Contains(string(diff), want) {
			t.Errorf("Diff() = %q, want it to contain %q", diff, want)
		}
	}

	if err := p.Commit(t.Context(), f.d, "update from here"); err != nil {
		t.Fatal(err)
	}
	if got := f.Git(t, "status", "--porcelain"); got != "" {
		t.Errorf("status after Commit() = %q, want everything committed", got)
	}
}

func TestPreparePermissionsChanged(t *testing.T) {
	f := newFixture(t)
	f.rename("dot_a", "executable_dot_a")
	f.rename("private_dot_p", "private_executable_dot_p")
	p := f.prepare(Options{})
	want := []Item{
		{Kind: Moved, HomePath: ".a", RepoPath: "home/executable_dot_a", From: "home/dot_a", Note: "permissions changed"},
		{Kind: Moved, HomePath: ".p", RepoPath: "home/private_executable_dot_p", From: "home/private_dot_p", Note: "permissions changed"},
	}
	if diff := cmp.Diff(want, p.Items); diff != "" {
		t.Errorf("Items mismatch (-want +got):\n%s", diff)
	}
}

func TestPrepareNothing(t *testing.T) {
	f := newFixture(t)
	if p := f.prepare(Options{}); p.Items != nil || p.Uncaptured != nil {
		t.Errorf("Prepare() = %+v, want nothing", p)
	}
}

func TestPrepareUncaptured(t *testing.T) {
	f := newFixture(t)
	testenv.WriteFile(t, filepath.Join(f.Home, ".a"), "edited here\n")
	if err := os.Remove(filepath.Join(f.Home, ".b")); err != nil {
		t.Fatal(err)
	}
	p := f.prepare(Options{})
	if diff := cmp.Diff([]string{".a", ".b"}, p.Uncaptured); diff != "" || p.Items != nil {
		t.Errorf("Uncaptured mismatch (-want +got):\n%s; Items = %v", diff, p.Items)
	}
}

func TestPrepareProtected(t *testing.T) {
	f := newFixture(t)
	testenv.WriteFile(t, f.source("dot_ssh/private_id_ed25519"), "key\n")
	opts := Options{Protected: []string{".ssh/id_*"}, PackageLists: "lists", Pacman: fakePacman(t, "")}
	_, err := Prepare(t.Context(), f.d, opts)
	want := `the repo has an uncommitted copy of ~/.ssh/id_ed25519, which capture.protected ("~/.ssh/id_*") keeps out of the repo, ` +
		"so nothing is committed until home/dot_ssh/private_id_ed25519 is deleted from the repo"
	if err == nil || err.Error() != want {
		t.Errorf("Prepare() error = %v, want %q", err, want)
	}
	if got := f.Git(t, "status", "--porcelain", "--untracked-files=all"); got != "?? home/dot_ssh/private_id_ed25519\n" {
		t.Errorf("status = %q, want the package lists put back", got)
	}
	testenv.WriteFile(t, f.source("dot_gnupg/private-keys-v1.d/x.key"), "key\n")
	if _, err := Prepare(t.Context(), f.d, Options{Protected: []string{".gnupg"}}); err == nil ||
		!strings.Contains(err.Error(), "~/.gnupg/private-keys-v1.d/x.key") {
		t.Errorf("Prepare() error = %v, want a file in a protected folder refused", err)
	}
	if err := os.RemoveAll(f.source("dot_gnupg")); err != nil {
		t.Fatal(err)
	}

	// Deleting a protected file from the repo is always allowed.
	f.Git(t, "add", "home/dot_ssh")
	f.Git(t, "commit", "--quiet", "--message=oops")
	if err := os.Remove(f.source("dot_ssh/private_id_ed25519")); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(t.Context(), f.d, Options{Protected: []string{".ssh"}}); err != nil {
		t.Errorf("Prepare() deleting a protected file: %v", err)
	}
}

func TestPreparePackageLists(t *testing.T) {
	f := newFixture(t)
	opts := Options{PackageLists: "scripts/pkglists", Pacman: fakePacman(t, "")}
	dir := filepath.Join(f.Repo, "scripts/pkglists", f.host)

	p := f.prepare(opts)
	if testenv.ReadFile(t, filepath.Join(dir, "pkglist.txt")) != "base\nlinux\n" ||
		testenv.ReadFile(t, filepath.Join(dir, "aurpkglist.txt")) != "yay\n" {
		t.Error("Prepare() did not write the package lists")
	}
	want := []Item{
		{Kind: Added, RepoPath: "scripts/pkglists/" + f.host + "/aurpkglist.txt"},
		{Kind: Added, RepoPath: "scripts/pkglists/" + f.host + "/pkglist.txt"},
	}
	if diff := cmp.Diff(want, p.Items); diff != "" {
		t.Errorf("Items mismatch (-want +got):\n%s", diff)
	}
	if err := p.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pkglist.txt")); err == nil {
		t.Error("Discard() left a new package list behind")
	}

	// Committed lists that have not changed are left alone; a changed one
	// is put back as it was by Discard.
	p = f.prepare(opts)
	if err := p.Commit(t.Context(), f.d, "lists"); err != nil {
		t.Fatal(err)
	}
	if err := p.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pkglist.txt")); err != nil {
		t.Errorf("Discard() after Commit() removed a committed list: %v", err)
	}
	if p := f.prepare(opts); p.Items != nil {
		t.Errorf("Items with the same packages = %v, want none", p.Items)
	}
	p = f.prepare(Options{PackageLists: "scripts/pkglists", Pacman: fakePacman(t, "none")})
	if got := testenv.ReadFile(t, filepath.Join(dir, "aurpkglist.txt")); got != "" {
		t.Errorf("aurpkglist.txt with no foreign packages = %q, want it empty", got)
	}
	if err := p.Discard(); err != nil {
		t.Fatal(err)
	}
	if got := testenv.ReadFile(t, filepath.Join(dir, "aurpkglist.txt")); got != "yay\n" {
		t.Errorf("aurpkglist.txt after Discard() = %q, want it put back", got)
	}
	if got := f.Git(t, "status", "--porcelain"); got != "" {
		t.Errorf("status after Discard() = %q, want nothing changed", got)
	}
}

func TestPreparePackageListsError(t *testing.T) {
	f := newFixture(t)
	_, err := Prepare(t.Context(), f.d, Options{PackageLists: "scripts/pkglists", Pacman: fakePacman(t, "error")})
	if err == nil || !strings.Contains(err.Error(), "listing packages") {
		t.Errorf("Prepare() error = %v, want pacman's failure", err)
	}
	if got := f.Git(t, "status", "--porcelain"); got != "" {
		t.Errorf("status = %q, want nothing written", got)
	}
}

func TestPreparePackageListsSkipped(t *testing.T) {
	f := newFixture(t)
	if p := f.prepare(Options{Pacman: fakePacman(t, "")}); p.Items != nil {
		t.Errorf("Items with the lists turned off = %v, want none", p.Items)
	}
	// Without pacman in PATH, as on a machine that is not Arch.
	bin := t.TempDir()
	for _, name := range []string{"git", "chezmoi"} {
		found, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(found, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	if p := f.prepare(Options{PackageLists: "scripts/pkglists"}); p.Items != nil {
		t.Errorf("Items without pacman = %v, want none", p.Items)
	}
}

func TestDefaultMessage(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 5, 0, 0, time.Local)
	if got, want := DefaultMessage("desktop", now), "update from desktop, 2026-10-29 09:05"; got != want {
		t.Errorf("DefaultMessage() = %q, want %q", got, want)
	}
}
