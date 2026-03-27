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

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v in %s failed: %v", args, dir, err)
	}
}

func TestPublishSkip(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()

	mfs := fstest.MapFS{
		"testfile":      &fstest.MapFile{Data: []byte("test"), Mode: 0o644},
		"dotfiles.toml": &fstest.MapFile{Data: []byte("public = true"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	gitRun(t, repoDir, "init")
	gitRun(t, repoDir, "config", "user.email", "test@example.com")
	gitRun(t, repoDir, "config", "user.name", "Test User")
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "initial commit")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Publish()

	out := stdout.String()
	expectedSkip := "skipping publish for " + filepath.Base(repoDir) + ": remote 'publish' not found"
	if !strings.Contains(out, expectedSkip) {
		t.Errorf("Expected skip message '%s', got stdout: '%s'", expectedSkip, out)
	}
}

func TestPublishRemoteExists(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	remoteDir := t.TempDir()

	gitRun(t, remoteDir, "init", "--bare")
	gitRun(t, repoDir, "init")
	gitRun(t, repoDir, "config", "user.email", "test@example.com")
	gitRun(t, repoDir, "config", "user.name", "Test User")
	gitRun(t, repoDir, "remote", "add", "publish", remoteDir)

	mfs := fstest.MapFS{
		"testfile":      &fstest.MapFile{Data: []byte("test"), Mode: 0o644},
		"dotfiles.toml": &fstest.MapFile{Data: []byte("public = true"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "initial commit")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Publish()

	out := stdout.String()
	expectedPush := "[" + filepath.Base(repoDir) + "] git push -q publish master"
	if !strings.Contains(out, expectedPush) {
		t.Errorf("Expected push message '%s', got stdout: '%s'", expectedPush, out)
	}
}

func TestPublishNotPublic(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	remoteDir := t.TempDir()

	gitRun(t, remoteDir, "init", "--bare")
	gitRun(t, repoDir, "init")
	gitRun(t, repoDir, "config", "user.email", "test@example.com")
	gitRun(t, repoDir, "config", "user.name", "Test User")
	gitRun(t, repoDir, "remote", "add", "publish", remoteDir)

	mfs := fstest.MapFS{
		"testfile": &fstest.MapFile{Data: []byte("test"), Mode: 0o644},
		// No dotfiles.toml or public = false
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "initial commit")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Publish()

	out := stdout.String()
	if strings.Contains(out, "git push -q publish") {
		t.Errorf("Did not expect push command for non-public repo, got stdout: '%s'", out)
	}
}

func TestPublishWithBackupBranches(t *testing.T) {
	repoDir := t.TempDir()
	remoteDir := t.TempDir()

	gitRun(t, remoteDir, "init", "--bare")
	gitRun(t, repoDir, "init")
	gitRun(t, repoDir, "config", "user.email", "test@example.com")
	gitRun(t, repoDir, "config", "user.name", "Test User")
	gitRun(t, repoDir, "remote", "add", "publish", remoteDir)

	mfs := fstest.MapFS{
		"testfile":      &fstest.MapFile{Data: []byte("test"), Mode: 0o644},
		"dotfiles.toml": &fstest.MapFile{Data: []byte("public = true"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "initial commit")
	gitRun(t, repoDir, "branch", "backup-stuff")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Publish()

	out := stdout.String()
	if !strings.Contains(out, "git push -q publish master") {
		t.Errorf("Expected push command with master branch, got stdout: '%s'", out)
	}
}
