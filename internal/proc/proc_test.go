package proc

import (
	"testing"

	"github.com/jtieri/dot/internal/testenv"
)

func TestRunning(t *testing.T) {
	// A name no other process has, longer than the kernel keeps.
	name := "dot-proc-test-sleeper"
	if got, err := Running(name); err != nil || got {
		t.Fatalf("Running(%q) before starting it = %t, %v; want false", name, got, err)
	}
	testenv.StartProgram(t, name)
	if got, err := Running(name); err != nil || !got {
		t.Errorf("Running(%q) = %t, %v; want true", name, got, err)
	}
	if got, err := Running(name[:10]); err != nil || got {
		t.Errorf("Running(%q) = %t, %v; want false for part of the name", name[:10], got, err)
	}
}
