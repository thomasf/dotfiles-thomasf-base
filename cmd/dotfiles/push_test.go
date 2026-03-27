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
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Push()

	out := stdout.String()
	t.Logf("Stdout: %s", out)

	expectedPush := "[" + filepath.Base(repoDir) + "] git push -q origin master"
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
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Push()

	out := stdout.String()
	t.Logf("Stdout: %s", out)

	expectedPush := "[" + filepath.Base(repoDir) + "] git push -q origin main"
	if !strings.Contains(out, expectedPush) {
		t.Errorf("Expected push command '%s', got stdout: '%s'", expectedPush, out)
	}
}
