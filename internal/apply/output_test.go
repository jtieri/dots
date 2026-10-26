package apply

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestWriteText(t *testing.T) {
	plan := &Plan{Changes: []Change{
		{Path: ".both", Kind: Conflict},
		{Path: ".gone", Kind: ChangedHere, DeletedHere: true},
		{Path: ".here", Kind: ChangedHere},
		{Path: ".config/new.conf", Kind: Create},
		{Path: ".old", Kind: Delete},
		{Path: ".profile", Kind: Update},
		{Path: ".bashrc", Kind: Update},
		{Path: ".chezmoiscripts/10-stage-papirus.sh", Kind: Run},
	}}
	want := `Will update
  ~/.profile
  ~/.bashrc

Will create
  ~/.config/new.conf

Will delete
  ~/.old

Will run setup steps
  10-stage-papirus.sh

Changed on this machine, would be replaced
  ~/.gone (deleted here)
  ~/.here

Changed both here and in the repo, would be replaced
  ~/.both
`
	var b strings.Builder
	if err := plan.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Errorf("WriteText() mismatch (-want +got):\n%s", diff)
	}

	b.Reset()
	if err := (&Plan{}).WriteText(&b); err != nil || b.Len() != 0 {
		t.Errorf("WriteText() of an empty plan = %q, %v; want nothing", b.String(), err)
	}
}
