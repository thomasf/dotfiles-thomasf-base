package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

// lockedBuffer is a bytes.Buffer that the parallel git commands can write to
// at the same time.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestParseSSHURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url    string
		want   sshEndpoint
		wantOK bool
	}{
		{"ssh://git@gogs.medeltiden.org:10022/t/base.git", sshEndpoint{"git", "gogs.medeltiden.org", "10022"}, true},
		{"ssh://example.com/repo.git", sshEndpoint{host: "example.com"}, true},
		{"git+ssh://git@example.com/repo.git", sshEndpoint{user: "git", host: "example.com"}, true},
		{"git@github.com:t/base.git", sshEndpoint{user: "git", host: "github.com"}, true},
		{"github.com:t/base.git", sshEndpoint{host: "github.com"}, true},
		{"https://github.com/t/base.git", sshEndpoint{}, false},
		{"file:///srv/base.git", sshEndpoint{}, false},
		{"/srv/base.git", sshEndpoint{}, false},
		{"./dir:with/colon", sshEndpoint{}, false},
		{"", sshEndpoint{}, false},
	}
	for _, tt := range tests {
		if got, ok := parseSSHURL(tt.url); got != tt.want || ok != tt.wantOK {
			t.Errorf("parseSSHURL(%q) = %+v, %v; want %+v, %v", tt.url, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestPullSummary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	beta := filepath.Join(root, "repos", "beta")
	broken := filepath.Join(root, "repos", "broken")
	for _, dir := range []string{alpha, beta, broken} {
		newGitRepo(t, dir, nil)
	}

	// alpha gets two new commits upstream, beta none, and broken's remote
	// does not exist.
	alphaUp := filepath.Join(root, "up", "alpha")
	gitRun(t, root, "clone", "-q", alpha, alphaUp)
	gitRun(t, alphaUp, "config", "user.email", "test@example.com")
	gitRun(t, alphaUp, "config", "user.name", "Test User")
	for i, content := range []string{"one\ntwo\n", "one\n"} {
		if err := os.WriteFile(filepath.Join(alphaUp, "testfile"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, alphaUp, "commit", "-q", "-am", "change "+string(rune('a'+i)))
	}
	gitRun(t, alpha, "remote", "add", "origin", alphaUp)
	betaUp := filepath.Join(root, "up", "beta")
	gitRun(t, root, "clone", "-q", "--bare", beta, betaUp)
	gitRun(t, beta, "remote", "add", "origin", betaUp)
	gitRun(t, broken, "remote", "add", "origin", filepath.Join(root, "missing"))

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, repos: []string{alpha, beta, broken}}
	d.Pull()

	summary := stdout.String()
	if strings.Contains(summary, "git pull") {
		t.Errorf("printed git commands without -v:\n%s", summary)
	}
	lines := strings.Split(strings.TrimSpace(summary), "\n")
	want := []string{
		"alpha   origin  2 commits, 1 file changed, 1 insertion(+), 1 deletion(-)",
		"beta    origin  up to date",
		"broken  origin  FAILED: exit status 1: fatal: ",
	}
	if len(lines) != len(want) {
		t.Fatalf("summary:\n%s\nwant %d lines", summary, len(want))
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Errorf("summary line %d = %q, want prefix %q", i, lines[i], w)
		}
	}
	if len(d.runErrors) != 1 {
		t.Errorf("runErrors = %v, want one for broken", d.runErrors)
	}
}

func TestPullChecksSSHHosts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// fakessh runs the command git asks the remote end to run locally.
	fakeSSH := filepath.Join(root, "fakessh")
	script := "#!/bin/sh\nfor a; do cmd=$a; done\nexec sh -c \"git ${cmd#git-}\"\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var repos []string
	for _, name := range []string{"alpha", "beta", "gamma"} {
		dir := filepath.Join(root, "repos", name)
		newGitRepo(t, dir, nil)
		up := filepath.Join(root, "up", name+".git")
		gitRun(t, root, "clone", "-q", "--bare", dir, up)
		if name == "gamma" {
			gitRun(t, dir, "remote", "add", "origin", "git@bad.example:"+up)
			gitRun(t, dir, "config", "core.sshCommand", "false")
		} else {
			gitRun(t, dir, "remote", "add", "origin", "ssh://git@good.example"+up)
			gitRun(t, dir, "config", "core.sshCommand", fakeSSH)
		}
		repos = append(repos, dir)
	}

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, Verbose: true, repos: repos}
	d.Pull()

	out := stdout.String()
	if n := strings.Count(out, "git ls-remote "); n != 2 {
		t.Errorf("ran %d host checks, want 2 (one per host):\n%s", n, out)
	}
	if strings.Index(out, "git pull") < strings.LastIndex(out, "git ls-remote") {
		t.Errorf("pulled before all hosts were checked:\n%s", out)
	}
	if strings.Contains(out, "[gamma] git pull") {
		t.Errorf("pulled from a host that failed the check:\n%s", out)
	}
	for _, want := range []string{
		"alpha  origin  up to date",
		"beta   origin  up to date",
		"gamma  origin  SKIPPED: cannot connect to git@bad.example: exit status 128: fatal: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	if len(d.runErrors) != 1 {
		t.Errorf("runErrors = %v, want one for bad.example", d.runErrors)
	}
}

