package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
	"golang.org/x/term"
)

// ThemeCommand runs dotfiles theme: it shows the theme variant in use, lists
// the themes, or switches the theme.
func (d *Dotfiles) ThemeCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles theme", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme [show | list [-names | -json] | preview [-all] [<variant>] | lint [-all] [<variant>] | color [options] <color> | set <variant> | toggle | apply | background-image]\n\n")
		fmt.Fprintf(d.Stderr, "Show the theme variant in use, list the themes and their variants, show\n")
		fmt.Fprintf(d.Stderr, "the colors of a variant in the terminal, check variants for styling issues,\n")
		fmt.Fprintf(d.Stderr, "print a single color, switch the theme, or apply the currently selected theme.\n")
		fmt.Fprintf(d.Stderr, "color prints a single named color in the specified format (default hex).\n")
		fmt.Fprintf(d.Stderr, "preview, lint, color and set take dark or light, theme:variant, or :variant of the theme\n")
		fmt.Fprintf(d.Stderr, "in use; toggle switches between dark and light mode; apply applies the active theme.\n")
		fmt.Fprintf(d.Stderr, "background-image writes the background image of the theme in use and prints its path.\n\n")
		fmt.Fprintf(d.Stderr, "A switch saves the choice in ~/%s,\n", themeStatePath)
		fmt.Fprintf(d.Stderr, "installs the files of every repository (not [[go]] packages) and runs\n")
		fmt.Fprintf(d.Stderr, "the [[script]] entries with on = \"theme\". See dotfiles doc theme.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	usage := func(msg string) error {
		fmt.Fprintf(d.Stderr, "theme: %s\n\n", msg)
		fs.Usage()
		return fmt.Errorf("theme: %s", msg)
	}

	sub, args := "show", fs.Args()
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	wantArgs := map[string]int{"show": 0, "set": 1, "toggle": 0, "apply": 0, "background-image": 0}
	n, ok := wantArgs[sub]
	switch {
	case sub != "list" && sub != "color" && sub != "preview" && sub != "lint" && !ok:
		return usage("unknown command " + sub)
	case sub != "list" && sub != "color" && sub != "preview" && sub != "lint" && len(args) != n:
		return usage(fmt.Sprintf("%s takes %d arguments", sub, n))
	case (sub == "set" || sub == "toggle") && d.Theme != "":
		return usage("-theme can not be used with " + sub)
	}

	repos := d.loadRepos()
	if len(d.runErrors) > 0 {
		return errors.New("theme: the configs or themes failed to load")
	}
	switch sub {
	case "show":
		if d.active == nil {
			fmt.Fprintln(d.Stdout, "no theme is configured")
			return nil
		}
		fmt.Fprintf(d.Stdout, "%s (%s mode)\n", d.active.ref(), d.active.mode())
	case "list":
		return d.listThemes(args)
	case "preview":
		return d.previewTheme(args)
	case "lint":
		return d.lintTheme(args)
	case "color":
		return d.colorTheme(args)
	case "set", "toggle":
		st, err := d.nextThemeState(sub, args)
		if err != nil {
			return err
		}
		st.Seed = rand.Int64()
		next, err := chooseTheme(d.themes, "", st)
		if err != nil {
			return err
		}
		if next == nil {
			return errors.New("theme: no theme is configured")
		}
		if err := writeThemeState(d.DstPath, st); err != nil {
			return err
		}
		fmt.Fprintf(d.Stdout, "theme: %s (%s mode)\n", next.ref(), next.mode())
		d.sync(false, nil, eventTheme)
	case "apply":
		if d.active == nil {
			return errors.New("theme: no theme is configured")
		}
		// A new seed, for new random values in generated backgrounds.
		st, err := readThemeState(d.DstPath)
		if err != nil {
			return err
		}
		st.Seed = rand.Int64()
		if err := writeThemeState(d.DstPath, st); err != nil {
			return err
		}
		fmt.Fprintf(d.Stdout, "theme: %s (%s mode)\n", d.active.ref(), d.active.mode())
		d.sync(false, nil, eventTheme)
	case "background-image":
		if d.active == nil {
			return errors.New("theme: no theme is configured")
		}
		// Every repository sees the dpi var of the one that defines it.
		var dpi string
		if len(repos) > 0 {
			dpi = repos[0].vars["dpi"]
		}
		path, err := themeBackground(d.active, dpi)
		if err != nil {
			return err
		}
		fmt.Fprintln(d.Stdout, path)
	}
	return nil
}

// nextThemeState returns the state that set with args, or toggle, switches
// to. A variant that [theme] dark or light names is saved as only its mode,
// so that a change of [theme] applies to it.
func (d *Dotfiles) nextThemeState(sub string, args []string) (themeState, error) {
	if sub == "toggle" {
		if d.active == nil {
			return themeState{}, errors.New("theme: no theme is configured")
		}
		if d.active.mode() == "dark" {
			return themeState{Mode: "light"}, nil
		}
		return themeState{Mode: "dark"}, nil
	}

	arg := args[0]
	if arg == "dark" || arg == "light" {
		return themeState{Mode: arg}, nil
	}
	if variant, ok := strings.CutPrefix(arg, ":"); ok {
		if d.active == nil {
			return themeState{}, fmt.Errorf("theme: no theme is selected to take variant %s of", variant)
		}
		arg = d.active.theme.Name + ":" + variant
	}
	name, variant, err := parseThemeRef(arg)
	if err != nil {
		return themeState{}, fmt.Errorf("theme set: %w", err)
	}
	t, ok := d.themes.themes[name]
	if !ok {
		return themeState{}, fmt.Errorf("theme set: no theme %s", name)
	}
	v, ok := t.Variants[variant]
	if !ok {
		return themeState{}, fmt.Errorf("theme set: theme %s has no variant %s", name, variant)
	}
	st := themeState{Mode: v.Mode}
	if (v.Mode == "dark" && arg != d.themes.dark) || (v.Mode == "light" && arg != d.themes.light) {
		st.Theme = arg
	}
	return st, nil
}

