package main

import (
	"bytes"
	"errors"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

	if got := run("apply"); got != "theme: test:dark (dark mode)\n" {
		t.Errorf("apply = %q, want theme: test:dark (dark mode)\n", got)
	}
	if got := state(); got != "mode = 'dark'\n" {
		t.Errorf("state after apply = %q, want mode = 'dark'\n", got)
	}
	if got := readOr(t, filepath.Join(dst, "theme.txt")); got != "test:dark\n" {
		t.Errorf("theme.txt after apply = %q", got)
	}
	if got := scripts(); got != "theme dark\nboth\n" {
		t.Errorf("scripts run by apply = %q, want theme dark and both", got)
	}

	d.Theme = "test:black"
	out := &bytes.Buffer{}
	d.Stdout, d.runErrors = out, nil
	if err := d.ThemeCommand([]string{"apply"}); err != nil {
		t.Fatal(err)
	}
	d.Theme = ""
	if got := out.String(); got != "theme: test:black (dark mode)\n" {
		t.Errorf("apply with -theme = %q, want theme: test:black (dark mode)\n", got)
	}
	if got := scripts(); got != "theme black\nboth\n" {
		t.Errorf("scripts run by apply with -theme = %q, want theme black and both", got)
	}

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
		{args: []string{"preview", "extra1", "extra2"}, want: "unexpected arguments: extra2"},
		{args: []string{"preview", "-theme", ":black", ":black"}, want: "<variant> argument and -theme cannot be combined"},
		{args: []string{"apply", "extra"}, want: "apply takes 0 arguments"},
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

func TestThemeApplyNoTheme(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "dotfiles.toml"), []byte("public = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Dotfiles{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, DstPath: t.TempDir(), repos: []string{repo}}
	err := d.ThemeCommand([]string{"apply"})
	if err == nil || !strings.Contains(err.Error(), "no theme is configured") {
		t.Errorf("apply with no theme: err = %v, want no theme is configured", err)
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
		"Text on bg_0",              // backgrounds
		"Text on bg_1",
		"Text on bg_2",
		"\nSurfaces:\n",             // surface hierarchy
		"\nAccent spectrum",         // accent spectrum
		"\nTerminal colors:\n   0 ", // 16 terminal colors
		"\nTerminal examples:\n",    // terminal examples below terminal colors
		"\x1b[7m",                   // inverse video (search match)
		"\x1b[7;1m",                 // inverse video + bold (badge)
		"\x1b[1;4m",                 // bold + underline
		"\x1b[9m",                   // strikethrough
		"\x1b[2;3m",                 // dim + italic
		"\x1b[38;2;0;0;3m\x1b[48;2;0;0;0m42 \x1b[0m",   // dim_0 on bg_0 (line number)
		"\x1b[38;2;0;0;1m\x1b[48;2;0;0;0m│ \x1b[0m",    // bg_1 on bg_0 (gutter / guide)
		"https://example.com",       // documentation URL
		"creator:",                  // metadata creator
		"Alice",
		"license:",                  // metadata license
		"MIT",
		"Test description",          // metadata description
		"file:",                     // variant file metadata at top
		"emacs:",                    // configured app
		"x-black",
		" 1.0  · \n",                // contrast ratio and WCAG rating badge (fails for near-black colors)
		"Warnings: invisible text in terminal examples:\n", // warnings for low-contrast test:black
		"'│' (bg_1 #000001 on bg_0 #000000) has contrast ratio 1.0:1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview does not contain %q:\n%s", want, got)
		}
	}
	// Verify metadata appears at the top before the color table
	metaIdx := strings.Index(got, "file:")
	colorIdx := strings.Index(got, "Text on bg_0")
	if metaIdx == -1 || colorIdx == -1 || metaIdx >= colorIdx {
		t.Errorf("Metadata (%d) should be before Color table (%d)", metaIdx, colorIdx)
	}

	// Verify examples section appears after terminal colors
	exIdx := strings.Index(got, "\nTerminal examples:\n")
	colIdx := strings.Index(got, "\nTerminal colors:\n")
	if exIdx == -1 || colIdx == -1 || colIdx >= exIdx {
		t.Errorf("Terminal colors (%d) should be before Terminal examples (%d)", colIdx, exIdx)
	}

	// Verify preview ends with an empty newline
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("preview output should end with an empty newline, got suffix %q", got[max(0, len(got)-10):])
	}

	if strings.Contains(got, "color0 ") {
		t.Error("color0 is listed with the colors, not only with the terminal colors")
	}
	if err := d.ThemeCommand([]string{"preview", ":dim"}); err == nil || !strings.Contains(err.Error(), "no variant dim") {
		t.Errorf("preview :dim: err = %v", err)
	}

	// -theme flag
	out.Reset()
	if err := d.ThemeCommand([]string{"preview", "-theme", ":black"}); err != nil {
		t.Fatalf("preview -theme :black: %v", err)
	}
	if !strings.Contains(out.String(), "test:black (dark mode)\n") {
		t.Errorf("preview -theme :black did not show test:black")
	}

	// -all flag
	out.Reset()
	if err := d.ThemeCommand([]string{"preview", "-all"}); err != nil {
		t.Fatalf("preview -all: %v", err)
	}
	allPreview := out.String()
	for _, want := range []string{"test:black (dark mode)\n", "test:dark (dark mode)\n", "test:light (light mode)\n"} {
		if !strings.Contains(allPreview, want) {
			t.Errorf("preview -all output missing %q", want)
		}
	}
	if !strings.Contains(allPreview, strings.Repeat("─", 80)) {
		t.Errorf("preview -all output missing full width horizontal divider")
	}

	// -all flag error combinations
	for _, tt := range []struct {
		args    []string
		wantErr string
	}{
		{[]string{"preview", "-all", ":black"}, "<variant> and -all cannot be combined"},
		{[]string{"preview", "-all", "-theme", ":black"}, "<variant> and -all cannot be combined"},
		{[]string{"preview", "-theme", ":black", ":light"}, "<variant> argument and -theme cannot be combined"},
		{[]string{"preview", ":black", ":light"}, "unexpected arguments"},
	} {
		err := d.ThemeCommand(tt.args)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%v error = %v, want containing %q", tt.args, err, tt.wantErr)
		}
	}

	// -h help flag
	stderr := &bytes.Buffer{}
	d.Stderr = stderr
	err := d.ThemeCommand([]string{"preview", "-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("preview -h: err = %v, want flag.ErrHelp", err)
	}
	helpOut := stderr.String()
	for _, want := range []string{
		"Usage: dotfiles theme preview",
		"WCAG 2.1 contrast",
		"Contrast Badges (WCAG 2.1):",
		"AAA   >= 7.0:1",
		"AA    >= 4.5:1",
		"lg    >= 3.0:1",
		"·      < 3.0:1",
		"invisible text",
	} {
		if !strings.Contains(helpOut, want) {
			t.Errorf("preview -h help output missing %q:\n%s", want, helpOut)
		}
	}
}

