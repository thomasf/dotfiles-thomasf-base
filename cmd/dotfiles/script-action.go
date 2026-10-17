package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

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
