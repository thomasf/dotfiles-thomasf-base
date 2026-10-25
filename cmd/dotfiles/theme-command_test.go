package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"go.starlark.net/starlark"
)

// themeCommandRepo writes a repository with the theme test, whose variants
// are dark, light and black, a second dark one, and an empty destination.
// The config has [theme], a template that writes the
// variant in use to theme.txt, and scripts that append to scripts.txt in the
// destination: one for install, one for theme, and one for both.
func themeCommandRepo(t *testing.T) (*Dotfiles, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	dst := t.TempDir()
	log := shellQuoteOrFatal(t, filepath.Join(dst, "scripts.txt"))
	files := fstest.MapFS{
		"dotfiles.toml": tomlFile(`
[theme]
dirs  = ["themes"]
dark  = "test:dark"
light = "test:light"

[[template]]
src = "theme.tmpl"
dst = "theme.txt"

[[script]]
src = "echo install >> ` + log + `"

[[script]]
on  = "theme"
src = "echo theme {{.theme.variant}} >> ` + log + `"

[[script]]
on  = ["install", "theme"]
src = "echo both >> ` + log + `"

[[ignore]]
match = ["themes", "theme.tmpl"]
`),
		"theme.tmpl": tomlFile("{{.theme.name}}:{{.theme.variant}}\n"),
	}
	for name, content := range themeFiles("themes/test") {
		files[name] = tomlFile(content)
	}
	black := testColors()
	black["br_red"] = "#000001"
	files["themes/test/variants/black.toml"] = tomlFile(variantTOML("dark", black, map[string]string{"emacs": "x-black"}))
	if err := os.CopyFS(repo, files); err != nil {
		t.Fatal(err)
	}
	return &Dotfiles{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, DstPath: dst, repos: []string{repo}}, dst
}

func shellQuoteOrFatal(t *testing.T, s string) string {
	t.Helper()
	q, err := shellQuote(s)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// readOr returns the content of path, or "" if it does not exist.
func readOr(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestThemeCommand(t *testing.T) {
	t.Parallel()
	d, dst := themeCommandRepo(t)
	run := func(args ...string) string {
		t.Helper()
		out := &bytes.Buffer{}
		d.Stdout, d.runErrors, d.Theme = out, nil, ""
		if err := d.ThemeCommand(args); err != nil {
			t.Fatalf("theme %v: %v\nstderr:\n%s", args, err, d.Stderr)
		}
		if len(d.runErrors) > 0 {
			t.Fatalf("theme %v: %v", args, d.runErrors)
		}
		return out.String()
	}
	state := func() string { return readOr(t, filepath.Join(dst, themeStatePath)) }
	scripts := func() string {
		s := readOr(t, filepath.Join(dst, "scripts.txt"))
		os.Remove(filepath.Join(dst, "scripts.txt"))
		return s
	}

	if got := run(); got != "test:light (light mode)\n" {
		t.Errorf("show = %q", got)
	}
	want := "  test:black  dark\n  test:dark   dark   [theme] dark\n* test:light  light  [theme] light\n"
	if got := run("list"); got != want {
		t.Errorf("list =\n%s\nwant\n%s", got, want)
	}
	wantNames := "test:black\ntest:dark\ntest:light\n"
	if got := run("list", "-names"); got != wantNames {
		t.Errorf("list -names =\n%s\nwant\n%s", got, wantNames)
	}
	wantJSON := `[
  {
    "name": "test:black",
    "theme": "test",
    "variant": "black",
    "mode": "dark",
    "active": false,
    "dark": false,
    "light": false
  },
  {
    "name": "test:dark",
    "theme": "test",
    "variant": "dark",
    "mode": "dark",
    "active": false,
    "dark": true,
    "light": false
  },
  {
    "name": "test:light",
    "theme": "test",
    "variant": "light",
    "mode": "light",
    "active": true,
    "dark": false,
    "light": true
  }
]
`
	if got := run("list", "-json"); got != wantJSON {
		t.Errorf("list -json =\n%s\nwant\n%s", got, wantJSON)
	}

	run("set", "dark")
	if got := state(); got != "mode = 'dark'\n" {
		t.Errorf("state after set dark = %q", got)
	}
	if got := readOr(t, filepath.Join(dst, "theme.txt")); got != "test:dark\n" {
		t.Errorf("theme.txt after set dark = %q", got)
	}
	if got := scripts(); got != "theme dark\nboth\n" {
		t.Errorf("scripts run by set dark = %q, want theme and both", got)
	}

	run("set", ":black")
	if got := state(); got != "mode = 'dark'\ntheme = 'test:black'\n" {
		t.Errorf("state after set :black = %q", got)
	}
	if got := readOr(t, filepath.Join(dst, "theme.txt")); got != "test:black\n" {
		t.Errorf("theme.txt after set :black = %q", got)
	}
	scripts()

	run("toggle")
	if got := state(); got != "mode = 'light'\n" {
		t.Errorf("state after toggle = %q, want light mode without the theme", got)
	}
	if got := readOr(t, filepath.Join(dst, "theme.txt")); got != "test:light\n" {
		t.Errorf("theme.txt after toggle = %q", got)
	}
	scripts()

	run("set", "test:dark")
	if got := state(); got != "mode = 'dark'\n" {
		t.Errorf("state after set test:dark = %q, want only the mode, as [theme] dark is test:dark", got)
	}
	scripts()

	if err := d.InstallCommand(nil); err != nil {
		t.Fatal(err)
	}
	if got := scripts(); got != "install\nboth\n" {
		t.Errorf("scripts run by install = %q, want install and both", got)
	}
}

func TestThemeCommandErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args   []string
		option string
		want   string
	}{
		{args: []string{"set"}, want: "set takes 1 arguments"},
		{args: []string{"paint"}, want: "unknown command paint"},
		{args: []string{"set", "dim"}, want: `invalid "dim"`},
		{args: []string{"set", "nope:dark"}, want: "no theme nope"},
		{args: []string{"set", ":dim"}, want: "theme test has no variant dim"},
		{args: []string{"toggle"}, option: ":dark", want: "-theme can not be used with toggle"},
		{args: []string{"list", "-names", "-json"}, want: "-names and -json cannot be combined"},
		{args: []string{"list", "extra"}, want: "unexpected arguments: extra"},
	} {
		d, dst := themeCommandRepo(t)
		d.Theme = tt.option
		err := d.ThemeCommand(tt.args)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("theme %v: err = %v, want one containing %q", tt.args, err, tt.want)
		}
		if _, err := os.Stat(filepath.Join(dst, themeStatePath)); err == nil {
			t.Errorf("theme %v: the state file was written", tt.args)
		}
	}
}

