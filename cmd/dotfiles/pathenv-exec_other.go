//go:build !unix

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// execBash runs bash with argv as a child, since dotfiles cannot replace
// itself here, and returns its exit status as an exitStatus. Cancelling
// ctx kills bash.
func (d *Dotfiles) execBash(ctx context.Context, argv []string) error {
	cmd := exec.CommandContext(ctx, "bash", argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, d.Stdout, d.Stderr
	// The command gets SIGINT from the terminal too, and decides what
	// happens; dotfiles waits for it.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)
	err := cmd.Run()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitStatus(ee.ExitCode())
	}
	return err
}
