package chezmoi

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jtieri/dot/internal/run"
	"github.com/jtieri/dot/internal/testenv"
)

func TestTargetPath(t *testing.T) {
	tests := []struct {
		source string
		want   string // empty when there is no target
	}{
		{"dot_bashrc", ".bashrc"},
		{"dot_config/i3/config", ".config/i3/config"},
		{"private_dot_config/private_weechat/irc.conf", ".config/weechat/irc.conf"},
		{"dot_config/zed/modify_private_settings.json", ".config/zed/settings.json"},
		{"dot_config/i3/scripts/executable_lock.sh", ".config/i3/scripts/lock.sh"},
		{"exact_dot_local/bin/readonly_tool", ".local/bin/tool"},
		{"external_dot_oh-my-zsh/x", ".oh-my-zsh/x"},
		{"external_dot_oh-my-zsh/dot_sub/private_x.tmpl", ".oh-my-zsh/dot_sub/private_x.tmpl"},
		{"external_dot_d/.keep", ".d/.keep"},
		{"symlink_dot_vimrc", ".vimrc"},
		{"symlink_private_x", "private_x"},
		{"create_empty_dot_hushlogin", ".hushlogin"},
		{"modify_empty_x", "empty_x"},
		{"dot_gitconfig.tmpl", ".gitconfig"},
		{"literal_dot_x", "dot_x"},
		{"x.literal", "x"},
		{"x.literal.tmpl", "x"},
		{"x.tmpl.literal", "x.tmpl"},
		{"encrypted_private_dot_netrc.age", ".netrc"},
		{"encrypted_dot_key.asc", ".key"},
		{"dot_Xresources.base", ".Xresources.base"},
		{"Pictures/wallpapers/japan 3.jpg", "Pictures/wallpapers/japan 3.jpg"},

		{"run_once_after_setup.sh", ""},
		{".chezmoiscripts/run_onchange_after_10-papirus.sh.tmpl", ""},
		{".chezmoiignore", ""},
		{".chezmoiremove", ""},
		{".chezmoitemplates/palette", ""},
		{".chezmoidata/hosts.toml", ""},
		{".keep", ""},
		{"dot_config/.keep", ""},
		{"remove_dot_old", ""},
		{"remove_dot_olddir/x", ""},
		{"dot_", ""},
		{"literal_", ""},
		{"dot_.tmpl", ""},
		{"dot_/x", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got, ok := TargetPath(tt.source)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("TargetPath(%q) = %q, %t; want %q, %t", tt.source, got, ok, tt.want, tt.want != "")
		}
	}
}

// TestTargetPathMatchesChezmoi checks TargetPath against chezmoi for every
// combination of chezmoi's prefixes and suffixes, in every kind of directory.
// "chezmoi managed" says what chezmoi really makes of each name. Names
// TargetPath rejects are checked with "chezmoi target-path" instead, since an
// invalid name stops managed from listing anything.
func TestTargetPathMatchesChezmoi(t *testing.T) {
	home := testenv.Isolate(t)
	src, bad := t.TempDir(), t.TempDir()
	config := filepath.Join(t.TempDir(), "chezmoi.toml")
	// Encryption must be configured for chezmoi to drop the .age suffix.
	testenv.WriteFile(t, config, "encryption = \"age\"\n[age]\nidentity = \"/nonexistent\"\nrecipient = \"age1x\"\n")

	want := make(map[string]string) // source path to target
	sources := sourceNames()
	for i, s := range sources {
		target, ok := TargetPath(s)
		if !ok {
			testenv.WriteFile(t, filepath.Join(bad, s), "")
			if out, err := chezmoiTargetPath(t, home, bad, config, s); err == nil {
				t.Errorf("TargetPath(%q) has no target, chezmoi says %q", s, out)
			}
			continue
		}
		// chezmoi refuses two names with the same target, so each name
		// gets a folder of its own.
		dir := fmt.Sprintf("n%d", i)
		testenv.WriteFile(t, filepath.Join(src, dir, s), "")
		want[dir+"/"+s] = dir + "/" + target
	}
	if len(want) < len(sources)/2 {
		t.Fatalf("only %d of %d names have a target", len(want), len(sources))
	}

	got := make(map[string]string)
	for target, paths := range chezmoiManaged(t, home, src, config, "files,symlinks") {
		got[paths.SourceRelative] = target
	}
	for s, target := range want {
		if got[s] != target {
			t.Errorf("TargetPath(%q) = %q, chezmoi says %q", s, target, got[s])
		}
	}
}

// sourceNames returns every combination of chezmoi's prefixes and suffixes,
// for files at the top of the source directory and in each kind of
// directory, followed by names that produce no target.
func sourceNames() []string {
	dirs := []string{
		"", "exact_", "private_", "readonly_", "external_", "dot_", "literal_",
		"exact_private_dot_", "private_readonly_literal_dot_",
	}
	files := []string{
		"", "private_", "readonly_", "empty_", "executable_", "encrypted_private_",
		"create_", "create_encrypted_private_readonly_empty_executable_",
		"modify_", "modify_private_executable_", "modify_empty_",
		"symlink_", "symlink_private_",
	}
	bases := []string{
		"dot_bashrc", "literal_dot_x", "plain", "dot_x.tmpl", "x.literal",
		"x.literal.tmpl", "x.tmpl.literal", "literal_.x", "x.age", "dot_x.tmpl.age",
	}
	invalid := []string{"dot_", "literal_", "dot_.tmpl", "literal_.tmpl", "dot_..tmpl", "x.tmpl.tmpl.literal"}

	var sources []string
	for _, d := range dirs {
		for _, f := range files {
			for _, b := range bases {
				name := f + b
				if d != "" {
					name = d + "d/" + name
				}
				sources = append(sources, name)
			}
		}
	}
	for _, f := range files {
		for _, b := range invalid {
			sources = append(sources, f+b)
		}
	}
	return sources
}

// managedPaths is how "chezmoi managed --path-style=all" describes a target.
type managedPaths struct {
	SourceRelative string `json:"sourceRelative"`
}

// chezmoiManaged returns the targets of the given types that chezmoi makes
// from src, relative to home.
func chezmoiManaged(t *testing.T, home, src, config, types string) map[string]managedPaths {
	t.Helper()
	out, err := run.Output(exec.CommandContext(t.Context(), "chezmoi", "--no-tty", "--config", config,
		"--source", src, "--destination", home, "managed", "--include="+types,
		"--path-style=all", "--format=json"))
	if err != nil {
		t.Fatal(err)
	}
	var managed map[string]managedPaths
	if err := json.Unmarshal(out, &managed); err != nil {
		t.Fatal(err)
	}
	return managed
}

func chezmoiTargetPath(t *testing.T, home, src, config string, sources ...string) (string, error) {
	t.Helper()
	args := []string{"--no-tty", "--config", config, "--source", src, "--destination", home, "target-path"}
	for _, s := range sources {
		args = append(args, filepath.Join(src, s))
	}
	out, err := exec.CommandContext(t.Context(), "chezmoi", args...).Output()
	return string(out), err
}