func TestPullCommandNoInstall(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	up := filepath.Join(root, "up", "alpha.git")
	gitRun(t, root, "clone", "-q", "--bare", alpha, up)
	gitRun(t, alpha, "remote", "add", "origin", up)

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, repos: []string{alpha}}
	if err := d.PullCommand([]string{"extra"}); err == nil {
		t.Error("PullCommand(extra) succeeded, want an error")
	}
	if err := d.PullCommand([]string{"-no-install"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, "alpha  origin  up to date") {
		t.Errorf("not pulled:\n%s", out)
	}
	if strings.Contains(out, "Installing") {
		t.Errorf("installed despite -no-install:\n%s", out)
	}
}

func TestPullHostCheckTimeout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// The host never answers.
	hangSSH := filepath.Join(root, "hangssh")
	if err := os.WriteFile(hangSSH, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	gitRun(t, alpha, "remote", "add", "origin", "git@slow.example:alpha.git")
	gitRun(t, alpha, "config", "core.sshCommand", hangSSH)

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, repos: []string{alpha}, hostCheckTimeout: 100 * time.Millisecond}
	if d.Pull() {
		t.Error("Pull() = true, want false")
	}
	if want := "alpha  origin  SKIPPED: cannot connect to git@slow.example: no answer within 100ms"; !strings.Contains(stdout.String(), want) {
		t.Errorf("summary missing %q:\n%s", want, stdout)
	}
}

func TestPullSkipsOtherBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	up := filepath.Join(root, "up", "alpha.git")
	gitRun(t, root, "clone", "-q", "--bare", alpha, up)
	gitRun(t, alpha, "remote", "add", "origin", up)
	gitRun(t, alpha, "switch", "-q", "-c", "feature")

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, Verbose: true, repos: []string{alpha}}
	if !d.Pull() {
		t.Errorf("Pull() = false, want true: a skipped repository is not a failure\n%s", stdout)
	}
	out := stdout.String()
	if strings.Contains(out, "git pull") {
		t.Errorf("pulled into another branch:\n%s", out)
	}
	if want := "alpha  origin  SKIPPED: on feature, not main"; !strings.Contains(out, want) {
		t.Errorf("summary missing %q:\n%s", want, out)
	}
}

func TestPullCommandFailedPullSkipsInstall(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	gitRun(t, alpha, "remote", "add", "origin", filepath.Join(root, "missing"))

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, DstPath: t.TempDir(), repos: []string{alpha}}
	if err := d.PullCommand(nil); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if strings.Contains(out, "Installing") || !strings.Contains(out, "Not installing because a pull failed") {
		t.Errorf("installed after a failed pull:\n%s", out)
	}
}

// TestPullCountsRemoteCommits checks that the summary counts the commits that
// came from the remote, not the local commits that pulling rebased, and the
// local commits that are not pushed.
func TestPullCountsRemoteCommits(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	newGitRepo(t, alpha, nil)
	up := filepath.Join(root, "up", "alpha.git")
	gitRun(t, root, "clone", "-q", "--bare", alpha, up)
	gitRun(t, alpha, "remote", "add", "origin", up)
	gitRun(t, alpha, "fetch", "-q", "origin")
	gitRun(t, alpha, "config", "pull.rebase", "true")

	// Two commits arrive upstream while alpha makes one of its own.
	other := filepath.Join(root, "other")
	gitRun(t, root, "clone", "-q", up, other)
	gitRun(t, other, "config", "user.email", "test@example.com")
	gitRun(t, other, "config", "user.name", "Test User")
	for _, name := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(other, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, other, "add", name)
		gitRun(t, other, "commit", "-q", "-m", name)
	}
	gitRun(t, other, "push", "-q", "origin", "main")
	if err := os.WriteFile(filepath.Join(alpha, "local"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, alpha, "add", "local")
	gitRun(t, alpha, "commit", "-q", "-m", "local")

	stdout := &lockedBuffer{}
	d := &Dotfiles{Stdout: stdout, Stderr: &lockedBuffer{}, repos: []string{alpha}}
	if !d.Pull() {
		t.Fatalf("Pull() = false\n%s", stdout)
	}
	want := "alpha  origin  2 commits, 2 files changed, 2 insertions(+), 1 local commit not pushed"
	if !strings.Contains(stdout.String(), want) {
		t.Errorf("summary missing %q:\n%s", want, stdout)
	}
}

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

func TestPullConfiguredRemote(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	alpha := filepath.Join(root, "repos", "alpha")
	config := `
[repositories.remote.upstream]
kind = "private"
url = '/srv/{name}.git'
`
	newGitRepo(t, alpha, fstest.MapFS{"dotfiles.toml": tomlFile(config)})

	d := &Dotfiles{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, repos: []string{alpha}}
	remote, err := d.pullRemote()
	if err != nil || remote != "upstream" {
		t.Errorf("pullRemote() = %q, %v; want upstream", remote, err)
	}
}
