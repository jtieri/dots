package lock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dots", "lock")
	ctx := t.Context()

	release, err := Acquire(ctx, path, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(ctx, path, false, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("Acquire() while held = %v, want ErrBusy", err)
	}

	// A waiting Acquire gets the lock once it is released.
	waited := make(chan struct{})
	got := make(chan error)
	go func() {
		release2, err := Acquire(ctx, path, true, func() { close(waited) })
		if err == nil {
			release2()
		}
		got <- err
	}()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire() never said it was waiting")
	}
	release()
	select {
	case err := <-got:
		if err != nil {
			t.Errorf("waiting Acquire() = %v, want the lock", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting Acquire() did not get the released lock")
	}

	// And it is free again afterwards.
	release, err = Acquire(ctx, path, false, nil)
	if err != nil {
		t.Fatalf("Acquire() after release = %v", err)
	}
	release()
}

func TestAcquireWaitCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, err := Acquire(t.Context(), path, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path, true, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Acquire() = %v, want context.DeadlineExceeded", err)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got, _ := DefaultPath(); got != "/run/user/1000/dots/lock" {
		t.Errorf("DefaultPath() = %q, want /run/user/1000/dots/lock", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "/home/me/.cache")
	if got, _ := DefaultPath(); got != "/home/me/.cache/dots/lock" {
		t.Errorf("DefaultPath() without XDG_RUNTIME_DIR = %q, want /home/me/.cache/dots/lock", got)
	}
}
