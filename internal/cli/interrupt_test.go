package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/testenv"
)

// TestInterrupt checks that Ctrl-C during the fetch stops a command
// promptly, kills the fetch, and exits 130 without going on to anything else.
func TestInterrupt(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"sync", "--yes"}, {"push", "--yes"}} {
		t.Run(args[0], func(t *testing.T) {
			d := testenv.NewDotfiles(t, map[string]string{"dot_bashrc": "bashrc\n"})
			// A change sync would apply if it went on after the fetch.
			testenv.WriteFile(t, filepath.Join(d.Source, "dot_bashrc"), "bashrc from the repo\n")
			pidFile := filepath.Join(t.TempDir(), "ssh.pid")
			d.Git(t, "remote", "set-url", "origin", "ssh://git@example.invalid/dotfiles.git")
			t.Setenv("GIT_SSH_COMMAND", "echo $$ >"+pidFile+"; exec sleep 30 #")

			done := make(chan int)
			streams, _, _, errOut := iostreams.Test()
			go func() { done <- Main(args, streams) }()

			// Once ssh has started, Main is waiting on the fetch and
			// handling Ctrl-C itself, so the signal cannot kill the test.
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
			if strings.Contains(errOut.String(), "Could not fetch") {
				t.Errorf("stderr = %q, want no report of a failed fetch", errOut)
			}
			if data, _ := os.ReadFile(filepath.Join(d.Home, ".bashrc")); string(data) != "bashrc\n" {
				t.Errorf(".bashrc = %q, want nothing applied", data)
			}
			if got := d.Git(t, "log", "-1", "--format=%s"); got != "initial\n" {
				t.Errorf("last commit = %q, want nothing committed", got)
			}
			for range 50 {
				if !testenv.Running(pid) {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Errorf("ssh (pid %d) is still running", pid)
		})
	}
}
