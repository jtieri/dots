package config

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dot/internal/testenv"
)

var defaults = Config{
	Branch:          "main",
	Capture:         Capture{Protected: []string{".ssh/id_*", ".gnupg"}},
	FetchTimeout:    10 * time.Second,
	Leftovers:       Leftovers{Skip: []string{".", ".config", ".ssh"}},
	ModifyTemplates: []ModifyTemplate{},
	OverwriteBackup: OverwriteBackup{Enabled: true, KeepDays: 30},
	Push:            Push{PackageLists: "scripts/pkglists"},
	RestartNotices:  []RestartNotice{},
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

[capture]
protected = ["~/.ssh/id_*", "~/.ssh/*.key", "~/.password-store"]

[leftovers]
skip = ["~", "~/.config", "~/.ssh", "~/Pictures"]

[overwrite_backup]
enabled = false
keep_days = 0

[push]
package_lists = "packages"

[[restart_notices]]
program = "zed-editor"
files = ["~/.config/zed/settings.json"]

[[restart_notices]]
program = "polybar"
files = ["~/.config/polybar/config.ini", "~/.config/polybar/colors.ini"]

[[modify_templates]]
file = "~/.config/zed/settings.json"
keep = '"theme"\s*:\s*\{[^{}]*\}'
`)
	got, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		Branch:       "trunk",
		Capture:      Capture{Protected: []string{".ssh/id_*", ".ssh/*.key", ".password-store"}},
		FetchTimeout: 90 * time.Second,
		Leftovers:    Leftovers{Skip: []string{".", ".config", ".ssh", "Pictures"}},
		ModifyTemplates: []ModifyTemplate{
			{File: ".config/zed/settings.json", Keep: regexp.MustCompile(`"theme"\s*:\s*\{[^{}]*\}`)},
		},
		OverwriteBackup: OverwriteBackup{Enabled: false, KeepDays: 0},
		Push:            Push{PackageLists: "packages"},
		RestartNotices: []RestartNotice{
			{Program: "zed-editor", Files: []string{".config/zed/settings.json"}},
			{Program: "polybar", Files: []string{".config/polybar/config.ini", ".config/polybar/colors.ini"}},
		},
	}
	if diff := cmp.Diff(want, got, cmpRegexp); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

// cmpRegexp compares regular expressions by their source text.
var cmpRegexp = cmp.Comparer(func(a, b *regexp.Regexp) bool { return a.String() == b.String() })

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	home := testenv.Isolate(t)
	writeConfig(t, home, "branch = \"trunk\"\nfetch_timeout = \"5s\"\n[leftovers]\nskip = [\"~\"]\n")
	t.Setenv("DOT_BRANCH", "env-branch")
	t.Setenv("DOT_FETCH_TIMEOUT", "1m")
	t.Setenv("DOT_LEFTOVERS_SKIP", "~/a,~/b c")
	t.Setenv("DOT_CAPTURE_PROTECTED", "~/.ssh/k*")
	t.Setenv("DOT_OVERWRITE_BACKUP_ENABLED", "false")
	t.Setenv("DOT_OVERWRITE_BACKUP_KEEP_DAYS", "7")
	t.Setenv("DOT_PUSH_PACKAGE_LISTS", "lists/pacman")
	got, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		Branch:          "env-branch",
		Capture:         Capture{Protected: []string{".ssh/k*"}},
		FetchTimeout:    time.Minute,
		Leftovers:       Leftovers{Skip: []string{"a", "b c"}},
		ModifyTemplates: []ModifyTemplate{},
		OverwriteBackup: OverwriteBackup{Enabled: false, KeepDays: 7},
		Push:            Push{PackageLists: "lists/pacman"},
		RestartNotices:  []RestartNotice{},
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
		{name: "number in a list", file: "[leftovers]\nskip = [5]\n", wantErr: "leftovers.skip"},
		{name: "empty branch", file: "branch = \"\"\n", wantErr: `branch "" is not a branch name`},
		{name: "zero timeout", file: "fetch_timeout = \"0s\"\n", wantErr: "fetch_timeout 0s must be more than zero"},
		{name: "negative timeout", file: "fetch_timeout = \"-1s\"\n", wantErr: "must be more than zero"},
		{name: "timeout without a unit", file: "fetch_timeout = \"10\"\n", wantErr: "fetch_timeout"},
		{name: "absolute path", file: "[leftovers]\nskip = [\"/etc\"]\n", wantErr: `leftovers.skip: "/etc" must be ~ or start with ~/`},
		{name: "path without ~", file: "[leftovers]\nskip = [\"Pictures\"]\n", wantErr: `leftovers.skip: "Pictures"`},
		{name: "path leaving home", file: "[leftovers]\nskip = [\"~/../x\"]\n", wantErr: `leftovers.skip: "~/../x"`},
		{name: "number for a bool", file: "[overwrite_backup]\nenabled = 1\n", wantErr: "overwrite_backup.enabled"},
		{name: "fraction for a number", file: "[overwrite_backup]\nkeep_days = 1.5\n", wantErr: "'overwrite_backup.keep_days' 1.5 is not a whole number"},
		{name: "negative days", file: "[overwrite_backup]\nkeep_days = -1\n", wantErr: "overwrite_backup.keep_days -1 must not be negative"},
		{name: "bad environment bool", env: map[string]string{"DOT_OVERWRITE_BACKUP_ENABLED": "maybe"}, wantErr: "overwrite_backup.enabled"},
		{name: "bad environment number", env: map[string]string{"DOT_OVERWRITE_BACKUP_KEEP_DAYS": "a month"}, wantErr: "overwrite_backup.keep_days"},
		{name: "unknown key in a notice", file: "[[restart_notices]]\nprogram = \"zed\"\nfile = [\"~/x\"]\n", wantErr: "restart_notices[0]' has invalid keys: file"},
		{name: "notice without files", file: "[[restart_notices]]\nprogram = \"zed\"\n", wantErr: "restart_notices[0]: files is empty"},
		{name: "notice with a path for a program", file: "[[restart_notices]]\nprogram = \"/usr/bin/zed\"\nfiles = [\"~/x\"]\n", wantErr: "must be a process name"},
		{name: "notice file outside home", file: "[[restart_notices]]\nprogram = \"zed\"\nfiles = [\"/etc/x\"]\n", wantErr: "restart_notices[0].files: \"/etc/x\""},
		{name: "bad pattern", file: "[capture]\nprotected = [\"~/.ssh/[\"]\n", wantErr: `capture.protected: "~/.ssh/[" is not a valid pattern`},
		{name: "protected outside home", file: "[capture]\nprotected = [\"/etc/*\"]\n", wantErr: `capture.protected: "/etc/*" must be ~`},
		{name: "package lists outside the repo", file: "[push]\npackage_lists = \"../lists\"\n", wantErr: `push.package_lists "../lists" must be a folder inside the repo`},
		{name: "package lists absolute", file: "[push]\npackage_lists = \"/lists\"\n", wantErr: `push.package_lists "/lists"`},
		{name: "package lists not clean", file: "[push]\npackage_lists = \"scripts/\"\n", wantErr: `push.package_lists "scripts/"`},
		{name: "notices not a list", file: "restart_notices = \"zed\"\n", wantErr: "restart_notices"},
		{name: "template as a path", file: "modify_templates = [\"~/x\"]\n", wantErr: "modify_templates"},
		{name: "template without keep", file: "[[modify_templates]]\nfile = \"~/x\"\n", wantErr: "modify_templates[0]: keep is missing"},
		{name: "template without file", file: "[[modify_templates]]\nkeep = \"x\"\n", wantErr: `modify_templates[0].file: ""`},
		{name: "template file outside home", file: "[[modify_templates]]\nfile = \"/etc/x\"\nkeep = \"x\"\n", wantErr: `modify_templates[0].file: "/etc/x"`},
		{name: "template keep not a pattern", file: "[[modify_templates]]\nfile = \"~/x\"\nkeep = \"(\"\n", wantErr: "'modify_templates[0].keep' error parsing regexp: missing closing )"},
		{name: "template keep not a string", file: "[[modify_templates]]\nfile = \"~/x\"\nkeep = 5\n", wantErr: "'modify_templates[0].keep' 5 is not a regular expression"},
		{name: "unknown key in a template", file: "[[modify_templates]]\nfile = \"~/x\"\nkeep = \"x\"\nkept = \"y\"\n", wantErr: "invalid keys: kept"},
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

func TestLoadPackageListsOff(t *testing.T) {
	home := testenv.Isolate(t)
	writeConfig(t, home, "[push]\npackage_lists = \"\"\n")
	got, err := Load("")
	if err != nil || got.Push.PackageLists != "" {
		t.Errorf("Load() package_lists = %q, %v; want it empty", got.Push.PackageLists, err)
	}
}

func TestDefaultText(t *testing.T) {
	want := map[string]string{
		"branch":                     `"main"`,
		"capture.protected":          `["~/.ssh/id_*", "~/.gnupg"]`,
		"fetch_timeout":              `"10s"`,
		"leftovers.skip":             `["~", "~/.config", "~/.ssh"]`,
		"modify_templates":           "[]",
		"overwrite_backup.enabled":   "true",
		"overwrite_backup.keep_days": "30",
		"push.package_lists":         `"scripts/pkglists"`,
		"restart_notices":            "[]",
	}
	for _, s := range Settings {
		w, ok := want[s.Key]
		if !ok {
			t.Errorf("no expected default text for %s", s.Key)
			continue
		}
		if got := s.DefaultText(); got != w {
			t.Errorf("%s DefaultText() = %s, want %s", s.Key, got, w)
		}
	}
	if len(want) != len(Settings) {
		t.Errorf("want covers %d settings, there are %d", len(want), len(Settings))
	}
}

// TestDefaultTextLoads checks that each default, written into a
// configuration file, loads as the default does.
func TestDefaultTextLoads(t *testing.T) {
	home := testenv.Isolate(t)
	// Top-level settings come first: in TOML, those after a table belong
	// to it.
	var top, tables strings.Builder
	table := ""
	for _, s := range Settings {
		tbl, key, ok := strings.Cut(s.Key, ".")
		if !ok {
			top.WriteString(s.Key + " = " + s.DefaultText() + "\n")
			continue
		}
		if tbl != table {
			tables.WriteString("[" + tbl + "]\n")
			table = tbl
		}
		tables.WriteString(key + " = " + s.DefaultText() + "\n")
	}
	file := top.String() + tables.String()
	writeConfig(t, home, file)
	got, err := Load("")
	if err != nil {
		t.Fatalf("Load() of the defaults written out:\n%s\nerror: %v", file, err)
	}
	if diff := cmp.Diff(&defaults, got, cmpRegexp); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}
