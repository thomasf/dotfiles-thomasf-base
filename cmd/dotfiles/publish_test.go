package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishSkip(t *testing.T) {
	repoDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "testfile"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "testfile")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Publish()

	out := stdout.String()
	errOut := stderr.String()

	t.Logf("Stdout: %s", out)
	t.Logf("Stderr: %s", errOut)

	expectedSkip := "skipping publish for " + filepath.Base(repoDir) + ": remote 'publish' not found"
	if !strings.Contains(out, expectedSkip) {
		t.Errorf("Expected skip message '%s', got stdout: '%s'", expectedSkip, out)
	}
	if errOut != "" {
		t.Errorf("Did not expect errors in stderr, got: %s", errOut)
	}
}

func TestPublishRemoteExists(t *testing.T) {
	repoDir := t.TempDir()
	remoteDir := t.TempDir()

	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	cmd.Run()

	cmd = exec.Command("git", "remote", "add", "publish", remoteDir)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "testfile"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "testfile")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Run()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	d := &Dotfiles{
		Stdout: stdout,
		Stderr: stderr,
		repos:  []string{repoDir},
	}

	d.Publish()

	out := stdout.String()
	errOut := stderr.String()

	t.Logf("Stdout: %s", out)
	t.Logf("Stderr: %s", errOut)

	expectedPush := "[" + filepath.Base(repoDir) + "] git push -q publish master"
	if !strings.Contains(out, expectedPush) {
		t.Errorf("Expected push message '%s', got stdout: '%s'", expectedPush, out)
	}
	if errOut != "" {
		t.Errorf("Did not expect errors in stderr, got: %s", errOut)
	}
}
