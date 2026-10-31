package chezmoi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dots/internal/testenv"
)

func TestParseAddDiff(t *testing.T) {
	const src = "/src"
	tests := []struct {
		name string
		diff string
		want []SourceChange
	}{
		{name: "nothing", diff: ""},
		{
			name: "modified",
			diff: "diff --git a/home/dot_y b/home/dot_y\n" +
				"index 975fbec..a15e185 100644\n" +
				"--- a/home/dot_y\n" +
				"+++ b/home/dot_y\n" +
				"@@ -1 +1 @@\n" +
				"-y\n" +
				"+diff --git a/home/x b/home/x\n",
			want: []SourceChange{{Kind: Modified, Path: "/src/home/dot_y"}},
		},
		{
			name: "new folder",
			diff: "diff --git a/home/dot_sp ace b/home/dot_sp ace\n" +
				"new file mode 40755\n" +
				"index 0000000..e69de29\n" +
				"--- /dev/null\n" +
				"+++ b/home/dot_sp ace\n" +
				"diff --git a/home/dot_sp ace/a b.txt b/home/dot_sp ace/a b.txt\n" +
				"new file mode 100644\n" +
				"--- /dev/null\n" +
				"+++ b/home/dot_sp ace/a b.txt\n" +
				"@@ -0,0 +1 @@\n" +
				"+1\n" +
				"diff --git a/home/dot_sp ace/bin.dat b/home/dot_sp ace/bin.dat\n" +
				"new file mode 100644\n" +
				"Binary files /dev/null and b/home/dot_sp ace/bin.dat differ\n" +
				"diff --git a/home/dot_sp ace/tab\tq b/home/dot_sp ace/tab\tq\n" +
				"new file mode 100644\n" +
				"diff --git a/home/dot_x b/y b/home/dot_x b/y\n" +
				"new file mode 100644\n" +
				"diff --git a/home/ünï b/home/ünï\n" +
				"new file mode 100644\n",
			want: []SourceChange{
				{Kind: Created, Path: "/src/home/dot_sp ace", Dir: true},
				{Kind: Created, Path: "/src/home/dot_sp ace/a b.txt"},
				{Kind: Created, Path: "/src/home/dot_sp ace/bin.dat"},
				{Kind: Created, Path: "/src/home/dot_sp ace/tab\tq"},
				{Kind: Created, Path: "/src/home/dot_x b/y"},
				{Kind: Created, Path: "/src/home/ünï"},
			},
		},
		{
			name: "file no longer private",
			diff: "diff --git a/home/dot_priv b/home/dot_priv\n" +
				"new file mode 100644\n" +
				"--- /dev/null\n" +
				"+++ b/home/dot_priv\n" +
				"@@ -0,0 +1 @@\n" +
				"+secret\n" +
				"diff --git a/home/private_dot_priv b/home/private_dot_priv\n" +
				"deleted file mode 100644\n" +
				"--- a/home/private_dot_priv\n" +
				"+++ /dev/null\n" +
				"@@ -1 +0,0 @@\n" +
				"-secret\n",
			want: []SourceChange{
				{Kind: Created, Path: "/src/home/dot_priv"},
				{Kind: Deleted, Path: "/src/home/private_dot_priv"},
			},
		},
		{
			name: "folder no longer private",
			diff: "diff --git a/home/private_dot_q/z b/home/private_dot_q/z\n" +
				"index bca70f3..d7f758c 100644\n" +
				"--- a/home/private_dot_q/z\n" +
				"+++ b/home/private_dot_q/z\n" +
				"@@ -1 +1 @@\n" +
				"-q\n" +
				"+zz\n" +
				"diff --git a/home/private_dot_q b/home/dot_q\n" +
				"rename from home/private_dot_q\n" +
				"rename to home/dot_q\n",
			want: []SourceChange{
				{Kind: Modified, Path: "/src/home/private_dot_q/z"},
				{Kind: Renamed, Path: "/src/home/dot_q", From: "/src/home/private_dot_q"},
			},
		},
		{
			name: "deleted folder",
			diff: "diff --git a/dot_d b/dot_d\n" +
				"deleted file mode 40755\n",
			want: []SourceChange{{Kind: Deleted, Path: "/src/dot_d", Dir: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAddDiff(tt.diff, src)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseAddDiff() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseAddDiffErrors(t *testing.T) {
	for _, diff := range []string{
		"--- a/home/dot_y\n",
		"diff --git a/home/dot_a b/home/dot_b\n",
		"diff --git a/home/dot_a b/home/dot_b\nrename to home/dot_b\n",
		"diff --git a/home/dot_a\n",
		"diff --git a/ b/\n",
	} {
		if got, err := parseAddDiff(diff, "/src"); err == nil {
			t.Errorf("parseAddDiff(%q) = %v, want an error", diff, got)
		}
	}
}

func TestTrimQuestions(t *testing.T) {
	const diff = "diff --git a/x b/x\n+adding x would remove private attribute, continue (yes/no/all/quit)? \n"
	tests := []struct{ out, want string }{
		{"", ""},
		{diff, diff},
		{"adding .p would remove private attribute, continue (yes/no/all/quit)? " + diff, diff},
		{"adding .a would remove private attribute, continue (yes/no/all/quit)? " +
			"adding .b would remove template attribute, continue (yes/no/all/quit)? " + diff, diff},
		{"adding .p would remove private attribute, continue (yes/no/all/quit)? ", ""},
	}
	for _, tt := range tests {
		if got := string(trimQuestions([]byte(tt.out))); got != tt.want {
			t.Errorf("trimQuestions(%q) = %q, want %q", tt.out, got, tt.want)
		}
	}
}

func TestClientAdd(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{
		"dot_a":             "a\n",
		"private_dot_p":     "p\n",
		"private_dot_d/f":   "f\n",
		"dot_config/dot_ok": "ok\n",
	})
	c := &Client{}
	ctx := t.Context()
	home := func(rel string) string { return filepath.Join(d.Home, rel) }
	source := func(rel string) string { return filepath.Join(d.Source, rel) }
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("dry run", func(t *testing.T) {
		testenv.WriteFile(t, home(".a"), "edited\n")
		got, err := c.Add(ctx, home(".a"), AddOptions{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]SourceChange{{Kind: Modified, Path: source("dot_a")}}, got.Changes); diff != "" {
			t.Errorf("Add() mismatch (-want +got):\n%s", diff)
		}
		if !strings.Contains(string(got.Text), "+edited\n") {
			t.Errorf("Add() text = %q, want the diff", got.Text)
		}
		if read(source("dot_a")) != "a\n" {
			t.Error("a dry run changed the source directory")
		}
	})

	t.Run("add", func(t *testing.T) {
		got, err := c.Add(ctx, home(".a"), AddOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]SourceChange{{Kind: Modified, Path: source("dot_a")}}, got.Changes); diff != "" {
			t.Errorf("Add() mismatch (-want +got):\n%s", diff)
		}
		if read(source("dot_a")) != "edited\n" {
			t.Error("Add() did not change the source directory")
		}
	})

	t.Run("nothing to add", func(t *testing.T) {
		got, err := c.Add(ctx, home(".config/.ok"), AddOptions{})
		if err != nil || len(got.Changes) != 0 || len(got.Text) != 0 {
			t.Errorf("Add() = %+v, %v; want no changes", got, err)
		}
	})

	t.Run("no longer private", func(t *testing.T) {
		if err := os.Chmod(home(".p"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(home(".d"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Add(ctx, home(".p"), AddOptions{DryRun: true}); err == nil {
			t.Error("Add(.p) without Force: error = nil, want one")
		}
		got, err := c.Add(ctx, home(".p"), AddOptions{DryRun: true, Force: true})
		if err != nil {
			t.Fatal(err)
		}
		want := []SourceChange{{Kind: Created, Path: source("dot_p")}, {Kind: Deleted, Path: source("private_dot_p")}}
		if diff := cmp.Diff(want, got.Changes); diff != "" {
			t.Errorf("Add(.p) mismatch (-want +got):\n%s", diff)
		}
		if !strings.HasPrefix(string(got.Text), "diff --git ") {
			t.Errorf("Add(.p) text = %q, want only the diff", got.Text)
		}
		// chezmoi does not ask about a directory.
		got, err = c.Add(ctx, home(".d"), AddOptions{})
		if err != nil {
			t.Fatal(err)
		}
		want = []SourceChange{{Kind: Renamed, Path: source("dot_d"), From: source("private_dot_d")}}
		if diff := cmp.Diff(want, got.Changes); diff != "" {
			t.Errorf("Add(.d) mismatch (-want +got):\n%s", diff)
		}
		if read(source("dot_d/f")) != "f\n" {
			t.Error("Add(.d) did not rename the folder")
		}
	})

	t.Run("new folder", func(t *testing.T) {
		testenv.WriteFile(t, home(".n/x"), "x\n")
		got, err := c.Add(ctx, home(".n"), AddOptions{DryRun: true})
		want := []SourceChange{{Kind: Created, Path: source("dot_n"), Dir: true}, {Kind: Created, Path: source("dot_n/x")}}
		if diff := cmp.Diff(want, got.Changes); err != nil || diff != "" {
			t.Errorf("Add() error = %v, mismatch (-want +got):\n%s", err, diff)
		}
	})

	t.Run("secret", func(t *testing.T) {
		// Built here so that the source holds nothing that looks like a key.
		key := "-----BEGIN OPENSSH " + "PRIVATE KEY-----\n" + strings.Repeat("b3BlbnNzaC1rZXktdjEAAAAA", 4) +
			"\n-----END OPENSSH " + "PRIVATE KEY-----\n"
		testenv.WriteFile(t, home(".k"), key)
		if _, err := c.Add(ctx, home(".k"), AddOptions{DryRun: true}); err == nil ||
			!strings.Contains(err.Error(), "Private Key") {
			t.Errorf("Add() error = %v, want chezmoi's secret finding", err)
		}
		if _, err := c.Add(ctx, home(".k"), AddOptions{DryRun: true, Force: true}); err == nil {
			t.Error("Add(Force) error = nil, want Force to leave the scanner on")
		}
		if _, err := c.Add(ctx, home(".k"), AddOptions{Force: true}); err == nil {
			t.Error("Add(Force) error = nil, want the file refused")
		}
		if _, err := os.Stat(source("dot_k")); err == nil {
			t.Error("Add() added a file with a secret in it")
		}
		got, err := c.Add(ctx, home(".k"), AddOptions{DryRun: true, AllowSecrets: true})
		if err != nil || len(got.Changes) != 1 {
			t.Errorf("Add(AllowSecrets) = %+v, %v; want the file added", got, err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		if _, err := c.Add(ctx, home(".nope"), AddOptions{DryRun: true}); err == nil {
			t.Error("Add() of a missing file: error = nil, want an error")
		}
	})
}
