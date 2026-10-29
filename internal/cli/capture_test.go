package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtieri/dot/internal/testenv"
)

// newCaptureFixture returns a home whose repo holds ~/.a, ~/.b, a private
// ~/.p and ~/.c.
func newCaptureFixture(t *testing.T) *fixture {
	t.Helper()
	d := testenv.NewDotfiles(t, map[string]string{
		"dot_a":         "a\n",
		"dot_b":         "b\n",
		"private_dot_p": "p\n",
		"dot_c":         "c\n",
	})
	return &fixture{Dotfiles: d, t: t}
}

func (f *fixture) source(name string) string {
	f.t.Helper()
	return testenv.ReadFile(f.t, filepath.Join(f.Source, name))
}

const pushReminder = "\nRun dot push to commit and share the captured changes.\n"

func TestCapture(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, f.path(".a"), "edited\n")
	code, out, errOut := f.run("", false, "capture", "~/.a", f.path(".b"))
	if want := "Captured ~/.a.\n~/.b already matches the repo.\n" + pushReminder; code != exitOK || out != want {
		t.Errorf("capture = %d, %q, stderr %q; want 0, %q", code, out, errOut, want)
	}
	if f.source("dot_a") != "edited\n" {
		t.Errorf("dot_a = %q, want the captured file", f.source("dot_a"))
	}
}

func TestCaptureRelativePath(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, f.path(".a"), "edited\n")
	t.Chdir(f.Home)
	if code, _, errOut := f.run("", false, "capture", ".a"); code != exitOK || f.source("dot_a") != "edited\n" {
		t.Errorf("capture .a = %d, stderr %q; want it captured", code, errOut)
	}
}

func TestCaptureNothingToCapture(t *testing.T) {
	f := newCaptureFixture(t)
	code, out, _ := f.run("", false, "capture", "~/.a")
	if want := "~/.a already matches the repo.\n"; code != exitOK || out != want {
		t.Errorf("capture = %d, %q; want 0, %q", code, out, want)
	}
}

func TestCaptureNewFile(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		terminal     bool
		args         []string
		wantCode     int
		wantCaptured bool
		wantOut      string
		wantErrOut   string
	}{
		{name: "yes", input: "y\n", terminal: true, wantCode: exitOK, wantCaptured: true},
		{name: "enter leaves it", input: "\n", terminal: true, wantCode: exitOutOfSync, wantErrOut: "Left ~/.n as it was.\n"},
		{name: "diff, then yes", input: "d\ny\n", terminal: true, wantCode: exitOK, wantCaptured: true, wantOut: "+++ b/home/dot_n\n"},
		{name: "unknown answer, then no", input: "x\nn\n", terminal: true, wantCode: exitOutOfSync, wantErrOut: "Please answer with one of the letters in brackets."},
		{name: "input ends", terminal: true, wantCode: exitError, wantErrOut: "dot: nothing more captured: no answer was given\n"},
		{name: "no terminal", wantCode: exitError, wantErrOut: "dot: nothing captured from ~/.n: there is no terminal to ask at; use --yes\n"},
		{name: "no terminal, yes", args: []string{"--yes"}, wantCode: exitOK, wantCaptured: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCaptureFixture(t)
			testenv.WriteFile(t, f.path(".n"), "n\n")
			code, out, errOut := f.run(tt.input, tt.terminal, append([]string{"capture", "~/.n"}, tt.args...)...)
			if code != tt.wantCode {
				t.Errorf("capture = %d, want %d; stderr %q", code, tt.wantCode, errOut)
			}
			if !strings.HasPrefix(errOut, "New to the repo, so every machine will get it\n  ~/.n\n") {
				t.Errorf("stderr = %q, want it to start with what is new", errOut)
			}
			if !strings.Contains(errOut, tt.wantErrOut) || !strings.Contains(out, tt.wantOut) {
				t.Errorf("stdout %q, stderr %q; want them to contain %q and %q", out, errOut, tt.wantOut, tt.wantErrOut)
			}
			_, err := os.Stat(filepath.Join(f.Source, "dot_n"))
			if captured := err == nil; captured != tt.wantCaptured {
				t.Errorf("dot_n in the repo: %t, want %t", captured, tt.wantCaptured)
			}
			if got := strings.HasSuffix(out, pushReminder); got != tt.wantCaptured {
				t.Errorf("stdout = %q; want the push reminder %t", out, tt.wantCaptured)
			}
		})
	}
}

