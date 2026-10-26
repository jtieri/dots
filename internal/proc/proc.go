// Package proc looks at the processes running on this machine.
package proc

import (
	"os"
	"path/filepath"
	"strings"
)

// commLen is the most of a process name Linux keeps in /proc/<pid>/comm,
// leaving out the terminating null byte.
const commLen = 15

// Running reports whether a process named name is running for any user. As
// in ps and pgrep, a name longer than 15 bytes is matched by its first 15.
func Running(name string) (bool, error) {
	if len(name) > commLen {
		name = name[:commLen]
	}
	comms, err := filepath.Glob("/proc/[0-9]*/comm")
	if err != nil {
		return false, err
	}
	for _, comm := range comms {
		data, err := os.ReadFile(comm)
		if err != nil {
			continue // the process has exited
		}
		if strings.TrimSuffix(string(data), "\n") == name {
			return true, nil
		}
	}
	return false, nil
}
