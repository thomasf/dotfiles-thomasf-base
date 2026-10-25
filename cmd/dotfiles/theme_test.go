package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.starlark.net/starlark"
)

// testColors returns a value for each of the requiredColors, all different.
func testColors() map[string]string {
	colors := map[string]string{}
	for i, name := range requiredColors {
		colors[name] = fmt.Sprintf("#0000%02x", i)
	}
	return colors
}

// testAnsi returns an [ansi] that maps colorN to the requiredColors in turn.
func testAnsi() map[string]string {
	ansi := map[string]string{}
	for i, name := range ansiColors {
		ansi[name] = requiredColors[i%len(requiredColors)]
	}
	return ansi
}

// tomlTable returns the TOML table name with the string values of m.
func tomlTable(name string, m map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", name)
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(&b, "%s = %q\n", k, m[k])
	}
	return b.String()
}

// variantTOML returns a variant file.
func variantTOML(mode string, colors, apps map[string]string) string {
	return fmt.Sprintf("mode = %q\n\n%s\n%s", mode, tomlTable("colors", colors), tomlTable("apps", apps))
}

// writeTheme writes files to a theme directory named test and returns it.
func writeTheme(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "test")
	mfs := fstest.MapFS{}
	for name, content := range files {
		mfs[name] = tomlFile(content)
	}
	if err := os.CopyFS(dir, mfs); err != nil {
		t.Fatal(err)
	}
	return dir
}

// validTheme returns the files of a theme with a dark and a light variant.
func validTheme() map[string]string {
	dark, light := testColors(), testColors()
	dark["br_red"], light["br_red"] = "#ff0000", "#aa0000"
	return map[string]string{
		"theme.toml":          tomlTable("ansi", testAnsi()),
		"variants/dark.toml":  variantTOML("dark", dark, map[string]string{"emacs": "x-dark"}),
		"variants/light.toml": variantTOML("light", light, map[string]string{"emacs": "x-light"}),
	}
}

func TestLoadTheme(t *testing.T) {
	t.Parallel()
	dir := writeTheme(t, validTheme())
	theme, err := loadTheme(dir)
	if err != nil {
		t.Fatal(err)
	}
	if theme.Name != "test" || theme.Dir != dir {
		t.Errorf("Name, Dir = %q, %q, want test, %q", theme.Name, theme.Dir, dir)
	}
	if got := slices.Sorted(maps.Keys(theme.Variants)); !slices.Equal(got, []string{"dark", "light"}) {
		t.Fatalf("variants = %v, want [dark light]", got)
	}

	dark, light := theme.Variants["dark"], theme.Variants["light"]
	if dark.Mode != "dark" || light.Mode != "light" {
		t.Errorf("modes = %q, %q, want dark, light", dark.Mode, light.Mode)
	}
	if dark.Colors["br_red"] != "#ff0000" || light.Colors["br_red"] != "#aa0000" {
		t.Errorf("br_red = %q, %q, want #ff0000, #aa0000", dark.Colors["br_red"], light.Colors["br_red"])
	}
	for name, ref := range testAnsi() {
		if got, want := dark.Colors[name], dark.Colors[ref]; got != want {
			t.Errorf("%s = %q, want %q, the value of %s", name, got, want, ref)
		}
	}
	if len(dark.Colors) != len(requiredColors)+1+len(ansiColors) {
		t.Errorf("dark has %d colors, want %d", len(dark.Colors), len(requiredColors)+1+len(ansiColors))
	}
	if dark.Apps["emacs"] != "x-dark" || light.Apps["emacs"] != "x-light" {
		t.Errorf("apps.emacs = %q, %q, want x-dark, x-light", dark.Apps["emacs"], light.Apps["emacs"])
	}
}

func TestLoadThemeErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(files map[string]string)
		want   []string // every one is in the error
	}{
		{
			name:   "no theme.toml",
			change: func(f map[string]string) { delete(f, "theme.toml") },
			want:   []string{"theme.toml"},
		},
		{
			name: "no variants",
			change: func(f map[string]string) {
				delete(f, "variants/dark.toml")
				delete(f, "variants/light.toml")
				f["variants/README"] = "not a variant"
			},
			want: []string{"no variants/*.toml"},
		},
		{
			name:   "variant name",
			change: func(f map[string]string) { f["variants/Dim.toml"] = f["variants/dark.toml"] },
			want:   []string{`variant name "Dim"`},
		},
		{
			name:   "unknown key",
			change: func(f map[string]string) { f["variants/dark.toml"] = "colour = 1\n" + f["variants/dark.toml"] },
			want:   []string{"dark.toml"},
		},
		{
			name: "mode, missing and invalid colors, all at once",
			change: func(f map[string]string) {
				colors := testColors()
				delete(colors, "bg_2")
				colors["fg_0"] = "#abc"
				colors["Bright"] = "#ffffff"
				colors["color3"] = "#ffffff"
				f["variants/dark.toml"] = variantTOML("dim", colors, map[string]string{"emacs": "x", "Emacs": "x"})
			},
			want: []string{
				`mode is "dim"`,
				"missing colors: bg_2\n",
				`colors.fg_0: "#abc" is not a #rrggbb color`,
				"colors.Bright: a name is",
				"colors.color3: color0 to color15 are set in [ansi]",
				"apps.Emacs: a name is",
			},
		},
		{
			name: "colors and apps that only some variants have",
			change: func(f map[string]string) {
				f["variants/light.toml"] = variantTOML("light", testColors(), map[string]string{})
			},
			want: []string{"colors.br_red is missing in variants light", "apps.emacs is missing in variants light"},
		},
		{
			name: "ansi",
			change: func(f map[string]string) {
				ansi := testAnsi()
				delete(ansi, "color0")
				ansi["color1"] = "pink"
				ansi["color16"] = "red"
				f["theme.toml"] = tomlTable("ansi", ansi)
			},
			want: []string{
				"ansi.color0 is missing",
				`ansi.color1: "pink" is not a color of the variants`,
				"ansi.color16: only color0 to color15 can be set",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := validTheme()
			tt.change(files)
			_, err := loadTheme(writeTheme(t, files))
			if err == nil {
				t.Fatal("no error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not contain %q:\n%v", want, err)
				}
			}
		})
	}
}

// TestSelenizedTheme loads the theme in base, so that a broken palette fails
// the tests.
func TestSelenizedTheme(t *testing.T) {
	t.Parallel()
	theme, err := loadTheme(filepath.Join("..", "..", "config-base", "themes", "selenized"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]string{"dark": "dark", "light": "light"} {
		v, ok := theme.Variants[name]
		if !ok {
			t.Errorf("no variant %s", name)
			continue
		}
		if v.Mode != mode {
			t.Errorf("%s: mode = %q, want %q", name, v.Mode, mode)
		}
	}
}

func TestLoadThemeName(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "Bad Name")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTheme(dir); err == nil || !strings.Contains(err.Error(), `theme name "Bad Name"`) {
		t.Errorf("err = %v, want one about the theme name", err)
	}
}

// themeRepo writes the repository root/name with the config and files, reads
// its config and returns it.
func themeRepo(t *testing.T, root, name, config string, files map[string]string) *Repository {
	t.Helper()
	dir := filepath.Join(root, name)
	mfs := fstest.MapFS{"dotfiles.toml": tomlFile(config)}
	for n, content := range files {
		mfs[n] = tomlFile(content)
	}
	if err := os.CopyFS(dir, mfs); err != nil {
		t.Fatal(err)
	}
	r := NewRepository(dir, t.TempDir())
	if err := r.readConfig(); err != nil {
		t.Fatal(err)
	}
	return r
}

// themeFiles returns the files of validTheme in the directory dir.
func themeFiles(dir string) map[string]string {
	files := map[string]string{}
	for name, content := range validTheme() {
		files[dir+"/"+name] = content
	}
	return files
}