// listThemes prints every variant of every theme. With -names, it prints only
// their names (theme:variant); with -json, it prints them as a JSON array.
// Otherwise it prints a table marking the one in use with * and naming the
// modes that [theme] dark and light use it for.
func (d *Dotfiles) listThemes(args []string) error {
	fs := flag.NewFlagSet("dotfiles theme list", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme list [-names | -json]\n\n")
		fmt.Fprintf(d.Stderr, "List every variant of every theme.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	names := fs.Bool("names", false, "only list the theme:variant names")
	asJSON := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("theme list: unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *names && *asJSON {
		fs.Usage()
		return errors.New("theme list: -names and -json cannot be combined")
	}

	var refs []string
	for _, name := range slices.Sorted(maps.Keys(d.themes.themes)) {
		for _, variant := range slices.Sorted(maps.Keys(d.themes.themes[name].Variants)) {
			refs = append(refs, name+":"+variant)
		}
	}

	if *names {
		for _, ref := range refs {
			fmt.Fprintln(d.Stdout, ref)
		}
		return nil
	}

	if *asJSON {
		type variantJSON struct {
			Name    string `json:"name"`
			Theme   string `json:"theme"`
			Variant string `json:"variant"`
			Mode    string `json:"mode"`
			Active  bool   `json:"active"`
			Dark    bool   `json:"dark"`
			Light   bool   `json:"light"`
		}
		entries := make([]variantJSON, len(refs))
		for i, ref := range refs {
			name, variant, _ := strings.Cut(ref, ":")
			entries[i] = variantJSON{
				Name:    ref,
				Theme:   name,
				Variant: variant,
				Mode:    d.themes.themes[name].Variants[variant].Mode,
				Active:  d.active != nil && d.active.ref() == ref,
				Dark:    d.themes.dark == ref,
				Light:   d.themes.light == ref,
			}
		}
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(d.Stdout, "%s\n", data)
		return nil
	}

	width := 0
	for _, ref := range refs {
		width = max(width, len(ref))
	}
	for _, ref := range refs {
		name, variant, _ := strings.Cut(ref, ":")
		mark := " "
		if d.active != nil && d.active.ref() == ref {
			mark = "*"
		}
		var uses []string
		if d.themes.dark == ref {
			uses = append(uses, "[theme] dark")
		}
		if d.themes.light == ref {
			uses = append(uses, "[theme] light")
		}
		line := fmt.Sprintf("%s %-*s  %-5s  %s", mark, width, ref, d.themes.themes[name].Variants[variant].Mode, strings.Join(uses, ", "))
		fmt.Fprintln(d.Stdout, strings.TrimRight(line, " "))
	}
	return nil
}

// resolveVariant returns the variant and theme:variant ref for the given
// variant specifier (dark, light, theme:variant, or :variant), or the active
// theme variant if ref is "".
func (d *Dotfiles) resolveVariant(ref, command string) (*Variant, string, error) {
	switch {
	case ref == "":
		if d.active == nil {
			return nil, "", fmt.Errorf("theme %s: no theme is configured", command)
		}
		ref = d.active.ref()
	case ref == "dark":
		ref = d.themes.dark
		if ref == "" {
			return nil, "", fmt.Errorf("theme %s: no dark theme is configured", command)
		}
	case ref == "light":
		ref = d.themes.light
		if ref == "" {
			return nil, "", fmt.Errorf("theme %s: no light theme is configured", command)
		}
	case strings.HasPrefix(ref, ":"):
		if d.active == nil {
			return nil, "", fmt.Errorf("theme %s: no theme is selected to take the variant of", command)
		}
		ref = d.active.theme.Name + ref
	}
	name, variant, err := parseThemeRef(ref)
	if err != nil {
		return nil, "", fmt.Errorf("theme %s: %w", command, err)
	}
	t, ok := d.themes.themes[name]
	if !ok {
		return nil, "", fmt.Errorf("theme %s: no theme %s", command, name)
	}
	v, ok := t.Variants[variant]
	if !ok {
		return nil, "", fmt.Errorf("theme %s: theme %s has no variant %s", command, name, variant)
	}
	return v, ref, nil
}

// colorTheme prints a single named color from a theme variant.
func (d *Dotfiles) colorTheme(args []string) error {
	fs := flag.NewFlagSet("dotfiles theme color", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme color [options] <color>\n\n")
		fmt.Fprintf(d.Stderr, "Print a single named color from a theme.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	themeOpt := fs.String("theme", "", "theme to use (dark, light, theme:variant, or :variant)")
	formatOpt := fs.String("format", "hex", "output format: hex, raw, 0x, rgb")
	rawOpt := fs.Bool("raw", false, "output without leading # (equivalent to -format raw)")
	rgbOpt := fs.Bool("rgb", false, "output as r,g,b (equivalent to -format rgb)")
	hex0xOpt := fs.Bool("0x", false, "output with leading 0x (equivalent to -format 0x)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("theme color: missing color name")
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return fmt.Errorf("theme color: unexpected arguments: %s", strings.Join(fs.Args()[1:], " "))
	}
	colorName := fs.Arg(0)

	numShort := 0
	for _, b := range []bool{*rawOpt, *rgbOpt, *hex0xOpt} {
		if b {
			numShort++
		}
	}
	if numShort > 1 {
		fs.Usage()
		return errors.New("theme color: only one of -raw, -rgb, and -0x can be specified")
	}

	format := *formatOpt
	if *rawOpt {
		if *formatOpt != "hex" && *formatOpt != "raw" {
			fs.Usage()
			return errors.New("theme color: -raw and -format cannot be combined")
		}
		format = "raw"
	} else if *rgbOpt {
		if *formatOpt != "hex" && *formatOpt != "rgb" {
			fs.Usage()
			return errors.New("theme color: -rgb and -format cannot be combined")
		}
		format = "rgb"
	} else if *hex0xOpt {
		if *formatOpt != "hex" && *formatOpt != "0x" {
			fs.Usage()
			return errors.New("theme color: -0x and -format cannot be combined")
		}
		format = "0x"
	}

	ref := *themeOpt
	if ref == "" {
		ref = d.Theme
	}
	v, refName, err := d.resolveVariant(ref, "color")
	if err != nil {
		return err
	}
	c, ok := v.Colors[colorName]
	if !ok && strings.HasPrefix(colorName, "br_") {
		base := strings.TrimPrefix(colorName, "br_")
		switch base {
		case "black", "white":
			c = brightNeutral(v.Colors, base)
			ok = c != ""
		default:
			c = brightAccent(v.Colors, base)
			ok = c != ""
		}
	}
	terminalBright := "palette"
	if d.active != nil {
		terminalBright = d.active.terminalBright
	} else if d.themes != nil && d.themes.terminalBright != "" {
		terminalBright = d.themes.terminalBright
	}
	if ok && terminalBright == "accents" {
		ansiIdx := slices.Index(ansiColors, colorName)
		if ansiIdx >= 8 && ansiIdx <= 15 {
			accents := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
			accName := accents[ansiIdx-8]
			if accName == "black" || accName == "white" {
				c = brightNeutral(v.Colors, accName)
			} else {
				c = brightAccent(v.Colors, accName)
			}
		}
	}
	if !ok {
		return fmt.Errorf("theme color: theme %s has no color %q", refName, colorName)
	}

	var out string
	switch format {
	case "hex":
		out = c
	case "raw", "strip-hash", "hexDigits":
		out = strings.TrimPrefix(c, "#")
	case "0x", "hex0x":
		out = "0x" + strings.TrimPrefix(c, "#")
	case "rgb":
		r, g, b := rgb8(c)
		out = fmt.Sprintf("%d,%d,%d", r, g, b)
	default:
		fs.Usage()
		return fmt.Errorf("theme color: unknown format %q", format)
	}

	fmt.Fprintln(d.Stdout, out)
	return nil
}

// previewTheme prints the colors of the variant ref, given as set takes it,
// or of the one in use if ref is "": each color as a swatch, its name and
// value, text in it on all backgrounds with WCAG contrast ratios, terminal
// text styling examples, then the terminal colors. -all previews all variants
// of all themes.
func (d *Dotfiles) previewTheme(args []string) error {
	fs := flag.NewFlagSet("dotfiles theme preview", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme preview [options] [<variant>]\n\n")
		fmt.Fprintf(d.Stderr, "Show the colors of a theme variant in the terminal with 24-bit colors:\n")
		fmt.Fprintf(d.Stderr, "metadata, swatches, hex values, text on every background with WCAG contrast\n")
		fmt.Fprintf(d.Stderr, "ratios, surface hierarchy, accent spectrum, terminal colors, and styled\n")
		fmt.Fprintf(d.Stderr, "terminal text.\n\n")
		fmt.Fprintf(d.Stderr, "The floating number after each background sample is the WCAG 2.1 contrast\n")
		fmt.Fprintf(d.Stderr, "ratio (1.0 to 21.0) between that color and background.\n\n")
		fmt.Fprintf(d.Stderr, "Contrast Badges (WCAG 2.1):\n")
		fmt.Fprintf(d.Stderr, "  AAA   >= 7.0:1   Enhanced contrast; recommended for all body text\n")
		fmt.Fprintf(d.Stderr, "  AA    >= 4.5:1   Standard minimum contrast for normal body text\n")
		fmt.Fprintf(d.Stderr, "  lg    >= 3.0:1   Acceptable for large text (>= 18pt / 14pt bold) and UI components\n")
		fmt.Fprintf(d.Stderr, "  ·      < 3.0:1   Low contrast; fails WCAG text guidelines (decorative only)\n\n")
		fmt.Fprintf(d.Stderr, "A warning is printed below the terminal examples if any combination produces\n")
		fmt.Fprintf(d.Stderr, "invisible text (< 1.2:1 contrast ratio, or < 1.10:1 in light mode).\n\n")
		fmt.Fprintf(d.Stderr, "preview takes dark or light, theme:variant, or :variant of the theme\n")
		fmt.Fprintf(d.Stderr, "in use, or the active variant if omitted. -all previews every variant.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	allOpt := fs.Bool("all", false, "preview all variants of all themes")
	themeOpt := fs.String("theme", "", "theme to use (dark, light, theme:variant, or :variant)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *allOpt && (fs.NArg() > 0 || *themeOpt != "") {
		fs.Usage()
		return errors.New("theme preview: <variant> and -all cannot be combined")
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return fmt.Errorf("theme preview: unexpected arguments: %s", strings.Join(fs.Args()[1:], " "))
	}
	if fs.NArg() == 1 && *themeOpt != "" {
		fs.Usage()
		return errors.New("theme preview: <variant> argument and -theme cannot be combined")
	}

	w := d.Stdout
	if *allOpt {
		var themeNames []string
		for name := range d.themes.themes {
			themeNames = append(themeNames, name)
		}
		slices.Sort(themeNames)

		first := true
		for _, name := range themeNames {
			t := d.themes.themes[name]
			for _, variantName := range slices.Sorted(maps.Keys(t.Variants)) {
				v := t.Variants[variantName]
				ref := name + ":" + variantName
				if !first {
					fmt.Fprintf(w, "%s\n\n", strings.Repeat("─", terminalWidth(w)))
				}
				first = false
				printVariantPreview(w, ref, v, t, variantName)
			}
		}
		return nil
	}

	ref := *themeOpt
	if fs.NArg() == 1 {
		ref = fs.Arg(0)
	}
	if ref == "" {
		ref = d.Theme
	}

	v, ref, err := d.resolveVariant(ref, "preview")
	if err != nil {
		return err
	}
	name, variant, _ := strings.Cut(ref, ":")
	t := d.themes.themes[name]

	printVariantPreview(w, ref, v, t, variant)
	return nil
}

// printVariantPreview displays the full terminal color preview for a variant:
// metadata, swatches, text contrast table, surfaces, accent spectrum, terminal
// colors table, and terminal text examples.
func printVariantPreview(w io.Writer, ref string, v *Variant, t *Theme, variant string) {
	fmt.Fprintf(w, "%s (%s mode)\n", ref, v.Mode)
	printAppsMetadata(w, v, t, variant)
	fmt.Fprintln(w)

	// Collect background colors: standard bg_0, bg_1, bg_2 first, then any other bg_* colors.
	var bgNames []string
	for _, name := range []string{"bg_0", "bg_1", "bg_2"} {
		if _, ok := v.Colors[name]; ok {
			bgNames = append(bgNames, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(v.Colors)) {
		if strings.HasPrefix(name, "bg_") && !slices.Contains(bgNames, name) {
			bgNames = append(bgNames, name)
		}
	}

	names := slices.Clone(requiredColors)
	for _, n := range slices.Sorted(maps.Keys(v.Colors)) {
		if !slices.Contains(names, n) && !slices.Contains(ansiColors, n) {
			names = append(names, n)
		}
	}
	width := 0
	for _, n := range names {
		width = max(width, len(n))
	}
	for _, n := range names {
		c := v.Colors[n]
		fmt.Fprintf(w, "  %s  %-*s  %s", swatch(c), width, n, c)
		for _, bgName := range bgNames {
			bgCol := v.Colors[bgName]
			cr := contrast(c, bgCol)
			fmt.Fprintf(w, "  %s %4.1f %s", onBackground(c, bgCol, fmt.Sprintf(" Text on %s ", bgName)), cr, wcagBadge(cr))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "\nSurfaces:\n")
	printSurfaces(w, v, bgNames)

	fmt.Fprintf(w, "\nAccent spectrum (red, orange, yellow, green, cyan, blue, violet, magenta):\n")
	printAccentSpectrum(w, v, bgNames)

	fmt.Fprintf(w, "\nTerminal colors:\n")
	for row := range 2 {
		fmt.Fprint(w, " ")
		for i := row * 8; i < row*8+8; i++ {
			fmt.Fprintf(w, " %2d %s", i, swatch(v.Colors[ansiColors[i]]))
		}
		fmt.Fprintln(w)
	}

	// Terminal examples below the terminal colors table: 8 lines incorporating most styling possibilities
	fmt.Fprintf(w, "\nTerminal examples:\n")
	printTerminalExamples(w, v)
	fmt.Fprintln(w)
}

// lintTheme checks a theme variant (or all themes if -all is specified) for
// styling and contrast issues such as invisible text combinations.
func (d *Dotfiles) lintTheme(args []string) error {
	fs := flag.NewFlagSet("dotfiles theme lint", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme lint [options] [<variant>]\n\n")
		fmt.Fprintf(d.Stderr, "Lint a theme variant or all themes for contrast and styling issues.\n")
		fmt.Fprintf(d.Stderr, "By default, lints the currently active theme.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	allOpt := fs.Bool("all", false, "lint all themes and variants")
	themeOpt := fs.String("theme", "", "theme to use (dark, light, theme:variant, or :variant)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *allOpt && (fs.NArg() > 0 || *themeOpt != "") {
		fs.Usage()
		return errors.New("theme lint: <variant> and -all cannot be combined")
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return fmt.Errorf("theme lint: unexpected arguments: %s", strings.Join(fs.Args()[1:], " "))
	}
	if fs.NArg() == 1 && *themeOpt != "" {
		fs.Usage()
		return errors.New("theme lint: <variant> argument and -theme cannot be combined")
	}

	w := d.Stdout
	if *allOpt {
		var themeNames []string
		for name := range d.themes.themes {
			themeNames = append(themeNames, name)
		}
		slices.Sort(themeNames)

		totalIssues := 0
		totalVariants := 0
		for _, name := range themeNames {
			t := d.themes.themes[name]
			for _, variantName := range slices.Sorted(maps.Keys(t.Variants)) {
				totalVariants++
				v := t.Variants[variantName]
				ref := name + ":" + variantName
				warnings := lintVariant(t, v)
				if len(warnings) > 0 {
					totalIssues += len(warnings)
					fmt.Fprintf(w, "%s:\n", ref)
					for _, warn := range warnings {
						fmt.Fprintf(w, "  warning: %s\n", warn.message)
						if warn.c1 != "" && warn.c2 != "" {
							fmt.Fprintf(w, "    example: %s\n", colorPairExample(warn.c1, warn.c2, warn.sample))
						} else if warn.c1 != "" {
							fmt.Fprintf(w, "    example: %s\n", swatch2(warn.c1))
						}
					}
				}
			}
		}
		if totalIssues == 0 {
			fmt.Fprintf(w, "all %d variants ok\n", totalVariants)
		} else {
			fmt.Fprintf(w, "\n%d warning(s) found across %d variants\n", totalIssues, totalVariants)
		}
		return nil
	}

	ref := *themeOpt
	if fs.NArg() == 1 {
		ref = fs.Arg(0)
	}
	if ref == "" {
		ref = d.Theme
	}

	v, ref, err := d.resolveVariant(ref, "lint")
	if err != nil {
		return err
	}
	name, _, _ := strings.Cut(ref, ":")
	t := d.themes.themes[name]

	warnings := lintVariant(t, v)
	if len(warnings) == 0 {
		fmt.Fprintf(w, "%s: ok\n", ref)
	} else {
		fmt.Fprintf(w, "%s:\n", ref)
		for _, warn := range warnings {
			fmt.Fprintf(w, "  warning: %s\n", warn.message)
			if warn.c1 != "" && warn.c2 != "" {
				fmt.Fprintf(w, "    example: %s\n", colorPairExample(warn.c1, warn.c2, warn.sample))
			} else if warn.c1 != "" {
				fmt.Fprintf(w, "    example: %s\n", swatch2(warn.c1))
			}
		}
	}
	return nil
}

// styleText returns text styled with the given ANSI SGR attribute codes
// (e.g. "1" for bold, "2" for dim, "3" for italic, "4" for underline,
// "7" for inverse video, "9" for strikethrough), with foreground color fg and
// background color bg (both #rrggbb hex or "").
func styleText(fg, bg, text string, styles ...string) string {
	var s strings.Builder
	if len(styles) > 0 {
		fmt.Fprintf(&s, "\x1b[%sm", strings.Join(styles, ";"))
	}
	if fg != "" {
		r, g, b := rgb8(fg)
		fmt.Fprintf(&s, "\x1b[38;2;%d;%d;%dm", r, g, b)
	}
	if bg != "" {
		r, g, b := rgb8(bg)
		fmt.Fprintf(&s, "\x1b[48;2;%d;%d;%dm", r, g, b)
	}
	s.WriteString(text)
	s.WriteString("\x1b[0m")
	return s.String()
}

type lintWarning struct {
	message string
	c1      string
	c2      string
	sample  string
}

type sampleWarning struct {
	text   string
	fgName string
	fgHex  string
	bgName string
	bgHex  string
	cr     float64
	minCR  float64
}

type terminalSnippet struct {
	text   string
	fgName string
	bgName string
	styles []string
}

// terminalExamples defines the text snippets, spacing, and styling used in
// the 8-line terminal examples demonstration.
var terminalExamples = [][]terminalSnippet{
	// Line 1: Command prompt with git branch, status badge in inverse video, command
	{
		{text: "❯ ", fgName: "green", bgName: "bg_0", styles: []string{"1"}},
		{text: "user@host", fgName: "blue", bgName: "bg_0", styles: []string{"1"}},
		{text: ":", fgName: "fg_0", bgName: "bg_0"},
		{text: "~/src/dotfiles", fgName: "cyan", bgName: "bg_0"},
		{text: " "},
		{text: "(main*)", fgName: "yellow", bgName: "bg_0"},
		{text: " "},
		{text: " STAGED ", fgName: "green", bgName: "bg_0", styles: []string{"7", "1"}},
		{text: " "},
		{text: `git commit -m "theme preview"`, fgName: "fg_1", bgName: "bg_0"},
	},

	// Line 2: Source code syntax highlighting with line number and indent guide
	{
		{text: "42 ", fgName: "dim_0", bgName: "bg_0"},
		{text: "│ ", fgName: "bg_1", bgName: "bg_0"},
		{text: "func ", fgName: "magenta", bgName: "bg_0", styles: []string{"1"}},
		{text: "PreviewTheme", fgName: "blue", bgName: "bg_0"},
		{text: "(ref ", fgName: "fg_0", bgName: "bg_0"},
		{text: "string", fgName: "cyan", bgName: "bg_0"},
		{text: ", verbose ", fgName: "fg_0", bgName: "bg_0"},
		{text: "bool", fgName: "cyan", bgName: "bg_0"},
		{text: ") (", fgName: "fg_0", bgName: "bg_0"},
		{text: "*Variant", fgName: "cyan", bgName: "bg_0"},
		{text: ", ", fgName: "fg_0", bgName: "bg_0"},
		{text: "error", fgName: "green", bgName: "bg_0"},
		{text: ") { ", fgName: "fg_0", bgName: "bg_0"},
		{text: "│ ", fgName: "bg_1", bgName: "bg_0"},
		{text: "// TODO: verify WCAG 2.1 contrast ratios", fgName: "dim_0", bgName: "bg_0", styles: []string{"2", "3"}},
	},

	// Line 3: Compiler diagnostic with bold red error badge, underline token, strikethrough deprecated
	{
		{text: "error[E0382]: ", fgName: "red", bgName: "bg_0", styles: []string{"1"}},
		{text: "use of moved value ", fgName: "fg_1", bgName: "bg_0"},
		{text: "theme", fgName: "red", bgName: "bg_0", styles: []string{"1", "4"}},
		{text: " in call to ", fgName: "fg_0", bgName: "bg_0"},
		{text: "render()", fgName: "blue", bgName: "bg_0"},
		{text: " (note: ", fgName: "dim_0", bgName: "bg_0"},
		{text: "renderLegacy()", fgName: "dim_0", bgName: "bg_0", styles: []string{"9"}},
		{text: " is deprecated)", fgName: "dim_0", bgName: "bg_0"},
	},

	// Line 4: Diff & search highlight with strikethrough, bold green, and inverse video search match
	{
		{text: "- ", fgName: "red", bgName: "bg_0"},
		{text: "oldContrastRatio(c, bg)", fgName: "dim_0", bgName: "bg_0", styles: []string{"2", "9"}},
		{text: "   "},
		{text: "+ ", fgName: "green", bgName: "bg_0", styles: []string{"1"}},
		{text: "contrast", fgName: "green", bgName: "bg_0", styles: []string{"7"}},
		{text: "(color, bg)", fgName: "green", bgName: "bg_0"},
		{text: " "},
		{text: "/* 4.5:1 text, 3.0:1 UI */", fgName: "orange", bgName: "bg_0", styles: []string{"3"}},
	},

	// Line 5: Git commit log with branch decorations, author, relative time
	{
		{text: "* ", fgName: "yellow", bgName: "bg_0", styles: []string{"1"}},
		{text: "a512d2c", fgName: "yellow", bgName: "bg_0"},
		{text: " (", fgName: "fg_0", bgName: "bg_0"},
		{text: "HEAD -> ", fgName: "green", bgName: "bg_0", styles: []string{"1"}},
		{text: "master", fgName: "cyan", bgName: "bg_0", styles: []string{"1"}},
		{text: ", ", fgName: "fg_0", bgName: "bg_0"},
		{text: "origin/master", fgName: "red", bgName: "bg_0", styles: []string{"1"}},
		{text: ") ", fgName: "fg_0", bgName: "bg_0"},
		{text: "feat(theme): preview styling", fgName: "fg_1", bgName: "bg_0"},
		{text: " (2 hours ago) <dev@example.com>", fgName: "dim_0", bgName: "bg_0", styles: []string{"3"}},
	},

	// Line 6: Test runner / CI result line with inverse video pass badge, duration, coverage
	{
		{text: " PASS ", fgName: "green", bgName: "bg_0", styles: []string{"7", "1"}},
		{text: " cmd/dotfiles/theme-command_test.go", fgName: "fg_1", bgName: "bg_0"},
		{text: " (14 passed, 0 failed)", fgName: "fg_0", bgName: "bg_0"},
		{text: " [1.32s]", fgName: "dim_0", bgName: "bg_0"},
		{text: " (coverage: 92.4%)", fgName: "cyan", bgName: "bg_0"},
	},

	// Line 7: URL / Hyperlink, faint divider in bg_1 on bg_0, and hint
	{
		{text: "ℹ ", fgName: "blue", bgName: "bg_0", styles: []string{"1"}},
		{text: "Documentation: ", fgName: "fg_0", bgName: "bg_0"},
		{text: "https://example.com", fgName: "cyan", bgName: "bg_0", styles: []string{"4"}},
		{text: " "},
		{text: "│ ", fgName: "bg_1", bgName: "bg_0"},
		{text: "[ctrl+click to open]", fgName: "dim_0", bgName: "bg_0", styles: []string{"2", "3"}},
		{text: " (see ", fgName: "dim_0", bgName: "bg_0"},
		{text: "dotfiles doc theme", fgName: "yellow", bgName: "bg_0"},
		{text: ")", fgName: "dim_0", bgName: "bg_0"},
	},

	// Line 8: Editor status bar with inverse video mode badge, filename on bg_1, cursor on bg_2
	{
		{text: " NORMAL ", fgName: "green", bgName: "bg_0", styles: []string{"7", "1"}},
		{text: " theme-command.go ", fgName: "fg_1", bgName: "bg_1", styles: []string{"1"}},
		{text: "[+] ", fgName: "yellow", bgName: "bg_1"},
		{text: "go ", fgName: "cyan", bgName: "bg_1"},
		{text: "utf-8 [unix] ", fgName: "dim_0", bgName: "bg_1"},
		{text: "85% ", fgName: "fg_0", bgName: "bg_1"},
		{text: " 42:15 ", fgName: "fg_1", bgName: "bg_2", styles: []string{"1"}},
	},
}

// lintTerminalVisibility checks the terminal examples against the variant colors
// for any combinations that produce invisible text (contrast ratio < 1.22:1, or
// < 1.10:1 in light mode where low contrast remains readable).
func lintTerminalVisibility(v *Variant) []sampleWarning {
	var warnings []sampleWarning
	seen := make(map[string]bool)
	minCR := 1.22
	if v.Mode == "light" {
		minCR = 1.1
	}
	for _, line := range terminalExamples {
		for _, s := range line {
			if s.fgName == "" || s.bgName == "" {
				continue
			}
			fgCol := v.Colors[s.fgName]
			bgCol := v.Colors[s.bgName]
			effectiveFgName, effectiveBgName := s.fgName, s.bgName
			effectiveFgCol, effectiveBgCol := fgCol, bgCol
			for _, style := range s.styles {
				if style == "7" {
					effectiveFgName, effectiveBgName = s.bgName, s.fgName
					effectiveFgCol, effectiveBgCol = bgCol, fgCol
					break
				}
			}
			if effectiveFgCol != "" && effectiveBgCol != "" {
				cr := contrast(effectiveFgCol, effectiveBgCol)
				trimmed := strings.TrimSpace(s.text)
				lowContrast := math.Round(cr*100)/100 < minCR
				if lowContrast && trimmed != "" {
					key := fmt.Sprintf("%s on %s", effectiveFgName, effectiveBgName)
					if !seen[key] {
						seen[key] = true
						runes := []rune(trimmed)
						if len(runes) > 20 {
							trimmed = string(runes[:17]) + "..."
						}
						warnings = append(warnings, sampleWarning{
							text:   trimmed,
							fgName: effectiveFgName,
							fgHex:  effectiveFgCol,
							bgName: effectiveBgName,
							bgHex:  effectiveBgCol,
							cr:     cr,
							minCR:  minCR,
						})
					}
				}
			}
		}
	}
	return warnings
}

// lintVariant runs styling, accessibility, and consistency lint checks on a variant,
// returning any warning messages.
func lintVariant(t *Theme, v *Variant) []lintWarning {
	var msgs []lintWarning

	// 1. Invisible text in terminal examples
	for _, warn := range lintTerminalVisibility(v) {
		crStr := fmt.Sprintf("%.1f:1", warn.cr)
		limitStr := fmt.Sprintf("%.2f:1", warn.minCR)
		msgs = append(msgs, lintWarning{
			message: fmt.Sprintf("invisible text in terminal examples: '%s' (%s %s on %s %s) has contrast ratio %s (< %s)", warn.text, warn.fgName, warn.fgHex, warn.bgName, warn.bgHex, crStr, limitStr),
			c1:      warn.fgHex,
			c2:      warn.bgHex,
			sample:  warn.text,
		})
	}

	bg0, hasBg0 := v.Colors["bg_0"]
	bg1, hasBg1 := v.Colors["bg_1"]
	bg2, hasBg2 := v.Colors["bg_2"]
	fg0, hasFg0 := v.Colors["fg_0"]
	fg1, hasFg1 := v.Colors["fg_1"]

	// 2. Surface hierarchy and monotonicity
	if hasBg0 && hasBg1 && hasBg2 {
		l0 := relativeLuminance(bg0)
		l1 := relativeLuminance(bg1)
		l2 := relativeLuminance(bg2)
		if v.Mode == "dark" {
			if l1 < l0-0.001 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("surface hierarchy inverted in dark mode: bg_1 (%s) is darker than bg_0 (%s)", bg1, bg0),
					c1:      bg1,
					c2:      bg0,
				})
			}
			if l2 < l1-0.001 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("surface hierarchy inverted in dark mode: bg_2 (%s) is darker than bg_1 (%s)", bg2, bg1),
					c1:      bg2,
					c2:      bg1,
				})
			}
		} else if v.Mode == "light" {
			if l1 > l0+0.001 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("surface hierarchy inverted in light mode: bg_1 (%s) is lighter than bg_0 (%s)", bg1, bg0),
					c1:      bg1,
					c2:      bg0,
				})
			}
			if l2 > l1+0.001 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("surface hierarchy inverted in light mode: bg_2 (%s) is lighter than bg_1 (%s)", bg2, bg1),
					c1:      bg2,
					c2:      bg1,
				})
			}
		}
		if bg0 == bg1 {
			msgs = append(msgs, lintWarning{
				message: fmt.Sprintf("surfaces bg_0 and bg_1 have identical color (%s)", bg0),
				c1:      bg0,
				c2:      bg1,
			})
		}
		if bg1 == bg2 {
			msgs = append(msgs, lintWarning{
				message: fmt.Sprintf("surfaces bg_1 and bg_2 have identical color (%s)", bg1),
				c1:      bg1,
				c2:      bg2,
			})
		}
	}

	// 3. Mode declaration and luminance alignment
	if hasBg0 && hasFg0 {
		l0 := relativeLuminance(bg0)
		lfg := relativeLuminance(fg0)
		if v.Mode == "dark" {
			if l0 >= lfg {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("mode is \"dark\" but background bg_0 (%s) is brighter than text fg_0 (%s)", bg0, fg0),
					c1:      fg0,
					c2:      bg0,
				})
			} else if l0 > 0.4 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("mode is \"dark\" but background bg_0 (%s) has high luminance (%.2f > 0.40)", bg0, l0),
					c1:      bg0,
				})
			}
		} else if v.Mode == "light" {
			if l0 <= lfg {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("mode is \"light\" but background bg_0 (%s) is darker than text fg_0 (%s)", bg0, fg0),
					c1:      fg0,
					c2:      bg0,
				})
			} else if l0 < 0.4 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("mode is \"light\" but background bg_0 (%s) has low luminance (%.2f < 0.40)", bg0, l0),
					c1:      bg0,
				})
			}
		}
	}

	// 4. Body text contrast
	if hasBg0 {
		if hasFg0 {
			cr := contrast(fg0, bg0)
			if cr < 3.0 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("low body text contrast: fg_0 (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 3.0:1)", fg0, bg0, cr),
					c1:      fg0,
					c2:      bg0,
				})
			}
		}
		if hasFg1 {
			cr := contrast(fg1, bg0)
			if cr < 4.5 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("low body text contrast: fg_1 (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 4.5:1)", fg1, bg0, cr),
					c1:      fg1,
					c2:      bg0,
				})
			}
		}
	}

	// 5. Dim / secondary text contrast (deliberately faint, threshold 1.5:1)
	if hasBg0 {
		if dim0, ok := v.Colors["dim_0"]; ok {
			cr := contrast(dim0, bg0)
			if cr < 1.5 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("dim text contrast too low: dim_0 (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 1.5:1)", dim0, bg0, cr),
					c1:      dim0,
					c2:      bg0,
				})
			}
		}
	}

	// 6. Syntax accents on base background
	accents := []string{"red", "orange", "yellow", "green", "cyan", "blue", "magenta", "violet"}
	if hasBg0 {
		for _, acc := range accents {
			if c, ok := v.Colors[acc]; ok {
				cr := contrast(c, bg0)
				if cr < 2.0 {
					msgs = append(msgs, lintWarning{
						message: fmt.Sprintf("syntax accent has very low contrast: %s (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 2.0:1)", acc, c, bg0, cr),
						c1:      c,
						c2:      bg0,
					})
				}
			}
		}
	}

	// 7. ANSI terminal color contrast on base background
	if hasBg0 {
		for i := 1; i <= 6; i++ {
			name := fmt.Sprintf("color%d", i)
			if c, ok := v.Colors[name]; ok {
				cr := contrast(c, bg0)
				if cr < 1.5 {
					msgs = append(msgs, lintWarning{
						message: fmt.Sprintf("ANSI accent on terminal background has very low contrast: %s (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 1.5:1)", name, c, bg0, cr),
						c1:      c,
						c2:      bg0,
					})
				}
			}
		}
		for i := 9; i <= 14; i++ {
			name := fmt.Sprintf("color%d", i)
			if c, ok := v.Colors[name]; ok {
				cr := contrast(c, bg0)
				if cr < 1.5 {
					msgs = append(msgs, lintWarning{
						message: fmt.Sprintf("ANSI bright accent on terminal background has very low contrast: %s (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 1.5:1)", name, c, bg0, cr),
						c1:      c,
						c2:      bg0,
					})
				}
			}
		}
		if c, ok := v.Colors["color7"]; ok {
			cr := contrast(c, bg0)
			if cr < 1.5 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("ANSI muted foreground on terminal background has very low contrast: color7 (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 1.5:1)", c, bg0, cr),
					c1:      c,
					c2:      bg0,
				})
			}
		}
		if c, ok := v.Colors["color15"]; ok {
			cr := contrast(c, bg0)
			if cr < 3.0 {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("ANSI bright foreground on terminal background has low contrast: color15 (%s) on bg_0 (%s) has contrast ratio %3.1f:1 (< 3.0:1)", c, bg0, cr),
					c1:      c,
					c2:      bg0,
				})
			}
		}
	}

	// 8. Accent color collisions / distinctiveness
	for i := 0; i < len(accents); i++ {
		for j := i + 1; j < len(accents); j++ {
			a1, a2 := accents[i], accents[j]
			c1, ok1 := v.Colors[a1]
			c2, ok2 := v.Colors[a2]
			if ok1 && ok2 {
				if c1 == c2 {
					msgs = append(msgs, lintWarning{
						message: fmt.Sprintf("accent color collision: %s (%s) and %s (%s) have identical color", a1, c1, a2, c2),
						c1:      c1,
						c2:      c2,
					})
				} else {
					col1, err1 := colorful.Hex(c1)
					col2, err2 := colorful.Hex(c2)
					if err1 == nil && err2 == nil {
						d := col1.DistanceCIEDE2000(col2)
						if d < 0.03 {
							msgs = append(msgs, lintWarning{
								message: fmt.Sprintf("accent color collision: %s (%s) and %s (%s) are visually indistinguishable (ΔE = %.3f < 0.030)", a1, c1, a2, c2, d),
								c1:      c1,
								c2:      c2,
							})
						}
					}
				}
			}
		}
	}

	// 9. Semantic ANSI mapping
	if t != nil && t.Ansi != nil {
		expectedRoots := map[string][]string{
			"color1":  {"red"},
			"color2":  {"green"},
			"color3":  {"yellow"},
			"color4":  {"blue"},
			"color5":  {"magenta", "purple", "violet"},
			"color6":  {"cyan", "aqua", "teal"},
			"color9":  {"red", "orange"},
			"color10": {"green"},
			"color11": {"yellow", "orange"},
			"color12": {"blue"},
			"color13": {"magenta", "purple", "violet"},
			"color14": {"cyan", "aqua", "teal"},
		}
		for _, name := range slices.Sorted(maps.Keys(expectedRoots)) {
			roots := expectedRoots[name]
			target, ok := t.Ansi[name]
			if !ok {
				continue
			}
			matched := false
			for _, r := range roots {
				if strings.Contains(strings.ToLower(target), r) {
					matched = true
					break
				}
			}
			if !matched {
				colVal := v.Colors[name]
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("ANSI mapping mismatch: ansi.%s maps to %q (%s, expected a %s color)", name, target, colVal, strings.Join(roots, "/")),
					c1:      colVal,
					c2:      bg0,
				})
			}
		}
	}

	// 10. Background asset integrity: a generated background is valid, an
	// image file exists.
	if bgAsset := v.Apps["background"]; bgAsset != "" {
		_, generated, err := generatedBackground(bgAsset, v.Colors, 0)
		if err != nil {
			msgs = append(msgs, lintWarning{message: fmt.Sprintf("invalid generated background: %v", err)})
		}
		if !generated && t != nil && t.Dir != "" {
			assetPath := bgAsset
			if !filepath.IsAbs(assetPath) {
				assetPath = filepath.Join(t.Dir, bgAsset)
			}
			if _, err := os.Stat(assetPath); err != nil {
				msgs = append(msgs, lintWarning{
					message: fmt.Sprintf("missing background asset: %s (not found at %s)", bgAsset, assetPath),
				})
			}
		}
	}

	return msgs
}

