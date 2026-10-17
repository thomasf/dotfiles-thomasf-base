package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// setupFixRepos creates a public repository alpha holding the config and a
// private repository beta with a wrong origin, an extra remote and, if
// withPublish is set, a publish remote.
func setupFixRepos(t *testing.T, withPublish bool) (root string, d *Dotfiles, stdout *bytes.Buffer) {
	root = t.TempDir()
	reposPath := filepath.Join(root, "repos")
	alpha := filepath.Join(reposPath, "alpha")
	beta := filepath.Join(reposPath, "beta")
	newGitRepo(t, alpha, fstest.MapFS{
		"dotfiles.toml": tomlFile("public = true\n" + reposConfig(root, "alpha", "beta", "gamma")),
	})
	newGitRepo(t, beta, nil)
	gitRun(t, beta, "remote", "add", "origin", "/wrong/beta.git")
	gitRun(t, beta, "remote", "set-url", "--push", "origin", "/stray/beta.git")
	gitRun(t, beta, "remote", "add", "other", "/other/beta.git")
	if withPublish {
		gitRun(t, beta, "remote", "add", "publish", "/leak/beta.git")
	}
	if err := os.Mkdir(filepath.Join(root, "backup"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout = &bytes.Buffer{}
	d = &Dotfiles{
		Stdout:    stdout,
		Stderr:    &bytes.Buffer{},
		ReposPath: reposPath,
		repos:     []string{alpha, beta},
	}
	return root, d, stdout
}

func TestReposFix(t *testing.T) {
	t.Parallel()
	root, d, _ := setupFixRepos(t, false)

	d.ReposFixCommand()
	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, d.Stderr)
	}

	assertRemotes(t, filepath.Join(root, "repos", "alpha"), map[string]string{
		"origin":      root + "/origin/alpha.git",
		"backup.disk": root + "/backup/alpha.git",
		"publish":     root + "/publish/alpha.git push=" + root + "/publish-push/alpha.git",
	})
	// beta is not public: no publish remote, and the extra remote is kept.
	assertRemotes(t, filepath.Join(root, "repos", "beta"), map[string]string{
		"origin":      root + "/origin/beta.git",
		"backup.disk": root + "/backup/beta.git",
		"other":       "/other/beta.git",
	})
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(root, "backup", name+".git", "HEAD")); err != nil {
			t.Errorf("backup repository for %s not created: %v", name, err)
		}
	}
	if isDir(filepath.Join(root, "repos", "gamma")) {
		t.Error("fix must not clone")
	}
}

func TestReposFixDryRun(t *testing.T) {
	t.Parallel()
	root, d, stdout := setupFixRepos(t, false)
	d.DryRun = true

	d.ReposFixCommand()
	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v", d.runErrors)
	}
	assertRemotes(t, filepath.Join(root, "repos", "alpha"), map[string]string{})
	if isDir(filepath.Join(root, "backup", "alpha.git")) {
		t.Error("dry run created backup repository")
	}
	for _, want := range []string{
		"plan: [alpha] git init -q --bare " + root + "/backup/alpha.git",
		"plan: [alpha] git remote add origin " + root + "/origin/alpha.git",
		"plan: [alpha] git remote set-url --push publish " + root + "/publish-push/alpha.git",
		"plan: [beta] git remote set-url origin " + root + "/origin/beta.git",
		"plan: [beta] git config --unset-all remote.origin.pushurl",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestReposFixPublishGuard(t *testing.T) {
	t.Parallel()
	root, d, _ := setupFixRepos(t, true)

	d.ReposFixCommand()
	if len(d.runErrors) != 1 || !strings.Contains(d.runErrors[0].Error(), "beta: publish remote publish on a repository that is not public") {
		t.Errorf("errors = %v", d.runErrors)
	}
	// The other remotes are still fixed.
	assertRemotes(t, filepath.Join(root, "repos", "beta"), map[string]string{
		"origin":      root + "/origin/beta.git",
		"backup.disk": root + "/backup/beta.git",
		"other":       "/other/beta.git",
		"publish":     "/leak/beta.git",
	})
}

func TestReposStatus(t *testing.T) {
	t.Parallel()
	_, d, stdout := setupFixRepos(t, true)

	d.ReposStatusCommand()
	if len(d.runErrors) != 1 {
		t.Errorf("errors = %v, want the publish guard error", d.runErrors)
	}

	lines := map[string][]string{}
	for line := range strings.Lines(stdout.String()) {
		fields := strings.Fields(line)
		lines[fields[0]] = fields
	}
	if got, want := strings.Join(lines["REPO"], " "), "REPO backup.disk backup.off origin publish NOTES"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
	for repo, want := range map[string]string{
		"alpha": "alpha missing inactive missing missing",
		"beta":  "beta missing inactive differs ERROR: not public extra: other",
		"gamma": "gamma - - - - not cloned",
	} {
		if got := strings.Join(lines[repo], " "); got != want {
			t.Errorf("row %s = %q, want %q", repo, got, want)
		}
	}
}

func TestReposClone(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reposPath := filepath.Join(root, "repos")
	if err := os.Mkdir(filepath.Join(root, "backup"), 0o755); err != nil {
		t.Fatal(err)
	}

	// alpha is already cloned and lists beta, which declares itself public.
	for name, files := range map[string]fstest.MapFS{
		"alpha": {"dotfiles.toml": tomlFile(reposConfig(root, "alpha", "beta"))},
		"beta":  {"dotfiles.toml": tomlFile("public = true")},
	} {
		src := filepath.Join(root, "src", name)
		newGitRepo(t, src, files)
		gitRun(t, root, "clone", "-q", "--bare", src, filepath.Join(root, "origin", name+".git"))
	}
	gitRun(t, root, "clone", "-q", filepath.Join(root, "origin", "alpha.git"), filepath.Join(reposPath, "alpha"))

	stdout := &bytes.Buffer{}
	d := &Dotfiles{
		Stdout:    stdout,
		Stderr:    &bytes.Buffer{},
		ReposPath: reposPath,
		repos:     []string{filepath.Join(reposPath, "alpha")},
	}
	d.ReposCloneCommand(nil)
	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, d.Stderr)
	}
	if want := "beta  origin  cloned"; !strings.Contains(stdout.String(), want) {
		t.Errorf("summary missing %q:\n%s", want, stdout)
	}

	beta := filepath.Join(reposPath, "beta")
	if _, err := os.Stat(filepath.Join(beta, "dotfiles.toml")); err != nil {
		t.Fatalf("beta not cloned: %v", err)
	}
	assertRemotes(t, beta, map[string]string{
		"origin":      root + "/origin/beta.git",
		"backup.disk": root + "/backup/beta.git",
		"publish":     root + "/publish/beta.git push=" + root + "/publish-push/beta.git",
	})
	// alpha was already cloned; its remotes are fixed too.
	assertRemotes(t, filepath.Join(reposPath, "alpha"), map[string]string{
		"origin":      root + "/origin/alpha.git",
		"backup.disk": root + "/backup/alpha.git",
	})
}