func TestMergeThemes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	aFiles := themeFiles("themes/one")
	aFiles["themes/.hidden/theme.toml"] = "not a theme"
	aFiles["themes/README"] = "not a theme"
	a := themeRepo(t, root, "a", `
[theme]
dirs  = ["themes"]
dark  = "one:dark"
light = "two:light"
mode  = "dark"
`, aFiles)
	b := themeRepo(t, root, "b", `
[theme]
dirs = ["themes"]
mode = "light"
`, themeFiles("themes/two"))

	ts, warnings, err := mergeThemes([]*Repository{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(ts.themes)); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("themes = %v, want [one two]", got)
	}
	if want := filepath.Join(root, "b", "themes", "two"); ts.themes["two"].Dir != want {
		t.Errorf("two.Dir = %q, want %q", ts.themes["two"].Dir, want)
	}
	if ts.dark != "one:dark" || ts.light != "two:light" || ts.mode != "dark" {
		t.Errorf("dark, light, mode = %q, %q, %q, want one:dark, two:light, dark", ts.dark, ts.light, ts.mode)
	}
	want := "theme.mode is set differently in " + a.configPath + ", " + b.configPath + "; using " + a.configPath
	if !slices.Equal(warnings, []string{want}) {
		t.Errorf("warnings = %q, want [%q]", warnings, want)
	}
}

func TestMergeThemesErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		theme string // [theme] of the repository a, which has themes/one
		b     map[string]string
		want  []string // every one is in the error
	}{
		{
			name:  "theme in two repositories",
			theme: `dirs = ["themes"]`,
			b:     themeFiles("themes/one"),
			want:  []string{"theme one is in both"},
		},
		{
			name:  "no theme",
			theme: "dirs = [\"themes\"]\ndark = \"nope:dark\"",
			want:  []string{"theme.dark: no theme nope"},
		},
		{
			name:  "no variant",
			theme: "dirs = [\"themes\"]\nlight = \"one:dim\"",
			want:  []string{"theme.light: theme one has no variant dim"},
		},
		{
			name:  "variant of the other mode",
			theme: "dirs = [\"themes\"]\ndark = \"one:light\"",
			want:  []string{"theme.dark: one:light is a light variant"},
		},
		{
			name:  "no dir",
			theme: `dirs = ["nope"]`,
			want:  []string{"theme.dirs"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			repos := []*Repository{themeRepo(t, root, "a", "[theme]\n"+tt.theme, themeFiles("themes/one"))}
			if tt.b != nil {
				repos = append(repos, themeRepo(t, root, "b", `theme.dirs = ["themes"]`, tt.b))
			}
			_, _, err := mergeThemes(repos)
			if err == nil {
				t.Fatal("no error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not contain %q:\n%v", want, err)
				}
			}
		})
	}
}

// TestMergeThemesBroken checks that a theme that fails to load is reported
// once and left out, while the others are loaded.
func TestMergeThemesBroken(t *testing.T) {
	t.Parallel()
	files := themeFiles("themes/one")
	files["themes/bad/variants/dark.toml"] = `mode = "dark"`
	r := themeRepo(t, t.TempDir(), "a", "[theme]\ndirs = [\"themes\"]\ndark = \"bad:dark\"", files)
	ts, _, err := mergeThemes([]*Repository{r})
	if err == nil || !strings.Contains(err.Error(), "bad") || strings.Contains(err.Error(), "no theme bad") {
		t.Errorf("err = %v, want only the error loading bad", err)
	}
	if got := slices.Sorted(maps.Keys(ts.themes)); !slices.Equal(got, []string{"one"}) {
		t.Errorf("themes = %v, want [one]", got)
	}
}

