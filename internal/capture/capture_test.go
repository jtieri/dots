package capture

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/jtieri/dots/internal/chezmoi"
	"github.com/jtieri/dots/internal/dotfiles"
	"github.com/jtieri/dots/internal/testenv"
)

const zedSource = "dot_config/zed/modify_private_settings.json"

// repoFiles are the files every test starts with.
var repoFiles = map[string]string{
	"dot_a":           "a\n",
	"private_dot_p":   "p\n",
	"private_dot_d/f": "f\n",
	"private_dot_d/h": "h\n",
	"dot_t.tmpl":      "t {{ .chezmoi.os }}\n",
	"create_dot_c":    "c\n",
	"modify_dot_m":    "{{/* chezmoi:modify-template */}}{{ .chezmoi.stdin }}",
	zedSource:         zedHead + zedBody,
	"dot_ssh/config":  "Host *\n",
	".chezmoiremove":  ".old\n.config/stale\n",
	".chezmoiignore":  ".config/ignored\n",
}

type fixture struct {
	*testenv.Dotfiles
	t *testing.T
	c *Capturer
}

// newFixture sets up the repo files and returns a Capturer for them. cz runs
// chezmoi; nil runs it as dots does.
func newFixture(t *testing.T, cz *chezmoi.Client) *fixture {
	t.Helper()
	f := &fixture{Dotfiles: testenv.NewDotfiles(t, repoFiles), t: t}
	if cz == nil {
		cz = &chezmoi.Client{}
	}
	d, err := dotfiles.Open(t.Context(), f.Home, cz)
	if err != nil {
		t.Fatal(err)
	}
	f.c = &Capturer{d: d, opts: Options{
		Protected:       []string{".ssh/id_*", ".gnupg"},
		ModifyTemplates: []ModifyTemplate{{File: ".config/zed/settings.json", Keep: themeKeep}},
	}}
	return f
}

// refresh makes the Capturer see changes made since it was created.
func (f *fixture) refresh() {
	f.t.Helper()
	c, err := New(f.t.Context(), f.c.d, f.c.opts)
	if err != nil {
		f.t.Fatal(err)
	}
	f.c = c
}

func (f *fixture) home(rel string) string   { return filepath.Join(f.Home, rel) }
func (f *fixture) source(rel string) string { return filepath.Join(f.Source, rel) }

func (f *fixture) preview(rel string) (*Plan, error) {
	f.t.Helper()
	f.refresh()
	return f.c.Preview(f.t.Context(), f.home(rel))
}

func (f *fixture) mustPreview(rel string) *Plan {
	f.t.Helper()
	p, err := f.preview(rel)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) capture(p *Plan) []string {
	f.t.Helper()
	reset, err := f.c.Capture(f.t.Context(), p)
	if err != nil {
		f.t.Fatal(err)
	}
	return reset
}

// inSync checks that chezmoi sees no difference at rel.
func (f *fixture) inSync(rel string) {
	f.t.Helper()
	if out := f.Chezmoi(f.t, "status", f.home(rel)); out != "" {
		f.t.Errorf("chezmoi status %s = %q, want nothing", rel, out)
	}
}

