package chezmoi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dots/internal/testenv"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    []StatusEntry
		wantErr bool
	}{
		{name: "empty", out: ""},
		{
			name: "every kind of line",
			out: "MM .bashrc\n" +
				"DA .bash_profile\n" +
				" M .profile\n" +
				" A .config/new file\n" +
				" D .config/old\n" +
				" R .chezmoiscripts/10-stage-papirus.sh\n",
			want: []StatusEntry{
				{'M', 'M', ".bashrc"},
				{'D', 'A', ".bash_profile"},
				{' ', 'M', ".profile"},
				{' ', 'A', ".config/new file"},
				{' ', 'D', ".config/old"},
				{' ', 'R', ".chezmoiscripts/10-stage-papirus.sh"},
			},
		},
		{name: "short line", out: "M\n", wantErr: true},
		{name: "no separator", out: "MMx.bashrc\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatus([]byte(tt.out))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseStatus() error = %v, wantErr %t", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseStatus() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClient(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{
		"dot_bashrc":                          "bashrc\n",
		"dot_profile":                         "profile\n",
		"dot_config/kitty/kitty.conf":         "kitty\n",
		"dot_config/kitty/themes/x.conf":      "theme\n",
		"dot_config/zed/modify_settings.json": "{{/* chezmoi:modify-template */}}{{ .chezmoi.stdin }}",
		".chezmoiremove":                      ".old\n.gone\n",
	})
	c := &Client{}
	ctx := t.Context()

	t.Run("SourceDir", func(t *testing.T) {
		got, err := c.SourceDir(ctx)
		if err != nil || got != d.Source {
			t.Errorf("SourceDir() = %q, %v; want %q", got, err, d.Source)
		}
	})

	t.Run("Hostname", func(t *testing.T) {
		host, err := os.Hostname()
		if err != nil {
			t.Fatal(err)
		}
		want, _, _ := strings.Cut(host, ".")
		if got, err := c.Hostname(ctx); err != nil || got != want {
			t.Errorf("Hostname() = %q, %v; want %q", got, err, want)
		}
		silent := filepath.Join(t.TempDir(), "chezmoi")
		testenv.WriteFile(t, silent, "#!/bin/sh\necho\n")
		if err := os.Chmod(silent, 0o755); err != nil {
			t.Fatal(err)
		}
		if got, err := (&Client{Program: silent}).Hostname(ctx); err == nil {
			t.Errorf("Hostname() with no name = %q, want an error", got)
		}
	})

	t.Run("SourcePath", func(t *testing.T) {
		got, err := c.SourcePath(ctx, filepath.Join(d.Home, ".config/zed/settings.json"))
		want := filepath.Join(d.Source, "dot_config/zed/modify_settings.json")
		if err != nil || got != want {
			t.Errorf("SourcePath() = %q, %v; want %q", got, err, want)
		}
		if _, err := c.SourcePath(ctx, filepath.Join(d.Home, ".nope")); err == nil {
			t.Error("SourcePath() of an unmanaged file: error = nil, want an error")
		}
	})

	t.Run("Status", func(t *testing.T) {
		got, err := c.Status(ctx)
		if err != nil || len(got) != 0 {
			t.Fatalf("Status() after apply = %v, %v; want nothing", got, err)
		}
		testenv.WriteFile(t, filepath.Join(d.Home, ".bashrc"), "edited\n")
		testenv.WriteFile(t, filepath.Join(d.Source, "dot_profile"), "newer\n")
		got, err = c.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []StatusEntry{{'M', 'M', ".bashrc"}, {' ', 'M', ".profile"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Status() mismatch (-want +got):\n%s", diff)
		}
		got, err = c.Status(ctx, filepath.Join(d.Home, ".profile"), filepath.Join(d.Home, ".config/kitty"))
		if diff := cmp.Diff(want[1:], got); err != nil || diff != "" {
			t.Errorf("Status(.profile, .config/kitty) error = %v, mismatch (-want +got):\n%s", err, diff)
		}
	})

	t.Run("Managed dirs", func(t *testing.T) {
		got, err := c.Managed(ctx, "dirs")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{
			filepath.Join(d.Home, ".config"),
			filepath.Join(d.Home, ".config/kitty"),
			filepath.Join(d.Home, ".config/kitty/themes"),
			filepath.Join(d.Home, ".config/zed"),
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Managed(dirs) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Managed within", func(t *testing.T) {
		kitty := filepath.Join(d.Home, ".config/kitty")
		got, err := c.Managed(ctx, "files,symlinks", kitty)
		want := []string{filepath.Join(kitty, "kitty.conf"), filepath.Join(kitty, "themes/x.conf")}
		if diff := cmp.Diff(want, got); err != nil || diff != "" {
			t.Errorf("Managed(files, .config/kitty) error = %v, mismatch (-want +got):\n%s", err, diff)
		}
	})

	t.Run("Managed remove", func(t *testing.T) {
		old := filepath.Join(d.Home, ".old")
		testenv.WriteFile(t, old, "old\n")
		t.Cleanup(func() { os.Remove(old) })
		got, err := c.Managed(ctx, "remove")
		// .gone is not in the home directory.
		if diff := cmp.Diff([]string{old}, got); err != nil || diff != "" {
			t.Errorf("Managed(remove) error = %v, mismatch (-want +got):\n%s", err, diff)
		}
	})

	t.Run("Unmanaged", func(t *testing.T) {
		kitty := filepath.Join(d.Home, ".config/kitty")
		testenv.WriteFile(t, filepath.Join(kitty, "left over.txt"), "x")
		if err := os.Mkdir(filepath.Join(kitty, "cache"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := c.Unmanaged(ctx, []string{kitty})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{filepath.Join(kitty, "cache"), filepath.Join(kitty, "left over.txt")}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unmanaged() mismatch (-want +got):\n%s", diff)
		}
		if got, err := c.Unmanaged(ctx, nil); err != nil || got != nil {
			t.Errorf("Unmanaged(nil) = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("Forget", func(t *testing.T) {
		themes := filepath.Join(d.Home, ".config/kitty/themes")
		if err := c.Forget(ctx, themes); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(d.Source, "dot_config/kitty/themes")); err == nil {
			t.Error("Forget() left the folder in the source directory")
		}
		if got := testenv.ReadFile(t, filepath.Join(themes, "x.conf")); got != "theme\n" {
			t.Errorf("x.conf = %q, want it left as it was", got)
		}
		if err := c.Forget(ctx, filepath.Join(d.Home, ".nope")); err == nil {
			t.Error("Forget() of an unmanaged file: error = nil, want one")
		}
	})

	t.Run("program not found", func(t *testing.T) {
		c := &Client{Program: "dots-no-such-chezmoi"}
		if _, err := c.Status(ctx); err == nil {
			t.Error("Status() error = nil, want an error")
		}
	})
}

func TestClientApply(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{
		"dot_a": "a\n",
		"dot_b": "b\n",
		".chezmoiscripts/run_onchange_after_10-hello.sh": "#!/bin/sh\necho hello from the script\ntouch \"$HOME/.ran\"\n",
	})
	c := &Client{}
	ctx := t.Context()
	home := func(name string) string { return filepath.Join(d.Home, name) }
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(home(name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "a from the repo\n")
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_b"), "b from the repo\n")

	t.Run("no targets does nothing", func(t *testing.T) {
		if err := c.Apply(ctx, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		if read(".a") != "a\n" || read(".b") != "b\n" {
			t.Error("Apply() with no targets changed files")
		}
	})

	t.Run("only the targets", func(t *testing.T) {
		var out strings.Builder
		err := c.Apply(ctx, ApplyOptions{
			Targets: []string{home(".a"), home(".chezmoiscripts/10-hello.sh")},
			Stdout:  &out,
			Stderr:  &out,
		})
		if err != nil {
			t.Fatal(err)
		}
		if read(".a") != "a from the repo\n" || read(".b") != "b\n" {
			t.Errorf(".a = %q, .b = %q; want only .a applied", read(".a"), read(".b"))
		}
		if _, err := os.Stat(home(".ran")); err != nil {
			t.Errorf("the script did not run: %v", err)
		}
		if !strings.Contains(out.String(), "hello from the script") {
			t.Errorf("output = %q, want the script's output", out.String())
		}
	})

	t.Run("changed here", func(t *testing.T) {
		testenv.WriteFile(t, home(".b"), "b edited here\n")
		err := c.Apply(ctx, ApplyOptions{Targets: []string{home(".b")}, Stdout: io.Discard, Stderr: io.Discard})
		if err == nil {
			t.Error("Apply() over a file changed here: error = nil, want an error")
		}
		if read(".b") != "b edited here\n" {
			t.Errorf(".b = %q, want it left alone", read(".b"))
		}
		err = c.Apply(ctx, ApplyOptions{Targets: []string{home(".b")}, Force: true})
		if err != nil {
			t.Fatal(err)
		}
		if read(".b") != "b from the repo\n" {
			t.Errorf(".b = %q after Force, want the repo's version", read(".b"))
		}
	})
}

func TestClientInit(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{
		".chezmoi.toml.tmpl": "sourceDir = {{ .chezmoi.config.sourceDir | quote }}\n[data]\n  fact = 1\n",
	})
	c := &Client{}
	if err := c.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(d.Home, ".config/chezmoi/chezmoi.toml")
	before, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(before), "fact = 1") {
		t.Fatalf("config after Init() = %q, %v; want the template's facts", before, err)
	}
	testenv.WriteFile(t, filepath.Join(d.Source, ".chezmoi.toml.tmpl"),
		"sourceDir = {{ .chezmoi.config.sourceDir | quote }}\n[data]\n  fact = 2\n")
	if err := c.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(config); !strings.Contains(string(after), "fact = 2") {
		t.Errorf("config after a template change = %q, want the new fact", after)
	}
}

func TestClientCatDiffAndEntryStates(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{
		"dot_a":         "a\n",
		"symlink_dot_l": "target",
	})
	c := &Client{}
	ctx := t.Context()
	a, l := filepath.Join(d.Home, ".a"), filepath.Join(d.Home, ".l")

	states, err := c.EntryStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ path, typ, contents string }{{a, "file", "a\n"}, {l, "symlink", "target"}} {
		got := states[tt.path]
		sum := sha256.Sum256([]byte(tt.contents))
		if got.Type != tt.typ || got.ContentsSHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("EntryStates()[%s] = %+v, want type %s and the hash of %q", tt.path, got, tt.typ, tt.contents)
		}
	}

	testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "a from the repo\n")
	if got, err := c.Cat(ctx, a); err != nil || string(got) != "a from the repo\n" {
		t.Errorf("Cat(.a) = %q, %v; want the repo's version", got, err)
	}
	if got, err := c.Cat(ctx, l); err != nil || strings.TrimSpace(string(got)) != "target" {
		t.Errorf("Cat(.l) = %q, %v; want the link's target", got, err)
	}

	diff, err := c.Diff(ctx, []string{a})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diff), "-a\n+a from the repo\n") {
		t.Errorf("Diff() = %q, want the change from a to the repo's version", diff)
	}
	if diff, err := c.Diff(ctx, nil); err != nil || diff != nil {
		t.Errorf("Diff(nil) = %q, %v; want nothing", diff, err)
	}
}