func TestScriptOnValidate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dotfiles.toml"), []byte("[[script]]\non = [\"theme\", \"boot\"]\nsrc = \"true\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := NewRepository(dir, t.TempDir()).readConfig()
	if err == nil || !strings.Contains(err.Error(), `script 0: invalid on "boot": want install or theme`) {
		t.Errorf("err = %v", err)
	}
}

func TestTemplateThemeErrors(t *testing.T) {
	t.Parallel()
	repo, dst := themeConfigRepo(t, "")
	r := NewRepository(repo, dst)
	if err := r.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	none := evalContext{}
	for _, tt := range []struct {
		text string
		ec   evalContext
		want string // the output, or the error if it starts with "error: "
	}{
		{text: "{{.theme.mode}}", ec: none, want: "error: no theme is selected (set [theme] dark and light)"},
		{text: "{{if .theme}}{{.theme.mode}}{{else}}none{{end}}", ec: none, want: "none"},
		{text: "{{.theme.colors.nope}}", ec: r.evalContext(), want: `error: <.theme.colors.nope>: map has no entry for key "nope"`},
		{text: `{{or (index .theme.colors "nope") "x"}}`, ec: r.evalContext(), want: "x"},
		{text: `{{expr "theme.mode"}}`, ec: none, want: "error: no theme is selected for theme.mode"},
	} {
		out, err := renderTemplate("t", tt.text, tt.ec)
		if want, ok := strings.CutPrefix(tt.want, "error: "); ok {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: err = %v, want one containing %q", tt.text, err, want)
			}
			continue
		}
		if err != nil || string(out) != tt.want {
			t.Errorf("%s = %q, %v, want %q", tt.text, out, err, tt.want)
		}
	}
}