func chmod(t *testing.T, name string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureModified(t *testing.T) {
	f := newFixture(t, nil)
	testenv.WriteFile(t, f.home(".a"), "edited\n")
	p := f.mustPreview(".a")
	if !strings.Contains(string(p.Diff), "-a\n+edited\n") {
		t.Errorf("Diff = %q, want the change to dot_a", p.Diff)
	}
	if p.New != nil || p.Unprivate != nil || p.RepoNewer != nil || p.Secret != "" {
		t.Errorf("Preview() = %+v, want nothing to ask about", p)
	}
	if testenv.ReadFile(t, f.source("dot_a")) != "a\n" {
		t.Fatal("Preview() changed the repo")
	}
	if reset := f.capture(p); reset != nil {
		t.Errorf("Capture() reset = %v, want none", reset)
	}
	if got := testenv.ReadFile(t, f.source("dot_a")); got != "edited\n" {
		t.Errorf("dot_a = %q, want the captured file", got)
	}
	f.inSync(".a")
}

func TestCaptureUnchanged(t *testing.T) {
	f := newFixture(t, nil)
	p := f.mustPreview(".a")
	if len(p.Diff) != 0 {
		t.Errorf("Diff = %q, want none", p.Diff)
	}
	if reset := f.capture(p); reset != nil {
		t.Errorf("Capture() reset = %v, want none", reset)
	}
}

func TestCaptureNew(t *testing.T) {
	f := newFixture(t, nil)
	testenv.WriteFile(t, f.home(".n/x"), "x\n")
	testenv.WriteFile(t, f.home(".n/sub/y"), "y\n")
	chmod(t, f.home(".n/sub/y"), 0o600)
	p := f.mustPreview(".n")
	if diff := cmp.Diff([]string{".n/sub/y", ".n/x"}, p.New); diff != "" {
		t.Errorf("New mismatch (-want +got):\n%s", diff)
	}
	f.capture(p)
	if testenv.ReadFile(t, f.source("dot_n/x")) != "x\n" || testenv.ReadFile(t, f.source("dot_n/sub/private_y")) != "y\n" {
		t.Error("Capture() did not add the folder")
	}
	f.inSync(".n")
}

func TestCaptureUnprivate(t *testing.T) {
	tests := []struct {
		name, rel string
		mode      fs.FileMode
		wantGone  string
		wantNow   string
	}{
		{name: "file", rel: ".p", mode: 0o644, wantGone: "private_dot_p", wantNow: "dot_p"},
		{name: "folder", rel: ".d", mode: 0o755, wantGone: "private_dot_d", wantNow: "dot_d/f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			chmod(t, f.home(tt.rel), tt.mode)
			p := f.mustPreview(tt.rel)
			if diff := cmp.Diff([]string{tt.rel}, p.Unprivate); diff != "" || p.New != nil {
				t.Errorf("Unprivate mismatch (-want +got):\n%s; New = %v", diff, p.New)
			}
			f.capture(p)
			if _, err := os.Lstat(f.source(tt.wantGone)); err == nil {
				t.Errorf("%s still exists", tt.wantGone)
			}
			if _, err := os.Lstat(f.source(tt.wantNow)); err != nil {
				t.Error(err)
			}
			f.inSync(tt.rel)
		})
	}
}

// TestCaptureOtherAttributes checks that a file whose other attributes change
// is neither new nor made readable to others.
func TestCaptureOtherAttributes(t *testing.T) {
	tests := []struct {
		name, rel string
		mode      fs.FileMode
		wantNow   string
	}{
		{name: "executable", rel: ".a", mode: 0o755, wantNow: "executable_dot_a"},
		{name: "private and executable", rel: ".p", mode: 0o700, wantNow: "private_executable_dot_p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			chmod(t, f.home(tt.rel), tt.mode)
			p := f.mustPreview(tt.rel)
			if p.New != nil || p.Unprivate != nil {
				t.Errorf("Preview() New = %v, Unprivate = %v; want neither", p.New, p.Unprivate)
			}
			f.capture(p)
			if _, err := os.Lstat(f.source(tt.wantNow)); err != nil {
				t.Error(err)
			}
			f.inSync(tt.rel)
		})
	}
}

func TestCaptureSecret(t *testing.T) {
	f := newFixture(t, nil)
	// Built here so that the source holds nothing that looks like a key.
	key := "-----BEGIN OPENSSH " + "PRIVATE KEY-----\n" + strings.Repeat("b3BlbnNzaC1rZXktdjEAAAAA", 4) +
		"\n-----END OPENSSH " + "PRIVATE KEY-----\n"
	testenv.WriteFile(t, f.home(".k"), key)
	p := f.mustPreview(".k")
	if want := "~/.k:1: Identified a Private Key"; !strings.HasPrefix(p.Secret, want) {
		t.Errorf("Secret = %q, want chezmoi's finding, starting %q", p.Secret, want)
	}
	if diff := cmp.Diff([]string{".k"}, p.New); diff != "" {
		t.Errorf("New mismatch (-want +got):\n%s", diff)
	}
	f.capture(p)
	f.inSync(".k")
}

