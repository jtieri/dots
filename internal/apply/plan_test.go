package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/testenv"
)

// themeTemplate keeps a file as it is on the machine except for its first
// line, which it sets to the repo's theme, much as Zed's settings are kept.
const themeTemplate = `{{- /* chezmoi:modify-template */ -}}
theme = mocha
{{ .chezmoi.stdin | splitList "\n" | rest | join "\n" }}`

var files = map[string]string{
	"dot_update":     "update\n",
	"dot_here":       "here\n",
	"dot_both":       "both\n",
	"dot_gone":       "gone\n",
	"dot_gone_both":  "gone both\n",
	"dot_same":       "same\n",
	"symlink_dot_ln": "target",
	"modify_dot_zed": themeTemplate,
}

// newDotfiles sets up dotfiles from files plus a themed file for the modify
// template to keep, and opens them.
func newDotfiles(t *testing.T) (*testenv.Dotfiles, *dotfiles.Dotfiles) {
	t.Helper()
	td := testenv.NewDotfiles(t, files)
	testenv.WriteFile(t, filepath.Join(td.Home, ".zed"), "theme = latte\nfont = mono\n")
	td.Chezmoi(t, "apply", "--exclude=scripts")
	d, err := dotfiles.Open(t.Context(), td.Home, &chezmoi.Client{})
	if err != nil {
		t.Fatal(err)
	}
	return td, d
}

// makeEveryKind changes the home and the repo so that applying would do one
// of each kind of change.
func makeEveryKind(t *testing.T, td *testenv.Dotfiles) {
	t.Helper()
	home := func(name string) string { return filepath.Join(td.Home, name) }
	src := func(name string) string { return filepath.Join(td.Source, name) }

	testenv.WriteFile(t, src("dot_update"), "update from the repo\n")
	testenv.WriteFile(t, src("dot_new"), "new\n")
	testenv.WriteFile(t, src("remove_dot_old"), "")
	testenv.WriteFile(t, home(".old"), "old\n")
	testenv.WriteFile(t, src(".chezmoiscripts/run_onchange_after_10-setup.sh"), "#!/bin/sh\ntouch \"$HOME/.setup-ran\"\n")

	testenv.WriteFile(t, home(".here"), "edited here\n")
	testenv.WriteFile(t, home(".both"), "edited here\n")
	testenv.WriteFile(t, src("dot_both"), "edited in the repo\n")
	remove(t, home(".gone"))
	remove(t, home(".gone_both"))
	testenv.WriteFile(t, src("dot_gone_both"), "edited in the repo\n")
	remove(t, home(".ln"))
	if err := os.Symlink("elsewhere", home(".ln")); err != nil {
		t.Fatal(err)
	}
	// An edit the modify template keeps is no change for apply.
	testenv.WriteFile(t, home(".zed"), "theme = mocha\nfont = serif\n")
}

func remove(t *testing.T, name string) {
	t.Helper()
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewNothingToDo(t *testing.T) {
	_, d := newDotfiles(t)
	plan, err := Preview(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 {
		t.Errorf("Preview() = %+v, want no changes", plan.Changes)
	}
}

func TestPreview(t *testing.T) {
	td, d := newDotfiles(t)
	makeEveryKind(t, td)
	plan, err := Preview(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{Path: ".both", Kind: Conflict},
		{Path: ".gone", Kind: ChangedHere, DeletedHere: true},
		{Path: ".gone_both", Kind: Conflict, DeletedHere: true},
		{Path: ".here", Kind: ChangedHere},
		{Path: ".ln", Kind: ChangedHere},
		{Path: ".new", Kind: Create},
		{Path: ".old", Kind: Delete},
		{Path: ".update", Kind: Update},
		{Path: ".chezmoiscripts/10-setup.sh", Kind: Run},
	}
	if diff := cmp.Diff(want, plan.Changes); diff != "" {
		t.Errorf("Preview() mismatch (-want +got):\n%s", diff)
	}
}

func TestPreviewRefreshesFacts(t *testing.T) {
	td := testenv.NewDotfiles(t, map[string]string{
		".chezmoi.toml.tmpl": "sourceDir = {{ .chezmoi.config.sourceDir | quote }}\n[data]\n  color = \"blue\"\n",
	})
	d, err := dotfiles.Open(t.Context(), td.Home, &chezmoi.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Preview(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, filepath.Join(td.Source, "dot_color.tmpl"), "{{ .color }}\n")
	td.Chezmoi(t, "apply", "--exclude=scripts")

	testenv.WriteFile(t, filepath.Join(td.Source, ".chezmoi.toml.tmpl"),
		"sourceDir = {{ .chezmoi.config.sourceDir | quote }}\n[data]\n  color = \"green\"\n")
	plan, err := Preview(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Change{{Path: ".color", Kind: Update}}; !cmp.Equal(want, plan.Changes) {
		t.Errorf("Preview() after the facts changed = %+v, want %+v", plan.Changes, want)
	}
}
