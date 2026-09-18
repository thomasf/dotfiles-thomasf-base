package main

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// GetTerminalCols returns the number of columns of the current terminal.
// It checks stdout, stderr, and stdin via golang.org/x/term (supporting Linux, macOS, BSD, Windows, etc.),
// then falls back to the COLUMNS environment variable, `tput cols`, and defaults to 100 if undetected.
func GetTerminalCols() int {
	for _, f := range []*os.File{os.Stdout, os.Stderr, os.Stdin} {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}

	if colsStr := os.Getenv("COLUMNS"); colsStr != "" {
		if c, err := strconv.Atoi(colsStr); err == nil && c > 0 {
			return c
		}
	}

	if tputPath, err := exec.LookPath("tput"); err == nil && tputPath != "" {
		cmd := exec.Command(tputPath, "cols")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			val := strings.TrimSpace(out.String())
			if c, err := strconv.Atoi(val); err == nil && c > 0 {
				return c
			}
		}
	}

	return 100
}