func TestCaptureRepoNewer(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture)
		rel   string
		want  []string
	}{
		{
			name: "changed in both",
			setup: func(f *fixture) {
				testenv.WriteFile(f.t, f.source("dot_a"), "a from the repo\n")
				testenv.WriteFile(f.t, f.home(".a"), "a from here\n")
			},
			rel: ".a", want: []string{".a"},
		},
		{
			name:  "pulled but not applied",
			setup: func(f *fixture) { testenv.WriteFile(f.t, f.source("dot_a"), "a from the repo\n") },
			rel:   ".a", want: []string{".a"},
		},
		{
			name: "in a folder",
			setup: func(f *fixture) {
				testenv.WriteFile(f.t, f.source("private_dot_d/f"), "f from the repo\n")
				testenv.WriteFile(f.t, f.home(".d/g"), "g\n")
			},
			rel: ".d", want: []string{".d/f"},
		},
		{
			// An empty file in the repo deletes its target.
			name:  "deleted by the repo",
			setup: func(f *fixture) { testenv.WriteFile(f.t, f.source("dot_ssh/config"), "") },
			rel:   ".ssh/config", want: []string{".ssh/config"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			tt.setup(f)
			p := f.mustPreview(tt.rel)
			if diff := cmp.Diff(tt.want, p.RepoNewer); diff != "" {
				t.Errorf("RepoNewer mismatch (-want +got):\n%s", diff)
			}
			f.capture(p)
			f.inSync(tt.rel)
		})
	}
}

func TestCaptureResetPermissions(t *testing.T) {
	f := newFixture(t, nil)
	testenv.WriteFile(t, f.home(".a"), "edited\n")
	chmod(t, f.home(".a"), 0o664)
	if reset := f.capture(f.mustPreview(".a")); !cmp.Equal(reset, []string{".a"}) {
		t.Errorf("Capture() reset = %v, want [.a]", reset)
	}
}

func TestCaptureRefuses(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *fixture)
		target  string // relative to the home directory, unless absolute
		wantErr string
	}{
		{name: "outside home", target: "/etc/hostname", wantErr: "/etc/hostname is not in the home directory"},
		{name: "above home", target: "..", wantErr: "is not in the home directory"},
		{name: "home itself", target: ".", wantErr: "the home directory itself cannot be named"},
		{name: "the repo", target: "dotfiles", wantErr: "~/dotfiles is the dotfiles repo or holds it"},
		{name: "in the repo", target: "dotfiles/home/dot_a", wantErr: "is the dotfiles repo or holds it"},
		{name: "missing", target: ".nope", wantErr: "~/.nope does not exist on this machine; dots apply puts back the repo's version, and dots rm deletes it"},
		{
			name:    "protected",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".ssh/id_ed25519"), "key\n") },
			target:  ".ssh/id_ed25519",
			wantErr: "~/.ssh/id_ed25519 is protected by the capture.protected setting",
		},
		{
			name:    "in a protected folder",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".gnupg/private-keys-v1.d/x.key"), "key\n") },
			target:  ".gnupg/private-keys-v1.d",
			wantErr: "~/.gnupg/private-keys-v1.d is protected",
		},
		{
			name:    "folder holding a protected file",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".ssh/id_rsa"), "key\n") },
			target:  ".ssh",
			wantErr: "~/.ssh/id_rsa is protected",
		},
		{
			name:    "on .chezmoiremove",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".old"), "old\n") },
			target:  ".old",
			wantErr: "the repo's .chezmoiremove deletes ~/.old on every machine",
		},
		{
			name:    "folder holding a file on .chezmoiremove",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".config/stale"), "old\n") },
			target:  ".config",
			wantErr: "the repo's .chezmoiremove deletes ~/.config/stale on every machine",
		},
		{
			name:    "template",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".t"), "edited\n") },
			target:  ".t",
			wantErr: "the repo makes ~/.t from home/dot_t.tmpl, and capturing would replace it with a plain copy",
		},
		{
			name:    "modify template not in the setting",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".m"), "edited\n") },
			target:  ".m",
			wantErr: "the repo makes ~/.m from home/modify_dot_m",
		},
		{
			name:    "folder holding a modify template",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".config/zed/settings.json"), "{}\n") },
			target:  ".config/zed",
			wantErr: "the repo keeps ~/.config/zed/settings.json as a modify template; capture it on its own",
		},
		{
			name:   "create_ file",
			setup:  func(f *fixture) { testenv.WriteFile(f.t, f.home(".c"), "edited\n") },
			target: ".c",
			wantErr: "the repo only creates ~/.c when it is missing, so changes to it stay on this machine; " +
				"to change what new machines get, edit home/create_dot_c",
		},
		{
			name:    "ignored",
			setup:   func(f *fixture) { testenv.WriteFile(f.t, f.home(".config/ignored"), "x\n") },
			target:  ".config/ignored",
			wantErr: "~/.config/ignored is ignored on this machine (see .chezmoiignore in the repo)",
		},
		{
			name: "modify template replaced by a plain copy",
			setup: func(f *fixture) {
				if err := os.Rename(f.source(zedSource), f.source("dot_config/zed/settings.json")); err != nil {
					f.t.Fatal(err)
				}
			},
			target:  ".config/zed/settings.json",
			wantErr: "the repo should keep ~/.config/zed/settings.json as a modify template, but it is home/dot_config/zed/settings.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			if tt.setup != nil {
				tt.setup(f)
			}
			target := tt.target
			if !filepath.IsAbs(target) {
				target = filepath.Join(f.Home, target)
			}
			f.refresh()
			before := sourceTree(t, f.Source)
			p, err := f.c.Preview(t.Context(), target)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Preview() = %+v, %v; want an error containing %q", p, err, tt.wantErr)
			}
			if diff := cmp.Diff(before, sourceTree(t, f.Source)); diff != "" {
				t.Errorf("Preview() changed the repo (-before +after):\n%s", diff)
			}
		})
	}
}

