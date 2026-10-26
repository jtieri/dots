package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dot/internal/testenv"
)

var defaults = Config{
	Branch:          "main",
	FetchTimeout:    10 * time.Second,
	Leftovers:       Leftovers{Skip: []string{".", ".config", ".ssh"}},
	ModifyTemplates: []string{},
	OverwriteBackup: OverwriteBackup{Enabled: true, KeepDays: 30},
}

// writeConfig writes the default configuration file.
func writeConfig(t *testing.T, home, data string) {
	t.Helper()
	testenv.WriteFile(t, filepath.Join(home, ".config", "dot", "config.toml"), data)
}

func TestLoadDefaults(t *testing.T) {
	testenv.Isolate(t)
	got, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(&defaults, got); diff != "" {
		t.Errorf("Load() without a file mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadFile(t *testing.T) {
	home := testenv.Isolate(t)
	writeConfig(t, home, `
branch = "trunk"
fetch_timeout = "1m30s"
modify_templates = ["~/.config/zed/settings.json"]

[leftovers]
skip = ["~", "~/.config", "~/.ssh", "~/Pictures"]

[overwrite_backup]
enabled = false
keep_days = 0
`)
	got, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		Branch:          "trunk",
		FetchTimeout:    90 * time.Second,
		Leftovers:       Leftovers{Skip: []string{".", ".config", ".ssh", "Pictures"}},
		ModifyTemplates: []string{".config/zed/settings.json"},
		OverwriteBackup: OverwriteBackup{Enabled: false, KeepDays: 0},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	home := testenv.Isolate(t)
	writeConfig(t, home, "branch = \"trunk\"\nfetch_timeout = \"5s\"\n[leftovers]\nskip = [\"~\"]\n")
	t.Setenv("DOT_BRANCH", "env-branch")
	t.Setenv("DOT_FETCH_TIMEOUT", "1m")
	t.Setenv("DOT_LEFTOVERS_SKIP", "~/a,~/b c")
	t.Setenv("DOT_MODIFY_TEMPLATES", "~/.config/zed/settings.json")
	t.Setenv("DOT_OVERWRITE_BACKUP_ENABLED", "false")
	t.Setenv("DOT_OVERWRITE_BACKUP_KEEP_DAYS", "7")
	got, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		Branch:          "env-branch",
		FetchTimeout:    time.Minute,
		Leftovers:       Leftovers{Skip: []string{"a", "b c"}},
		ModifyTemplates: []string{".config/zed/settings.json"},
		OverwriteBackup: OverwriteBackup{Enabled: false, KeepDays: 7},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadChoosesFile(t *testing.T) {
	home := testenv.Isolate(t)
	writeConfig(t, home, "branch = \"default-file\"\n")
	envFile := filepath.Join(home, "env.toml")
	testenv.WriteFile(t, envFile, "branch = \"env-file\"\n")
	flagFile := filepath.Join(home, "flag.toml")
	testenv.WriteFile(t, flagFile, "branch = \"flag-file\"\n")

	t.Setenv(EnvConfig, envFile)
	if got, err := Load(""); err != nil || got.Branch != "env-file" {
		t.Errorf("Load() with DOT_CONFIG: branch %v, %v; want env-file", branch(got), err)
	}
	if got, err := Load(flagFile); err != nil || got.Branch != "flag-file" {
		t.Errorf("Load(flagFile) with DOT_CONFIG: branch %v, %v; want flag-file", branch(got), err)
	}
}

func branch(c *Config) string {
	if c == nil {
		return "<nil>"
	}
	return c.Branch
}

func TestLoadMissingNamedFile(t *testing.T) {
	home := testenv.Isolate(t)
	missing := filepath.Join(home, "missing.toml")
	if _, err := Load(missing); err == nil {
		t.Error("Load() of a missing named file: error = nil, want an error")
	}
	t.Setenv(EnvConfig, missing)
	if _, err := Load(""); err == nil {
		t.Error("Load() with DOT_CONFIG naming a missing file: error = nil, want an error")
	}
}

// TestLoadKeysIgnoreCase documents that Viper matches keys regardless of case.
// A key in the wrong case still takes effect, so nothing is ignored.
func TestLoadKeysIgnoreCase(t *testing.T) {
	home := testenv.Isolate(t)
	writeConfig(t, home, "Branch = \"trunk\"\n")
	if got, err := Load(""); err != nil || got.Branch != "trunk" {
		t.Errorf("Load() with Branch: branch %v, %v; want trunk", branch(got), err)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		env     map[string]string
		wantErr string
	}{
		{name: "unknown setting", file: "brnach = \"main\"\n", wantErr: `unknown setting "brnach"`},
		{name: "unknown setting in a table", file: "[leftovers]\nskp = []\n", wantErr: `unknown setting "leftovers.skp"`},
		{name: "unknown table", file: "[colors]\nbranch = \"x\"\n", wantErr: `unknown setting "colors.branch"`},
		{name: "not TOML", file: "branch = \n", wantErr: "config file"},
		{name: "number for a string", file: "branch = 5\n", wantErr: "branch"},
		{name: "number for a duration", file: "fetch_timeout = 10\n", wantErr: "fetch_timeout"},
		{name: "number in a list", file: "modify_templates = [5]\n", wantErr: "modify_templates"},
		{name: "empty branch", file: "branch = \"\"\n", wantErr: `branch "" is not a branch name`},
		{name: "zero timeout", file: "fetch_timeout = \"0s\"\n", wantErr: "fetch_timeout 0s must be more than zero"},
		{name: "negative timeout", file: "fetch_timeout = \"-1s\"\n", wantErr: "must be more than zero"},
		{name: "timeout without a unit", file: "fetch_timeout = \"10\"\n", wantErr: "fetch_timeout"},
		{name: "absolute path", file: "[leftovers]\nskip = [\"/etc\"]\n", wantErr: `leftovers.skip: "/etc" must be ~ or start with ~/`},
		{name: "path without ~", file: "[leftovers]\nskip = [\"Pictures\"]\n", wantErr: `leftovers.skip: "Pictures"`},
		{name: "path leaving home", file: "modify_templates = [\"~/../x\"]\n", wantErr: `modify_templates: "~/../x"`},
		{name: "number for a bool", file: "[overwrite_backup]\nenabled = 1\n", wantErr: "overwrite_backup.enabled"},
		{name: "fraction for a number", file: "[overwrite_backup]\nkeep_days = 1.5\n", wantErr: "'overwrite_backup.keep_days' 1.5 is not a whole number"},
		{name: "negative days", file: "[overwrite_backup]\nkeep_days = -1\n", wantErr: "overwrite_backup.keep_days -1 must not be negative"},
		{name: "bad environment bool", env: map[string]string{"DOT_OVERWRITE_BACKUP_ENABLED": "maybe"}, wantErr: "overwrite_backup.enabled"},
		{name: "bad environment number", env: map[string]string{"DOT_OVERWRITE_BACKUP_KEEP_DAYS": "a month"}, wantErr: "overwrite_backup.keep_days"},
		{name: "bad environment value", env: map[string]string{"DOT_FETCH_TIMEOUT": "soon"}, wantErr: "fetch_timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := testenv.Isolate(t)
			if tt.file != "" {
				writeConfig(t, home, tt.file)
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := Load("")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}
