package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type GitConfigAction struct {
	SrcRoot string
	Config  map[string]string
}

func (g *GitConfigAction) String() string {
	var set, unset int
	for _, v := range g.Config {
		if v == "" {
			unset++
		} else {
			set++
		}
	}

	var parts []string
	if set > 0 {
		parts = append(parts, fmt.Sprintf("set %d", set))
	}
	if unset > 0 {
		parts = append(parts, fmt.Sprintf("unset %d", unset))
	}

	return fmt.Sprintf("[%s] git config: %s", filepath.Base(g.SrcRoot), strings.Join(parts, ", "))
}

func (g *GitConfigAction) Run() error {
	var keys []string
	for k := range g.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var errs []error
	for _, k := range keys {
		v := g.Config[k]
		var args []string
		if v == "" {
			args = []string{"config", "--global", "--unset", k}
		} else {
			args = []string{"config", "--global", k, v}
		}

		cmd := exec.Command("git", args...)
		prefix := fmt.Sprintf("[%s] git config %s: ", filepath.Base(g.SrcRoot), k)
		cmd.Stdout = newPrefixWriter(os.Stdout, prefix)
		cmd.Stderr = newPrefixWriter(os.Stderr, prefix)
		err := cmd.Run()
		if err != nil {
			if v == "" {
				if exitError, ok := err.(*exec.ExitError); ok {
					if exitError.ExitCode() == 5 {
						continue
					}
				}
			}
			errs = append(errs, fmt.Errorf("git config %s: %w", k, err))
		}
	}
	return errors.Join(errs...)
}
