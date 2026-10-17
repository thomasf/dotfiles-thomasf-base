package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPushWithMasterBranch(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()

	mfs := fstest.MapFS{
		"testfile": &fstest.MapFile{Data: []byte("test"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	gitRun("init", "-b", "master")
	gitRun("config", "user.email", "test@example.com")
	gitRun("config", "user.name", "Test User")
	gitRun("add", ".")
	gitRun("commit", "-m", "initial commit")
	gitRun("branch", "backup-feature")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Verbose: true, // print the git commands
		Stdout:  stdout,
		Stderr:  stderr,
		repos:   []string{repoDir},
	}

	d.PushCommand()

	out := stdout.String()
	t.Logf("Stdout: %s", out)

	expectedPush := "[" + filepath.Base(repoDir) + "] git push --porcelain origin master"
	if !strings.Contains(out, expectedPush) {
		t.Errorf("Expected push command '%s', got stdout: '%s'", expectedPush, out)
	}
}

func TestPushWithMainBranch(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()

	mfs := fstest.MapFS{
		"testfile": &fstest.MapFile{Data: []byte("test"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "test@example.com")
	gitRun("config", "user.name", "Test User")
	gitRun("add", ".")
	gitRun("commit", "-m", "initial commit")
	gitRun("branch", "backup-feature")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Verbose: true, // print the git commands
		Stdout:  stdout,
		Stderr:  stderr,
		repos:   []string{repoDir},
	}

	d.PushCommand()

	out := stdout.String()
	t.Logf("Stdout: %s", out)

	expectedPush := "[" + filepath.Base(repoDir) + "] git push --porcelain origin main"
	if !strings.Contains(out, expectedPush) {
		t.Errorf("Expected push command '%s', got stdout: '%s'", expectedPush, out)
	}
}

func TestPushSummary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// alpha has two commits that origin lacks, beta is in sync with origin,
	// gamma's origin is empty, and delta's backup cannot be pushed to.
	var repos []string
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		dir := filepath.Join(root, "repos", name)
		newGitRepo(t, dir, nil)
		up := filepath.Join(root, "up", name+".git")
		if name == "gamma" {
			gitRun(t, root, "init", "-q", "--bare", up)
		} else {
			gitRun(t, root, "clone", "-q", "--bare", dir, up)
		}
		gitRun(t, dir, "remote", "add", "origin", up)
		gitRun(t, dir, "fetch", "-q", "origin")
		repos = append(repos, dir)
	}
	alpha, delta := repos[0], repos[3]
	for _, content := range []string{"one\ntwo\n", "one\n"} {
		if err := os.WriteFile(filepath.Join(alpha, "testfile"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, alpha, "commit", "-q", "-am", "change")
	}
	backup := filepath.Join(root, "backup", "delta.git")
	gitRun(t, root, "init", "-q", "--bare", backup)
	gitRun(t, delta, "remote", "add", "backup.disk", backup)
	// The backup refuses every push.
	hook := filepath.Join(backup, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := &lockedBuffer{}, &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: stderr, repos: repos}
	d.PushCommand()

	out := stdout.String()
	for _, want := range []string{
		"alpha  origin       2 commits, 1 file changed, 1 insertion(+), 1 deletion(-)",
		"beta   origin       up to date",
		"gamma  origin       new branch",
		"delta  origin       up to date",
		"delta  backup.disk  FAILED: exit status 1: error: failed to push some refs",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	if len(d.runErrors) != 0 {
		t.Errorf("runErrors = %v, want none: a failed backup is a warning", d.runErrors)
	}
	if !strings.Contains(stderr.String(), "warning: [delta] git push --porcelain backup.disk main: ") {
		t.Errorf("no warning for the failed backup:\n%s", stderr)
	}
}

func TestPushChecksPushURL(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	up := filepath.Join(root, "up", "alpha.git")
	gitRun(t, root, "clone", "-q", "--bare", alpha, up)
	// Fetching works, but the push URL is on a host that cannot be reached.
	gitRun(t, alpha, "remote", "add", "origin", up)
	gitRun(t, alpha, "remote", "set-url", "--push", "origin", "git@bad.example:"+up)
	gitRun(t, alpha, "config", "core.sshCommand", "false")

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, repos: []string{alpha}}
	d.PushCommand()

	out := stdout.String()
	if strings.Contains(out, "git push") {
		t.Errorf("pushed to a host that failed the check:\n%s", out)
	}
	if want := "alpha  origin  SKIPPED: cannot connect to git@bad.example"; !strings.Contains(out, want) {
		t.Errorf("summary missing %q:\n%s", want, out)
	}
	if len(d.runErrors) != 1 {
		t.Errorf("runErrors = %v, want one for bad.example", d.runErrors)
	}
}

func TestParsePushPorcelain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		out            string
		note, from, to string
	}{
		{"To up.git\n*\trefs/heads/main:refs/heads/main\t[new branch]\nDone\n", "new branch", "", ""},
		{"To up.git\n=\trefs/heads/main:refs/heads/main\t[up to date]\nDone\n", "", "", ""},
		{"To up.git\n \trefs/heads/main:refs/heads/main\t27994d4..cd76988\nDone\n", "", "27994d4", "cd76988"},
		{"To up.git\n+\trefs/heads/main:refs/heads/main\tcd76988...1322461 (forced update)\nDone\n", "forced update", "cd76988", "1322461"},
		{"Done\n", "", "", ""},
	}
	for _, tt := range tests {
		note, from, to := parsePushPorcelain(tt.out)
		if note != tt.note || from != tt.from || to != tt.to {
			t.Errorf("parsePushPorcelain(%q) = %q, %q, %q; want %q, %q, %q", tt.out, note, from, to, tt.note, tt.from, tt.to)
		}
	}
}

