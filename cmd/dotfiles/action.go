package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

type Action interface {
	Run() error
	String() string
}

type SymLinker struct {
	SrcRoot string
	Src     string
	DstRoot string
	Dst     string
	Force   bool
}

func (s *SymLinker) String() string {
	return fmt.Sprintf("symlink %s/%s -> %s", filepath.Base(s.SrcRoot), s.Src, s.Dst)
}

func (s *SymLinker) Run() error {
	fullSrc := filepath.Join(s.SrcRoot, s.Src)
	fullDst := filepath.Join(s.DstRoot, s.Dst)

	absSrc, err := filepath.Abs(fullSrc)
	if err != nil {
		return err
	}

	dstDir := filepath.Dir(fullDst)
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}

	if info, err := os.Lstat(fullDst); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(fullDst)
			if err == nil && target == absSrc {
				// already correctly linked
				return nil
			}
		}

		if !s.Force {
			return fmt.Errorf("target exists: %s", fullDst)
		}

		if err := os.RemoveAll(fullDst); err != nil {
			return err
		}
	}

	return os.Symlink(absSrc, fullDst)
}

type GitConfigAction struct {
	Config map[string]string
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

	return "git config: " + strings.Join(parts, ", ")
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
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
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

type GoInstallAction struct {
	SrcRoot string
}

func (g *GoInstallAction) String() string {
	return "go install ./cmd/..."
}

func (g *GoInstallAction) Run() error {
	cmd := exec.Command("go", "install", "./cmd/...")
	cmd.Dir = g.SrcRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type ScriptAction struct {
	SrcRoot string
	Script  string
}

func (s *ScriptAction) String() string {
	return fmt.Sprintf("run script from %s", filepath.Base(s.SrcRoot))
}

func (s *ScriptAction) Run() error {
	wrapErr := func(err error) error {
		if err == nil {
			return nil
		}
		return fmt.Errorf("script error in '%s': %w", s.SrcRoot, err)
	}

	reader := strings.NewReader(s.Script)
	f, err := syntax.NewParser().Parse(reader, "")
	if err != nil {
		return wrapErr(err)
	}
	runner, err := interp.New(
		interp.Dir(s.SrcRoot),
		interp.StdIO(os.Stdin, os.Stdout, os.Stderr),
		interp.Params("-e"),
	)
	if err != nil {
		return wrapErr(err)
	}

	return wrapErr(runner.Run(context.Background(), f))
}

type ExecCommandAction struct {
	SrcRoot string
	Command string
	Args    []string
}

func (s *ExecCommandAction) String() string {
	return fmt.Sprintf("[%s] %s %s", filepath.Base(s.SrcRoot), s.Command, strings.Join(s.Args, " "))
}

func (s *ExecCommandAction) Run() error {
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Dir = s.SrcRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