func TestCaptureRefusesFolderHoldingRepo(t *testing.T) {
	f := newFixture(t, nil)
	f.refresh()
	// As if the repo were kept further down in the home directory.
	f.c.d.Repo.Dir = f.home(".config/zed/repo")
	if _, err := f.c.Preview(t.Context(), f.home(".config")); err == nil ||
		err.Error() != "~/.config is the dotfiles repo or holds it" {
		t.Errorf("Preview() error = %v, want the folder refused", err)
	}
}

func TestCaptureTemplate(t *testing.T) {
	f := newFixture(t, nil)
	settings := f.home(".config/zed/settings.json")
	file := "// Zed settings\n{\n  \"theme\": {\n    \"dark\": \"Mine\"\n  },\n  \"ui_font_size\": 18\n}\n"
	testenv.WriteFile(t, settings, file)
	p := f.mustPreview(".config/zed/settings.json")
	for _, want := range []string{
		"--- a/home/" + zedSource + "\n",
		"-  \"ui_font_size\": 16\n",
		"+  \"ui_font_size\": 18\n",
	} {
		if !strings.Contains(string(p.Diff), want) {
			t.Errorf("Diff = %q, want it to contain %q", p.Diff, want)
		}
	}
	if strings.Contains(string(p.Diff), "Mine") {
		t.Errorf("Diff = %q, want this machine's theme left out", p.Diff)
	}
	f.capture(p)
	got := testenv.ReadFile(t, f.source(zedSource))
	if !strings.HasPrefix(got, zedHead) || !strings.Contains(got, "{{ $theme }},\n  \"ui_font_size\": 18") {
		t.Errorf("template = %q, want the logic kept and the settings captured", got)
	}
	if info, err := os.Stat(f.source(zedSource)); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("template mode = %v, %v; want it kept", info.Mode(), err)
	}
	f.inSync(".config/zed/settings.json")
}