func TestThemeLint(t *testing.T) {
	t.Parallel()
	d, _ := themeCommandRepo(t)
	run := func(args ...string) (string, error) {
		t.Helper()
		out := &bytes.Buffer{}
		d.Stdout, d.runErrors, d.Theme = out, nil, ""
		err := d.ThemeCommand(append([]string{"lint"}, args...))
		return out.String(), err
	}

	// Default: lints active theme (test:light in themeCommandRepo)
	got, err := run()
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if !strings.Contains(got, "test:light:\n") || !strings.Contains(got, "warning: invisible text") {
		t.Errorf("lint default = %q, want warnings for test:light", got)
	}
	if !strings.Contains(got, "example:") || !strings.Contains(got, "██") || !strings.Contains(got, "Aa") {
		t.Errorf("lint default missing color example blocks:\n%s", got)
	}

	// Positional argument
	got, err = run(":black")
	if err != nil {
		t.Fatalf("lint :black: %v", err)
	}
	if !strings.Contains(got, "test:black:\n") || !strings.Contains(got, "warning: invisible text") {
		t.Errorf("lint :black = %q, want warnings for test:black", got)
	}

	// -theme flag
	got, err = run("-theme", ":black")
	if err != nil {
		t.Fatalf("lint -theme :black: %v", err)
	}
	if !strings.Contains(got, "test:black:\n") {
		t.Errorf("lint -theme :black = %q, want test:black", got)
	}

	// -all flag
	got, err = run("-all")
	if err != nil {
		t.Fatalf("lint -all: %v", err)
	}
	for _, want := range []string{"test:black:\n", "test:dark:\n", "test:light:\n", "warning(s) found across 3 variants"} {
		if !strings.Contains(got, want) {
			t.Errorf("lint -all output missing %q:\n%s", want, got)
		}
	}

	// -h flag
	stderr := &bytes.Buffer{}
	d.Stderr = stderr
	err = d.ThemeCommand([]string{"lint", "-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("lint -h err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "Usage: dotfiles theme lint") {
		t.Errorf("lint -h missing usage:\n%s", stderr.String())
	}

	// Error cases
	for _, tt := range []struct {
		args    []string
		wantErr string
	}{
		{[]string{"-all", ":black"}, "<variant> and -all cannot be combined"},
		{[]string{"-all", "-theme", ":black"}, "<variant> and -all cannot be combined"},
		{[]string{"-theme", ":black", ":light"}, "<variant> argument and -theme cannot be combined"},
		{[]string{":black", ":light"}, "unexpected arguments"},
		{[]string{"nonexistent:variant"}, "no theme nonexistent"},
	} {
		_, err := run(tt.args...)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("lint %v error = %v, want containing %q", tt.args, err, tt.wantErr)
		}
	}
}

