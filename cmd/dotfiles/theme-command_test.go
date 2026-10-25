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
// are dark, light and black, a second dark one, and a destination without
// the darkmode file. The config has [theme], a template that writes the
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
	darkmode := func() bool { _, err := os.Stat(filepath.Join(dst, darkmodePath)); return err == nil }
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

	run("set", "dark")
	if got := state(); got != "mode = 'dark'\n" {
		t.Errorf("state after set dark = %q", got)
	}
	if !darkmode() {
		t.Error("set dark did not create the darkmode file")
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
	if darkmode() {
		t.Error("toggle to light did not remove the darkmode file")
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