func TestCaptureNeedsTerminal(t *testing.T) {
	// Built here so that the source holds nothing that looks like a key.
	key := "-----BEGIN OPENSSH " + "PRIVATE KEY-----\n" + strings.Repeat("b3BlbnNzaC1rZXktdjEAAAAA", 4) +
		"\n-----END OPENSSH " + "PRIVATE KEY-----\n"
	tests := []struct {
		name     string
		setup    func(f *fixture)
		file     string
		source   string
		wantNote string
	}{
		{
			name:     "no longer private",
			setup:    func(f *fixture) { chmodFile(f.t, f.path(".p"), 0o644) },
			file:     "~/.p",
			source:   "dot_p",
			wantNote: "Private in the repo; capturing makes it readable to others on every machine\n  ~/.p\n",
		},
		{
			name:     "secret",
			setup:    func(f *fixture) { testenv.WriteFile(f.t, f.path(".a"), key) },
			file:     "~/.a",
			source:   "dot_a",
			wantNote: "chezmoi's secret scanner found what may be a secret\n  ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCaptureFixture(t)
			tt.setup(f)
			before, _ := os.ReadFile(filepath.Join(f.Source, tt.source))

			code, _, errOut := f.run("", false, "capture", "--yes", tt.file)
			want := "dot: nothing captured from " + tt.file + ": it needs a yes at a terminal, which --yes cannot give\n"
			if code != exitError || !strings.Contains(errOut, tt.wantNote) || !strings.HasSuffix(errOut, want) {
				t.Errorf("capture --yes = %d, stderr %q; want 2 and %q", code, errOut, want)
			}
			if after, _ := os.ReadFile(filepath.Join(f.Source, tt.source)); string(after) != string(before) {
				t.Errorf("%s changed without a yes", tt.source)
			}

			code, _, errOut = f.run("y\n", true, "capture", "--yes", tt.file)
			if code != exitOK || !strings.Contains(errOut, "Capture "+tt.file+"? [y/d/N] ") {
				t.Errorf("capture --yes at a terminal = %d, stderr %q; want it to ask and capture", code, errOut)
			}
			if _, err := os.Stat(filepath.Join(f.Source, tt.source)); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestCaptureRefusalLeavesTheRest(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, f.path(".ssh/id_ed25519"), "key\n")
	testenv.WriteFile(t, f.path(".a"), "edited\n")
	code, out, errOut := f.run("", false, "capture", "~/.ssh/id_ed25519", "~/.a")
	want := "dot: ~/.ssh/id_ed25519 is protected by the capture.protected setting, so it is never copied into the repo\n"
	if code != exitError || errOut != want {
		t.Errorf("capture = %d, stderr %q; want 2, %q", code, errOut, want)
	}
	if out != "Captured ~/.a.\n"+pushReminder || f.source("dot_a") != "edited\n" {
		t.Errorf("stdout = %q; want ~/.a captured", out)
	}
}

func TestCaptureWalk(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, f.path(".a"), "a edited\n")
	testenv.WriteFile(t, f.path(".b"), "b edited\n")
	if err := os.Remove(f.path(".c")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := f.run("y\nn\n", true, "capture")
	if code != exitOutOfSync {
		t.Errorf("capture = %d, want %d; stderr %q", code, exitOutOfSync, errOut)
	}
	wantErrOut := "Deleted on this machine, so not captured; dot apply puts it back\n  ~/.c\n\n" +
		"~/.a changed on this machine.\nCapture ~/.a? [y/d/N] \n" +
		"~/.b changed on this machine.\nCapture ~/.b? [y/d/N] Left ~/.b as it was.\n"
	if errOut != wantErrOut {
		t.Errorf("stderr =\n%s\nwant\n%s", errOut, wantErrOut)
	}
	if out != "Captured ~/.a.\n"+pushReminder {
		t.Errorf("stdout = %q", out)
	}
	if f.source("dot_a") != "a edited\n" || f.source("dot_b") != "b\n" {
		t.Errorf("dot_a = %q, dot_b = %q; want only ~/.a captured", f.source("dot_a"), f.source("dot_b"))
	}
}

func TestCaptureWalkWithoutTerminal(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, f.path(".a"), "a edited\n")
	code, _, errOut := f.run("", false, "capture")
	if want := "dot: nothing captured: there is no terminal to ask at; name the files to capture, or use --yes\n"; code != exitError || errOut != want {
		t.Errorf("capture = %d, stderr %q; want 2, %q", code, errOut, want)
	}
	if code, out, errOut := f.run("", false, "capture", "--yes"); code != exitOK || out != "Captured ~/.a.\n"+pushReminder {
		t.Errorf("capture --yes = %d, %q, stderr %q; want ~/.a captured", code, out, errOut)
	}
}

func TestCaptureWalkNothingChanged(t *testing.T) {
	f := newCaptureFixture(t)
	code, out, _ := f.run("", false, "capture")
	if want := "Nothing to capture; no files changed on this machine.\n"; code != exitOK || out != want {
		t.Errorf("capture = %d, %q; want 0, %q", code, out, want)
	}
}

func TestCaptureWalkOnlyDeleted(t *testing.T) {
	f := newCaptureFixture(t)
	if err := os.Remove(f.path(".c")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := f.run("", false, "capture")
	if code != exitOutOfSync || out != "" || !strings.HasPrefix(errOut, "Deleted on this machine") {
		t.Errorf("capture = %d, %q, stderr %q; want 1 and the deleted file listed", code, out, errOut)
	}
}

func TestCaptureRepoNewer(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, filepath.Join(f.Source, "dot_a"), "a from the repo\n")
	testenv.WriteFile(t, f.path(".a"), "a from here\n")
	code, _, errOut := f.run("", false, "capture", "~/.a")
	want := "Changed in the repo since this machine last applied it; capturing replaces the repo's version\n  ~/.a\n" +
		"dot: nothing captured from ~/.a: there is no terminal to ask at; use --yes\n"
	if code != exitError || errOut != want {
		t.Errorf("capture = %d, stderr %q; want 2, %q", code, errOut, want)
	}
	if code, _, _ := f.run("", false, "capture", "--yes", "~/.a"); code != exitOK || f.source("dot_a") != "a from here\n" {
		t.Errorf("capture --yes = %d, dot_a = %q; want ~/.a captured", code, f.source("dot_a"))
	}
}

func TestCaptureNewFolder(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, f.path(".n/x"), "x\n")
	testenv.WriteFile(t, f.path(".n/y"), "y\n")
	code, _, errOut := f.run("", false, "capture", "--yes", "~/.n")
	if want := "New to the repo, so every machine will get them\n  ~/.n/x\n  ~/.n/y\n"; code != exitOK || errOut != want {
		t.Errorf("capture = %d, stderr %q; want 0, %q", code, errOut, want)
	}
}

func TestCaptureResetPermissions(t *testing.T) {
	f := newCaptureFixture(t)
	testenv.WriteFile(t, f.path(".a"), "edited\n")
	chmodFile(t, f.path(".a"), 0o664)
	code, out, _ := f.run("", false, "capture", "~/.a")
	want := "Captured ~/.a.\n  The repo cannot keep the permissions of ~/.a, so dot apply will reset them.\n" + pushReminder
	if code != exitOK || out != want {
		t.Errorf("capture = %d, %q; want 0, %q", code, out, want)
	}
}

func chmodFile(t *testing.T, name string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}