func TestPushUnreadableRemotes(t *testing.T) {
	t.Parallel()
	// A directory that is not a git repository has no remotes to read.
	notRepo := filepath.Join(t.TempDir(), "notrepo")
	if err := os.Mkdir(notRepo, 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, repos: []string{notRepo}}
	d.PushCommand()

	if want := "notrepo    FAILED: cannot read remotes: "; !strings.Contains(stdout.String(), want) {
		t.Errorf("summary missing %q:\n%s", want, stdout)
	}
	if len(d.runErrors) != 1 {
		t.Errorf("runErrors = %v, want one", d.runErrors)
	}
}

func TestPushConfiguredRemotes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, fstest.MapFS{
		"dotfiles.toml": tomlFile(reposConfig(root, "alpha")),
	})
	originDir := filepath.Join(root, "origin", "alpha.git")
	offDir := filepath.Join(root, "off", "alpha.git")
	gitRun(t, root, "init", "-q", "--bare", originDir)
	gitRun(t, root, "init", "-q", "--bare", offDir)
	gitRun(t, alpha, "remote", "add", "origin", originDir)
	gitRun(t, alpha, "remote", "add", "backup.off", offDir)
	// The backup disk is not mounted.
	gitRun(t, alpha, "remote", "add", "backup.disk", root+"/backup/alpha.git")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: stderr, Verbose: true, repos: []string{alpha}}
	d.PushCommand()

	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, stderr)
	}
	if !strings.Contains(stdout.String(), "[alpha] git push --porcelain origin main") {
		t.Errorf("origin not pushed:\n%s", stdout)
	}
	if strings.Contains(stdout.String(), "backup.off") {
		t.Errorf("inactive remote pushed:\n%s", stdout)
	}
	if want := "warning: remote backup.disk is unavailable, skipped for 1 repos: alpha"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr missing %q:\n%s", want, stderr)
	}
	gitRun(t, originDir, "rev-parse", "--verify", "-q", "main")
}
