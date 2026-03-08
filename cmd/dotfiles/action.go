package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

type Action interface {
	Run() error
	String() string
}

type SymLinker struct {
	RepoName string
	Src      string
	SrcRel   string
	Dst      string
	Force    bool
}

func (s *SymLinker) String() string {
	return fmt.Sprintf("symlink %s/%s -> %s", s.RepoName, s.SrcRel, s.Dst)
}

func (s *SymLinker) Run() error {
	absSrc, err := filepath.Abs(s.Src)
	if err != nil {
		return err
	}

	dstDir := filepath.Dir(s.Dst)
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}

	if info, err := os.Lstat(s.Dst); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(s.Dst)
			if err == nil && target == absSrc {
				// already correctly linked
				return nil
			}
		}

		if !s.Force {
			return fmt.Errorf("target exists: %s", s.Dst)
		}

		if err := os.RemoveAll(s.Dst); err != nil {
			return err
		}
	}

	return os.Symlink(absSrc, s.Dst)
}

type GitConfigAction struct {
	Key   string
	Value string
}

func (g *GitConfigAction) String() string {
	if g.Value == "" {
		return fmt.Sprintf("git config --global --unset %s", g.Key)
	}
	return fmt.Sprintf("git config --global %s %s", g.Key, g.Value)
}

func (g *GitConfigAction) Run() error {
	var args []string
	if g.Value == "" {
		args = []string{"config", "--global", "--unset", g.Key}
	} else {
		args = []string{"config", "--global", g.Key, g.Value}
	}

	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil && g.Value == "" {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() == 5 {
				return nil
			}
		}
	}
	return err
}

type GoInstallAction struct {
	RepoPath string
}

func (g *GoInstallAction) String() string {
	return "go install ./cmd/..."
}

func (g *GoInstallAction) Run() error {
	cmd := exec.Command("go", "install", "./cmd/...")
	cmd.Dir = g.RepoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type ScriptAction struct {
	RepoName string
	RepoPath string
	Script   string
}

func (s *ScriptAction) String() string {
	return fmt.Sprintf("run script from %s", s.RepoName)
}

func (s *ScriptAction) Run() error {
	wrapErr := func(err error) error {
		if err == nil {
			return nil
		}
		return fmt.Errorf("script error in '%s': %w", s.RepoPath, err)
	}

	reader := strings.NewReader(s.Script)
	f, err := syntax.NewParser().Parse(reader, "")
	if err != nil {
		return wrapErr(err)
	}
	runner, err := interp.New(
		interp.Dir(s.RepoPath),
		interp.StdIO(os.Stdin, os.Stdout, os.Stderr),
		interp.Params("-e"),
	)
	if err != nil {
		return wrapErr(err)
	}

	return wrapErr(runner.Run(context.Background(), f))

}

type ExecCommandAction struct {
	RepoName string
	RepoPath string
	Command  string
	Args     []string
}

func (s *ExecCommandAction) String() string {
	return fmt.Sprintf("[%s] %s %s", s.RepoName, s.Command, strings.Join(s.Args, " "))
}

func (s *ExecCommandAction) Run() error {
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Dir = s.RepoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
