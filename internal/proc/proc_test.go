package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunning(t *testing.T) {
	// A copy of sleep with a name no other process has, and one longer
	// than the kernel keeps.
	name := "dot-proc-test-sleeper"
	exe := filepath.Join(t.TempDir(), name)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, data, 0o755); err != nil {
		t.Fatal(err)
	}

	if got, err := Running(name); err != nil || got {
		t.Fatalf("Running(%q) before starting it = %t, %v; want false", name, got, err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if got, err := Running(name); err != nil || !got {
		t.Errorf("Running(%q) = %t, %v; want true", name, got, err)
	}
	if got, err := Running(name[:10]); err != nil || got {
		t.Errorf("Running(%q) = %t, %v; want false for part of the name", name[:10], got, err)
	}
}
