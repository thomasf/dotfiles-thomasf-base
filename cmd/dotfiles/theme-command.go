package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
)

// ThemeCommand runs dotfiles theme: it shows the theme variant in use, lists
// the themes, or switches the theme.
func (d *Dotfiles) ThemeCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles theme", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme [show | list [-names | -json] | preview [<variant>] | color [options] <color> | set <variant> | toggle | apply]\n\n")
		fmt.Fprintf(d.Stderr, "Show the theme variant in use, list the themes and their variants, show\n")
		fmt.Fprintf(d.Stderr, "the colors of a variant in the terminal, print a single color, switch the\n")
		fmt.Fprintf(d.Stderr, "theme, or apply the currently selected theme. color prints a single named color\n")
		fmt.Fprintf(d.Stderr, "in the specified format (default hex).\n")
		fmt.Fprintf(d.Stderr, "preview, color and set take dark or light, theme:variant, or :variant of the theme\n")
		fmt.Fprintf(d.Stderr, "in use; toggle switches between dark and light mode; apply applies the active theme.\n\n")
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
	wantArgs := map[string]int{"show": 0, "preview": 1, "set": 1, "toggle": 0, "apply": 0}
	n, ok := wantArgs[sub]
	switch {
	case sub != "list" && sub != "color" && !ok:
		return usage("unknown command " + sub)
	case sub == "preview" && len(args) > n, sub != "preview" && sub != "list" && sub != "color" && len(args) != n:
		return usage(fmt.Sprintf("%s takes %d arguments", sub, n))
	case (sub == "set" || sub == "toggle") && d.Theme != "":
		return usage("-theme can not be used with " + sub)
	}

	d.loadRepos()
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
		ref := ""
		if len(args) > 0 {
			ref = args[0]
		}
		return d.previewTheme(ref)
	case "color":
		return d.colorTheme(args)
	case "set", "toggle":
		st, err := d.nextThemeState(sub, args)
		if err != nil {
			return err
		}
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
		fmt.Fprintf(d.Stdout, "theme: %s (%s mode)\n", d.active.ref(), d.active.mode())
		d.sync(false, nil, eventTheme)
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
// value, and text in it on bg_0 with the contrast ratio, then the terminal
// colors.
func (d *Dotfiles) previewTheme(ref string) error {
	v, ref, err := d.resolveVariant(ref, "preview")
	if err != nil {
		return err
	}

	w := d.Stdout
	bg := v.Colors["bg_0"]
	fmt.Fprintf(w, "%s (%s mode)\n\n", ref, v.Mode)
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
		fmt.Fprintf(w, "  %s  %-*s  %s  %s %4.1f\n", swatch(c), width, n, c,
			onBackground(c, bg, " Text on bg_0 "), contrast(c, bg))
	}
	fmt.Fprintf(w, "\nTerminal colors:\n")
	for row := range 2 {
		fmt.Fprint(w, " ")
		for i := row * 8; i < row*8+8; i++ {
			fmt.Fprintf(w, " %2d %s", i, swatch(v.Colors[ansiColors[i]]))
		}
		fmt.Fprintln(w)
	}
	return nil
}

// swatch returns a block in the #rrggbb color c, for a terminal with 24-bit
// colors.
func swatch(c string) string {
	r, g, b := rgb8(c)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm████\x1b[0m", r, g, b)
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

// contrast returns the WCAG contrast ratio of the #rrggbb colors a and b,
// from 1 to 21.
func contrast(a, b string) float64 {
	lum := func(c string) float64 {
		col, _ := colorful.Hex(c)
		r, g, b := col.LinearRgb()
		return 0.2126*r + 0.7152*g + 0.0722*b
	}
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}
