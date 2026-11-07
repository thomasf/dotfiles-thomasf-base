package main

import (
	"flag"
	"fmt"
)

// PlanCommand runs dotfiles plan: it prints the actions that InstallCommand
// would run with the same arguments.
func (d *Dotfiles) PlanCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles plan", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	diff := fs.Bool("diff", false, "show unified diff of rendered templates and modified files against destination targets")
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles plan [options] [repo|path...]\n\n")
		fmt.Fprintf(d.Stderr, "Print the actions of every repository without running them,\n")
		fmt.Fprintf(d.Stderr, "or only the actions that the arguments select: all actions of a named\n")
		fmt.Fprintf(d.Stderr, "repository, and the actions that read or write a path or something inside\n")
		fmt.Fprintf(d.Stderr, "it. An argument that is . or .. or contains a / is a path.\n")
		fmt.Fprintf(d.Stderr, "See dotfiles doc installing part.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	sel, err := d.parseSelection(fs.Args())
	if err != nil {
		fmt.Fprintf(d.Stderr, "plan: %v\n\n", err)
		fs.Usage()
		return fmt.Errorf("plan: %w", err)
	}
	d.sync(true, *diff, sel, eventInstall)
	return nil
}
