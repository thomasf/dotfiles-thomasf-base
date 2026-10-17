package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func (d *Dotfiles) MagitCommand() {
	var magitArgs []string
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		changed, err := hasChanges(repoPath)
		if err != nil {
			fmt.Fprintf(d.Stderr, "error checking status for %s: %v\n", name, err)
			d.collectErr(fmt.Errorf("magit %s: %w", name, err))
			continue
		}
		if changed {
			absPath, _ := filepath.Abs(repoPath)
			magitArgs = append(magitArgs, fmt.Sprintf("(magit-status \"%s/\")", absPath))
		}
	}

	if len(magitArgs) == 0 {
		fmt.Fprintln(d.Stdout, "no changes")
	} else {
		evalStr := fmt.Sprintf("(progn %s (delete-other-windows) )", strings.Join(magitArgs, " "))
		cmd := exec.Command("emacs", "-eval", evalStr)
		prefix := "magit: "
		cmd.Stdout = newPrefixWriter(d.Stdout, prefix)
		cmd.Stderr = newPrefixWriter(d.Stderr, prefix)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(d.Stderr, "error: %v\n", err)
			d.collectErr(fmt.Errorf("magit: %w", err))
		}
	}
}