func TestStarlarkReadThemeDir(t *testing.T) {
	t.Parallel()
	repo, dst := themeConfigRepo(t, "")
	r := NewRepository(repo, dst)
	if err := r.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.theme.dir(), "extra.toml"), []byte("a = \"b\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]string{
		`doc["x"] = read(theme["dir"] + "/extra.toml")["a"]`: "",
		`read(theme["dir"] + "/../outside.toml")`:            "must be inside the theme directory",
		`read("/etc/passwd.toml")`:                           "must be inside the theme directory",
	} {
		prog, err := compileStarlark("test", code)
		if err != nil {
			t.Fatal(err)
		}
		doc := newStarlarkDict()
		_, err = prog.Init(newStarlarkThread(), starlarkPredeclared(repo, "x", r.evalContext(), doc))
		if want == "" {
			if err != nil {
				t.Errorf("%s: %v", code, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want one containing %q", code, err, want)
		}
	}
}

func newStarlarkDict() *starlark.Dict     { return starlark.NewDict(1) }
func newStarlarkThread() *starlark.Thread { return &starlark.Thread{} }

func TestThemePreview(t *testing.T) {
	t.Parallel()
	d, _ := themeCommandRepo(t)
	out := &bytes.Buffer{}
	d.Stdout = out
	if err := d.ThemeCommand([]string{"preview", ":black"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"test:black (dark mode)\n",
		"  bg_0     #000000  ",      // names padded to the longest, magenta
		"br_red   #000001",          // extras after the required colors
		"\x1b[38;2;0;0;1m",          // br_red as a 24-bit color
		"\nTerminal colors:\n   0 ", // then the 16 terminal colors
		" 1.0\n",                    // bg_0 on itself
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "color0 ") {
		t.Error("color0 is listed with the colors, not only with the terminal colors")
	}
	if err := d.ThemeCommand([]string{"preview", ":dim"}); err == nil || !strings.Contains(err.Error(), "no variant dim") {
		t.Errorf("preview :dim: err = %v", err)
	}
}

func TestThemeColor(t *testing.T) {
	t.Parallel()
	d, _ := themeCommandRepo(t)

	run := func(args ...string) (string, error) {
		out := &bytes.Buffer{}
		d.Stdout = out
		err := d.ThemeCommand(append([]string{"color"}, args...))
		return out.String(), err
	}

	// Default active theme color
	got, err := run("bg_0")
	if err != nil || got != "#000000\n" {
		t.Errorf("color bg_0 = %q, %v, want #000000\\n, nil", got, err)
	}

	// Formats
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"-format", "hex", "bg_0"}, "#000000\n"},
		{[]string{"-format", "raw", "bg_0"}, "000000\n"},
		{[]string{"-format", "strip-hash", "bg_0"}, "000000\n"},
		{[]string{"-format", "0x", "bg_0"}, "0x000000\n"},
		{[]string{"-format", "rgb", "bg_0"}, "0,0,0\n"},
		{[]string{"-raw", "bg_0"}, "000000\n"},
		{[]string{"-rgb", "bg_0"}, "0,0,0\n"},
		{[]string{"-0x", "bg_0"}, "0x000000\n"},
	} {
		got, err := run(tt.args...)
		if err != nil || got != tt.want {
			t.Errorf("color %v = %q, %v, want %q, nil", tt.args, got, err, tt.want)
		}
	}

	// Terminal colors (ANSI color0-15)
	got, err = run("color0")
	if err != nil || got != "#000000\n" {
		t.Errorf("color color0 = %q, %v, want #000000\\n, nil", got, err)
	}

	// Specific theme variant
	got, err = run("-theme", ":black", "br_red")
	if err != nil || got != "#000001\n" {
		t.Errorf("color -theme :black br_red = %q, %v, want #000001\\n, nil", got, err)
	}
	got, err = run("-theme", "test:black", "-raw", "br_red")
	if err != nil || got != "000001\n" {
		t.Errorf("color -theme test:black -raw br_red = %q, %v, want 000001\\n, nil", got, err)
	}
	got, err = run("-theme", "dark", "bg_0")
	if err != nil || got != "#000000\n" {
		t.Errorf("color -theme dark bg_0 = %q, %v, want #000000\\n, nil", got, err)
	}

	// Global -theme option
	d.Theme = "test:black"
	got, err = run("br_red")
	d.Theme = ""
	if err != nil || got != "#000001\n" {
		t.Errorf("color with global -theme = %q, %v, want #000001\\n, nil", got, err)
	}

	// Errors
	for _, tt := range []struct {
		args    []string
		wantErr string
	}{
		{nil, "missing color name"},
		{[]string{"bg_0", "extra"}, "unexpected arguments"},
		{[]string{"-raw", "-rgb", "bg_0"}, "only one of -raw, -rgb, and -0x"},
		{[]string{"-raw", "-format", "rgb", "bg_0"}, "-raw and -format cannot be combined"},
		{[]string{"-format", "invalid", "bg_0"}, "unknown format"},
		{[]string{"nonexistent"}, "has no color"},
		{[]string{"-theme", ":dim", "bg_0"}, "no variant dim"},
		{[]string{"-theme", "missing:dark", "bg_0"}, "no theme missing"},
	} {
		_, err := run(tt.args...)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("color %v error = %v, want error containing %q", tt.args, err, tt.wantErr)
		}
	}
}
