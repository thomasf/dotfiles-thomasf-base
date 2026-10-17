// Gendoc renders a Markdown file as plain text with glow, for the dotfiles
// doc command.
//
// Usage:
//
//	gendoc <input.md> <output.txt>
//
// The text is at most 79 columns wide. Headings keep their leading #s and
// code blocks are indented, so the doc command can still find the sections.
// Glow is run with go run at a fixed version, so that every machine renders
// the same text. If it fails, for example because the module cannot be
// downloaded, gendoc prints a warning and copies the Markdown file as it is.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// glow is the version of glow that renders the text.
const glow = "github.com/charmbracelet/glow/v2@v2.1.2"

// width is the maximum width of the text. Glow puts a margin of two columns
// on each side of it.
const width = 98

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: gendoc <input.md> <output.txt>\n")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "gendoc: %v\n", err)
		os.Exit(1)
	}
}

// run writes input rendered by glow to output. If glow fails, it prints a
// warning and copies input as it is.
func run(input, output string) error {
	cmd := exec.Command("go", "run", glow, "-s", "notty", "-w", strconv.Itoa(width+4), input)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendoc: warning: glow: %v; copying %s as it is\n", err, input)
		md, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		return os.WriteFile(output, md, 0o644)
	}
	return os.WriteFile(output, []byte(trim(string(out))), 0o644)
}

// trim returns the output of glow without the two-space margin it puts on
// every line, the spaces it pads lines with and the blank lines around the
// text.
func trim(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(strings.TrimPrefix(line, "  "), " ")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n") + "\n"
}
