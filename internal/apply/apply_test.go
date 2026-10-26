package apply

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtieri/dot/internal/dotfiles"
)

var now = time.Date(2026, 10, 25, 14, 2, 5, 0, time.Local)

// previewEveryKind sets up dotfiles where applying would make every kind of
// change, and returns them with the plan.
func previewEveryKind(t *testing.T) (*dotfiles.Dotfiles, *Plan) {
	t.Helper()
	td, d := newDotfiles(t)
	makeEveryKind(t, td)
	plan, err := Preview(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	return d, plan
}

func options(choice Choice, backupDir string) Options {
	return Options{Choice: choice, BackupDir: backupDir, Now: now, Stdout: io.Discard, Stderr: io.Discard}
}

func wantFile(t *testing.T, name, contents string) {
	t.Helper()
	if data, err := os.ReadFile(name); err != nil || string(data) != contents {
		t.Errorf("%s = %q, %v; want %q", name, data, err, contents)
	}
}

func wantNoFile(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		t.Errorf("%s exists, want it gone", name)
	}
}

func wantLink(t *testing.T, name, target string) {
	t.Helper()
	if got, err := os.Readlink(name); err != nil || got != target {
		t.Errorf("%s links to %q, %v; want %q", name, got, err, target)
	}
}

// wantRestApplied checks the changes that touch nothing changed here.
func wantRestApplied(t *testing.T, home string) {
	t.Helper()
	wantFile(t, filepath.Join(home, ".update"), "update from the repo\n")
	wantFile(t, filepath.Join(home, ".new"), "new\n")
	wantNoFile(t, filepath.Join(home, ".old"))
	wantFile(t, filepath.Join(home, ".setup-ran"), "")
	wantFile(t, filepath.Join(home, ".zed"), "theme = mocha\nfont = serif\n")
}

func TestApplyKeep(t *testing.T) {
	d, plan := previewEveryKind(t)
	backups := filepath.Join(d.Home, "backups")
	folder, err := plan.Apply(t.Context(), d, options(Keep, backups))
	if err != nil {
		t.Fatal(err)
	}
	wantRestApplied(t, d.Home)
	wantFile(t, filepath.Join(d.Home, ".here"), "edited here\n")
	wantFile(t, filepath.Join(d.Home, ".both"), "edited here\n")
	wantNoFile(t, filepath.Join(d.Home, ".gone"))
	wantNoFile(t, filepath.Join(d.Home, ".gone_both"))
	wantLink(t, filepath.Join(d.Home, ".ln"), "elsewhere")
	if folder != "" {
		t.Errorf("Apply() made backups in %s, want none when keeping", folder)
	}
	wantNoFile(t, backups)
}

func TestApplyOverwrite(t *testing.T) {
	d, plan := previewEveryKind(t)
	backups := filepath.Join(d.Home, "backups")
	folder, err := plan.Apply(t.Context(), d, options(Overwrite, backups))
	if err != nil {
		t.Fatal(err)
	}
	wantRestApplied(t, d.Home)
	wantFile(t, filepath.Join(d.Home, ".here"), "here\n")
	wantFile(t, filepath.Join(d.Home, ".both"), "edited in the repo\n")
	wantFile(t, filepath.Join(d.Home, ".gone"), "gone\n")
	wantFile(t, filepath.Join(d.Home, ".gone_both"), "edited in the repo\n")
	wantLink(t, filepath.Join(d.Home, ".ln"), "target")

	if want := filepath.Join(backups, "2026-10-25T14-02-05"); folder != want {
		t.Fatalf("backup folder = %q, want %q", folder, want)
	}
	wantFile(t, filepath.Join(folder, ".here"), "edited here\n")
	wantFile(t, filepath.Join(folder, ".both"), "edited here\n")
	wantLink(t, filepath.Join(folder, ".ln"), "elsewhere")
	wantNoFile(t, filepath.Join(folder, ".gone"))
	wantNoFile(t, filepath.Join(folder, ".update"))
}

func TestApplyOverwriteWithoutBackup(t *testing.T) {
	d, plan := previewEveryKind(t)
	folder, err := plan.Apply(t.Context(), d, options(Overwrite, ""))
	if err != nil {
		t.Fatal(err)
	}
	if folder != "" {
		t.Errorf("backup folder = %q, want none", folder)
	}
	wantFile(t, filepath.Join(d.Home, ".here"), "here\n")
}

func TestApplyStopsWhenBackupFails(t *testing.T) {
	d, plan := previewEveryKind(t)
	// A file where the backup folder should go makes the backup fail.
	backups := filepath.Join(d.Home, "backups")
	if err := os.WriteFile(backups, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Apply(t.Context(), d, options(Overwrite, backups)); err == nil {
		t.Fatal("Apply() error = nil, want the backup's error")
	}
	wantFile(t, filepath.Join(d.Home, ".here"), "edited here\n")
	wantFile(t, filepath.Join(d.Home, ".update"), "update\n")
	wantNoFile(t, filepath.Join(d.Home, ".setup-ran"))
}

func TestApplyNothing(t *testing.T) {
	_, d := newDotfiles(t)
	plan := &Plan{}
	if folder, err := plan.Apply(t.Context(), d, options(Overwrite, filepath.Join(d.Home, "backups"))); err != nil || folder != "" {
		t.Errorf("Apply() of an empty plan = %q, %v; want nothing", folder, err)
	}
}

func TestDiff(t *testing.T) {
	d, plan := previewEveryKind(t)
	diff, err := plan.Diff(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-edited here\n+here\n", "+++ b/.new", "-update\n+update from the repo\n"} {
		if !strings.Contains(string(diff), want) {
			t.Errorf("Diff() lacks %q:\n%s", want, diff)
		}
	}
}
