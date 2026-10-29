// Package text formats text for people to read.
package text

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Pluralize writes a count of things, such as "1 commit" or "2 commits".
// thing is the singular, which takes an s for the plural.
func Pluralize(n int, thing string) string {
	s := strconv.Itoa(n) + " " + thing
	if n != 1 {
		s += "s"
	}
	return s
}

// Wrap breaks s into lines of at most width characters, at spaces. A word
// longer than width has a line of its own.
func Wrap(s string, width int) []string {
	var lines []string
	var line strings.Builder
	n := 0 // characters in line
	for word := range strings.FieldsSeq(s) {
		w := utf8.RuneCountInString(word)
		if n > 0 && n+1+w > width {
			lines = append(lines, line.String())
			line.Reset()
			n = 0
		}
		if n > 0 {
			line.WriteByte(' ')
			n++
		}
		line.WriteString(word)
		n += w
	}
	if n > 0 {
		lines = append(lines, line.String())
	}
	return lines
}
