//go:build unix

package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// execBash replaces dotfiles with bash run with argv, as exec(2), unless
// ctx is done. It returns only on failure.
func (d *Dotfiles) execBash(ctx context.Context, argv []string) error {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return err
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return syscall.Exec(bash, argv, os.Environ())
}