// TestCaptureTemplateRepoNewer checks that a theme switch since the last
// apply is not taken for a change in the repo, while a repo change this
// machine has not applied still is.
func TestCaptureTemplateRepoNewer(t *testing.T) {
	f := newFixture(t, nil)
	settings := ".config/zed/settings.json"
	edited := "// Zed settings\n{\n  \"theme\": {\n    \"dark\": \"Mine\"\n  },\n  \"ui_font_size\": 18\n}\n"
	testenv.WriteFile(t, f.home(settings), edited)
	if p := f.mustPreview(settings); p.RepoNewer != nil {
		t.Errorf("RepoNewer after a theme switch = %v, want none", p.RepoNewer)
	}

	f = newFixture(t, nil)
	testenv.WriteFile(t, f.source(zedSource), strings.Replace(zedHead+zedBody, "16", "20", 1))
	if p := f.mustPreview(settings); !cmp.Equal(p.RepoNewer, []string{settings}) {
		t.Errorf("RepoNewer after a repo change = %v, want [%s]", p.RepoNewer, settings)
	}
}

func TestCaptureTemplateRefuses(t *testing.T) {
	f := newFixture(t, nil)
	testenv.WriteFile(t, f.home(".config/zed/settings.json"), "{}\n")
	_, err := f.preview(".config/zed/settings.json")
	if want := "cannot rewrite the template home/" + zedSource + " from ~/.config/zed/settings.json: nothing in the file matches"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Preview() error = %v, want one containing %q", err, want)
	}
}

// TestCaptureRestores checks that a capture after which the repo does not
// produce the file puts the repo back exactly as it was.
func TestCaptureRestores(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture)
		rel   string
	}{
		{name: "modified", setup: func(f *fixture) { testenv.WriteFile(f.t, f.home(".a"), "edited\n") }, rel: ".a"},
		{
			name: "new folder",
			setup: func(f *fixture) {
				testenv.WriteFile(f.t, f.home(".n/x"), "x\n")
				testenv.WriteFile(f.t, f.home(".n/sub/y"), "y\n")
			},
			rel: ".n",
		},
		{
			name: "no longer private",
			setup: func(f *fixture) {
				chmod(f.t, f.home(".d"), 0o755)
				testenv.WriteFile(f.t, f.home(".d/f"), "edited\n")
				testenv.WriteFile(f.t, f.home(".d/g"), "g\n")
			},
			rel: ".d",
		},
		{name: "file no longer private", setup: func(f *fixture) { chmod(f.t, f.home(".p"), 0o644) }, rel: ".p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, breakingChezmoi(t))
			tt.setup(f)
			p := f.mustPreview(tt.rel)
			before := sourceTree(t, f.Source)
			_, err := f.c.Capture(t.Context(), p)
			want := "after capturing, the repo does not produce ~/"
			if err == nil || !strings.Contains(err.Error(), want) || !strings.HasSuffix(err.Error(), "; the repo was put back as it was") {
				t.Errorf("Capture() error = %v, want the check to fail and the repo put back", err)
			}
			if !errors.Is(err, ErrRestored) {
				t.Errorf("Capture() error = %v, want it to wrap ErrRestored", err)
			}
			if diff := cmp.Diff(before, sourceTree(t, f.Source)); diff != "" {
				t.Errorf("repo not put back (-before +after):\n%s", diff)
			}
		})
	}
}

