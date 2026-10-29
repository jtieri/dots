package status

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jtieri/dot/internal/dotfiles"
	"github.com/jtieri/dot/internal/style"
	"github.com/jtieri/dot/internal/text"
)

// homeKinds gives the order and wording of the kinds of HomeChange.
var homeKinds = []struct{ kind, text string }{
	{ChangedHere, "changed on this machine"},
	{DeletedHere, "deleted on this machine"},
	{RepoNewer, "repo has a newer version"},
	{NewInRepo, "new in the repo"},
	{RemovedInRepo, "removed in the repo"},
}

// WriteText writes the report in English, marked up with st.
func (r *Report) WriteText(w io.Writer, st style.Style) error {
	var b strings.Builder
	repo := r.repoLines()
	if r.InSync {
		b.WriteString(st.Good("In sync.") + "\n")
		for _, line := range repo {
			b.WriteString(line + "\n")
		}
		_, err := io.WriteString(w, b.String())
		return err
	}

	section := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(st.Heading(title) + "\n")
		for _, line := range lines {
			b.WriteString("  " + line + "\n")
		}
	}

	section("Repo "+r.Repo.Path, repo)

	var home []string
	for _, k := range homeKinds {
		for _, c := range r.Home {
			if c.Kind == k.kind {
				home = append(home, fmt.Sprintf("%-25s %s", k.text, dotfiles.Tilde(c.Path)))
			}
		}
	}
	section("Home", home)
	section("Setup steps waiting to run", r.SetupSteps)
	section("Leftovers: in tracked folders but not tracked", homePaths(r.Leftovers))
	section("Should be modify templates, but the repo has plain copies", homePaths(r.ReplacedTemplates))
	_, err := io.WriteString(w, b.String())
	return err
}

// repoLines describes the repo's state, one finding per line. A failed fetch
// is always mentioned, even when everything else is in sync.
func (r *Report) repoLines() []string {
	var lines []string
	if r.Repo.Branch != r.Repo.WantBranch {
		if r.Repo.Branch == "" {
			lines = append(lines, "Not on a branch (detached HEAD), should be on "+r.Repo.WantBranch+".")
		} else {
			lines = append(lines, "On branch "+r.Repo.Branch+", should be on "+r.Repo.WantBranch+".")
		}
	}
	if r.Repo.NoUpstream && r.Repo.Branch != "" {
		lines = append(lines, "The branch is not tracking a remote branch.")
	}
	if r.Repo.Ahead > 0 {
		lines = append(lines, text.Pluralize(r.Repo.Ahead, "commit")+" not pushed.")
	}
	if r.Repo.Behind > 0 {
		lines = append(lines, text.Pluralize(r.Repo.Behind, "commit")+" not pulled.")
	}
	if !r.Repo.Fetch.OK {
		line := "Could not fetch from the remote: " + r.Repo.Fetch.Error + "."
		if r.Repo.Fetch.LastFetch.IsZero() {
			line += " It has never been fetched."
		} else {
			line += " Last fetched " + r.Repo.Fetch.LastFetch.Local().Format("2006-01-02 15:04") + "."
		}
		lines = append(lines, line)
	}
	if len(r.Repo.Uncommitted) > 0 {
		lines = append(lines, "Uncommitted changes:")
		for _, u := range r.Repo.Uncommitted {
			p := u.RepoPath
			if u.HomePath != "" {
				p = dotfiles.Tilde(u.HomePath)
			}
			if u.From != "" {
				p += " (was " + u.From + ")"
			}
			lines = append(lines, fmt.Sprintf("  %-9s %s", u.Change, p))
		}
	}
	return lines
}

func homePaths(ps []string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = dotfiles.Tilde(p)
	}
	return out
}

// WriteJSON writes the report as indented JSON. Lists are written as empty
// arrays rather than null, so readers need no special case.
func (r *Report) WriteJSON(w io.Writer) error {
	c := *r
	c.Repo.Uncommitted = orEmpty(c.Repo.Uncommitted)
	c.Home = orEmpty(c.Home)
	c.SetupSteps = orEmpty(c.SetupSteps)
	c.Leftovers = orEmpty(c.Leftovers)
	c.ReplacedTemplates = orEmpty(c.ReplacedTemplates)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(&c)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
