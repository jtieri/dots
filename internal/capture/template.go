package capture

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Marker is the line in a modify template between its logic and the file it
// produces. dots rewrites everything below it.
const Marker = "{{- /* dots capture: the file follows */ -}}"

// escapedDelim is how a template writes the text "{{".
const escapedDelim = `{{ "{{" }}`

// rewriteTemplate returns tmpl, a modify template, with everything below its
// marker line replaced by file. The one part of file that keep matches is
// replaced by the template's placeholder: the single action below the marker,
// which puts back the part each machine keeps for itself.
func rewriteTemplate(tmpl, file []byte, keep *regexp.Regexp) ([]byte, error) {
	head, body, err := splitTemplate(tmpl)
	if err != nil {
		return nil, err
	}
	placeholder, err := findPlaceholder(body)
	if err != nil {
		return nil, err
	}
	matches := keep.FindAllIndex(file, -1)
	switch {
	case len(matches) == 0:
		return nil, fmt.Errorf("nothing in the file matches the keep pattern %q", keep)
	case len(matches) > 1:
		return nil, fmt.Errorf("the keep pattern %q matches %d parts of the file, not one", keep, len(matches))
	}
	// The marker trims the space that follows it.
	if len(file) > 0 && strings.ContainsRune(" \t\r\n", rune(file[0])) {
		return nil, errors.New("the file starts with blank space, which the marker would remove")
	}
	m := matches[0]
	var b bytes.Buffer
	b.Write(head)
	b.WriteString(escape(file[:m[0]]))
	b.WriteString(placeholder)
	b.WriteString(escape(file[m[1]:]))
	return b.Bytes(), nil
}

// splitTemplate splits tmpl after its marker line.
func splitTemplate(tmpl []byte) (head, body []byte, err error) {
	var at []int
	offset := 0
	for line := range bytes.Lines(tmpl) {
		if string(bytes.TrimSuffix(line, []byte("\n"))) == Marker {
			at = append(at, offset+len(line))
		}
		offset += len(line)
	}
	switch len(at) {
	case 0:
		return nil, nil, fmt.Errorf("the template has no marker line %s", Marker)
	case 1:
	default:
		return nil, nil, fmt.Errorf("the template has %d marker lines, not one", len(at))
	}
	return tmpl[:at[0]], tmpl[at[0]:], nil
}

// findPlaceholder returns the one action in body other than escaped
// delimiters.
func findPlaceholder(body []byte) (string, error) {
	var actions []string
	rest := string(body)
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			break
		}
		rest = rest[i:]
		if strings.HasPrefix(rest, escapedDelim) {
			rest = rest[len(escapedDelim):]
			continue
		}
		end := strings.Index(rest, "}}")
		if end < 0 {
			return "", errors.New("the template has an unclosed action below its marker")
		}
		actions = append(actions, rest[:end+len("}}")])
		rest = rest[end+len("}}"):]
	}
	if len(actions) != 1 {
		return "", fmt.Errorf("below its marker the template must have one action, "+
			"for the part each machine keeps, but it has %d", len(actions))
	}
	return actions[0], nil
}

// escape writes text so that a template produces it unchanged.
func escape(text []byte) string {
	return strings.ReplaceAll(string(text), "{{", escapedDelim)
}
