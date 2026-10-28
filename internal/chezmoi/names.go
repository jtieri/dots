// Package chezmoi runs chezmoi and interprets its source directory.
package chezmoi

import (
	"path"
	"strings"
)

// TargetPath returns the path, relative to the home directory, of the target
// that a file in the source directory produces. sourceRel is relative to the
// source directory and uses forward slashes.
//
// TargetPath follows chezmoi's naming rules without consulting chezmoi, so it
// also works for source files that no longer exist, where "chezmoi
// target-path" fails. ok is false for entries that produce no target:
// chezmoi's own files, scripts, remove_ entries and names chezmoi ignores.
func TargetPath(sourceRel string) (target string, ok bool) {
	target, _, ok = Parse(sourceRel, false)
	return target, ok
}

// Attributes are what a source name says about its target, beyond its name.
// Only the attributes dot acts on are reported.
type Attributes struct {
	// Create means the target is written only when it does not exist.
	Create bool
	// Modify means the source computes the target from its current
	// contents, as a script or a modify template.
	Modify bool
	// Private means the target is readable only by its owner.
	Private bool
	// Template means the source is a template.
	Template bool
}

// Parse returns the target that the entry at sourceRel produces, as
// TargetPath does, and the entry's attributes. dir says whether the entry is
// a directory, since chezmoi names directories by different rules.
func Parse(sourceRel string, dir bool) (target string, attrs Attributes, ok bool) {
	parts := strings.Split(sourceRel, "/")
	for i, part := range parts {
		last := i == len(parts)-1
		if last && !dir {
			if parts[i], attrs, ok = fileTarget(part); !ok {
				return "", Attributes{}, false
			}
			break
		}
		var external, private bool
		if parts[i], external, private, ok = dirTarget(part); !ok {
			return "", Attributes{}, false
		}
		if last {
			attrs.Private = private
		} else if external {
			// chezmoi copies everything inside an external_ directory as
			// it is, names included.
			break
		}
	}
	return path.Join(parts...), attrs, true
}

// dirTarget returns the target name of a source directory, and whether it is
// an external_ directory and a private one. The prefixes are stripped in the
// order chezmoi strips them.
func dirTarget(name string) (target string, external, private, ok bool) {
	// chezmoi ignores names starting with a dot, and its own .chezmoi
	// directories hold templates, scripts and the like rather than targets.
	if name == "" || strings.HasPrefix(name, ".") {
		return "", false, false, false
	}
	if strings.HasPrefix(name, "remove_") {
		return "", false, false, false
	}
	name, external = strings.CutPrefix(name, "external_")
	name = strings.TrimPrefix(name, "exact_")
	name, private = strings.CutPrefix(name, "private_")
	name = strings.TrimPrefix(name, "readonly_")
	target, ok = baseName(name)
	return target, external, private, ok
}

// fileTarget returns the target name and attributes of a source file. The
// prefixes are stripped in the order chezmoi strips them.
func fileTarget(name string) (target string, attrs Attributes, ok bool) {
	if name == "" || strings.HasPrefix(name, ".") {
		return "", attrs, false
	}
	var encrypted bool
	switch {
	case strings.HasPrefix(name, "remove_"), strings.HasPrefix(name, "run_"):
		return "", attrs, false
	case strings.HasPrefix(name, "symlink_"):
		name = strings.TrimPrefix(name, "symlink_")
	case strings.HasPrefix(name, "modify_"):
		attrs.Modify = true
		name = strings.TrimPrefix(name, "modify_")
		name, encrypted = strings.CutPrefix(name, "encrypted_")
		name, attrs.Private = strings.CutPrefix(name, "private_")
		name = strings.TrimPrefix(name, "readonly_")
		name = strings.TrimPrefix(name, "executable_")
	default:
		name, attrs.Create = strings.CutPrefix(name, "create_")
		name, encrypted = strings.CutPrefix(name, "encrypted_")
		name, attrs.Private = strings.CutPrefix(name, "private_")
		name = strings.TrimPrefix(name, "readonly_")
		name = strings.TrimPrefix(name, "empty_")
		name = strings.TrimPrefix(name, "executable_")
	}
	name, ok = baseName(name)
	if !ok {
		return "", Attributes{}, false
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
		attrs.Template = true
	}
	if name == "" || name == "." || name == ".." {
		return "", Attributes{}, false
	}
	return name, attrs, true
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
