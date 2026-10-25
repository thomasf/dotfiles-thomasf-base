package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/pelletier/go-toml/v2"
)

// A Theme is a set of color palettes, one for each variant, read from a theme
// directory:
//
//	theme.toml            [ansi]: the terminal colors
//	variants/<name>.toml  mode, [colors] and [apps] of the variant <name>
type Theme struct {
	Name     string              // the name of the directory
	Dir      string              // the directory
	Variants map[string]*Variant // by name
}

// A Variant is one palette of a [Theme].
type Variant struct {
	Mode   string            // "dark" or "light"
	Colors map[string]string // [colors], and color0 to color15 from [ansi]
	Apps   map[string]string // [apps]: names of app themes and other settings
}

// themeFile is the content of theme.toml.
type themeFile struct {
	Ansi map[string]string `toml:"ansi"`
}

// variantFile is the content of a file in variants/.
type variantFile struct {
	Mode   string            `toml:"mode"`
	Colors map[string]string `toml:"colors"`
	Apps   map[string]string `toml:"apps"`
}

// requiredColors are the colors that every variant defines: the neutral
// colors, with their Selenized names, and the accents.
var requiredColors = []string{
	"bg_0", "bg_1", "bg_2", "dim_0", "fg_0", "fg_1",
	"yellow", "orange", "red", "magenta", "violet", "blue", "cyan", "green",
}

// ansiColors are the names of the 16 terminal colors, color0 to color15.
var ansiColors = func() []string {
	names := make([]string, 16)
	for i := range names {
		names[i] = "color" + strconv.Itoa(i)
	}
	return names
}()

