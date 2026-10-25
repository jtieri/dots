package chezmoi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// TestTargetPathMatchesChezmoi checks TargetPath against "chezmoi
// target-path" for every combination of chezmoi's prefixes and suffixes.
// target-path maps names only, so it also maps scripts and ignored names;
// those are left out, as TargetPath rejects them on purpose.
func TestTargetPathMatchesChezmoi(t *testing.T) {
	home := testenv.Isolate(t)
	src := t.TempDir()
	config := filepath.Join(t.TempDir(), "chezmoi.toml")
	// Encryption must be configured for chezmoi to drop the .age suffix.
	writeFile(t, config, "encryption = \"age\"\n[age]\nidentity = \"/nonexistent\"\nrecipient = \"age1x\"\n")

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

	var valid []string
	for _, s := range sources {
		writeFile(t, filepath.Join(src, s), "")
		if _, ok := TargetPath(s); ok {
			valid = append(valid, s)
		} else if out, err := chezmoiTargetPath(t, home, src, config, s); err == nil {
			t.Errorf("TargetPath(%q) has no target, chezmoi says %q", s, out)
		}
	}
	if len(valid) < len(sources)/2 {
		t.Fatalf("only %d of %d names have a target", len(valid), len(sources))
	}

	out, err := chezmoiTargetPath(t, home, src, config, valid...)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(valid) {
		t.Fatalf("chezmoi printed %d paths for %d names", len(lines), len(valid))
	}
	for i, s := range valid {
		want, err := filepath.Rel(home, lines[i])
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := TargetPath(s); got != want {
			t.Errorf("TargetPath(%q) = %q, chezmoi says %q", s, got, want)
		}
	}
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

func writeFile(t *testing.T, name, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