// printTerminalExamples prints 8 lines demonstrating terminal-specific styling
// possibilities: command prompt with inverse video badge, source code syntax
// highlighting with dim italic comments, compiler diagnostics with underline
// and strikethrough, git diff and search highlights with inverse video, git commit
// graph log, test runner output with inverse video pass badge, hyperlinked
// documentation URL with shortcut hints, and an editor status line with inverse
// video mode badge on background colors. It prints a warning below the examples
// if any combination produces invisible text (contrast ratio < 1.2:1).
func printTerminalExamples(w io.Writer, v *Variant) {
	for _, line := range terminalExamples {
		fmt.Fprint(w, "  ")
		for _, s := range line {
			if s.fgName == "" && s.bgName == "" {
				fmt.Fprint(w, s.text)
			} else {
				fmt.Fprint(w, styleText(v.Colors[s.fgName], v.Colors[s.bgName], s.text, s.styles...))
			}
		}
		fmt.Fprintln(w)
	}

	warnings := lintTerminalVisibility(v)
	if len(warnings) == 1 {
		fmt.Fprintf(w, "\nWarning: invisible text in terminal examples:\n")
	} else if len(warnings) > 1 {
		fmt.Fprintf(w, "\nWarnings: invisible text in terminal examples:\n")
	}
	for _, warn := range warnings {
		fmt.Fprintf(w, "  '%s' (%s %s on %s %s) has contrast ratio %3.1f:1\n", warn.text, warn.fgName, warn.fgHex, warn.bgName, warn.bgHex, warn.cr)
	}
}

