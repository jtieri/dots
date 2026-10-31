package apply

import (
	"io"
	"strings"

	"github.com/jtieri/dots/internal/dotfiles"
	"github.com/jtieri/dots/internal/style"
)

// headings gives the preview's heading for each Kind.
var headings = map[Kind]string{
	Update:      "Will update",
	Create:      "Will create",
	Delete:      "Will delete",
	Run:         "Will run setup steps",
	ChangedHere: "Changed on this machine, would be replaced",
	Conflict:    "Changed both here and in the repo, would be replaced",
}

// WriteText writes the plan as a preview, grouped by kind and marked up with
// st.
func (p *Plan) WriteText(w io.Writer, st style.Style) error {
	var b strings.Builder
	for kind := Update; kind <= Conflict; kind++ {
		var lines []string
		for _, c := range p.Changes {
			if c.Kind == kind {
				lines = append(lines, c.describe())
			}
		}
		if len(lines) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(st.Heading(headings[kind]) + "\n")
		for _, line := range lines {
			b.WriteString("  " + line + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func (c Change) describe() string {
	if c.Kind == Run {
		return strings.TrimPrefix(c.Path, ".chezmoiscripts/")
	}
	s := dotfiles.Tilde(c.Path)
	if c.DeletedHere {
		s += " (deleted here)"
	}
	return s
}