func TestWCAGBadge(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		cr   float64
		want string
	}{
		{1.0, " · "},
		{2.9, " · "},
		{3.0, "lg "},
		{4.4, "lg "},
		{4.5, "AA "},
		{6.9, "AA "},
		{7.0, "AAA"},
		{21.0, "AAA"},
	} {
		if got := wcagBadge(tt.cr); got != tt.want {
			t.Errorf("wcagBadge(%v) = %q, want %q", tt.cr, got, tt.want)
		}
	}
}

func TestThemePreviewInvisibleTextWarning(t *testing.T) {
	t.Parallel()
	highContrastColors := map[string]string{
		"bg_0":    "#000000",
		"bg_1":    "#303030", // contrast ~ 1.7:1 vs bg_0
		"bg_2":    "#505050", // contrast ~ 2.8:1 vs bg_0
		"dim_0":   "#808080",
		"fg_0":    "#d0d0d0",
		"fg_1":    "#ffffff",
		"yellow":  "#ffff00",
		"orange":  "#ff8800",
		"red":     "#ff0000",
		"magenta": "#ff00ff",
		"violet":  "#aa00ff",
		"blue":    "#0088ff",
		"cyan":    "#00ffff",
		"green":   "#00ff00",
	}
	var buf bytes.Buffer
	vHigh := &Variant{Colors: highContrastColors}
	printTerminalExamples(&buf, vHigh)
	if strings.Contains(buf.String(), "invisible text") {
		t.Errorf("unexpected invisible text warning for high-contrast variant:\n%s", buf.String())
	}

	buf.Reset()
	lowContrastColors := maps.Clone(highContrastColors)
	lowContrastColors["bg_1"] = "#050505" // contrast ~ 1.03:1 vs bg_0 (#000000)
	vLow := &Variant{Colors: lowContrastColors}
	printTerminalExamples(&buf, vLow)
	wantWarning := "Warning: invisible text in terminal examples:\n  '│' (bg_1 #050505 on bg_0 #000000) has contrast ratio 1.0:1\n"
	if !strings.Contains(buf.String(), wantWarning) {
		t.Errorf("missing expected warning for low contrast:\n%s\nwant substring:\n%s", buf.String(), wantWarning)
	}
}

