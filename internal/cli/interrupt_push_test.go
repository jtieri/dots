package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jtieri/dots/internal/iostreams"
	"github.com/jtieri/dots/internal/testenv"
)

// TestInterruptPush checks that Ctrl-C while git pushes stops dots push with
// exit code 130, keeps the commit, and says it was made.
func TestInterruptPush(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	testenv.WriteFile(t, filepath.Join(d.Home, ".config/dots/config.toml"), "[push]\npackage_lists = \"\"\n")
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "a captured\n")
	// The fetch reaches the remote; the push goes over ssh, which hangs.
	pidFile := filepath.Join(t.TempDir(), "ssh.pid")
	d.Git(t, "config", "remote.origin.pushurl", "ssh://git@example.invalid/dotfiles.git")
	t.Setenv("GIT_SSH_COMMAND", "echo $$ >"+pidFile+"; exec sleep 30 #")

	done := make(chan int)
	streams, _, out, _ := iostreams.Test()
	go func() { done <- Main([]string{"push", "--yes", "interrupted"}, streams) }()
	pid := waitForPID(t, pidFile)
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
	if !strings.HasSuffix(out.String(), "Committed: interrupted\n") {
		t.Errorf("stdout = %q, want the commit reported", out)
	}
	if got := d.Git(t, "log", "-1", "--format=%s"); got != "interrupted\n" {
		t.Errorf("last commit = %q, want the commit kept", got)
	}
	for range 50 {
		if !testenv.Running(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("ssh (pid %d) is still running", pid)
}