// printSurfaces prints the UI surface hierarchy and contrast ratios between
// consecutive background shades (e.g. bg_0 -> bg_1 -> bg_2).
func printSurfaces(w io.Writer, v *Variant, bgNames []string) {
	var parts []string
	for i, name := range bgNames {
		c := v.Colors[name]
		parts = append(parts, fmt.Sprintf("%s %s (%s)", swatch(c), name, c))
		if i < len(bgNames)-1 {
			nextName := bgNames[i+1]
			nextC := v.Colors[nextName]
			cr := contrast(c, nextC)
			parts = append(parts, fmt.Sprintf("──[ %3.1f:1 ]──>", cr))
		}
	}
	if len(bgNames) > 2 {
		first := v.Colors[bgNames[0]]
		last := v.Colors[bgNames[len(bgNames)-1]]
		parts = append(parts, fmt.Sprintf(" (%s vs %s: %3.1f:1)", bgNames[0], bgNames[len(bgNames)-1], contrast(first, last)))
	}
	fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  "))
}

// printAccentSpectrum prints a continuous swatch ribbon of the theme accents
// against each background shade to visualize hue progression and contrast.
func printAccentSpectrum(w io.Writer, v *Variant, bgNames []string) {
	accents := []string{"red", "orange", "yellow", "green", "cyan", "blue", "violet", "magenta"}
	labelWidth := len("bright:")
	for _, bgName := range bgNames {
		labelWidth = max(labelWidth, len(bgName)+1)
	}
	for _, bgName := range bgNames {
		bgCol := v.Colors[bgName]
		fmt.Fprintf(w, "  %-*s ", labelWidth, bgName+":")
		fmt.Fprint(w, styleText("", bgCol, "  "))
		for _, acc := range accents {
			c, ok := v.Colors[acc]
			if !ok {
				continue
			}
			fmt.Fprint(w, styleText(c, bgCol, "████")+styleText("", bgCol, "  "))
		}
		fmt.Fprintln(w)
	}

	// If bright accents exist, display them on bg_0
	hasBright := false
	for _, acc := range accents {
		if _, ok := v.Colors["br_"+acc]; ok {
			hasBright = true
			break
		}
	}
	if hasBright && len(bgNames) > 0 {
		bg0 := v.Colors[bgNames[0]]
		fmt.Fprintf(w, "  %-*s ", labelWidth, "bright:")
		fmt.Fprint(w, styleText("", bg0, "  "))
		for _, acc := range accents {
			c, ok := v.Colors["br_"+acc]
			if !ok {
				c = v.Colors[acc]
			}
			fmt.Fprint(w, styleText(c, bg0, "████")+styleText("", bg0, "  "))
		}
		fmt.Fprintln(w)
	}
}

