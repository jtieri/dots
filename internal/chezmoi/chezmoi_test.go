package chezmoi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dot/internal/testenv"
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
	})
	c := &Client{}
	ctx := t.Context()

	t.Run("SourceDir", func(t *testing.T) {
		got, err := c.SourceDir(ctx)
		if err != nil || got != d.Source {
			t.Errorf("SourceDir() = %q, %v; want %q", got, err, d.Source)
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
	})

	t.Run("ManagedDirs", func(t *testing.T) {
		got, err := c.ManagedDirs(ctx)
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
			t.Errorf("ManagedDirs() mismatch (-want +got):\n%s", diff)
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

	t.Run("program not found", func(t *testing.T) {
		c := &Client{Program: "dot-no-such-chezmoi"}
		if _, err := c.Status(ctx); err == nil {
			t.Error("Status() error = nil, want an error")
		}
	})
}
