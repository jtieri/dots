// Package chezmoi runs chezmoi and interprets its source directory.
package chezmoi

import (
	"path"
	"strings"
)

// TargetPath returns the path, relative to the home directory, of the target
// that a file or directory in the source directory produces. sourceRel is
// relative to the source directory and uses forward slashes.
//
// TargetPath follows chezmoi's naming rules without consulting chezmoi, so it
// also works for source files that no longer exist, where "chezmoi
// target-path" fails. ok is false for entries that produce no target:
// chezmoi's own files, scripts, remove_ entries and names chezmoi ignores.
func TargetPath(sourceRel string) (target string, ok bool) {
	parts := strings.Split(sourceRel, "/")
	for i, part := range parts {
		var name string
		if i < len(parts)-1 {
			name, ok = dirTarget(part)
		} else {
			name, ok = fileTarget(part)
		}
		if !ok {
			return "", false
		}
		parts[i] = name
	}
	return path.Join(parts...), true
}

// dirTarget returns the target name of a source directory. The prefixes are
// stripped in the order chezmoi strips them.
func dirTarget(name string) (string, bool) {
	// chezmoi ignores names starting with a dot, and its own .chezmoi
	// directories hold templates, scripts and the like rather than targets.
	if name == "" || strings.HasPrefix(name, ".") {
		return "", false
	}
	if strings.HasPrefix(name, "remove_") {
		return "", false
	}
	name = strings.TrimPrefix(name, "external_")
	name = strings.TrimPrefix(name, "exact_")
	name = strings.TrimPrefix(name, "private_")
	name = strings.TrimPrefix(name, "readonly_")
	return baseName(name)
}

// fileTarget returns the target name of a source file. The prefixes are
// stripped in the order chezmoi strips them.
func fileTarget(name string) (string, bool) {
	if name == "" || strings.HasPrefix(name, ".") {
		return "", false
	}
	var encrypted bool
	switch {
	case strings.HasPrefix(name, "remove_"), strings.HasPrefix(name, "run_"):
		return "", false
	case strings.HasPrefix(name, "symlink_"):
		name = strings.TrimPrefix(name, "symlink_")
	case strings.HasPrefix(name, "modify_"):
		name = strings.TrimPrefix(name, "modify_")
		name, encrypted = strings.CutPrefix(name, "encrypted_")
		name = strings.TrimPrefix(name, "private_")
		name = strings.TrimPrefix(name, "readonly_")
		name = strings.TrimPrefix(name, "executable_")
	default:
		name = strings.TrimPrefix(name, "create_")
		name, encrypted = strings.CutPrefix(name, "encrypted_")
		name = strings.TrimPrefix(name, "private_")
		name = strings.TrimPrefix(name, "readonly_")
		name = strings.TrimPrefix(name, "empty_")
		name = strings.TrimPrefix(name, "executable_")
	}
	name, ok := baseName(name)
	if !ok {
		return "", false
	}
	if encrypted {
		// The suffix depends on the encryption tool: .age for age, .asc
		// for gpg.
		if s, cut := strings.CutSuffix(name, ".age"); cut {
			name = s
		} else {
			name = strings.TrimSuffix(name, ".asc")
		}
	}
	if s, cut := strings.CutSuffix(name, ".literal"); cut {
		name = s
	} else if s, cut := strings.CutSuffix(name, ".tmpl"); cut {
		name = strings.TrimSuffix(s, ".literal")
	}
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	return name, true
}

// baseName turns a leading dot_ into a dot and drops a leading literal_.
func baseName(name string) (string, bool) {
	if s, cut := strings.CutPrefix(name, "dot_"); cut {
		name = "." + s
	} else {
		name = strings.TrimPrefix(name, "literal_")
	}
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	return name, true
}
