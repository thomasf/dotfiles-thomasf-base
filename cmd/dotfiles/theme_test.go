package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
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
