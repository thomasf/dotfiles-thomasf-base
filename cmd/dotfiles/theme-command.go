package main

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ThemeCommand runs dotfiles theme: it shows the theme variant in use, lists
// the themes, or switches the theme.
func (d *Dotfiles) ThemeCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles theme", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles theme [show | list | set <variant> | toggle]\n\n")
		fmt.Fprintf(d.Stderr, "Show the theme variant in use, list the themes and their variants, or\n")
		fmt.Fprintf(d.Stderr, "switch the theme. set takes dark or light, theme:variant, or :variant of\n")
		fmt.Fprintf(d.Stderr, "the theme in use; toggle switches between dark and light mode.\n\n")
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
	wantArgs := map[string]int{"show": 0, "list": 0, "set": 1, "toggle": 0}
	n, ok := wantArgs[sub]
	switch {
	case !ok:
		return usage("unknown command " + sub)
	case len(args) != n:
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
		d.listThemes()
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

// listThemes prints every variant of every theme, marking the one in use with
// * and naming the modes that [theme] dark and light use it for.
func (d *Dotfiles) listThemes() {
	var refs []string
	for _, name := range slices.Sorted(maps.Keys(d.themes.themes)) {
		for _, variant := range slices.Sorted(maps.Keys(d.themes.themes[name].Variants)) {
			refs = append(refs, name+":"+variant)
		}
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
}
