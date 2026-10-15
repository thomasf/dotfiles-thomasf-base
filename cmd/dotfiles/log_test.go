package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// logTestRepos creates the repositories a and b with commits whose commit
// times interleave, and a global git config that shows dates as Unix times.
func logTestRepos(t *testing.T) (a, b string) {
	t.Helper()
	root := t.TempDir()
	global := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(global, []byte("[log]\n\tdate = unix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	commit := func(dir, file string, time string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(file+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", file)
		cmd := exec.Command("git", "commit", "-q", "-m", file)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+time, "GIT_AUTHOR_DATE="+time)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v: %s", err, out)
		}
	}
	a, b = filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, dir := range []string{a, b} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "init", "-q", "-b", "main")
		gitRun(t, dir, "config", "user.email", "test@example.com")
		gitRun(t, dir, "config", "user.name", "Test User")
	}
	commit(a, "a1", "1000000000 +0000")
	commit(b, "b1", "1000000100 +0000")
	commit(b, "b2", "1000000200 +0000")
	commit(a, "a2", "1000000300 +0000")
	return a, b
}

// runLog runs Log with args on the repositories and fails the test on
// errors.
func runLog(t *testing.T, repos []string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	d := &Dotfiles{Stdout: &stdout, Stderr: &stderr, repos: repos}
	if err := d.Log(args); err != nil {
		t.Fatalf("log %v: %v", args, err)
	}
	if len(d.runErrors) > 0 {
		t.Fatalf("log %v: %v: %s", args, d.runErrors, stderr.String())
	}
	return stdout.String()
}

func TestLog(t *testing.T) {
	a, b := logTestRepos(t)
	entry := func(repo, file, time string) string {
		hash := gitOutput(t, filepath.Join(filepath.Dir(a), repo), "rev-parse", ":/"+file)
		return time + " " + repo + " " + hash + " test@example.com\n\n" + file + "\n\n\n" +
			" " + file + " | 1 +\n 1 file changed, 1 insertion(+)\n"
	}

	want := entry("a", "a2", "1000000300") +
		"\n" + entry("b", "b2", "1000000200") +
		"\n" + entry("b", "b1", "1000000100") +
		"\n" + entry("a", "a1", "1000000000")
	if got := runLog(t, []string{a, b}); got != want {
		t.Errorf("log:\n%s\nwant:\n%s", got, want)
	}

	// -n limits the merged log, not each repository.
	want = entry("a", "a2", "1000000300") +
		"\n" + entry("b", "b2", "1000000200") +
		"\n" + entry("b", "b1", "1000000100")
	if got := runLog(t, []string{a, b}, "-n", "3"); got != want {
		t.Errorf("log -n 3:\n%s\nwant:\n%s", got, want)
	}
}

func TestLogCompactShort(t *testing.T) {
	a, b := logTestRepos(t)
	line := func(repo, file, time string) string {
		hash := gitOutput(t, filepath.Join(filepath.Dir(a), repo), "rev-parse", "--short", ":/"+file)
		return time + " " + repo + " " + hash + " test@example.com " + file + "\n"
	}
	shortstat := " 1 file changed, 1 insertion(+)\n"
	for _, tt := range []struct {
		flag, want string
	}{
		{"-compact", line("a", "a2", "1000000300") + line("b", "b2", "1000000200")},
		{"-short", line("a", "a2", "1000000300") + shortstat + line("b", "b2", "1000000200") + shortstat},
	} {
		if got := runLog(t, []string{a, b}, tt.flag, "-n", "2"); got != tt.want {
			t.Errorf("%s:\n%s\nwant:\n%s", tt.flag, got, tt.want)
		}
	}
}

func TestLogPatch(t *testing.T) {
	a, b := logTestRepos(t)
	for _, args := range [][]string{{"-patch"}, {"-patch", "-compact"}, {"-patch", "-short"}} {
		out := runLog(t, []string{a, b}, args...)
		for _, file := range []string{"a1", "a2", "b1", "b2"} {
			if !strings.Contains(out, "diff --git a/"+file+" b/"+file+"\n") {
				t.Errorf("%v: no patch for %s in:\n%s", args, file, out)
			}
		}
	}
}

func TestLogTextconv(t *testing.T) {
	a, b := logTestRepos(t)
	if err := os.WriteFile(filepath.Join(a, ".gitattributes"), []byte("a* diff=fail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, a, "config", "diff.fail.textconv", "false")

	runLog(t, []string{a, b}, "-patch")

	var stdout, stderr bytes.Buffer
	d := &Dotfiles{Stdout: &stdout, Stderr: &stderr, repos: []string{a, b}}
	d.Log([]string{"-patch", "-textconv"})
	if len(d.runErrors) != 1 {
		t.Errorf("-textconv did not turn textconv on: %v", d.runErrors)
	}
}

// TestLogMatchesGit checks that Log prints what git log prints with the same
// format, including merge commits and commit messages with a body.
func TestLogMatchesGit(t *testing.T) {
	a, _ := logTestRepos(t)
	gitRun(t, a, "checkout", "-q", "-b", "side", "HEAD~")
	if err := os.WriteFile(filepath.Join(a, "side"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, a, "add", "side")
	gitRun(t, a, "commit", "-q", "-m", "side\n\nwith a body")
	gitRun(t, a, "checkout", "-q", "main")
	gitRun(t, a, "merge", "-q", "--no-ff", "-m", "merge side", "side")

	format := "--format=format:" + strings.Replace(defaultLogFormat.format, "{name}", "%C(magenta)a%C(reset)", 1)
	for _, args := range [][]string{nil, {"-patch"}} {
		gitArgs := []string{"log", format, "--stat"}
		if len(args) > 0 {
			gitArgs = append(gitArgs, "--patch")
		}
		got := runLog(t, []string{a}, args...)
		if want := gitOutput(t, a, gitArgs...); strings.TrimSpace(got) != want {
			t.Errorf("%v:\n%s\nwant:\n%s", args, got, want)
		}
	}
}

func TestLogErrors(t *testing.T) {
	a, b := logTestRepos(t)
	notRepo := t.TempDir()
	var stdout, stderr bytes.Buffer
	d := &Dotfiles{Stdout: &stdout, Stderr: &stderr, repos: []string{a, notRepo, b}}
	if err := d.Log(nil); err != nil {
		t.Fatal(err)
	}
	if len(d.runErrors) != 1 || !strings.Contains(d.runErrors[0].Error(), "not a git repository") {
		t.Errorf("want an error for the directory that is not a repository, got %v", d.runErrors)
	}
	if !strings.Contains(stdout.String(), "\na2\n") || !strings.Contains(stdout.String(), "\nb2\n") {
		t.Errorf("the other repositories are missing from the log:\n%s", stdout.String())
	}

	for _, args := range [][]string{{"--oneline"}, {"extra"}, {"-compact", "-short"}, {"-h"}} {
		stderr.Reset()
		d := &Dotfiles{Stdout: &stdout, Stderr: &stderr, repos: []string{a, b}}
		err := d.Log(args)
		if err == nil {
			t.Errorf("%v: no error", args)
		}
		if !strings.Contains(stderr.String(), "Usage: dotfiles log") {
			t.Errorf("%v: no usage in %q", args, stderr.String())
		}
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
