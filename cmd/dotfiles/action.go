package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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

type Targeter interface {
	DstAbsolutePath() string
}

type SymLinker struct {
	SrcRoot string
	Src     string
	DstRoot string
	Dst     string
	Force   bool
}

func (s *SymLinker) DstAbsolutePath() string {
	return filepath.Join(s.DstRoot, s.Dst)
}

func (s *SymLinker) String() string {
	return fmt.Sprintf("[%s] symlink: %s -> %s", filepath.Base(s.SrcRoot), s.Src, s.Dst)
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

type CopyFile struct {
	SrcRoot string
	Src     string
	DstRoot string
	Dst     string
	Force   bool
}

func (c *CopyFile) DstAbsolutePath() string {
	return filepath.Join(c.DstRoot, c.Dst)
}

func (c *CopyFile) String() string {
	return fmt.Sprintf("[%s] copy: %s -> %s", filepath.Base(c.SrcRoot), c.Src, c.Dst)
}

func (c *CopyFile) Run() error {
	fullSrc := filepath.Join(c.SrcRoot, c.Src)
	fullDst := filepath.Join(c.DstRoot, c.Dst)

	dstDir := filepath.Dir(fullDst)
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}

	if _, err := os.Lstat(fullDst); err == nil {
		if !c.Force {
			return fmt.Errorf("target exists: %s", fullDst)
		}

		if err := os.RemoveAll(fullDst); err != nil {
			return err
		}
	}

	srcInfo, err := os.Stat(fullSrc)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return filepath.Walk(fullSrc, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(fullSrc, path)
			if err != nil {
				return err
			}
			target := filepath.Join(fullDst, rel)
			if info.IsDir() {
				return os.MkdirAll(target, info.Mode())
			}
			return copyFile(path, target, info.Mode())
		})
	}

	return copyFile(fullSrc, fullDst, srcInfo.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

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

type GoInstallAction struct {
	SrcRoot string
	Path    string
}

func (g *GoInstallAction) String() string {
	return fmt.Sprintf("[%s] go install %s", filepath.Base(g.SrcRoot), g.Path)
}

func (g *GoInstallAction) Run() error {
	dirPath := filepath.Join(g.SrcRoot, g.Path)
	hasGo, err := hasGoFiles(dirPath)
	if err != nil {
		return err
	}
	if !hasGo {
		return fmt.Errorf("no go files in %s", dirPath)
	}

	cmd := exec.Command("go", "install", ".")
	cmd.Dir = dirPath
	prefix := fmt.Sprintf("[%s] go install %s: ", filepath.Base(g.SrcRoot), g.Path)
	cmd.Stdout = newPrefixWriter(os.Stdout, prefix)
	cmd.Stderr = newPrefixWriter(os.Stderr, prefix)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install in %s: %w", g.Path, err)
	}
	return nil
}

func hasGoFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return true, nil
		}
	}
	return false, nil
}

type ScriptAction struct {
	SrcRoot string
	Script  string
}

func (s *ScriptAction) String() string {
	return fmt.Sprintf("[%s] run script: %s", filepath.Base(s.SrcRoot), ellipsis(s.Script, 66))
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
	prefix := fmt.Sprintf("[%s] script: ", filepath.Base(s.SrcRoot))
	runner, err := interp.New(
		interp.Dir(s.SrcRoot),
		interp.StdIO(os.Stdin, newPrefixWriter(os.Stdout, prefix), newPrefixWriter(os.Stderr, prefix)),
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
	prefix := s.String() + ": "
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Dir = s.SrcRoot
	cmd.Stdout = newPrefixWriter(os.Stdout, prefix)
	cmd.Stderr = newPrefixWriter(os.Stderr, prefix)
	return cmd.Run()
}

func ellipsis(input string, maxLen int) string {
	var lines []string
	for l := range strings.Lines(input) {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	cleaned := strings.Join(lines, "⏎")
	if len(cleaned) <= maxLen {
		return cleaned
	}
	runes := []rune(cleaned)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "…"
	}
	return cleaned
}