// printAppsMetadata prints the creator, upstream URL, license, variant file path,
// configured app theme mappings, and description indented under the theme title.
func printAppsMetadata(w io.Writer, v *Variant, t *Theme, variant string) {
	if len(v.Apps) == 0 && (t == nil || (t.Dir == "" && t.Creator == "" && t.URL == "" && t.License == "" && t.Description == "")) {
		return
	}
	width := 0
	if t != nil && t.Creator != "" {
		width = max(width, len("creator:"))
	}
	if t != nil && t.URL != "" {
		width = max(width, len("url:"))
	}
	if t != nil && t.License != "" {
		width = max(width, len("license:"))
	}
	if t != nil && t.Dir != "" {
		width = max(width, len("file:"))
	}
	for _, k := range slices.Sorted(maps.Keys(v.Apps)) {
		if k == "background_mode" {
			continue
		}
		width = max(width, len(k)+1)
	}
	if t != nil && t.Creator != "" {
		fmt.Fprintf(w, "  %-*s  %s\n", width, "creator:", t.Creator)
	}
	if t != nil && t.URL != "" {
		fmt.Fprintf(w, "  %-*s  %s\n", width, "url:", t.URL)
	}
	if t != nil && t.License != "" {
		fmt.Fprintf(w, "  %-*s  %s\n", width, "license:", t.License)
	}
	if t != nil && t.Dir != "" {
		filePath := filepath.Join(t.Dir, "variants", variant+".toml")
		fmt.Fprintf(w, "  %-*s  %s\n", width, "file:", filePath)
	}
	for _, k := range slices.Sorted(maps.Keys(v.Apps)) {
		if k == "background_mode" {
			continue
		}
		val := v.Apps[k]
		if k == "background" && v.Apps["background_mode"] != "" {
			val = fmt.Sprintf("%s (mode: %s)", val, v.Apps["background_mode"])
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, k+":", val)
	}
	if t != nil && t.Description != "" {
		fmt.Fprintln(w)
		for _, line := range strings.Split(strings.TrimSpace(t.Description), "\n") {
			if line == "" {
				fmt.Fprintln(w)
			} else {
				fmt.Fprintf(w, "  %s\n", line)
			}
		}
	}
}

