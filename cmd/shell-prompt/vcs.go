package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// VCSProvider retrieves the version control string for the prompt.
type VCSProvider interface {
	GetVCS(dir string, hostTags string) string
}

// DefaultVCSProvider uses vcprompt if available, or falls back to git status.
type DefaultVCSProvider struct {
	Timeout time.Duration
}

func (p *DefaultVCSProvider) GetVCS(dir string, hostTags string) string {
	if strings.Contains(hostTags, ":minimalshell:") {
		return ""
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}

	// 1. Try vcprompt if installed in PATH
	if vcpromptPath, err := exec.LookPath("vcprompt"); err == nil && vcpromptPath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, vcpromptPath)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			res := strings.TrimRight(out.String(), "\r\n")
			if res != "" {
				return res
			}
		}
	}

	// 2. Fallback to git
	return getGitStatus(dir, timeout)
}

// getGitStatus runs git status or inspects .git to provide (__git_ps1 " (%s)") compatible output.
func getGitStatus(dir string, timeout time.Duration) string {
	// First check if inside a git repository
	gitDir := findGitDir(dir)
	if gitDir == "" {
		return ""
	}

	// Try git command
	if gitPath, err := exec.LookPath("git"); err == nil && gitPath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, gitPath, "status", "--porcelain=v1", "-b")
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			return formatGitStatus(out.String())
		}
	}

	// Pure Go fallback by reading .git/HEAD
	headBytes, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(headBytes))
	branch := ""
	if strings.HasPrefix(head, "ref: refs/heads/") {
		branch = strings.TrimPrefix(head, "ref: refs/heads/")
	} else if len(head) >= 7 {
		branch = head[:7]
	}
	if branch != "" {
		return " (" + branch + ")"
	}
	return ""
}

// formatGitStatus formats the output of git status --porcelain=v1 -b into " (%s)"
func formatGitStatus(output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) == 0 || lines[0] == "" {
		return ""
	}

	header := lines[0]
	if !strings.HasPrefix(header, "## ") {
		return ""
	}
	header = strings.TrimPrefix(header, "## ")

	var branch string
	if strings.HasPrefix(header, "No commits yet on ") {
		branch = strings.TrimPrefix(header, "No commits yet on ")
	} else if strings.HasPrefix(header, "Initial commit on ") {
		branch = strings.TrimPrefix(header, "Initial commit on ")
	} else if strings.HasPrefix(header, "HEAD (no branch)") {
		branch = "HEAD"
	} else {
		// branch...upstream [ahead 1, behind 2]
		idxDot := strings.Index(header, "...")
		idxSpace := strings.Index(header, " ")
		if idxDot != -1 {
			branch = header[:idxDot]
		} else if idxSpace != -1 {
			branch = header[:idxSpace]
		} else {
			branch = header
		}
	}

	hasStaged := false
	hasUnstaged := false
	hasUntracked := false

	for _, line := range lines[1:] {
		if len(line) < 2 {
			continue
		}
		x := line[0]
		y := line[1]

		if x == '?' && y == '?' {
			hasUntracked = true
		} else {
			if x != ' ' && x != '?' {
				hasStaged = true
			}
			if y != ' ' && y != '?' {
				hasUnstaged = true
			}
		}
	}

	var flags string
	if hasUnstaged {
		flags += "*"
	}
	if hasStaged {
		flags += "+"
	}
	if hasUntracked {
		flags += "%"
	}

	res := " (" + branch
	if flags != "" {
		res += " " + flags
	}
	res += ")"
	return res
}

func findGitDir(startDir string) string {
	dir := startDir
	for {
		gitPath := filepath.Join(dir, ".git")
		fi, err := os.Stat(gitPath)
		if err == nil {
			if fi.IsDir() {
				return gitPath
			}
			// Could be a worktree or submodule file
			content, err := os.ReadFile(gitPath)
			if err == nil {
				text := strings.TrimSpace(string(content))
				if strings.HasPrefix(text, "gitdir: ") {
					target := strings.TrimPrefix(text, "gitdir: ")
					if !filepath.IsAbs(target) {
						target = filepath.Join(dir, target)
					}
					return target
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