func TestValidateTheme(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dotfiles.toml"), []byte(`
[theme]
dirs  = [""]
dark  = "selenized"
light = "selenized:light"
mode  = "dim"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := NewRepository(dir, t.TempDir()).readConfig()
	for _, want := range []string{
		"theme.dirs[0] is empty",
		`theme.dark: invalid "selenized": want theme:variant`,
		`theme.mode: invalid "dim": want dark or light`,
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error does not contain %q:\n%v", want, err)
		}
	}
	if err != nil && strings.Contains(err.Error(), "theme.light") {
		t.Errorf("valid theme.light reported:\n%v", err)
	}
}

// testThemeSet returns a themeSet with the theme test, which has the variants
// dark, light and black, a second dark one.
func testThemeSet(t *testing.T) *themeSet {
	t.Helper()
	files := validTheme()
	black := testColors()
	black["br_red"] = "#000001"
	files["variants/black.toml"] = variantTOML("dark", black, map[string]string{"emacs": "x-black"})
	theme, err := loadTheme(writeTheme(t, files))
	if err != nil {
		t.Fatal(err)
	}
	return &themeSet{themes: map[string]*Theme{"test": theme}, dark: "test:dark", light: "test:light"}
}

func TestSelectTheme(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		state   string // the state file, if not empty
		mode    string // [theme] mode
		unset   string // dark or light to unset in [theme]
		option  string
		want    string // the variant, or "" for none
		wantErr string
	}{
		{name: "light by default", want: "light"},
		{name: "[theme] mode", mode: "dark", want: "dark"},
		{name: "state file", state: `mode = "dark"`, mode: "light", want: "dark"},
		{name: "state file over [theme] mode", state: `mode = "light"`, mode: "dark", want: "light"},
		{name: "state file theme", state: "mode = \"dark\"\ntheme = \"test:black\"", want: "black"},
		{name: "option", state: `mode = "dark"`, option: "test:light", want: "light"},
		{name: "option variant", option: ":black", want: "black"},
		{name: "option no variant", option: ":dim", wantErr: "theme test has no variant dim"},
		{name: "option no theme", option: "nope:dark", wantErr: "nope:dark: no such theme"},
		{name: "option form", option: "test", wantErr: `-theme: invalid "test"`},
		{name: "state file mode", state: `mode = "dim"`, wantErr: `mode: invalid "dim"`},
		{name: "state file key", state: `colour = "red"`, wantErr: "theme.toml"},
		{name: "no theme for the mode", state: `mode = "dark"`, unset: "dark", wantErr: "no theme for dark mode"},
		{name: "option without a theme for the mode", state: `mode = "dark"`, unset: "dark", option: ":light", wantErr: "no theme is selected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := testThemeSet(t)
			ts.mode = tt.mode
			switch tt.unset {
			case "dark":
				ts.dark = ""
			case "light":
				ts.light = ""
			}
			dst := t.TempDir()
			files := fstest.MapFS{}
			if tt.state != "" {
				files[themeStatePath] = tomlFile(tt.state)
			}
			if err := os.CopyFS(dst, files); err != nil {
				t.Fatal(err)
			}

			got, err := selectTheme(ts, tt.option, dst)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.variant != tt.want || got.theme.Name != "test" {
				t.Errorf("selected %+v, want test:%s", got, tt.want)
			}
		})
	}
}

func TestSelectThemeNone(t *testing.T) {
	t.Parallel()
	got, err := selectTheme(&themeSet{}, "", t.TempDir())
	if got != nil || err != nil {
		t.Errorf("selectTheme without themes = %v, %v, want nil, nil", got, err)
	}
}

// themeConfigRepo writes a repository with the theme test in themes/ and
// config after its [theme], and a destination whose state file chooses dark
// mode. It
// returns their paths.
func themeConfigRepo(t *testing.T, config string) (repo, dst string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	files := fstest.MapFS{"dotfiles.toml": tomlFile(`
[theme]
dirs  = ["themes"]
dark  = "test:dark"
light = "test:light"
` + config)}
	for name, content := range themeFiles("themes/test") {
		files[name] = tomlFile(content)
	}
	if err := os.CopyFS(repo, files); err != nil {
		t.Fatal(err)
	}
	dst = t.TempDir()
	if err := os.CopyFS(dst, fstest.MapFS{themeStatePath: tomlFile(`mode = "dark"`)}); err != nil {
		t.Fatal(err)
	}
	return repo, dst
}

