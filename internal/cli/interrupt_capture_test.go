package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jtieri/dots/internal/iostreams"
	"github.com/jtieri/dots/internal/testenv"
)

// TestInterruptCapture checks that Ctrl-C while chezmoi adds a file stops dots
// capture with exit code 130 and puts the repo back as it was.
func TestInterruptCapture(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	testenv.WriteFile(t, filepath.Join(d.Home, ".a"), "edited\n")
	chezmoi, err := exec.LookPath("chezmoi")
	if err != nil {
		t.Fatal(err)
	}
	// chezmoi hangs once it has added the file, standing in for a slow add.
	bin, pidFile := t.TempDir(), filepath.Join(t.TempDir(), "add.pid")
	testenv.WriteFile(t, filepath.Join(bin, "chezmoi"), "#!/bin/sh\n"+
		chezmoi+" \"$@\" || exit\n"+
		"case \" $* \" in *\" --dry-run \"*) ;; *\" add \"*) echo $$ >"+pidFile+"; exec sleep 30;; esac\n")
	if err := os.Chmod(filepath.Join(bin, "chezmoi"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	done := make(chan int)
	streams, _, _, errOut := iostreams.Test()
	go func() { done <- Main([]string{"capture", "~/.a"}, streams) }()
	pid := waitForPID(t, pidFile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if got := testenv.ReadFile(t, filepath.Join(d.Source, "dot_a")); got != "edited\n" {
		t.Fatalf("dot_a = %q before Ctrl-C, want chezmoi to have added the file", got)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != exitInterrupted {
			t.Errorf("Main() = %d, want %d", code, exitInterrupted)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Main() did not return after Ctrl-C")
	}
	if want := "Stopped capturing ~/.a; the repo was put back as it was.\n"; errOut.String() != want {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
	if got := testenv.ReadFile(t, filepath.Join(d.Source, "dot_a")); got != "a\n" {
		t.Errorf("dot_a = %q, want the repo put back", got)
	}
}
