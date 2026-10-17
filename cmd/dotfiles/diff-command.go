package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func (d *Dotfiles) DiffCommand() {
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		changed, err := hasDiff(repoPath)
		if err != nil {
			fmt.Fprintf(d.Stderr, "error checking diff for %s: %v\n", name, err)
			d.collectErr(fmt.Errorf("diff %s: %w", name, err))
			continue
		}
		if changed {
			fmt.Fprintf(d.Stdout, "[- %s ]\n", name)
			cmd := exec.Command("git", "diff")
			cmd.Dir = repoPath
			cmd.Stdout = d.Stdout
			cmd.Stderr = d.Stderr
			cmd.Run()
		}
	}
}
