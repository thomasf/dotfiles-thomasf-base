package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// runGit runs git in repoPath, with its stderr going to d.Stderr prefixed by
// the command, and returns its stdout. If repoPath does not exist yet, as for
// a clone, git runs in its parent. With -v, it first prints the command.
// The error ends with the errorLine of stderr. If ctx ended first, the error
// is its cause, such as context.DeadlineExceeded, and if an interrupt stopped
// git, it is errInterrupted.
func (d *Dotfiles) runGit(ctx context.Context, repoPath string, args ...string) (string, error) {
	desc := gitDesc(repoPath, args...)
	if d.Verbose {
		fmt.Fprintln(d.Stdout, desc)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	if !isDir(repoPath) {
		cmd.Dir = filepath.Dir(repoPath)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(newPrefixWriter(d.Stderr, desc+": "), &stderr)
	// When ctx ends, let git clean up, such as its lock files, and kill it
	// if it has not exited a second later. Its ssh may keep our pipes open
	// after that; stop waiting for them too.
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill() // Windows has no SIGTERM.
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, errInterrupted) {
		return stdout.String(), cause
	}
	if context.Cause(ctx) != nil || interruptedBySignal(err) {
		// The terminal sends Ctrl-C to git as well, so git may die of it
		// before the interrupt cancels ctx.
		return stdout.String(), fmt.Errorf("%w during git %s", errInterrupted, strings.Join(args, " "))
	}
	if msg := errorLine(stderr.String()); err != nil && msg != "" {
		err = fmt.Errorf("%w: %s", err, msg)
	}
	return stdout.String(), err
}

// interruptedBySignal reports whether err is from a process that SIGINT or
// SIGTERM killed.
func interruptedBySignal(err error) bool {
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && (status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGTERM)
}

// gitDesc describes running git with args in repoPath.
func gitDesc(repoPath string, args ...string) string {
	return fmt.Sprintf("[%s] git %s", filepath.Base(repoPath), strings.Join(args, " "))
}

// gitText runs git in dir and returns its trimmed output.
func gitText(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// errorLine returns the line of git's stderr output s that best describes
// why it failed: the first fatal: or error: line, or else the last non-blank
// line.
func errorLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "fatal: ") || strings.HasPrefix(l, "error: ") {
			return strings.TrimSpace(l)
		}
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// getMainBranch returns main or master, whichever the repository at path has,
// preferring main, or master if it has neither.
func getMainBranch(path string) string {
	out, _ := gitText(path, "branch", "--list", "--format=%(refname:short)", "main", "master")
	if branch, _, _ := strings.Cut(out, "\n"); branch != "" {
		return branch
	}
	return "master"
}

// hasChanges reports whether the repository at path has uncommitted changes
// or untracked files.
func hasChanges(path string) (bool, error) {
	out, err := gitText(path, "status", "--porcelain")
	return out != "", err
}

// hasDiff reports whether the working tree of the repository at path differs
// from its index.
func hasDiff(path string) (bool, error) {
	cmd := exec.Command("git", "diff", "--quiet")
	cmd.Dir = path
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

// gitRemote holds the URLs configured for a git remote.
type gitRemote struct {
	url     string
	pushURL string // remote.<name>.pushurl, if set
}

// pushTo returns the URL that git pushes to.
func (g gitRemote) pushTo() string { return cmp.Or(g.pushURL, g.url) }

// gitRemotes returns the remotes of the repository at path, keyed by remote
// name.
func gitRemotes(path string) (map[string]gitRemote, error) {
	cmd := exec.Command("git", "config", "--get-regexp", `^remote\..*\.(url|pushurl)$`)
	cmd.Dir = path
	out, err := cmd.Output()
	remotes := make(map[string]gitRemote)
	if err != nil {
		// Exit status 1 means that no remote has a URL.
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
			return remotes, nil
		}
		return nil, err
	}
	for line := range strings.Lines(string(out)) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		key = strings.TrimPrefix(key, "remote.")
		if name, ok := strings.CutSuffix(key, ".pushurl"); ok {
			r := remotes[name]
			r.pushURL = value
			remotes[name] = r
		} else if name, ok := strings.CutSuffix(key, ".url"); ok {
			r := remotes[name]
			r.url = value
			remotes[name] = r
		}
	}
	return remotes, nil
}
