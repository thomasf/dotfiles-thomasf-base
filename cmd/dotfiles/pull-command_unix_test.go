//go:build unix

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// interruptWhenStarted sends SIGINT to the test process once the file
// started exists. The interrupt tests cannot run in parallel with other
// tests, which would get the signal too.
func interruptWhenStarted(t *testing.T, started string) {
	t.Helper()
	for range 500 {
		if _, err := os.Stat(started); err == nil {
			if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("git did not start")
}

func TestPullInterruptedDuringHostCheck(t *testing.T) {
	root := t.TempDir()
	started := filepath.Join(root, "started")
	hangSSH := filepath.Join(root, "hangssh")
	if err := os.WriteFile(hangSSH, []byte("#!/bin/sh\ntouch "+started+"\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	gitRun(t, alpha, "remote", "add", "origin", "git@slow.example:alpha.git")
	gitRun(t, alpha, "config", "core.sshCommand", hangSSH)

	stdout, stderr := &lockedBuffer{}, &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: stderr, repos: []string{alpha}}
	ok := make(chan bool)
	go func() { ok <- d.Pull() }()
	interruptWhenStarted(t, started)

	if <-ok {
		t.Error("Pull() = true, want false")
	}
	if want := "alpha  origin  SKIPPED: interrupted while checking git@slow.example"; !strings.Contains(stdout.String(), want) {
		t.Errorf("summary missing %q:\n%s", want, stdout)
	}
	if !strings.Contains(stderr.String(), "interrupt: stopping git") {
		t.Errorf("interrupt not reported:\n%s", stderr)
	}
	if len(d.runErrors) != 1 || !errors.Is(d.runErrors[0], errInterrupted) {
		t.Errorf("runErrors = %v, want only the interrupt", d.runErrors)
	}
}

func TestPullCommandInterruptedDuringPull(t *testing.T) {
	root := t.TempDir()
	started := filepath.Join(root, "started")
	checked := filepath.Join(root, "checked")
	// The host check gets through; the pull hangs.
	fakeSSH := filepath.Join(root, "fakessh")
	script := "#!/bin/sh\nfor a; do cmd=$a; done\n" +
		"if [ -e " + checked + " ]; then touch " + started + "; exec sleep 5; fi\n" +
		"touch " + checked + "\nexec sh -c \"git ${cmd#git-}\"\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	up := filepath.Join(root, "up", "alpha.git")
	gitRun(t, root, "clone", "-q", "--bare", alpha, up)
	gitRun(t, alpha, "remote", "add", "origin", "ssh://git@slow.example"+up)
	gitRun(t, alpha, "config", "core.sshCommand", fakeSSH)
	// Otherwise git first runs fakessh -G to find out which ssh it is.
	gitRun(t, alpha, "config", "ssh.variant", "ssh")

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, DstPath: t.TempDir(), repos: []string{alpha}}
	done := make(chan error)
	go func() { done <- d.PullCommand(nil) }()
	interruptWhenStarted(t, started)

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if want := "alpha  origin  FAILED: interrupted during git pull -q origin main"; !strings.Contains(out, want) {
		t.Errorf("summary missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "Installing") {
		t.Errorf("installed after an interrupt:\n%s", out)
	}
}