// terminalWidth returns the width of the terminal for w, or 80 if it cannot
// be determined.
func terminalWidth(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
			return width
		}
	}
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if c, err := strconv.Atoi(cols); err == nil && c > 0 {
			return c
		}
	}
	return 80
}

// swatch returns a block in the #rrggbb color c, for a terminal with 24-bit
// colors.
func swatch(c string) string {
	r, g, b := rgb8(c)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm████\x1b[0m", r, g, b)
}

// swatch2 returns a 2-character wide block in the #rrggbb color c, for a terminal
// with 24-bit colors.
func swatch2(c string) string {
	r, g, b := rgb8(c)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm██\x1b[0m", r, g, b)
}

// colorPairExample returns terminal output demonstrating both colors as
// separate 2-character wide blocks (swatch2) and composed on each other (with
// sample text if provided, or "Aa" by default).
func colorPairExample(c1, c2, sample string) string {
	res := swatch2(c1) + " " + swatch2(c2) + " " + onBackground(c1, c2, "Aa")
	if sample != "" && sample != "Aa" {
		res += " " + onBackground(c1, c2, sample)
	}
	return res
}

// onBackground returns text in the color fg on the color bg, for a terminal
// with 24-bit colors.
func onBackground(fg, bg, text string) string {
	fr, fg8, fb := rgb8(fg)
	br, bg8, bb := rgb8(bg)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%s\x1b[0m", fr, fg8, fb, br, bg8, bb, text)
}

// rgb8 returns the components of the #rrggbb color c.
func rgb8(c string) (r, g, b uint8) {
	col, _ := colorful.Hex(c) // checked when the theme was loaded
	return col.RGB255()
}

// relativeLuminance returns the relative luminance of the #rrggbb color c (0 to 1).
func relativeLuminance(c string) float64 {
	col, _ := colorful.Hex(c)
	r, g, b := col.LinearRgb()
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// contrast returns the WCAG contrast ratio of the #rrggbb colors a and b,
// from 1 to 21.
func contrast(a, b string) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// wcagBadge returns a compact WCAG 2.1 contrast rating badge:
// "AAA" for >= 7.0 (enhanced contrast), "AA " for >= 4.5 (normal text),
// "lg " for >= 3.0 (large text and UI components), and " · " for < 3.0.
func wcagBadge(cr float64) string {
	switch {
	case cr >= 7.0:
		return "AAA"
	case cr >= 4.5:
		return "AA "
	case cr >= 3.0:
		return "lg "
	default:
		return " · "
	}
}