// TestThemeEnv checks theme in [vars], conditions, templates and Starlark.
func TestThemeEnv(t *testing.T) {
	t.Parallel()
	repo, dst := themeConfigRepo(t, `
[vars]
red  = '{{.theme.colors.br_red}}'
mode = '{{expr "theme.mode"}}'
`)
	r := NewRepository(repo, dst)
	if err := r.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if r.vars["red"] != "#ff0000" || r.vars["mode"] != "dark" {
		t.Errorf("vars red, mode = %q, %q, want #ff0000, dark", r.vars["red"], r.vars["mode"])
	}

	got, err := evalExpr(r.exprEnv(), `theme.variant + " " + theme.apps.emacs + " " + theme["colors"]["bg_0"]`)
	if want := "dark x-dark " + testColors()["bg_0"]; err != nil || got != want {
		t.Errorf("expression = %v, %v, want %q", got, err, want)
	}

	out, err := renderTemplate("t", `{{.theme.name}} {{.theme.colors.color1}} {{.theme.dir}}`, r.evalContext())
	if want := "test " + testColors()[testAnsi()["color1"]] + " " + filepath.Join(repo, "themes", "test"); err != nil || string(out) != want {
		t.Errorf("template = %q, %v, want %q", out, err, want)
	}
	if _, err := renderTemplate("t", `{{.theme.colors.nope}}`, r.evalContext()); err == nil {
		t.Error("template with a missing color: no error")
	}

	prog, err := compileStarlark("test", `doc["red"] = theme["colors"]["br_red"]`)
	if err != nil {
		t.Fatal(err)
	}
	doc := starlark.NewDict(1)
	if _, err := prog.Init(&starlark.Thread{}, starlarkPredeclared("", "x", r.evalContext(), doc)); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := doc.Get(starlark.String("red")); v != starlark.String("#ff0000") {
		t.Errorf("starlark doc[red] = %v, want #ff0000", v)
	}
}

func TestThemeRefs(t *testing.T) {
	t.Parallel()
	repo, dst := themeConfigRepo(t, "")
	r := NewRepository(repo, dst)
	if err := r.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		src     string
		env     map[string]any
		want    any
		wantErr string
	}{
		{src: `theme.colors.nope`, wantErr: "undefined theme.colors.nope"},
		{src: `theme["colors"]["nope"]`, wantErr: "undefined theme.colors.nope"},
		{src: `theme.nope`, wantErr: "undefined theme.nope"},
		{src: `theme.colors.nope ?? "x"`, want: "x"},
		{src: `theme.mode`, env: exprEnv(evalContext{}), wantErr: "no theme is selected for theme.mode"},
	}
	for _, tt := range tests {
		env := tt.env
		if env == nil {
			env = r.exprEnv()
		}
		got, err := evalExpr(env, tt.src)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: err = %v, want one containing %q", tt.src, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s = %v, %v, want %v", tt.src, got, err, tt.want)
		}
	}
}

