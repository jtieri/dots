// Package lock makes sure only one dot runs at a time.
package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrBusy is returned when another process holds the lock and the caller
// chose not to wait.
var ErrBusy = errors.New("another dot is running")

// pollInterval is how often a waiting Acquire tries again.
const pollInterval = 100 * time.Millisecond

// DefaultPath returns the lock file: in $XDG_RUNTIME_DIR, which is meant for
// files like it, or in the cache directory when that is unset.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		var err error
		if dir, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "dot", "lock"), nil
}

// Acquire takes the lock at path, creating the file if needed. If another
// process holds it, Acquire returns ErrBusy, or, if wait is true, calls
// waiting once and tries again until it gets the lock or ctx is done. The
// lock is released by calling release, or when the process exits.
func Acquire(ctx context.Context, path string, wait bool, waiting func()) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for first := true; ; first = false {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		if !wait {
			f.Close()
			return nil, ErrBusy
		}
		if first && waiting != nil {
			waiting()
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
