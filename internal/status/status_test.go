package status

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/jtieri/dot/internal/chezmoi"
	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/testenv"
)

var files = map[string]string{
	"dot_bashrc":                  "bashrc\n",
	"dot_profile":                 "profile\n",
	"dot_x":                       "x\n",
	"dot_y":                       "y\n",
	"dot_z":                       "z\n",
	"dot_config/kitty/kitty.conf": "kitty\n",
	"dot_config/zed/modify_settings.json": "{{/* chezmoi:modify-template */}}" +
		"{{ .chezmoi.stdin }}",
	"dot_config/btop/btop.conf": "btop\n",
	"private_dot_ssh/config":    "Host *\n",
}

// ignoreTimes leaves out the fetch time, which the tests check separately.
var ignoreTimes = cmpopts.IgnoreFields(Fetch{}, "LastFetch")

func options() Options {
	return Options{
		Branch:          "main",
		FetchTimeout:    10 * time.Second,
		SkipDirs:        []string{".", ".config", ".ssh"},
		ModifyTemplates: []string{".config/zed/settings.json"},
	}
}

func TestCheckInSync(t *testing.T) {
	d := testenv.NewDotfiles(t, files)
	// Personal files beside tracked ones, in the folders that are skipped.
	testenv.WriteFile(t, filepath.Join(d.Home, "notes.txt"), "mine")
	testenv.WriteFile(t, filepath.Join(d.Home, ".config/newapp/config"), "mine")
	testenv.WriteFile(t, filepath.Join(d.Home, ".ssh/id_ed25519"), "secret")
	// An empty folder inside a checked one.
	if err := os.Mkdir(filepath.Join(d.Home, ".config/kitty/empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now().Add(-time.Second)
	got, err := Check(t.Context(), open(t, d), options())
	if err != nil {
		t.Fatal(err)
	}
	want := &Report{
		Version: Version,
		InSync:  true,
		Repo: Repo{
			Path:       d.Repo,
			Branch:     "main",
			WantBranch: "main",
			Fetch:      Fetch{OK: true},
		},
	}
	if diff := cmp.Diff(want, got, ignoreTimes); diff != "" {
		t.Errorf("Check() mismatch (-want +got):\n%s", diff)
	}
	if got.Repo.Fetch.LastFetch.Before(start) {
		t.Errorf("LastFetch = %v, want after %v", got.Repo.Fetch.LastFetch, start)
	}
}

func TestCheckFindsEverything(t *testing.T) {
	d := testenv.NewDotfiles(t, files)
	home := func(name string) string { return filepath.Join(d.Home, name) }
	src := func(name string) string { return filepath.Join(d.Source, name) }

	// A commit that is not pushed, and one on the remote that is not pulled.
	testenv.WriteFile(t, src("dot_x"), "x committed\n")
	d.Git(t, "commit", "--quiet", "--all", "--message=local")
	d.Chezmoi(t, "apply", "--exclude=scripts")
	other := filepath.Join(t.TempDir(), "other")
	testenv.Run(t, d.Home, "git", "clone", "--quiet", d.Remote, other)
	testenv.WriteFile(t, filepath.Join(other, "README.md"), "remote\n")
	testenv.Run(t, other, "git", "add", "README.md")
	testenv.Run(t, other, "git", "commit", "--quiet", "--message=remote")
	testenv.Run(t, other, "git", "push", "--quiet")

	// Changes on this machine.
	testenv.WriteFile(t, home(".bashrc"), "edited here\n")
	if err := os.Chmod(home(".profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(home(".x")); err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, home(".config/kitty/left over.conf"), "old")
	testenv.WriteFile(t, home(".config/kitty/cache/data"), "old")

	// Uncommitted changes in the repo.
	testenv.WriteFile(t, src("dot_y"), "y newer\n")
	testenv.WriteFile(t, src("dot_config/new.conf"), "new\n")
	testenv.WriteFile(t, src("remove_dot_old"), "")
	testenv.WriteFile(t, home(".old"), "old")
	if err := os.Remove(src("dot_z")); err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, src(".chezmoiscripts/run_onchange_after_10-setup.sh"), "#!/bin/sh\nexit 1\n")
	testenv.WriteFile(t, filepath.Join(d.Repo, "scripts/tool.sh"), "#!/bin/sh\n")
	// A file that should be a modify template but is tracked as a copy.
	opts := options()
	opts.ModifyTemplates = append(opts.ModifyTemplates, ".config/btop/btop.conf")

	got, err := Check(t.Context(), open(t, d), opts)
	if err != nil {
		t.Fatal(err)
	}
	want := &Report{
		Version: Version,
		Repo: Repo{
			Path:       d.Repo,
			Branch:     "main",
			WantBranch: "main",
			Ahead:      1,
			Behind:     1,
			Fetch:      Fetch{OK: true},
			Uncommitted: []Uncommitted{
				{Change: "modified", HomePath: ".y", RepoPath: "home/dot_y"},
				{Change: "deleted", HomePath: ".z", RepoPath: "home/dot_z"},
				{Change: "new", RepoPath: "home/.chezmoiscripts/run_onchange_after_10-setup.sh"},
				{Change: "new", HomePath: ".config/new.conf", RepoPath: "home/dot_config/new.conf"},
				{Change: "new", RepoPath: "home/remove_dot_old"},
				{Change: "new", RepoPath: "scripts/tool.sh"},
			},
		},
		Home: []HomeChange{
			{Path: ".bashrc", Kind: ChangedHere},
			{Path: ".config/new.conf", Kind: NewInRepo},
			{Path: ".old", Kind: RemovedInRepo},
			{Path: ".profile", Kind: ChangedHere},
			{Path: ".x", Kind: DeletedHere},
			{Path: ".y", Kind: RepoNewer},
		},
		SetupSteps:        []string{"10-setup.sh"},
		Leftovers:         []string{".config/kitty/cache", ".config/kitty/left over.conf"},
		ReplacedTemplates: []string{".config/btop/btop.conf"},
	}
	if diff := cmp.Diff(want, got, ignoreTimes); diff != "" {
		t.Errorf("Check() mismatch (-want +got):\n%s", diff)
	}
}

func TestCheckBranch(t *testing.T) {
	d := testenv.NewDotfiles(t, files)
	d.Git(t, "checkout", "--quiet", "-b", "experiment")
	got, err := Check(t.Context(), open(t, d), options())
	if err != nil {
		t.Fatal(err)
	}
	if got.InSync || got.Repo.Branch != "experiment" || !got.Repo.NoUpstream {
		t.Errorf("Check() on a local branch: InSync %t, Branch %q, NoUpstream %t; want false, experiment, true",
			got.InSync, got.Repo.Branch, got.Repo.NoUpstream)
	}

	d.Git(t, "checkout", "--quiet", "--detach")
	got, err = Check(t.Context(), open(t, d), options())
	if err != nil {
		t.Fatal(err)
	}
	if got.InSync || got.Repo.Branch != "" {
		t.Errorf("Check() detached: InSync %t, Branch %q; want false, empty", got.InSync, got.Repo.Branch)
	}
}

func TestCheckFetchFails(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		sshCmd    string
		wantError string
	}{
		{
			name:      "remote missing",
			remote:    "/nonexistent/dotfiles.git",
			wantError: "does not appear to be a git repository",
		},
		{
			name:      "remote does not answer",
			remote:    "ssh://git@example.invalid/dotfiles.git",
			sshCmd:    "sleep 30 #",
			wantError: "no answer from the remote within 300ms",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testenv.NewDotfiles(t, files)
			d.Git(t, "fetch", "--quiet")
			d.Git(t, "remote", "set-url", "origin", tt.remote)
			if tt.sshCmd != "" {
				t.Setenv("GIT_SSH_COMMAND", tt.sshCmd)
			}
			opts := options()
			opts.FetchTimeout = 300 * time.Millisecond
			got, err := Check(t.Context(), open(t, d), opts)
			if err != nil {
				t.Fatal(err)
			}
			f := got.Repo.Fetch
			if f.OK || !strings.Contains(f.Error, tt.wantError) || f.LastFetch.IsZero() {
				t.Errorf("Fetch = %+v, want a failure containing %q and the last fetch time", f, tt.wantError)
			}
			// What can be checked locally still counts.
			if !got.InSync {
				t.Errorf("InSync = false, want true")
			}
		})
	}
}

// open opens the dotfiles in d.
func open(t *testing.T, d *testenv.Dotfiles) *dotfiles.Dotfiles {
	t.Helper()
	got, err := dotfiles.Open(t.Context(), d.Home, &chezmoi.Client{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
