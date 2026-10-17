package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ExecAction struct {
	SrcRoot string
	Name    string
	Args    []string
}

func (s *ExecAction) String() string {
	return fmt.Sprintf("[%s] %s %s", filepath.Base(s.SrcRoot), s.Name, strings.Join(s.Args, " "))
}

func (s *ExecAction) Run() error {
	prefix := s.String() + ": "
	cmd := exec.Command(s.Name, s.Args...)
	cmd.Dir = s.SrcRoot
	cmd.Stdout = newPrefixWriter(os.Stdout, prefix)
	cmd.Stderr = newPrefixWriter(os.Stderr, prefix)
	return cmd.Run()
}
