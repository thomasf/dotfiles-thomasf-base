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
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme [show | list [-names | -json] | preview [<variant>] | set <variant> | toggle]\n\n")
		fmt.Fprintf(d.Stderr, "Show the theme variant in use, list the themes and their variants, show\n")
		fmt.Fprintf(d.Stderr, "the colors of a variant in the terminal, or switch the theme. preview and\n")
		fmt.Fprintf(d.Stderr, "set take dark or light, theme:variant, or :variant of the theme in use;\n")
		fmt.Fprintf(d.Stderr, "toggle switches between dark and light mode.\n\n")
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
	wantArgs := map[string]int{"show": 0, "preview": 1, "set": 1, "toggle": 0}
	n, ok := wantArgs[sub]
	switch {
	case sub != "list" && !ok:
		return usage("unknown command " + sub)
	case sub == "preview" && len(args) > n, sub != "preview" && sub != "list" && len(args) != n:
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

// previewTheme prints the colors of the variant ref, given as set takes it,
// or of the one in use if ref is "": each color as a swatch, its name and
// value, and text in it on bg_0 with the contrast ratio, then the terminal
// colors.
func (d *Dotfiles) previewTheme(ref string) error {
	switch {
	case ref == "":
		if d.active == nil {
			return errors.New("theme preview: no theme is configured")
		}
		ref = d.active.ref()
	case ref == "dark":
		ref = d.themes.dark
	case ref == "light":
		ref = d.themes.light
	case strings.HasPrefix(ref, ":"):
		if d.active == nil {
			return errors.New("theme preview: no theme is selected to take the variant of")
		}
		ref = d.active.theme.Name + ref
	}
	name, variant, err := parseThemeRef(ref)
	if err != nil {
		return fmt.Errorf("theme preview: %w", err)
	}
	t, ok := d.themes.themes[name]
	if !ok {
		return fmt.Errorf("theme preview: no theme %s", name)
	}
	v, ok := t.Variants[variant]
	if !ok {
		return fmt.Errorf("theme preview: theme %s has no variant %s", name, variant)
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
