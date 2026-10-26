// Package backup keeps copies of files that dot is about to overwrite.
//
// Each overwrite gets a folder of its own under the backup directory, named
// for the time it happened, holding the files at their paths relative to the
// home directory.
package backup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// layout names each backup folder. It sorts by time and has no colons, which
// some tools treat specially in file names.
const layout = "2006-01-02T15-04-05"

// DefaultDir returns $XDG_STATE_HOME/dot/overwritten, using the XDG default
// of ~/.local/state when XDG_STATE_HOME is unset.
func DefaultDir(home string) string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "dot", "overwritten")
}

// Save copies the files at paths, relative to home, into a new folder in dir
// and returns the folder. Regular files keep their permissions and symbolic
// links stay links. Paths that do not exist, such as files deleted on this
// machine, and directories are skipped, since there is nothing in them to
// lose. If nothing is copied, Save creates no folder and returns "". After
// an error the copies may be incomplete, so nothing should be overwritten.
func Save(dir, home string, paths []string, now time.Time) (string, error) {
	var folder string
	for _, p := range paths {
		src := filepath.Join(home, p)
		info, err := os.Lstat(src)
		if errors.Is(err, fs.ErrNotExist) || (err == nil && info.IsDir()) {
			continue
		}
		if err != nil {
			return folder, err
		}
		if folder == "" {
			if folder, err = newFolder(dir, now); err != nil {
				return "", err
			}
		}
		dst := filepath.Join(folder, p)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return folder, err
		}
		if err := copyEntry(src, dst, info); err != nil {
			return folder, err
		}
	}
	return folder, nil
}

// newFolder creates the folder for a backup made at now. A second backup in
// the same second gets a numbered folder rather than mixing with the first.
func newFolder(dir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := now.Format(layout)
	for i := 2; ; i++ {
		folder := filepath.Join(dir, name)
		err := os.Mkdir(folder, 0o700)
		if !errors.Is(err, fs.ErrExist) {
			return folder, err
		}
		name = fmt.Sprintf("%s-%d", now.Format(layout), i)
	}
}

func copyEntry(src, dst string, info fs.FileInfo) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file or symbolic link", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Prune deletes the backup folders in dir made more than keepDays days
// before now. A keepDays of 0 keeps every folder. Entries whose names are not
// backup folders are left alone, as is a missing dir.
func Prune(dir string, keepDays int, now time.Time) error {
	if keepDays == 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := now.AddDate(0, 0, -keepDays)
	for _, e := range entries {
		made, ok := folderTime(e.Name(), now.Location())
		if !ok || !e.IsDir() || !made.Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// folderTime returns when the backup folder name was made, reading the time
// in loc. It also accepts the numbered names newFolder makes.
func folderTime(name string, loc *time.Location) (time.Time, bool) {
	if len(name) < len(layout) {
		return time.Time{}, false
	}
	rest := name[len(layout):]
	if rest != "" && !isNumberSuffix(rest) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(layout, name[:len(layout)], loc)
	return t, err == nil
}

// isNumberSuffix reports whether s is "-" followed by digits.
func isNumberSuffix(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