func TestCaptureTemplateRestores(t *testing.T) {
	f := newFixture(t, nil)
	// The template keeps a block the setting does not match, so they
	// disagree about which part each machine keeps.
	f.c.opts.ModifyTemplates[0].Keep = regexp.MustCompile(`"ui_font_size": \d+`)
	testenv.WriteFile(t, f.home(".config/zed/settings.json"), "{\n  \"theme\": {},\n  \"ui_font_size\": 18\n}\n")
	f.refresh()
	p, err := f.c.Preview(t.Context(), f.home(".config/zed/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := sourceTree(t, f.Source)
	if _, err := f.c.Capture(t.Context(), p); err == nil || !strings.Contains(err.Error(), "put back") {
		t.Errorf("Capture() error = %v, want the check to fail", err)
	}
	if diff := cmp.Diff(before, sourceTree(t, f.Source)); diff != "" {
		t.Errorf("template not put back (-before +after):\n%s", diff)
	}
}

func TestCaptureChangedMeanwhile(t *testing.T) {
	f := newFixture(t, nil)
	testenv.WriteFile(t, f.home(".n/x"), "x\n")
	p := f.mustPreview(".n")
	before := sourceTree(t, f.Source)
	testenv.WriteFile(t, f.home(".n/later"), "later\n")
	_, err := f.c.Capture(t.Context(), p)
	if want := "~/.n changed while it was being captured; the repo was put back as it was"; err == nil || err.Error() != want {
		t.Errorf("Capture() error = %v, want %q", err, want)
	}
	if diff := cmp.Diff(before, sourceTree(t, f.Source)); diff != "" {
		t.Errorf("repo not put back (-before +after):\n%s", diff)
	}
}

// TestCaptureChangedMeanwhileUnsaved checks that a repo file the preview did
// not expect to change is reported rather than deleted when putting the repo
// back.
func TestCaptureChangedMeanwhileUnsaved(t *testing.T) {
	f := newFixture(t, nil)
	testenv.WriteFile(t, f.home(".d/g"), "g\n")
	p := f.mustPreview(".d")
	testenv.WriteFile(t, f.home(".d/h"), "h edited\n")
	_, err := f.c.Capture(t.Context(), p)
	for _, want := range []string{"~/.d changed while it was being captured", "home/private_dot_d/h changed and was not saved", "git status in"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Capture() error = %v, want it to contain %q", err, want)
		}
	}
	if errors.Is(err, ErrRestored) {
		t.Errorf("Capture() error = %v, want it not to claim the repo was put back", err)
	}
	if _, err := os.Lstat(f.source("private_dot_d/h")); err != nil {
		t.Errorf("the repo's copy of ~/.d/h is gone: %v", err)
	}
	if _, err := os.Lstat(f.source("private_dot_d/g")); err == nil {
		t.Error("the new file is still in the repo")
	}
}

func TestCaptureSymlink(t *testing.T) {
	f := newFixture(t, nil)
	if err := os.Symlink("a", f.home(".l")); err != nil {
		t.Fatal(err)
	}
	f.capture(f.mustPreview(".l"))
	f.inSync(".l")
	if err := os.Remove(f.home(".l")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", f.home(".l")); err != nil {
		t.Fatal(err)
	}
	p := f.mustPreview(".l")
	if p.New != nil || !strings.Contains(string(p.Diff), "+elsewhere") {
		t.Errorf("Preview() = %+v, want the link's new target", p)
	}
	f.capture(p)
	f.inSync(".l")
}

func TestCapturerLocal(t *testing.T) {
	f := newFixture(t, nil)
	testenv.WriteFile(t, f.home(".a"), "edited\n")
	if err := os.Remove(f.home(".ssh/config")); err != nil {
		t.Fatal(err)
	}
	f.refresh()
	if diff := cmp.Diff([]string{".a"}, f.c.Changed()); diff != "" {
		t.Errorf("Changed() mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{".ssh/config"}, f.c.Deleted()); diff != "" {
		t.Errorf("Deleted() mismatch (-want +got):\n%s", diff)
	}
}

// breakingChezmoi returns a client whose chezmoi, after every add that is not
// a dry run, appends a line to each file the add wrote, so that the repo no
// longer produces what was added.
func breakingChezmoi(t *testing.T) *chezmoi.Client {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "chezmoi")
	testenv.WriteFile(t, script, `#!/bin/sh
stamp=`+dir+`/stamp
touch "$stamp"
chezmoi "$@" || exit
case " $* " in
*" --dry-run "*) ;;
*" add "*) find "$HOME/dotfiles/home" -type f -newer "$stamp" -exec sh -c 'echo broken >>"$1"' sh {} \; ;;
esac
`)
	chmod(t, script, 0o755)
	return &chezmoi.Client{Program: script}
}

// sourceTree describes every entry in dir: its type, permissions and
// contents or link target.
func sourceTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	err := filepath.WalkDir(dir, func(name string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		desc := info.Mode().String()
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			desc += " -> " + target
		case info.Mode().IsRegular():
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			desc += " " + string(data)
		}
		rel, _ := filepath.Rel(dir, name)
		tree[rel] = desc
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
