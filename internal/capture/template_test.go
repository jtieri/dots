package capture

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jtieri/dot/internal/testenv"
)

// themeKeep matches Zed's "theme" block, as the dotfiles' setting does.
var themeKeep = regexp.MustCompile(`"theme"\s*:\s*\{[^{}]*\}`)

// zedHead is the logic of a modify template that keeps the machine's own
// "theme" block, or uses a default one when the machine has none.
const zedHead = `{{- /* chezmoi:modify-template */ -}}
{{- $theme := regexFind "\"theme\"\\s*:\\s*\\{[^{}]*\\}" .chezmoi.stdin -}}
{{- if not $theme -}}
{{-   $theme = "\"theme\": {\n    \"dark\": \"Default\"\n  }" -}}
{{- end -}}
` + Marker + "\n"

const zedBody = `// Zed settings
{
  "icon_theme": {
    "dark": "Catppuccin Mocha"
  },
  {{ $theme }},
  "ui_font_size": 16
}
`

func TestRewriteTemplate(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		file string
		want string
	}{
		{
			name: "settings changed",
			tmpl: zedHead + zedBody,
			file: "// Zed settings\n{\n  \"theme\": {\n    \"dark\": \"Mine\"\n  },\n  \"ui_font_size\": 18\n}\n",
			want: zedHead + "// Zed settings\n{\n  {{ $theme }},\n  \"ui_font_size\": 18\n}\n",
		},
		{
			name: "delimiters in the file",
			tmpl: zedHead + zedBody,
			file: "{ \"a\": \"{{x}}\", \"theme\": {}, \"b\": \"}} {{{\" }",
			want: zedHead + `{ "a": "{{ "{{" }}x}}", {{ $theme }}, "b": "}} {{ "{{" }}{" }`,
		},
		{
			name: "escaped delimiters already in the template",
			tmpl: zedHead + "{ \"a\": \"{{ \"{{\" }}\", {{ $theme }} }",
			file: "{ \"theme\": {} }",
			want: zedHead + "{ {{ $theme }} }",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rewriteTemplate([]byte(tt.tmpl), []byte(tt.file), themeKeep)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("rewriteTemplate() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestRewriteTemplateErrors(t *testing.T) {
	const file = "{\n  \"theme\": {}\n}\n"
	tests := []struct {
		name    string
		tmpl    string
		file    string
		wantErr string
	}{
		{name: "no marker", tmpl: "{{ .chezmoi.stdin }}", file: file, wantErr: "no marker line"},
		{name: "marker indented", tmpl: "  " + Marker + "\n{{ $theme }}", file: file, wantErr: "no marker line"},
		{name: "two markers", tmpl: zedHead + Marker + "\n" + zedBody, file: file, wantErr: "2 marker lines"},
		{name: "no placeholder", tmpl: zedHead + "{}\n", file: file, wantErr: "it has 0"},
		{name: "two placeholders", tmpl: zedHead + "{{ $theme }}{{ $theme }}", file: file, wantErr: "it has 2"},
		{name: "unclosed action", tmpl: zedHead + "{{ $theme }} {{ x", file: file, wantErr: "unclosed action"},
		{name: "no match", tmpl: zedHead + zedBody, file: "{}\n", wantErr: "nothing in the file matches"},
		{name: "two matches", tmpl: zedHead + zedBody, file: file + file, wantErr: "matches 2 parts"},
		{name: "leading space", tmpl: zedHead + zedBody, file: " " + file, wantErr: "starts with blank space"},
		{name: "leading newline", tmpl: zedHead + zedBody, file: "\n" + file, wantErr: "starts with blank space"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rewriteTemplate([]byte(tt.tmpl), []byte(tt.file), themeKeep)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("rewriteTemplate() = %q, %v; want an error containing %q", got, err, tt.wantErr)
			}
		})
	}
}

// TestRewriteTemplateRoundTrip checks the rewritten template with chezmoi:
// on this machine it produces the file exactly, and on other machines it
// gives them the new settings with their own theme block.
func TestRewriteTemplateRoundTrip(t *testing.T) {
	source := "dot_config/zed/modify_private_settings.json"
	d := testenv.NewDotfiles(t, map[string]string{source: zedHead + zedBody})
	live := filepath.Join(d.Home, ".config/zed/settings.json")
	file := "// Zed settings, {{ changed }} here\n{\n  \"icon_theme\": {\n    \"dark\": \"Catppuccin Mocha\"\n  },\n" +
		"  \"theme\": {\n    \"mode\": \"dark\",\n    \"dark\": \"Tokyo Night\"\n  },\n  \"ui_font_size\": 18\n}\n"
	testenv.WriteFile(t, live, file)

	tmpl := testenv.ReadFile(t, filepath.Join(d.Source, source))
	got, err := rewriteTemplate([]byte(tmpl), []byte(file), themeKeep)
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, filepath.Join(d.Source, source), string(got))

	if out := d.Chezmoi(t, "cat", live); out != file {
		t.Errorf("chezmoi cat on this machine =\n%s\nwant the file\n%s", out, file)
	}
	elsewhere := strings.Replace(file, "Tokyo Night", "Gruvbox", 1)
	elsewhere = strings.Replace(elsewhere, "18", "12", 1)
	testenv.WriteFile(t, live, elsewhere)
	want := strings.Replace(file, "Tokyo Night", "Gruvbox", 1)
	if out := d.Chezmoi(t, "cat", live); out != want {
		t.Errorf("chezmoi cat on another machine =\n%s\nwant its theme with the new settings\n%s", out, want)
	}
	testenv.WriteFile(t, live, "{}\n")
	if out := d.Chezmoi(t, "cat", live); !strings.Contains(out, "\"theme\": {\n    \"dark\": \"Default\"\n  },\n  \"ui_font_size\": 18") {
		t.Errorf("chezmoi cat on a new machine =\n%s\nwant the default theme with the new settings", out)
	}
}
