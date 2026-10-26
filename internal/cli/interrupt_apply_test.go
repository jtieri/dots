package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jtieri/dot/internal/iostreams"
	"github.com/jtieri/dot/internal/testenv"
)

// TestInterruptApply checks that Ctrl-C during a setup step stops dot apply
// with exit code 130, and that running it again finishes the job.
func TestInterruptApply(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "step.pid")
	// The step hangs the first time it runs, standing in for a slow one.
	step := "#!/bin/sh\n" +
		"if [ ! -e \"$HOME/.step-started\" ]; then\n" +
		"  touch \"$HOME/.step-started\"\n" +
		"  echo $$ >" + pidFile + "\n" +
		"  exec sleep 30\n" +
		"fi\n" +
		"touch \"$HOME/.step-done\"\n"
	d := testenv.NewDotfiles(t, map[string]string{"dot_a": "a\n"})
	testenv.WriteFile(t, filepath.Join(d.Source, "dot_a"), "a from the repo\n")
	testenv.WriteFile(t, filepath.Join(d.Source, ".chezmoiscripts/run_onchange_after_10-step.sh"), step)

	done := make(chan int)
	streams, _, _, _ := iostreams.Test()
	go func() { done <- Main([]string{"apply", "--yes"}, streams) }()
	pid := waitForPID(t, pidFile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

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

	streams, _, out, errOut := iostreams.Test()
	if code := Main([]string{"apply", "--yes"}, streams); code != exitOK {
		t.Fatalf("second apply = %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(d.Home, ".step-done")); err != nil {
		t.Errorf("the setup step did not finish on the second apply: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(d.Home, ".a")); string(data) != "a from the repo\n" {
		t.Errorf(".a = %q, want the repo's version", data)
	}
	if !strings.HasSuffix(out.String(), "Done. The home directory matches the repo.\n") {
		t.Errorf("stdout = %q, want it to end saying the home matches", out)
	}
}

// waitForPID waits for a process to write its ID to name and returns it.
func waitForPID(t *testing.T, name string) int {
	t.Helper()
	for range 500 {
		if data, err := os.ReadFile(name); err == nil && strings.HasSuffix(string(data), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no process ID in %s", name)
	return 0
}
