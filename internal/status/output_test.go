package status

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dot/internal/style"
)

func TestWriteText(t *testing.T) {
	lastFetch := time.Date(2026, 10, 25, 14, 2, 0, 0, time.Local)
	tests := []struct {
		name   string
		report Report
		want   string
	}{
		{
			name:   "in sync",
			report: Report{InSync: true, Repo: Repo{Branch: "main", WantBranch: "main", Fetch: Fetch{OK: true}}},
			want:   "In sync.\n",
		},
		{
			name: "in sync as far as can be told offline",
			report: Report{InSync: true, Repo: Repo{
				Branch: "main", WantBranch: "main",
				Fetch: Fetch{Error: "ssh: Could not resolve hostname github.com", LastFetch: lastFetch},
			}},
			want: "In sync.\n" +
				"Could not fetch from the remote: ssh: Could not resolve hostname github.com. Last fetched 2026-10-25 14:02.\n",
		},
		{
			name: "everything out of sync",
			report: Report{
				Repo: Repo{
					Path: "/home/me/dotfiles", Branch: "experiment", WantBranch: "main",
					Ahead: 2, Behind: 1, Fetch: Fetch{Error: "timed out"},
					Uncommitted: []Uncommitted{
						{Change: "modified", HomePath: ".bashrc", RepoPath: "home/dot_bashrc"},
						{Change: "renamed", HomePath: ".x", RepoPath: "home/private_dot_x", From: "home/dot_x"},
						{Change: "new", RepoPath: "scripts/tool.sh"},
					},
				},
				Home: []HomeChange{
					{".y", RepoNewer},
					{".bashrc", ChangedHere},
					{".old", RemovedInRepo},
					{".x", DeletedHere},
					{".config/new.conf", NewInRepo},
				},
				SetupSteps:        []string{"10-stage-papirus.sh"},
				Leftovers:         []string{".config/kitty/left over.conf"},
				ReplacedTemplates: []string{".config/zed/settings.json"},
			},
			want: `Repo /home/me/dotfiles
  On branch experiment, should be on main.
  2 commits not pushed.
  1 commit not pulled.
  Could not fetch from the remote: timed out. It has never been fetched.
  Uncommitted changes:
    modified  ~/.bashrc
    renamed   ~/.x (was home/dot_x)
    new       scripts/tool.sh

Home
  changed on this machine   ~/.bashrc
  deleted on this machine   ~/.x
  repo has a newer version  ~/.y
  new in the repo           ~/.config/new.conf
  removed in the repo       ~/.old

Setup steps waiting to run
  10-stage-papirus.sh

Leftovers: in tracked folders but not tracked
  ~/.config/kitty/left over.conf

Should be modify templates, but the repo has plain copies
  ~/.config/zed/settings.json
`,
		},
		{
			name: "only the home differs",
			report: Report{
				Repo: Repo{Path: "/home/me/dotfiles", Branch: "main", WantBranch: "main", Fetch: Fetch{OK: true}},
				Home: []HomeChange{{".bashrc", ChangedHere}},
			},
			want: "Home\n  changed on this machine   ~/.bashrc\n",
		},
		{
			name: "detached",
			report: Report{Repo: Repo{
				Path: "/r", WantBranch: "main", NoUpstream: true, Fetch: Fetch{OK: true},
			}},
			want: "Repo /r\n  Not on a branch (detached HEAD), should be on main.\n",
		},
		{
			name: "no upstream",
			report: Report{Repo: Repo{
				Path: "/r", Branch: "main", WantBranch: "main", NoUpstream: true, Fetch: Fetch{OK: true},
			}},
			want: "Repo /r\n  The branch is not tracking a remote branch.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			if err := tt.report.WriteText(&b, style.Style{}); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, b.String()); diff != "" {
				t.Errorf("WriteText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	r := &Report{Version: Version, InSync: true, Repo: Repo{Path: "/r", Branch: "main", WantBranch: "main", Fetch: Fetch{OK: true}}}
	var b strings.Builder
	if err := r.WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatalf("WriteJSON() wrote invalid JSON: %v\n%s", err, b.String())
	}
	if got["version"] != float64(Version) || got["in_sync"] != true {
		t.Errorf("version, in_sync = %v, %v; want %d, true", got["version"], got["in_sync"], Version)
	}
	for _, key := range []string{"home", "setup_steps", "leftovers", "replaced_templates"} {
		if list, ok := got[key].([]any); !ok || len(list) != 0 {
			t.Errorf("%s = %#v, want an empty array", key, got[key])
		}
	}
	repo := got["repo"].(map[string]any)
	if list, ok := repo["uncommitted"].([]any); !ok || len(list) != 0 {
		t.Errorf("repo.uncommitted = %#v, want an empty array", repo["uncommitted"])
	}
	if _, ok := repo["fetch"].(map[string]any)["last_fetch"]; ok {
		t.Error("repo.fetch.last_fetch is present, want it left out when never fetched")
	}
	if r.Home != nil {
		t.Error("WriteJSON() changed the report")
	}
}