func TestReposCloneNamed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reposPath := filepath.Join(root, "repos")

	for name, files := range map[string]fstest.MapFS{
		"alpha": {"dotfiles.toml": tomlFile(reposConfig(root, "alpha", "beta", "gamma"))},
		"beta":  nil,
		"gamma": nil,
	} {
		src := filepath.Join(root, "src", name)
		newGitRepo(t, src, files)
		gitRun(t, root, "clone", "-q", "--bare", src, filepath.Join(root, "origin", name+".git"))
	}
	gitRun(t, root, "clone", "-q", filepath.Join(root, "origin", "alpha.git"), filepath.Join(reposPath, "alpha"))

	d := &Dotfiles{
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		ReposPath: reposPath,
		repos:     []string{filepath.Join(reposPath, "alpha")},
	}
	d.ReposCloneCommand([]string{"gamma"})
	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, d.Stderr)
	}
	if !isDir(filepath.Join(reposPath, "gamma")) {
		t.Error("gamma not cloned")
	}
	if isDir(filepath.Join(reposPath, "beta")) {
		t.Error("beta cloned although not named")
	}

	d.ReposCloneCommand([]string{"delta"})
	if len(d.runErrors) != 1 || !strings.Contains(d.runErrors[0].Error(), `unknown repository "delta"`) {
		t.Errorf("errors = %v", d.runErrors)
	}
}

// TestReposClonePublic clones from the publish remote when only the public
// configuration is available.
func TestReposClonePublic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reposPath := filepath.Join(root, "repos")
	config := fmt.Sprintf(`
public = true

[repositories]
names = ["alpha", "beta"]

[repositories.remote.publish]
kind = "publish"
url = '%[1]s/public/{name}.git'
pushurl = '%[1]s/push/{name}.git'
`, root)

	for name, files := range map[string]fstest.MapFS{
		"alpha": {"dotfiles.toml": tomlFile(config)},
		"beta":  {"dotfiles.toml": tomlFile("public = true")},
	} {
		src := filepath.Join(root, "src", name)
		newGitRepo(t, src, files)
		gitRun(t, root, "clone", "-q", "--bare", src, filepath.Join(root, "public", name+".git"))
	}
	gitRun(t, root, "clone", "-q", "-o", "publish", filepath.Join(root, "public", "alpha.git"), filepath.Join(reposPath, "alpha"))

	d := &Dotfiles{
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		ReposPath: reposPath,
		repos:     []string{filepath.Join(reposPath, "alpha")},
	}
	d.ReposCloneCommand(nil)
	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, d.Stderr)
	}
	for _, name := range []string{"alpha", "beta"} {
		assertRemotes(t, filepath.Join(reposPath, name), map[string]string{
			"publish": root + "/public/" + name + ".git push=" + root + "/push/" + name + ".git",
		})
	}

	remote, err := d.pullRemote()
	if err != nil || remote != "publish" {
		t.Errorf("pullRemote() = %q, %v; want publish", remote, err)
	}
}
