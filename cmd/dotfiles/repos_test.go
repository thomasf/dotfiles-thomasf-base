package main

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// newGitRepo creates a git repository at dir with files committed on main.
func newGitRepo(t *testing.T, dir string, files fstest.MapFS) {
	t.Helper()
	if files == nil {
		files = fstest.MapFS{}
	}
	files["testfile"] = &fstest.MapFile{Data: []byte("test"), Mode: 0o644}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, files); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test User")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "initial commit")
}

func tomlFile(s string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(s), Mode: 0o644}
}

// reposConfig returns a [repositories] config with remotes under root.
func reposConfig(root string, names ...string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return fmt.Sprintf(`
[repositories]
names = [%s]

[repositories.remote.origin]
kind = "private"
url = '%[2]s/origin/{name}.git'

[repositories.remote."backup.disk"]
kind = "backup"
url = '%[2]s/backup/{name}.git'

[repositories.remote."backup.off"]
kind = "backup"
url = '%[2]s/off/{name}.git'
condition = "os == 'plan9'"

[repositories.remote.publish]
kind = "publish"
url = '%[2]s/publish/{name}.git'
`, strings.Join(quoted, ", "), root)
}

func assertRemotes(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	got, err := gitRemotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, want) {
		t.Errorf("remotes of %s:\ngot  %v\nwant %v", filepath.Base(dir), got, want)
	}
}

func TestMergeRepositories(t *testing.T) {
	origin := Remote{Kind: RemoteKindPrivate, URL: "/srv/{name}.git"}
	repo := func(path string, public bool, r Repositories) *Repository {
		return &Repository{
			srcRoot:    path,
			configPath: filepath.Join(path, "dotfiles.toml"),
			config:     Config{Public: public, Repositories: r},
		}
	}

	t.Run("merge", func(t *testing.T) {
		set, err := mergeRepositories([]*Repository{
			repo("/a", true, Repositories{
				Names:  []string{"b", "a"},
				Remote: map[string]Remote{"origin": origin},
			}),
			repo("/b", false, Repositories{
				Names: []string{"a", "c"},
				Remote: map[string]Remote{
					"origin":   origin,
					"backup.x": {Kind: RemoteKindBackup, URL: "/x/{name}", Condition: "os == 'plan9'"},
				},
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"a", "b", "c"}; !slices.Equal(set.names, want) {
			t.Errorf("names = %v, want %v", set.names, want)
		}
		if len(set.remotes) != 2 || set.remotes[0].Name != "backup.x" || set.remotes[1].Name != "origin" {
			t.Fatalf("remotes = %+v", set.remotes)
		}
		if set.remotes[0].active || !set.remotes[1].active {
			t.Errorf("active = %v, %v; want false, true", set.remotes[0].active, set.remotes[1].active)
		}
		if !set.public("/a") || set.public("/b") {
			t.Error("public flags not taken from each repository's config")
		}
		if got := set.remotes[1].urlFor("base"); got != "/srv/base.git" {
			t.Errorf("urlFor = %q", got)
		}
	})

	for name, tc := range map[string]struct {
		repos []*Repository
		want  string
	}{
		"conflict": {
			repos: []*Repository{
				repo("/a", false, Repositories{Remote: map[string]Remote{"origin": origin}}),
				repo("/b", false, Repositories{Remote: map[string]Remote{"origin": {Kind: RemoteKindPrivate, URL: "/other/{name}"}}}),
			},
			want: `remote "origin" is defined differently in /a/dotfiles.toml and /b/dotfiles.toml`,
		},
		"kind": {
			repos: []*Repository{repo("/a", false, Repositories{Remote: map[string]Remote{"x": {Kind: "mirror", URL: "/{name}"}}})},
			want:  `unknown kind "mirror"`,
		},
		"template": {
			repos: []*Repository{repo("/a", false, Repositories{Remote: map[string]Remote{"x": {Kind: RemoteKindBackup, URL: "/srv/repo.git"}}})},
			want:  "does not contain {name}",
		},
		"name": {
			repos: []*Repository{repo("/a", false, Repositories{Names: []string{"../x"}})},
			want:  `invalid repository name "../x"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := mergeRepositories(tc.repos)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

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

	d.ReposFix()
	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, d.Stderr)
	}

	assertRemotes(t, filepath.Join(root, "repos", "alpha"), map[string]string{
		"origin":      root + "/origin/alpha.git",
		"backup.disk": root + "/backup/alpha.git",
		"publish":     root + "/publish/alpha.git",
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

	d.ReposFix()
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
		"plan: [beta] git remote set-url origin " + root + "/origin/beta.git",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestReposFixPublishGuard(t *testing.T) {
	t.Parallel()
	root, d, _ := setupFixRepos(t, true)

	d.ReposFix()
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

	d.ReposStatus()
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

	d := &Dotfiles{
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		ReposPath: reposPath,
		repos:     []string{filepath.Join(reposPath, "alpha")},
	}
	d.ReposClone()
	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, d.Stderr)
	}

	beta := filepath.Join(reposPath, "beta")
	if _, err := os.Stat(filepath.Join(beta, "dotfiles.toml")); err != nil {
		t.Fatalf("beta not cloned: %v", err)
	}
	assertRemotes(t, beta, map[string]string{
		"origin":      root + "/origin/beta.git",
		"backup.disk": root + "/backup/beta.git",
		"publish":     root + "/publish/beta.git",
	})
	// alpha was already cloned and is left alone.
	assertRemotes(t, filepath.Join(reposPath, "alpha"), map[string]string{
		"origin": root + "/origin/alpha.git",
	})
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
	d := &Dotfiles{Stdout: stdout, Stderr: stderr, repos: []string{alpha}}
	d.Push()

	if len(d.runErrors) > 0 {
		t.Fatalf("errors: %v\nstderr: %s", d.runErrors, stderr)
	}
	if !strings.Contains(stdout.String(), "[alpha] git push -q origin main") {
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