func TestThemeLintRules(t *testing.T) {
	t.Parallel()

	baseColors := map[string]string{
		"bg_0":    "#000000",
		"bg_1":    "#202020",
		"bg_2":    "#404040",
		"dim_0":   "#808080",
		"fg_0":    "#c0c0c0",
		"fg_1":    "#ffffff",
		"yellow":  "#ffff00",
		"orange":  "#ff8800",
		"red":     "#ff0000",
		"magenta": "#ff00ff",
		"violet":  "#aa00ff",
		"blue":    "#0088ff",
		"cyan":    "#00ffff",
		"green":   "#00ff00",
		"color1":  "#ff0000",
		"color2":  "#00ff00",
		"color3":  "#ffff00",
		"color4":  "#0088ff",
		"color5":  "#ff00ff",
		"color6":  "#00ffff",
		"color7":  "#808080",
		"color8":  "#404040",
		"color9":  "#ff5555",
		"color10": "#55ff55",
		"color11": "#ffff55",
		"color12": "#5555ff",
		"color13": "#ff55ff",
		"color14": "#55ffff",
		"color15": "#ffffff",
	}

	thm := &Theme{
		Name: "test",
		Ansi: map[string]string{
			"color1": "red", "color2": "green", "color3": "yellow", "color4": "blue",
			"color5": "magenta", "color6": "cyan", "color9": "red", "color10": "green",
			"color11": "yellow", "color12": "blue", "color13": "magenta", "color14": "cyan",
		},
	}

	// 1. Clean variant
	vClean := &Variant{Mode: "dark", Colors: maps.Clone(baseColors)}
	if warns := lintVariant(thm, vClean); len(warns) > 0 {
		t.Errorf("clean variant had unexpected warnings: %v", warns)
	}

	// 2. Inverted surfaces
	vInvert := &Variant{Mode: "dark", Colors: maps.Clone(baseColors)}
	vInvert.Colors["bg_1"] = "#000000" // equal to bg_0
	warns := lintVariant(thm, vInvert)
	if !slices.ContainsFunc(warns, func(w lintWarning) bool { return strings.Contains(w.message, "identical color") && w.c1 == "#000000" && w.c2 == "#000000" }) {
		t.Errorf("expected identical surface warning, got: %v", warns)
	}

	vInvert.Colors["bg_1"] = "#505050"
	vInvert.Colors["bg_2"] = "#202020" // bg_2 darker than bg_1 in dark mode
	warns = lintVariant(thm, vInvert)
	if !slices.ContainsFunc(warns, func(w lintWarning) bool { return strings.Contains(w.message, "surface hierarchy inverted") && w.c1 == "#202020" && w.c2 == "#505050" }) {
		t.Errorf("expected surface hierarchy inverted warning, got: %v", warns)
	}

	// 3. Mode mismatch
	vMode := &Variant{Mode: "dark", Colors: maps.Clone(baseColors)}
	vMode.Colors["bg_0"] = "#ffffff"
	vMode.Colors["fg_0"] = "#000000"
	warns = lintVariant(thm, vMode)
	if !slices.ContainsFunc(warns, func(w lintWarning) bool { return strings.Contains(w.message, "mode is \"dark\" but background") }) {
		t.Errorf("expected mode mismatch warning, got: %v", warns)
	}

	// 4. Low body text contrast
	vBody := &Variant{Mode: "dark", Colors: maps.Clone(baseColors)}
	vBody.Colors["fg_0"] = "#202020"
	warns = lintVariant(thm, vBody)
	if !slices.ContainsFunc(warns, func(w lintWarning) bool { return strings.Contains(w.message, "low body text contrast: fg_0") && w.c1 == "#202020" && w.c2 == "#000000" }) {
		t.Errorf("expected low body text contrast warning, got: %v", warns)
	}

	// 5. Accent collision
	vCollision := &Variant{Mode: "dark", Colors: maps.Clone(baseColors)}
	vCollision.Colors["orange"] = vCollision.Colors["red"] // identical colors
	warns = lintVariant(thm, vCollision)
	if !slices.ContainsFunc(warns, func(w lintWarning) bool { return strings.Contains(w.message, "accent color collision") && w.c1 == vCollision.Colors["orange"] && w.c2 == vCollision.Colors["red"] }) {
		t.Errorf("expected accent color collision warning, got: %v", warns)
	}

	// 6. Semantic ANSI mismatch
	thmBadAnsi := &Theme{
		Name: "test",
		Ansi: map[string]string{"color1": "blue"},
	}
	warns = lintVariant(thmBadAnsi, vClean)
	if !slices.ContainsFunc(warns, func(w lintWarning) bool { return strings.Contains(w.message, "ANSI mapping mismatch: ansi.color1") && w.c1 == vClean.Colors["color1"] }) {
		t.Errorf("expected ANSI mapping mismatch warning, got: %v", warns)
	}

	// 7. Missing background asset
	tempDir := t.TempDir()
	thmAsset := &Theme{Name: "test", Dir: tempDir}
	vAsset := &Variant{
		Mode:   "dark",
		Colors: maps.Clone(baseColors),
		Apps:   map[string]string{"background": "nonexistent.png"},
	}
	warns = lintVariant(thmAsset, vAsset)
	if !slices.ContainsFunc(warns, func(w lintWarning) bool { return strings.Contains(w.message, "missing background asset") }) {
		t.Errorf("expected missing background asset warning, got: %v", warns)
	}

	// Valid absolute background asset produces no warning
	absAsset := filepath.Join(tempDir, "bg.png")
	if err := os.WriteFile(absAsset, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	vAsset.Apps["background"] = absAsset
	if warns := lintVariant(thmAsset, vAsset); len(warns) > 0 {
		t.Errorf("expected no warnings for valid absolute background asset, got: %v", warns)
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

	// br_* fallback when variant does not define bright accent or neutral
	got, err = run("br_green") // green is #00000d in test
	if err != nil || got != "#00000d\n" {
		t.Errorf("color br_green = %q, %v, want #00000d\\n, nil", got, err)
	}
	got, err = run("br_yellow") // falls back to orange (#000007 in test)
	if err != nil || got != "#000007\n" {
		t.Errorf("color br_yellow = %q, %v, want #000007\\n, nil", got, err)
	}
	got, err = run("br_black") // bg_2 is #000002 in test
	if err != nil || got != "#000002\n" {
		t.Errorf("color br_black = %q, %v, want #000002\\n, nil", got, err)
	}
	got, err = run("br_white") // fg_1 is #000005 in test
	if err != nil || got != "#000005\n" {
		t.Errorf("color br_white = %q, %v, want #000005\\n, nil", got, err)
	}

	// terminal_bright = "accents" reflects on color8-15
	st := themeState{Mode: "dark", TerminalBright: "accents"}
	if err := writeThemeState(d.DstPath, st); err != nil {
		t.Fatal(err)
	}
	got, err = run("color9") // br_red is #ff0000 in test:dark
	if err != nil || got != "#ff0000\n" {
		t.Errorf("color color9 with terminal_bright=accents = %q, %v, want #ff0000\\n, nil", got, err)
	}
	got, err = run("-theme", ":black", "color9") // br_red is #000001 in test:black
	if err != nil || got != "#000001\n" {
		t.Errorf("color -theme :black color9 with terminal_bright=accents = %q, %v, want #000001\\n, nil", got, err)
	}
	_ = writeThemeState(d.DstPath, themeState{Mode: "dark"})

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
