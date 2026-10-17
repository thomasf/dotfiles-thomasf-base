package main

import (
	"fmt"
	"path/filepath"
	"slices"
)

func (d *Dotfiles) InstallCommand() { d.sync(false) }

func (d *Dotfiles) sync(isPlan bool) {
	var (
		allActions     []Action
		allGoInstall   []Action
		allPreScripts  []Action
		allPostScripts []Action
	)

	for _, r := range d.loadRepos(WithForce(d.Force), WithCopy(d.Copy)) {
		name := filepath.Base(r.srcRoot)
		actions, err := r.Sync()
		if err != nil {
			fmt.Fprintf(d.Stderr, "error planning %s: %v\n", r.srcRoot, err)
			d.collectErr(fmt.Errorf("plan %s: %w", name, err))
			continue
		}
		allActions = append(allActions, actions...)

		allPreScripts = append(allPreScripts, r.PreScript()...)
		allPostScripts = append(allPostScripts, r.PostScript()...)
		allGoInstall = append(allGoInstall, r.GoInstall()...)
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