// TestThemeOption checks that -theme is used when loading repositories.
func TestThemeOption(t *testing.T) {
	t.Parallel()
	repo, dst := themeConfigRepo(t, "")
	var stderr strings.Builder
	d := &Dotfiles{Stdout: io.Discard, Stderr: &stderr, DstPath: dst, Theme: ":light", repos: []string{repo}}
	loaded := d.loadRepos()
	if len(loaded) != 1 || loaded[0].theme == nil || loaded[0].theme.variant != "light" {
		t.Fatalf("theme = %+v, want test:light; stderr:\n%s", loaded, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("stderr:\n%s", stderr.String())
	}
}

func TestColorFuncs(t *testing.T) {
	t.Parallel()
	env := exprEnv(evalContext{})
	for src, want := range map[string]string{
		`hexDigits("#103c48")`: "103c48",
		`rgb("#103c48")`:       "16,60,72",
		`rgb("#FFFFFF")`:       "255,255,255",
		`hex0x("#103c48")`:     "0x103c48",
	} {
		if got, err := evalExpr(env, src); err != nil || got != want {
			t.Errorf("%s = %v, %v, want %q", src, got, err, want)
		}
	}
	for _, src := range []string{`rgb("red")`, `hex0x("#103c4")`, `hexDigits("103c48")`} {
		if _, err := evalExpr(env, src); err == nil || !strings.Contains(err.Error(), "is not a #rrggbb color") {
			t.Errorf("%s: err = %v, want one about the color", src, err)
		}
	}

	out, err := renderTemplate("t", `{{rgb "#103c48"}} {{"#103c48" | hex0x}}`, evalContext{})
	if err != nil || string(out) != "16,60,72 0x103c48" {
		t.Errorf("template = %q, %v, want %q", out, err, "16,60,72 0x103c48")
	}
	if _, err := renderTemplate("t", `{{rgb "red"}}`, evalContext{}); err == nil {
		t.Error("template rgb of red: no error")
	}

	prog, err := compileStarlark("test", `doc["c"] = hexDigits("#103c48")`)
	if err != nil {
		t.Fatal(err)
	}
	doc := starlark.NewDict(1)
	if _, err := prog.Init(&starlark.Thread{}, starlarkPredeclared("", "x", evalContext{}, doc)); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := doc.Get(starlark.String("c")); v != starlark.String("103c48") {
		t.Errorf("starlark hexDigits = %v, want 103c48", v)
	}
	prog, err = compileStarlark("test", `rgb("red")`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prog.Init(&starlark.Thread{}, starlarkPredeclared("", "x", evalContext{}, starlark.NewDict(0))); err == nil || !strings.Contains(err.Error(), "is not a #rrggbb color") {
		t.Errorf("starlark rgb of red: err = %v", err)
	}
}

func TestMix(t *testing.T) {
	t.Parallel()
	env := exprEnv(evalContext{})
	for src, want := range map[string]string{
		`mix("#103c48", "#dbb32d", 0)`:   "#103c48",
		`mix("#103c48", "#dbb32d", 1.0)`: "#dbb32d",
		`mix("#000000", "#ffffff", 0.5)`: "#636363", // OKLab: the middle in lightness, not #808080
		`mix("#FFFFFF", "#ffffff", 0.3)`: "#ffffff",
	} {
		if got, err := evalExpr(env, src); err != nil || got != want {
			t.Errorf("%s = %v, %v, want %q", src, got, err, want)
		}
	}
	for src, wantErr := range map[string]string{
		`mix("red", "#ffffff", 0.5)`:     `mix: "red" is not a #rrggbb color`,
		`mix("#000000", "#ffffff", 1.5)`: "mix: 1.5 is not between 0 and 1",
	} {
		if _, err := evalExpr(env, src); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%s: err = %v, want %q", src, err, wantErr)
		}
	}

	out, err := renderTemplate("t", `{{mix "#000000" "#ffffff" 0.5}}`, evalContext{})
	if err != nil || string(out) != "#636363" {
		t.Errorf("template = %q, %v", out, err)
	}

	for code, want := range map[string]string{
		`doc["c"] = mix("#000000", "#ffffff", 0.5)`: "#636363",
		`doc["c"] = mix("#000000", "#ffffff", 1)`:   "#ffffff",
	} {
		prog, err := compileStarlark("test", code)
		if err != nil {
			t.Fatal(err)
		}
		doc := starlark.NewDict(1)
		if _, err := prog.Init(&starlark.Thread{}, starlarkPredeclared("", "x", evalContext{}, doc)); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if v, _, _ := doc.Get(starlark.String("c")); v != starlark.String(want) {
			t.Errorf("%s: doc[c] = %v, want %s", code, v, want)
		}
	}
}