var (
	// themeNameRe matches the names of themes and variants, which can be
	// written as theme:variant.
	themeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	// colorNameRe matches the names of colors and apps, which templates
	// can use as fields, as in {{.theme.colors.bg_0}}.
	colorNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	hexColorRe  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// loadTheme reads the theme in dir. Every variant must define the
// requiredColors, every variant must have the same colors and apps as the
// others, and [ansi] must map each of color0 to color15 to a color name.
// All problems are reported, not only the first.
func loadTheme(dir string) (*Theme, error) {
	t := &Theme{Name: filepath.Base(dir), Dir: dir, Variants: map[string]*Variant{}}
	if !themeNameRe.MatchString(t.Name) {
		return nil, fmt.Errorf("%s: theme name %q is not lower case letters, digits, _ and -", dir, t.Name)
	}

	var tf themeFile
	themePath := filepath.Join(dir, "theme.toml")
	data, err := os.ReadFile(themePath)
	if err != nil {
		return nil, err
	}
	if err := decodeTOML(themePath, data, &tf); err != nil {
		return nil, err
	}

	paths, err := filepath.Glob(filepath.Join(dir, "variants", "*.toml"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s: no variants/*.toml", dir)
	}

	var errs []error
	files := map[string]variantFile{}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".toml")
		vf, err := readVariant(path)
		if err == nil && !themeNameRe.MatchString(name) {
			err = fmt.Errorf("%s: variant name %q is not lower case letters, digits, _ and -", path, name)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		files[name] = vf
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	errs = append(errs, sameKeys(dir, "colors", files, func(vf variantFile) map[string]string { return vf.Colors })...)
	errs = append(errs, sameKeys(dir, "apps", files, func(vf variantFile) map[string]string { return vf.Apps })...)

	// sameKeys reports the colors that only some variants have, so [ansi] is
	// checked against the colors of all of them.
	known := map[string]bool{}
	for _, vf := range files {
		for name := range vf.Colors {
			known[name] = true
		}
	}
	for _, name := range ansiColors {
		ref, ok := tf.Ansi[name]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: ansi.%s is missing", themePath, name))
		case !known[ref]:
			errs = append(errs, fmt.Errorf("%s: ansi.%s: %q is not a color of the variants", themePath, name, ref))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(tf.Ansi)) {
		if !slices.Contains(ansiColors, name) {
			errs = append(errs, fmt.Errorf("%s: ansi.%s: only color0 to color15 can be set", themePath, name))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	for name, vf := range files {
		v := &Variant{Mode: vf.Mode, Colors: maps.Clone(vf.Colors), Apps: vf.Apps}
		if v.Apps == nil {
			v.Apps = map[string]string{}
		}
		for _, ansi := range ansiColors {
			v.Colors[ansi] = vf.Colors[tf.Ansi[ansi]]
		}
		t.Variants[name] = v
	}
	return t, nil
}

// readVariant reads and checks the variant file path on its own: its mode,
// the names and values of its colors and apps, and the requiredColors.
func readVariant(path string) (variantFile, error) {
	var vf variantFile
	data, err := os.ReadFile(path)
	if err != nil {
		return vf, err
	}
	if err := decodeTOML(path, data, &vf); err != nil {
		return vf, err
	}

	var errs []error
	if vf.Mode != "dark" && vf.Mode != "light" {
		errs = append(errs, fmt.Errorf("%s: mode is %q, not \"dark\" or \"light\"", path, vf.Mode))
	}
	var missing []string
	for _, name := range requiredColors {
		if _, ok := vf.Colors[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("%s: missing colors: %s", path, strings.Join(missing, ", ")))
	}
	for _, name := range slices.Sorted(maps.Keys(vf.Colors)) {
		switch value := vf.Colors[name]; {
		case slices.Contains(ansiColors, name):
			errs = append(errs, fmt.Errorf("%s: colors.%s: color0 to color15 are set in [ansi] of theme.toml", path, name))
		case !colorNameRe.MatchString(name):
			errs = append(errs, fmt.Errorf("%s: colors.%s: a name is lower case letters, digits and _, starting with a letter", path, name))
		case !hexColorRe.MatchString(value):
			errs = append(errs, fmt.Errorf("%s: colors.%s: %q is not a #rrggbb color", path, name, value))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(vf.Apps)) {
		if !colorNameRe.MatchString(name) {
			errs = append(errs, fmt.Errorf("%s: apps.%s: a name is lower case letters, digits and _, starting with a letter", path, name))
		}
	}
	return vf, errors.Join(errs...)
}

// sameKeys reports the keys of the table section that some variants in files
// have and others do not. keys returns the table of a variant.
func sameKeys(dir, section string, files map[string]variantFile, keys func(variantFile) map[string]string) []error {
	all := map[string]bool{}
	for _, vf := range files {
		for k := range keys(vf) {
			all[k] = true
		}
	}
	var errs []error
	for _, k := range slices.Sorted(maps.Keys(all)) {
		var missing []string
		for _, name := range slices.Sorted(maps.Keys(files)) {
			if _, ok := keys(files[name])[k]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			errs = append(errs, fmt.Errorf("%s: %s.%s is missing in variants %s", dir, section, k, strings.Join(missing, ", ")))
		}
	}
	return errs
}

// parseThemeRef splits ref, written theme:variant, into its parts.
func parseThemeRef(ref string) (theme, variant string, err error) {
	theme, variant, ok := strings.Cut(ref, ":")
	if !ok || !themeNameRe.MatchString(theme) || !themeNameRe.MatchString(variant) {
		return "", "", fmt.Errorf("invalid %q: want theme:variant", ref)
	}
	return theme, variant, nil
}

// A themeSet is the themes of all repositories, and the [theme] settings
// that choose between them.
type themeSet struct {
	themes map[string]*Theme // by name
	dark   string            // theme:variant of dark mode, or ""
	light  string            // theme:variant of light mode, or ""
	mode   string            // the mode until one is chosen, or ""
}

// mergeThemes loads the themes in the [theme] dirs of repos. Each
// subdirectory of a dir, except hidden ones, is a theme. dark, light and
// mode are taken from the first repository, in the order of orderRepos,
// that sets them; unlike [vars], every repository gets the same ones.
//
// It returns a warning for each of dark, light and mode that repositories
// set differently, and an error for each theme that fails to load, each
// theme name that is used twice, and each of dark and light that names no
// variant of that mode. A theme that fails to load is left out.
func mergeThemes(repos []*Repository) (*themeSet, []string, error) {
	ts := &themeSet{themes: map[string]*Theme{}}
	var warnings []string
	var errs []error
	failed := map[string]bool{}

	type setting struct {
		key    string
		get    func(ThemeConfig) string
		target *string
	}
	settings := []setting{
		{"dark", func(c ThemeConfig) string { return c.Dark }, &ts.dark},
		{"light", func(c ThemeConfig) string { return c.Light }, &ts.light},
		{"mode", func(c ThemeConfig) string { return c.Mode }, &ts.mode},
	}
	setBy := map[string][]*Repository{}

	for _, r := range orderRepos(repos) {
		for _, s := range settings {
			if v := s.get(r.config.Theme); v != "" {
				if *s.target == "" {
					*s.target = v
				}
				setBy[s.key] = append(setBy[s.key], r)
			}
		}
		for _, dir := range r.config.Theme.Dirs {
			dir = filepath.Join(r.srcRoot, dir)
			entries, err := os.ReadDir(dir)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: theme.dirs: %w", r.configPath, err))
				continue
			}
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				path, err := filepath.Abs(filepath.Join(dir, e.Name()))
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if prev, ok := ts.themes[e.Name()]; ok {
					errs = append(errs, fmt.Errorf("theme %s is in both %s and %s", e.Name(), prev.Dir, path))
					continue
				}
				t, err := loadTheme(path)
				if err != nil {
					errs = append(errs, err)
					failed[e.Name()] = true
					continue
				}
				ts.themes[t.Name] = t
			}
		}
	}

	for _, s := range settings {
		rs := setBy[s.key]
		if !slices.ContainsFunc(rs, func(r *Repository) bool { return s.get(r.config.Theme) != *s.target }) {
			continue
		}
		var paths []string
		for _, r := range rs {
			paths = append(paths, r.configPath)
		}
		warnings = append(warnings, fmt.Sprintf("theme.%s is set differently in %s; using %s",
			s.key, strings.Join(paths, ", "), rs[0].configPath))
	}

	for _, s := range settings[:2] {
		ref := *s.target
		if ref == "" {
			continue
		}
		name, variant, err := parseThemeRef(ref) // checked by validate
		if err != nil || failed[name] {
			continue
		}
		t, ok := ts.themes[name]
		if !ok {
			errs = append(errs, fmt.Errorf("theme.%s: no theme %s", s.key, name))
			continue
		}
		v, ok := t.Variants[variant]
		if !ok {
			errs = append(errs, fmt.Errorf("theme.%s: theme %s has no variant %s", s.key, name, variant))
			continue
		}
		if v.Mode != s.key {
			errs = append(errs, fmt.Errorf("theme.%s: %s is a %s variant", s.key, ref, v.Mode))
		}
	}
	return ts, warnings, errors.Join(errs...)
}

// An activeTheme is the theme variant in use.
type activeTheme struct {
	theme   *Theme
	variant string
}

// value returns the theme value of conditions, templates and Starlark code:
// a map with name, variant, mode, dir, colors and apps. It returns an untyped
// nil if a is nil, so that using theme then fails rather than giving empty
// values.
func (a *activeTheme) value() any {
	if a == nil {
		return nil
	}
	v := a.theme.Variants[a.variant]
	return map[string]any{
		"name":    a.theme.Name,
		"variant": a.variant,
		"mode":    v.Mode,
		"dir":     a.theme.Dir,
		"colors":  anyMap(v.Colors),
		"apps":    anyMap(v.Apps),
	}
}

// anyMap returns m as a map[string]any, which Starlark values are made from.
func anyMap(m map[string]string) map[string]any {
	am := make(map[string]any, len(m))
	for k, v := range m {
		am[k] = v
	}
	return am
}

// themeStatePath is the file, relative to the destination path, that holds
// the chosen mode and theme.
const themeStatePath = ".local/state/dotfiles/theme.toml"

// darkmodePath is the file, relative to the destination path, that the
// darkmode script creates while dark mode is on. Until the state file
// exists, it decides the mode.
const darkmodePath = ".config/darkmode"

// themeState is the content of the state file.
type themeState struct {
	Mode  string `toml:"mode"`
	Theme string `toml:"theme,omitempty"` // theme:variant to use instead of [theme] dark or light
}

// readThemeState reads the state file in dstRoot. Without one, the mode is
// dark if the darkmode file exists, and otherwise unset.
func readThemeState(dstRoot string) (themeState, error) {
	var st themeState
	path := filepath.Join(dstRoot, themeStatePath)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(dstRoot, darkmodePath)); err == nil {
			st.Mode = "dark"
		}
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := decodeTOML(path, data, &st); err != nil {
		return st, err
	}
	if st.Mode != "" && st.Mode != "dark" && st.Mode != "light" {
		return st, fmt.Errorf("%s: mode: invalid %q: want dark or light", path, st.Mode)
	}
	if st.Theme != "" {
		if _, _, err := parseThemeRef(st.Theme); err != nil {
			return st, fmt.Errorf("%s: theme: %w", path, err)
		}
	}
	return st, nil
}

// selectTheme returns the theme variant in use, with the state file in
// dstRoot (see readThemeState and chooseTheme).
func selectTheme(ts *themeSet, option, dstRoot string) (*activeTheme, error) {
	st, err := readThemeState(dstRoot)
	if err != nil {
		return nil, err
	}
	return chooseTheme(ts, option, st)
}

// chooseTheme returns the theme variant that the state st chooses, or nil if
// none is configured. The mode is that of st, else [theme] mode, else light.
// The variant is st's theme, else [theme] dark or light for the mode.
// option, the -theme option, overrides that: theme:variant replaces it, and
// :variant only replaces the variant.
func chooseTheme(ts *themeSet, option string, st themeState) (*activeTheme, error) {
	mode := cmp.Or(st.Mode, ts.mode, "light")
	ref := st.Theme
	if ref == "" {
		ref = ts.light
		if mode == "dark" {
			ref = ts.dark
		}
	}

	if option != "" {
		if variant, ok := strings.CutPrefix(option, ":"); ok {
			if ref == "" {
				return nil, fmt.Errorf("-theme %s: no theme is selected to take variant %s of", option, variant)
			}
			name, _, _ := strings.Cut(ref, ":")
			option = name + ":" + variant
		}
		if _, _, err := parseThemeRef(option); err != nil {
			return nil, fmt.Errorf("-theme: %w", err)
		}
		ref = option
	}

	if ref == "" {
		if ts.dark != "" || ts.light != "" {
			return nil, fmt.Errorf("no theme for %s mode: set [theme] %[1]s", mode)
		}
		return nil, nil
	}
	name, variant, err := parseThemeRef(ref)
	if err != nil {
		return nil, err
	}
	t, ok := ts.themes[name]
	if !ok {
		return nil, fmt.Errorf("theme %s: no such theme", ref)
	}
	if _, ok := t.Variants[variant]; !ok {
		return nil, fmt.Errorf("theme %s: theme %s has no variant %s", ref, name, variant)
	}
	return &activeTheme{theme: t, variant: variant}, nil
}

// colorFunc returns the function name of conditions, templates and Starlark
// code that writes a #rrggbb color in another form: format gets its six hex
// digits. Anything but a #rrggbb color is an error.
func colorFunc(name string, format func(hex string) string) func(string) (string, error) {
	return func(color string) (string, error) {
		if !hexColorRe.MatchString(color) {
			return "", fmt.Errorf("%s: %q is not a #rrggbb color", name, color)
		}
		return format(color[1:]), nil
	}
}

// ref returns the theme variant as theme:variant.
func (a *activeTheme) ref() string {
	return a.theme.Name + ":" + a.variant
}

// mode returns the mode of the theme variant.
func (a *activeTheme) mode() string {
	return a.theme.Variants[a.variant].Mode
}

// writeThemeState writes st to the state file in dstRoot, replacing it
// atomically. It also creates the darkmode file in dark mode and removes it
// in light mode, for the programs that still read it.
func writeThemeState(dstRoot string, st themeState) error {
	data, err := toml.Marshal(st)
	if err != nil {
		return err
	}
	path := filepath.Join(dstRoot, themeStatePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".theme-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}

	darkmode := filepath.Join(dstRoot, darkmodePath)
	if st.Mode == "dark" {
		if err := os.MkdirAll(filepath.Dir(darkmode), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(darkmode, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		return f.Close()
	}
	if err := os.Remove(darkmode); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// dir returns the directory of the theme, or "" if a is nil.
func (a *activeTheme) dir() string {
	if a == nil {
		return ""
	}
	return a.theme.Dir
}

// mixColors returns the color t of the way from the #rrggbb color a to b,
// with t between 0 and 1. The colors are mixed in OKLab, a perceptual color
// space, so that equal steps look equal whatever the hues.
func mixColors(a, b string, t float64) (string, error) {
	for _, c := range []string{a, b} {
		if !hexColorRe.MatchString(c) {
			return "", fmt.Errorf("mix: %q is not a #rrggbb color", c)
		}
	}
	if t < 0 || t > 1 {
		return "", fmt.Errorf("mix: %v is not between 0 and 1", t)
	}
	ca, _ := colorful.Hex(a) // checked above
	cb, _ := colorful.Hex(b)
	return ca.BlendOkLab(cb, t).Clamped().Hex(), nil
}
