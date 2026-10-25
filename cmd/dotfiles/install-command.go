package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// InstallCommand runs dotfiles install: it runs the actions of every
// repository, or only those that its arguments select.
func (d *Dotfiles) InstallCommand(args []string) error {
	sel, err := d.parseSyncArgs("install", "Synchronize the dotfiles: run the actions of every repository", args)
	if err != nil {
		return err
	}
	d.sync(false, sel, eventInstall)
	return nil
}

// parseSyncArgs parses the arguments of install and plan, whose usage line
// starts with the description desc.
func (d *Dotfiles) parseSyncArgs(name, desc string, args []string) (selection, error) {
	fs := flag.NewFlagSet("dotfiles "+name, flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles %s [repo|path...]\n\n", name)
		fmt.Fprintf(d.Stderr, "%s,\n", desc)
		fmt.Fprintf(d.Stderr, "or only the actions that the arguments select: all actions of a named\n")
		fmt.Fprintf(d.Stderr, "repository, and the actions that read or write a path or something inside\n")
		fmt.Fprintf(d.Stderr, "it. An argument that is . or .. or contains a / is a path.\n")
		fmt.Fprintf(d.Stderr, "See dotfiles doc installing part.\n")
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	sel, err := d.parseSelection(fs.Args())
	if err != nil {
		fmt.Fprintf(d.Stderr, "%s: %v\n\n", name, err)
		fs.Usage()
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return sel, nil
}

// sync runs the actions of every repository that sel selects, or prints
// them if isPlan. On eventTheme, [[go]] packages are not installed and the
// [[script]] entries of eventTheme run instead of those of eventInstall.
func (d *Dotfiles) sync(isPlan bool, sel selection, event string) {
	var (
		allActions     []Action
		allGoInstall   []Action
		allPreScripts  []Action
		allPostScripts []Action
	)

	for _, r := range d.loadRepos(WithForce(d.Force), WithCopy(d.Copy), WithEvent(event)) {
		name := filepath.Base(r.srcRoot)
		actions, err := r.Sync()
		if err != nil {
			fmt.Fprintf(d.Stderr, "error planning %s: %v\n", r.srcRoot, err)
			d.collectErr(fmt.Errorf("plan %s: %w", name, err))
			continue
		}
		allActions = append(allActions, sel.filter(r, actions)...)

		allPreScripts = append(allPreScripts, sel.filter(r, r.PreScript())...)
		allPostScripts = append(allPostScripts, sel.filter(r, r.PostScript())...)
		if event != eventTheme {
			allGoInstall = append(allGoInstall, sel.filter(r, r.GoInstall())...)
		}
	}
	if args := sel.unmatched(); len(args) > 0 {
		d.fail(fmt.Errorf("nothing selected by %s", strings.Join(args, ", ")))
	}

	all := slices.Concat(allGoInstall, allPreScripts, allActions, allPostScripts)

	// Check for multiple actions targeting the same destination path
	destMap := make(map[string][]Action)
	for _, action := range all {
		if t, ok := action.(Targeter); ok {
			dst := t.DstAbsolutePath()
			if dst != "" {
				destMap[dst] = append(destMap[dst], action)
			}
		}
	}

	for dst, actions := range destMap {
		if len(actions) > 1 {
			fmt.Fprintf(d.Stderr, "Warning: multiple actions target the same destination: %s\n", dst)
			for _, action := range actions {
				fmt.Fprintf(d.Stderr, "  - %s\n", action.String())
			}
		}
	}

	for _, script := range all {
		if isPlan {
			fmt.Fprintf(d.Stdout, "plan: %s\n", script.String())
			continue
		}
		if d.Verbose {
			fmt.Fprintf(d.Stdout, "%s\n", script.String())
		}
		if err := script.Run(); err != nil {
			if d.Verbose {
				fmt.Fprintf(d.Stderr, "error: %v\n", err)
			}
			d.collectErr(fmt.Errorf("%s: %w", script.String(), err))
		}
	}
}
