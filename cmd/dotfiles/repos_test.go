package main

import (
	"fmt"
	"io"
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
pushurl = '%[2]s/publish-push/{name}.git'
`, strings.Join(quoted, ", "), root)
}

// assertRemotes checks the remotes of the repository at dir. Each remote is
// written as its URL, followed by " push=" and its push URL if it has one.
func assertRemotes(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	remotes, err := gitRemotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string)
	for name, r := range remotes {
		got[name] = r.url
		if r.pushURL != "" {
			got[name] += " push=" + r.pushURL
		}
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
			want:  `url "/srv/repo.git" does not contain {name}`,
		},
		"push template": {
			repos: []*Repository{repo("/a", false, Repositories{Remote: map[string]Remote{"x": {Kind: RemoteKindPublish, URL: "/{name}", PushURL: "/srv/repo.git"}}})},
			want:  `pushurl "/srv/repo.git" does not contain {name}`,
		},
		"pushurl conflict": {
			repos: []*Repository{
				repo("/a", false, Repositories{Remote: map[string]Remote{"publish": {Kind: RemoteKindPublish, URL: "/{name}", PushURL: "/p/{name}"}}}),
				repo("/b", false, Repositories{Remote: map[string]Remote{"publish": {Kind: RemoteKindPublish, URL: "/{name}"}}}),
			},
			want: `remote "publish" is defined differently`,
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

// TestLoadReposParts checks that loadRepos loads only the parts it is asked
// for, so that a broken theme or template stops only what needs them.
func TestLoadReposParts(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	// [vars] resolve; the template's dst does not render; -theme names no
	// theme.
	config := "[vars]\nx = '{{\"y\"}}'\n\n[[template]]\nsrc = 'a'\ndst = '{{.vars.missing}}'\n"
	if err := os.WriteFile(filepath.Join(repo, "dotfiles.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		parts  configParts
		loaded bool   // the repository is returned
		vars   bool   // its [vars] are resolved
		errs   string // the errors collected, in this order
	}{
		{"nothing", 0, true, false, ""},
		{"theme", partTheme, true, false, "theme"},
		{"vars", partVars, true, true, ""},
		{"vars and render", partVars | partRender, false, false, "config"},
		{"all", allParts, false, false, "theme config"},
	} {
		d := &Dotfiles{Stdout: io.Discard, Stderr: io.Discard, DstPath: t.TempDir(), Theme: "nope:dark", repos: []string{repo}}
		loaded := d.loadRepos(tt.parts)
		var errs []string
		for _, err := range d.runErrors {
			kind, _, _ := strings.Cut(err.Error(), ":")
			kind, _, _ = strings.Cut(kind, " ")
			errs = append(errs, kind)
		}
		if got := strings.Join(errs, " "); got != tt.errs {
			t.Errorf("%s: errors %q, want %q: %v", tt.name, got, tt.errs, d.runErrors)
		}
		if (len(loaded) == 1) != tt.loaded {
			t.Errorf("%s: %d repositories loaded, want loaded %v", tt.name, len(loaded), tt.loaded)
			continue
		}
		if tt.loaded && (loaded[0].vars["x"] == "y") != tt.vars {
			t.Errorf("%s: vars %v, want resolved %v", tt.name, loaded[0].vars, tt.vars)
		}
	}

	defer func() {
		if recover() == nil {
			t.Error("partRender without partVars: no panic")
		}
	}()
	(&Dotfiles{Stderr: io.Discard, repos: []string{repo}}).loadRepos(partRender)
}
