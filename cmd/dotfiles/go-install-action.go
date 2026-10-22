package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GoInstallAction struct {
	SrcRoot string
	Path    string
}

func (g *GoInstallAction) Paths() []string {
	return []string{filepath.Join(g.SrcRoot, g.Path)}
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
