package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/testenv"
)

// TestInterrupt checks that Ctrl-C during the fetch stops dot status
// promptly, kills the fetch, and exits 130.
func TestInterrupt(t *testing.T) {
	d := testenv.NewDotfiles(t, map[string]string{"dot_bashrc": "bashrc\n"})
	pidFile := filepath.Join(t.TempDir(), "ssh.pid")
	d.Git(t, "remote", "set-url", "origin", "ssh://git@example.invalid/dotfiles.git")
	t.Setenv("GIT_SSH_COMMAND", "echo $$ >"+pidFile+"; exec sleep 30 #")

	done := make(chan int)
	streams, _, _, _ := iostreams.Test()
	go func() { done <- Main([]string{"status"}, streams) }()

	// Once ssh has started, Main is waiting on the fetch and handling
	// Ctrl-C itself, so the signal cannot kill the test.
	pid := waitForPID(t, pidFile)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != exitInterrupted {
			t.Errorf("Main() = %d, want %d", code, exitInterrupted)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Main() did not return after Ctrl-C")
	}
	for range 50 {
		if !testenv.Running(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("ssh (pid %d) is still running", pid)
}
