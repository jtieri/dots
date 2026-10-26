package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/lock"
	"github.com/jtieri/dot/internal/testenv"
)

func TestRefusesRoot(t *testing.T) {
	testenv.NewDotfiles(t, map[string]string{"dot_bashrc": "bashrc\n"})
	streams, _, out, errOut := iostreams.Test()
	a := &app{streams: streams, euid: 0, lockPath: filepath.Join(t.TempDir(), "lock")}
	if got := execute([]string{"status"}, a); got != exitError {
		t.Errorf("execute(status) as root = %d, want %d", got, exitError)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "dot: refusing to run as root") {
		t.Errorf("stdout %q, stderr %q; want nothing and a refusal", out, errOut)
	}

	// Commands that only print text work as root.
	for _, args := range [][]string{nil, {"help"}, {"--version"}, {"status", "--help"}, {"completion", "bash"}} {
		streams, _, _, _ := iostreams.Test()
		a := &app{streams: streams, euid: 0, lockPath: filepath.Join(t.TempDir(), "lock")}
		if got := execute(args, a); got != exitOK {
			t.Errorf("execute(%q) as root = %d, want %d", args, got, exitOK)
		}
	}
}

func TestLockBusy(t *testing.T) {
	testenv.NewDotfiles(t, map[string]string{"dot_bashrc": "bashrc\n"})
	path := filepath.Join(t.TempDir(), "lock")
	release, err := lock.Acquire(t.Context(), path, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	streams, _, out, errOut := iostreams.Test()
	a := &app{streams: streams, euid: 1000, lockPath: path}
	if got := execute([]string{"status"}, a); got != exitError {
		t.Errorf("execute(status) while locked = %d, want %d", got, exitError)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "dot: another dot is running") {
		t.Errorf("stdout %q, stderr %q; want nothing and a message about the other dot", out, errOut)
	}
}

func TestLockWaitsAtTerminal(t *testing.T) {
	testenv.NewDotfiles(t, map[string]string{"dot_bashrc": "bashrc\n"})
	path := filepath.Join(t.TempDir(), "lock")
	release, err := lock.Acquire(t.Context(), path, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(300*time.Millisecond, release)

	streams, _, out, errOut := iostreams.Test()
	streams.ErrIsTerminal = true
	a := &app{streams: streams, euid: 1000, lockPath: path}
	if got := execute([]string{"status"}, a); got != exitOK {
		t.Errorf("execute(status) = %d, want %d; stderr %q", got, exitOK, errOut)
	}
	if errOut.String() != "Waiting for another dot to finish...\n" || out.String() != "In sync.\n" {
		t.Errorf("stdout %q, stderr %q; want In sync. after waiting", out, errOut)
	}
}

func TestLockReleased(t *testing.T) {
	testenv.NewDotfiles(t, map[string]string{"dot_bashrc": "bashrc\n"})
	path := filepath.Join(t.TempDir(), "lock")
	for i := range 2 {
		streams, _, _, errOut := iostreams.Test()
		a := &app{streams: streams, euid: 1000, lockPath: path}
		if got := execute([]string{"status"}, a); got != exitOK {
			t.Fatalf("run %d of status = %d, want %d; stderr %q", i+1, got, exitOK, errOut)
		}
	}
}
