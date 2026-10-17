package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func (d *Dotfiles) StatusCommand() {
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		changed, err := hasChanges(repoPath)
		if err != nil {
			fmt.Fprintf(d.Stderr, "error checking status for %s: %v\n", name, err)
			d.collectErr(fmt.Errorf("status %s: %w", name, err))
			continue
		}
		if changed {
			fmt.Fprintf(d.Stdout, "[- %s ]\n", name)
			cmd := exec.Command("git", "status", "--short", "--branch")
			cmd.Dir = repoPath
			cmd.Stdout = d.Stdout
			cmd.Stderr = d.Stderr
			cmd.Run()
		}
	}
}
