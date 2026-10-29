package style

import "testing"

func TestStyle(t *testing.T) {
	plain := Style{}
	if got := plain.Heading("Home") + plain.Good("ok") + plain.Bad("dot:"); got != "Homeokdot:" {
		t.Errorf("plain Style = %q, want the text as it is", got)
	}
	on := New(true)
	if got, want := on.Heading("Home"), "\x1b[1mHome\x1b[0m"; got != want {
		t.Errorf("Heading() = %q, want %q", got, want)
	}
	if got, want := on.Good("In sync."), "\x1b[32mIn sync.\x1b[0m"; got != want {
		t.Errorf("Good() = %q, want %q", got, want)
	}
	if got, want := on.Bad("dot:"), "\x1b[1;31mdot:\x1b[0m"; got != want {
		t.Errorf("Bad() = %q, want %q", got, want)
	}
	if got := on.Heading(""); got != "" {
		t.Errorf("Heading(\"\") = %q, want nothing", got)
	}
}

func TestParseMode(t *testing.T) {
	for _, s := range []string{"auto", "always", "never"} {
		if m, err := ParseMode(s); err != nil || string(m) != s {
			t.Errorf("ParseMode(%q) = %q, %v", s, m, err)
		}
	}
	if _, err := ParseMode("yes"); err == nil || err.Error() != `--color "yes" must be auto, always or never` {
		t.Errorf("ParseMode(yes) error = %v", err)
	}
}

func TestEnabled(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}
	tests := []struct {
		mode     Mode
		terminal bool
		env      map[string]string
		want     bool
	}{
		{Auto, true, nil, true},
		{Auto, false, nil, false},
		{Auto, true, map[string]string{"NO_COLOR": "1"}, false},
		{Auto, true, map[string]string{"TERM": "dumb"}, false},
		{Auto, true, map[string]string{"TERM": "xterm-kitty"}, true},
		{Always, false, map[string]string{"NO_COLOR": "1"}, true},
		{Never, true, nil, false},
	}
	for _, tt := range tests {
		if got := tt.mode.Enabled(tt.terminal, env(tt.env)); got != tt.want {
			t.Errorf("%s.Enabled(%t, %v) = %t, want %t", tt.mode, tt.terminal, tt.env, got, tt.want)
		}
	}
}
