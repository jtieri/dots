// Package text formats text for people to read.
package text

import "strconv"

// Pluralize writes a count of things, such as "1 commit" or "2 commits".
// thing is the singular, which takes an s for the plural.
func Pluralize(n int, thing string) string {
	s := strconv.Itoa(n) + " " + thing
	if n != 1 {
		s += "s"
	}
	return s
}
