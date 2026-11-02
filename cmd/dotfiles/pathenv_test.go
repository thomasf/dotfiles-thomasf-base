package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestMain runs dotfiles itself instead of the tests when
// PATHENV_TEST_MAIN is set, so that pathenv exec, which runs
// os.Executable, can be tested.
func TestMain(m *testing.M) {
	if os.Getenv("PATHENV_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// pathenvFixture writes files, by path below the repositories directory,
// and the directories dirs below the home directory. It returns both
// directories.
func pathenvFixture(t *testing.T, files map[string]string, dirs ...string) (repos, home string) {
	t.Helper()
	// Without symlinks, as git prints the paths of worktrees (on macOS the
	// temporary directory is below the symlink /var).
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repos, home = filepath.Join(root, "repos"), filepath.Join(root, "home")
	for name, content := range files {
		path := filepath.Join(repos, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range append(dirs, ".") {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return repos, home
}

// loadTestPathenv loads the pathenv files of the repositories in repos.
func loadTestPathenv(t *testing.T, repos, home string) *pathenvSet {
	t.Helper()
	var stderr bytes.Buffer
	d := &Dotfiles{Stdout: &stderr, Stderr: &stderr, ReposPath: repos, DstPath: home}
	if err := d.discoverRepos(); err != nil {
		t.Fatal(err)
	}
	set := loadPathenv(d.loadRepos(), home, "bash")
	if len(d.runErrors) > 0 {
		t.Fatalf("loading the repositories: %v\n%s", d.runErrors, stderr.String())
	}
	return set
}

// runPathenv runs dotfiles pathenv with args for the repositories in repos.
func runPathenv(repos, home string, args ...string) (stdout, stderr string, err error) {
	var out, errb bytes.Buffer
	err = new(Dotfiles).Run(&out, &errb, append([]string{"-repos", repos, "-dst", home, "pathenv"}, args...))
	return out.String(), errb.String(), err
}

const pathenvRepoConfig = "[pathenv]\ndirs = ['pathenv']\n"

func TestPathenvLoad(t *testing.T) {
	t.Parallel()
	repos, home := pathenvFixture(t, map[string]string{
		"base/dotfiles.toml":                 pathenvRepoConfig,
		"base/pathenv/direnv.include.toml":   "[[run]]\nsrc = 'direnv'\n",
		"base/pathenv/dup.include.toml":      "[[run]]\nsrc = 'x'\n",
		"base/pathenv/script.sh":             "not a pathenv file",
		"base/pathenv/.#lock.toml":           "not a pathenv file",
		"work/dotfiles.toml":                 pathenvRepoConfig,
		"work/pathenv/dup.toml":              "paths = ['b']\n",
		"work/pathenv/ok.toml":               "paths = ['c']\n[[run]]\ninclude = 'direnv'\n[[run]]\ninclude = 'base:direnv'\n",
		"work/pathenv/syntax.toml":           "paths = [\n",
		"work/pathenv/unknown-key.toml":      "paths = ['d']\npath = 'd'\n",
		"work/pathenv/paths-in.include.toml": "paths = ['e']\n",
		"work/pathenv/no-paths.toml":         "[[run]]\nsrc = 'x'\n",
		"work/pathenv/two-kinds.toml":        "paths = ['f']\n[[run]]\nsrc = 'x'\nfile = 'y'\n",
		"work/pathenv/on-include.toml":       "paths = ['g']\n[[run]]\ninclude = 'direnv'\non = 'refresh'\n",
		"work/pathenv/bad-on.toml":           "paths = ['h']\n[[run]]\nsrc = 'x'\non = ['activate', 'leave']\n",
		"work/pathenv/bad-var.toml":          "paths = ['i']\n[vars]\n1x = 'y'\n",
		"work/pathenv/bad-expr.toml":         "paths = ['j']\ncondition = 'os =='\n",
		"work/pathenv/bad-template.toml":     "paths = ['k']\n[[run]]\nsrc = '{{.vars.x'\n",
		"work/pathenv/broken.include.toml":   "[[run]]\nsrc = 1\n",
		"work/pathenv/uses-broken.toml":      "paths = ['l']\n[[run]]\ninclude = 'broken'\n",
		"work/pathenv/unknown-include.toml":  "paths = ['m']\n[[run]]\ninclude = 'nope'\n",
		"work/pathenv/include-config.toml":   "paths = ['n']\n[[run]]\ninclude = 'ok'\n",
		"work/pathenv/cycle-a.include.toml":  "[[run]]\ninclude = 'cycle-b'\n",
		"work/pathenv/cycle-b.include.toml":  "[[run]]\ninclude = 'cycle-a'\n",
		"work/pathenv/uses-cycle.toml":       "paths = ['o']\n[[run]]\ninclude = 'cycle-a'\n",
		"work/pathenv/bad name.toml":         "paths = ['p']\n",
		"other/dotfiles.toml":                "[pathenv]\ndirs = ['missing']\n",
	})
	set := loadTestPathenv(t, repos, home)

	errs := map[string]string{}
	for _, f := range set.files {
		if base := filepath.Base(f.path); filepath.Ext(base) != ".toml" || strings.HasPrefix(base, ".") {
			t.Errorf("%s is loaded as a pathenv file", f.path)
		}
		if f.err != nil {
			errs[f.name] = f.err.Error()
		}
	}

	wantErrs := map[string]string{
		"base:dup":             "the name dup is also used by",
		"work:dup":             "the name dup is also used by",
		"work:syntax":          "syntax.toml:",
		"work:unknown-key":     "path",
		"work:paths-in":        "paths: not allowed in an include file",
		"work:no-paths":        "paths is required",
		"work:two-kinds":       "run 1: want exactly one of src, file and include",
		"work:on-include":      "run 1: on cannot be used with include",
		"work:bad-on":          `run 1: invalid on "leave"`,
		"work:bad-var":         "vars.1x: invalid name",
		"work:bad-expr":        "condition:",
		"work:bad-template":    "run 1: src",
		"work:broken":          "broken.include.toml:",
		"work:uses-broken":     "work:broken is broken",
		"work:unknown-include": `include "nope": no such include file`,
		"work:include-config":  "work:ok is a configuration, not an include file",
		"work:cycle-a":         "include cycle: work:cycle-a -> work:cycle-b -> work:cycle-a",
		"work:cycle-b":         "include cycle",
		"work:uses-cycle":      "work:cycle-a is broken",
		"work:bad name":        "invalid name",
	}
	for name, want := range wantErrs {
		if !strings.Contains(errs[name], want) {
			t.Errorf("%s: error %q, want it to contain %q", name, errs[name], want)
		}
		delete(errs, name)
	}
	for name, err := range errs {
		t.Errorf("%s: unexpected error %s", name, err)
	}

	if f := set.byName["direnv"]; f == nil || f.name != "base:direnv" {
		t.Errorf("byName[direnv] = %v, want base:direnv", f)
	}
	if f := set.byName["dup"]; f != nil {
		t.Errorf("byName[dup] = %s, want nil: the name is used twice", f.name)
	}
	if len(set.errs) != 1 || !strings.Contains(set.errs[0].Error(), "pathenv.dirs") {
		t.Errorf("set errors %v, want one about the missing directory", set.errs)
	}
}

// matchName returns the name of the configuration that matches dir below
// home, or "" for none.
func matchName(t *testing.T, set *pathenvSet, home, dir string) (string, error) {
	t.Helper()
	m, err := set.match(filepath.Join(home, dir))
	if m == nil {
		return "", err
	}
	return m.file.name, err
}

func TestPathenvMatch(t *testing.T) {
	t.Parallel()
	repos, home := pathenvFixture(t, map[string]string{
		"base/dotfiles.toml":  pathenvRepoConfig + "[vars]\nsrc = 'src'\n",
		"base/pathenv/a.toml": "paths = ['{{.vars.src}}/infra/']\n",
		"base/pathenv/b.toml": "paths = ['src/infra/api']\n",
		"base/pathenv/c.toml": "paths = ['src/infra/api/carve/']\n",
		"base/pathenv/d.toml": "paths = ['src/cond/']\ncondition = 'vars.on == \"yes\"'\n[vars]\non = 'no'\n",
		"base/pathenv/e.toml": "paths = ['src/cond/sub/']\ncondition = 'shell == \"fish\"'\n",
		"base/pathenv/f.toml": "paths = ['src/twice/']\n",
		"base/pathenv/g.toml": "paths = ['src/twice/']\ncondition = 'os != \"\"'\n",
		"base/pathenv/h.toml": "paths = ['src/once/']\ncondition = 'false'\n",
		"base/pathenv/i.toml": "paths = ['src/once/']\n",
		"base/pathenv/k.toml": "paths = ['src/']\n",
	})
	set := loadTestPathenv(t, repos, home)
	for _, tt := range []struct {
		dir, want, err string
	}{
		{"src/infra", "base:a", ""},
		{"src/infra-old", "base:k", ""},
		{"src/infra/api", "base:b", ""},
		{"src/infra/api/carve/x", "base:c", ""},
		{"src/cond/sub", "base:k", ""}, // both conditions false
		{"src/twice", "", "base:f and base:g all match"},
		{"src/once", "base:i", ""},
		{"src", "base:k", ""},
		{"other", "", ""},
	} {
		got, err := matchName(t, set, home, tt.dir)
		if got != tt.want {
			t.Errorf("match(%s) = %q, want %q", tt.dir, got, tt.want)
		}
		if tt.err == "" && err != nil || tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)) {
			t.Errorf("match(%s): error %v, want %q", tt.dir, err, tt.err)
		}
	}
}

func TestPathenvMatchAbsolute(t *testing.T) {
	abs := t.TempDir()
	t.Setenv("PATHENV_TEST_ABS", abs)
	repos, home := pathenvFixture(t, map[string]string{
		"base/dotfiles.toml":    pathenvRepoConfig,
		"base/pathenv/abs.toml": "paths = ['{{env \"PATHENV_TEST_ABS\"}}']\n",
	})
	set := loadTestPathenv(t, repos, home)
	m, err := set.match(filepath.Join(abs, "x"))
	if err != nil || m == nil || m.file.name != "base:abs" {
		t.Fatalf("match = %v, %v; want base:abs", m, err)
	}
	if m.loc.Prefix != abs || m.loc.Root != abs {
		t.Errorf("location %+v, want prefix and root %s", m.loc, abs)
	}
}

func TestPathenvLocation(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repos, home := pathenvFixture(t, map[string]string{
		"base/dotfiles.toml":      pathenvRepoConfig,
		"base/pathenv/repos.toml": "paths = ['src/']\n",
		"base/pathenv/sub.toml":   "paths = ['src/above/sub/']\n",
		"base/pathenv/repo.toml":  "paths = ['src/repo/']\n",
	}, "src/plain/x", "src/above/sub/x", "src/gitfile/x")
	src := filepath.Join(home, "src")
	newGitRepo(t, filepath.Join(src, "repo"), nil)
	if err := os.MkdirAll(filepath.Join(src, "repo", "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, filepath.Join(src, "repo"), "worktree", "add", "-q", "-b", "fix", filepath.Join(src, "repo@fix-x"))
	if err := os.MkdirAll(filepath.Join(src, "repo@fix-x", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "above", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "gitfile", ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	set := loadTestPathenv(t, repos, home)
	for _, tt := range []struct {
		dir  string
		name string
		loc  pathenvLocation
	}{
		{"src/repo/a/b", "base:repo", pathenvLocation{Root: "src/repo", Prefix: "src/repo"}},
		{"src/repo@fix-x/a", "base:repo", pathenvLocation{Root: "src/repo@fix-x", Worktree: "fix-x", Prefix: "src/repo"}},
		{"src/plain/x", "base:repos", pathenvLocation{Root: "src", Prefix: "src"}},
		{"src/gitfile/x", "base:repos", pathenvLocation{Root: "src/gitfile", Prefix: "src"}},
		// The checkout is above the prefix: not used.
		{"src/above/sub/x", "base:sub", pathenvLocation{Root: "src/above/sub", Prefix: "src/above/sub"}},
	} {
		m, err := set.match(filepath.Join(home, tt.dir))
		if err != nil || m == nil {
			t.Errorf("match(%s) = %v, %v", tt.dir, m, err)
			continue
		}
		want := pathenvLocation{
			Root:     filepath.Join(home, tt.loc.Root),
			Worktree: tt.loc.Worktree,
			Prefix:   filepath.Join(home, tt.loc.Prefix),
		}
		if m.file.name != tt.name || m.loc != want {
			t.Errorf("match(%s) = %s %+v, want %s %+v", tt.dir, m.file.name, m.loc, tt.name, want)
		}
	}
}

func TestPathenvEntries(t *testing.T) {
	t.Parallel()
	repos, home := pathenvFixture(t, map[string]string{
		"base/dotfiles.toml": pathenvRepoConfig + "[default_vars]\nwho = 'base'\n",
		"base/pathenv/venv.include.toml": `condition = "shell == 'bash' && vars.venv != ''"
[vars]
venv = ''
kind = 'default'
[[run]]
src = 'workon {{shellQuote .vars.venv}} # {{.vars.kind}} {{.vars.who}}'
`,
		"base/pathenv/outer.include.toml": `[vars]
kind = 'outer'
[[run]]
src = 'outer {{.root}}'
on = ['activate', 'refresh']
[[run]]
include = 'venv'
`,
		"base/pathenv/off.include.toml": "condition = 'false'\n[[run]]\nsrc = 'never'\n",
		"work/dotfiles.toml":            pathenvRepoConfig + "[default_vars]\nwho = 'work'\n",
		"work/pathenv/conf.toml": `paths = ['src/conf/']
[vars]
venv = 'my env'
kind = 'conf'
[[run]]
src = 'first {{.vars.who}} {{.prefix}} {{.worktree}}'
[[run]]
include = 'outer'
[[run]]
include = 'off'
[[run]]
src = 'skipped'
condition = "shell == 'fish'"
[[run]]
file = 'script.sh'
on = 'refresh'
[[run]]
src = '{{if expr "root == prefix"}}same{{end}}'
`,
		"work/pathenv/plain.toml": "paths = ['src/plain/']\n[[run]]\ninclude = 'venv'\n",
	}, "src/conf/x", "src/plain")
	set := loadTestPathenv(t, repos, home)

	m, err := set.match(filepath.Join(home, "src/conf/x"))
	if err != nil || m == nil {
		t.Fatalf("match = %v, %v", m, err)
	}
	entries, includes, err := m.entries()
	if err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(home, "src/conf")
	script := filepath.Join(repos, "work/pathenv/script.sh")
	want := []pathenvEntry{
		{[]string{"activate"}, "first work " + conf + " ", "work:conf [[run]] 1"},
		{[]string{"activate", "refresh"}, "outer " + conf, "base:outer [[run]] 1, from work:conf [[run]] 2"},
		// The configuration's vars win over the include files', and the
		// include file sees its own repository's [default_vars].
		{[]string{"activate"}, "workon 'my env' # conf base", "base:venv [[run]] 1, from base:outer [[run]] 2, from work:conf [[run]] 2"},
		{[]string{"refresh"}, ". " + script, "work:conf [[run]] 5"},
		{[]string{"activate"}, "same", "work:conf [[run]] 6"},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries:\n%+v\nwant\n%+v", entries, want)
	}
	for i := range want {
		if !slices.Equal(entries[i].on, want[i].on) || entries[i].code != want[i].code || entries[i].origin != want[i].origin {
			t.Errorf("entry %d:\n%+v\nwant\n%+v", i, entries[i], want[i])
		}
	}
	var names []string
	for _, f := range includes {
		names = append(names, f.name)
	}
	if !slices.Equal(names, []string{"base:outer", "base:venv"}) {
		t.Errorf("includes %v, want base:outer base:venv", names)
	}

	// Without a venv, the include's own default makes its condition false.
	m, err = set.match(filepath.Join(home, "src/plain"))
	if err != nil || m == nil {
		t.Fatalf("match = %v, %v", m, err)
	}
	if entries, includes, err = m.entries(); err != nil || len(entries) != 0 || len(includes) != 0 {
		t.Errorf("entries %+v, includes %v, err %v; want none", entries, includes, err)
	}
}

func TestPathenvEntriesError(t *testing.T) {
	t.Parallel()
	repos, home := pathenvFixture(t, map[string]string{
		"base/dotfiles.toml":  pathenvRepoConfig,
		"base/pathenv/a.toml": "paths = ['a/']\n[[run]]\nsrc = '{{.vars.missing}}'\n",
	}, "a")
	set := loadTestPathenv(t, repos, home)
	m, err := set.match(filepath.Join(home, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.entries(); err == nil || !strings.Contains(err.Error(), "a.toml") {
		t.Errorf("entries: error %v, want one about a.toml", err)
	}
}

func TestPlanPathenv(t *testing.T) {
	t.Parallel()
	entries := []pathenvEntry{
		{on: []string{"activate"}, code: "a"},
		{on: []string{"refresh"}, code: "r"},
		{on: []string{"activate", "refresh"}, code: "b"},
	}
	codes := func(p pathenvPlan) string {
		var s []string
		for _, e := range p.entries {
			s = append(s, e.code)
		}
		return strings.Join(s, " ")
	}
	for _, tt := range []struct {
		name    string
		active  []string
		reload  bool
		event   string
		codes   string
		onTopOf []string
		after   []string
	}{
		{"new shell", nil, false, "activate", "a b", nil, []string{"x"}},
		{"active", []string{"y", "x"}, false, "refresh", "r b", nil, nil},
		{"on top", []string{"y", "z"}, false, "activate", "a b", []string{"y", "z"}, []string{"y", "z", "x"}},
		{"reload active", []string{"x", "y"}, true, "activate", "a b", nil, []string{"x", "y"}},
		{"reload new", []string{"y"}, true, "activate", "a b", []string{"y"}, []string{"y", "x"}},
	} {
		p := planPathenv("x", entries, tt.active, tt.reload)
		if p.event != tt.event || codes(p) != tt.codes || !slices.Equal(p.onTopOf, tt.onTopOf) || !slices.Equal(p.active, tt.after) {
			t.Errorf("%s: plan %s %q on top of %v, active %v; want %s %q on top of %v, active %v",
				tt.name, p.event, codes(p), p.onTopOf, p.active, tt.event, tt.codes, tt.onTopOf, tt.after)
		}
	}
}

func TestRenderPathenvBash(t *testing.T) {
	t.Parallel()
	p := pathenvPlan{
		name:  "work:shop",
		event: "activate",
		entries: []pathenvEntry{
			{code: "op whoami ||\n  op signin\n", origin: "work:shop [[run]] 1"},
			{code: `eval "$(direnv export bash)"`, origin: "base:direnv [[run]] 1, from work:shop [[run]] 2"},
		},
		onTopOf: []string{"work:api"},
		active:  []string{"work:api", "work:shop"},
	}
	got, err := renderPathenvBash(p)
	if err != nil {
		t.Fatal(err)
	}
	want := `echo 'pathenv: work:shop activated on top of work:api' >&2
# work:shop activate
# work:shop [[run]] 1
op whoami ||
  op signin
# base:direnv [[run]] 1, from work:shop [[run]] 2
eval "$(direnv export bash)"
export PATHENV_ACTIVE='work:api work:shop'
`
	if got != want {
		t.Errorf("code:\n%s\nwant\n%s", got, want)
	}
}

// pathenvCommandFiles are the files of the command tests.
var pathenvCommandFiles = map[string]string{
	"base/dotfiles.toml":               pathenvRepoConfig,
	"base/pathenv/direnv.include.toml": "[[run]]\non = ['activate', 'refresh']\nsrc = 'export DIRENV=\"$PWD\"'\n",
	"work/dotfiles.toml":               pathenvRepoConfig,
	"work/pathenv/api.toml":            "paths = ['src/api/']\n[[run]]\nsrc = 'export SECRET=s3cret'\n[[run]]\ninclude = 'direnv'\n",
	"work/pathenv/broken.toml":         "paths = ['src/broken/']\n[[run]]\nsrc = 1\n",
	"work/pathenv/bad-paths.toml":      "paths = ['{{.vars.nope}}/']\n",
	"work/pathenv/broken-match.toml":   "paths = ['src/broken-match/']\n[[run]]\ninclude = 'nope'\n",
}

func TestPathenvCommand(t *testing.T) {
	repos, home := pathenvFixture(t, pathenvCommandFiles, "src/api/sub", "src/broken-match", "elsewhere")
	api := filepath.Join(home, "src/api")

	// The code of the configuration of the directory. Broken files that do
	// not match are reported, and the rest applies.
	t.Setenv("PATHENV_ACTIVE", "")
	stdout, stderr, err := runPathenv(repos, home, "-shell", "bash", "-C", filepath.Join(api, "sub"))
	if err != nil || !strings.HasPrefix(stdout, "# work:api activate\n") || !strings.Contains(stdout, "export SECRET=s3cret\n") {
		t.Errorf("pathenv: %v\n%s", err, stdout)
	}
	if !strings.Contains(stderr, "broken.toml") || !strings.Contains(stderr, "bad-paths.toml") {
		t.Errorf("stderr %q, want the errors of broken.toml and bad-paths.toml", stderr)
	}

	// Active in PATHENV_ACTIVE: a refresh, unless reloaded.
	t.Setenv("PATHENV_ACTIVE", "work:api")
	stdout, _, err = runPathenv(repos, home, "-shell", "bash", "-C", api)
	if err != nil || !strings.HasPrefix(stdout, "# work:api refresh\n") {
		t.Errorf("refresh: %v\n%s", err, stdout)
	}
	stdout, _, err = runPathenv(repos, home, "-shell", "bash", "-C", api, "reload")
	if err != nil || !strings.HasPrefix(stdout, "# work:api activate\n") {
		t.Errorf("reload: %v\n%s", err, stdout)
	}

	// No match.
	elsewhere := filepath.Join(home, "elsewhere")
	stdout, stderr, err = runPathenv(repos, home, "-shell", "bash", "-C", elsewhere)
	if err != nil || stdout != "" || !strings.Contains(stderr, "pathenv: no configuration for "+elsewhere+"\n") {
		t.Errorf("no match: %v, stdout %q, stderr %q", err, stdout, stderr)
	}
	_, stderr, err = runPathenv(repos, home, "-shell", "bash", "-q", "-C", elsewhere)
	if err != nil || strings.Contains(stderr, "no configuration") {
		t.Errorf("no match -q: %v, stderr %q", err, stderr)
	}

	// The broken configuration that matches: an error, no code.
	stdout, stderr, err = runPathenv(repos, home, "-shell", "bash", "-C", filepath.Join(home, "src/broken-match"))
	if err == nil || stdout != "" || !strings.Contains(stderr, "work:broken-match matches") {
		t.Errorf("broken match: %v, stdout %q, stderr %q", err, stdout, stderr)
	}

	_, stderr, err = runPathenv(repos, home, "-shell", "fish", "-C", api)
	if err == nil || !strings.Contains(stderr, `shell "fish" is not supported`) {
		t.Errorf("fish: %v, stderr %q", err, stderr)
	}
}

func TestPathenvStatus(t *testing.T) {
	repos, home := pathenvFixture(t, pathenvCommandFiles, "src/api/sub", "elsewhere")
	t.Setenv("PATHENV_ACTIVE", "work:other work:api")
	stdout, stderr, err := runPathenv(repos, home, "-shell", "bash", "status", "-C", filepath.Join(home, "src/api/sub"), "-json")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, stderr)
	}
	var st pathenvStatusJSON
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	dep := filepath.Join(home, "src/api")
	if st.Configuration == nil || st.Configuration.Name != "work:api" || !st.IsActive ||
		len(st.Includes) != 1 || st.Includes[0].Name != "base:direnv" ||
		st.Location == nil || st.Location.Prefix != dep || st.Location.Root != dep ||
		!slices.Equal(st.Active, []string{"work:other", "work:api"}) {
		t.Errorf("status %s", stdout)
	}

	stdout, _, err = runPathenv(repos, home, "-shell", "bash", "status", "-C", filepath.Join(home, "elsewhere"))
	if err != nil || !strings.HasPrefix(stdout, "configuration: none for ") {
		t.Errorf("status without a match: %v\n%s", err, stdout)
	}
}

// TestPathenvExec runs dotfiles pathenv exec as a program, since on Unix
// it replaces the process that runs it.
func TestPathenvExec(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	files := maps.Clone(pathenvCommandFiles)
	files["work/pathenv/greet.toml"] = "paths = ['src/greet/']\n[[run]]\nsrc = 'greet'\n"
	repos, home := pathenvFixture(t, files, "src/api/sub", "src/broken-match", "src/greet")
	// ~/.bashrc: its non-interactive part defines a function, then it
	// returns, as bash/init does.
	bashrc := "greet() { export GREETING=hello; }\necho \"bashrc skip=$PATHENV_SKIP\" >&2\n[ -z \"$PS1\" ] && return\necho interactive\n"
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(bashrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATHENV_TEST_MAIN", "1")
	t.Setenv("PATHENV_ACTIVE", "")
	t.Setenv("PATHENV_SKIP", "")
	os.Unsetenv("PATHENV_SKIP")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// run runs dotfiles pathenv exec -C dir with args and stdin, and
	// returns its process ID, output and exit status.
	run := func(dir, stdin string, args ...string) (pid int, stdout, stderr string, status int) {
		t.Helper()
		cmd := exec.Command(exe, append([]string{"-repos", repos, "-dst", home, "pathenv", "exec", "-C", dir}, args...)...)
		cmd.Stdin = strings.NewReader(stdin)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			status = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return cmd.Process.Pid, out.String(), errb.String(), status
	}

	sub := filepath.Join(home, "src/api/sub")
	pid, stdout, stderr, status := run(sub, "", "--", "bash", "-c",
		`echo "$$ $PWD $SECRET $DIRENV $PATHENV_ACTIVE ${PATHENV_SKIP-unset}"; exit 3`)
	if status != 3 {
		t.Errorf("exec: exit status %d, want 3\n%s", status, stderr)
	}
	// On Unix the command has the process of dotfiles.
	wantPID := "[0-9]+"
	if runtime.GOOS != "windows" {
		wantPID = strconv.Itoa(pid)
	}
	if want := regexp.MustCompile("^" + wantPID + " " + regexp.QuoteMeta(sub+" s3cret "+sub+" work:api unset") + "\n$"); !want.MatchString(stdout) {
		t.Errorf("exec stdout %q, want %s", stdout, want)
	}
	if !strings.Contains(stderr, "bashrc skip=1\n") || strings.Contains(stdout+stderr, "interactive") {
		t.Errorf("exec stderr %q: want ~/.bashrc sourced with PATHENV_SKIP=1, up to its return point", stderr)
	}

	// Without a command: nothing, not even bash reading stdin.
	_, stdout, stderr, status = run(sub, `echo ran; exit 4`)
	if status != 0 || stdout != "" || stderr != "" {
		t.Errorf("exec without a command: exit status %d, stdout %q, stderr %q; want nothing", status, stdout, stderr)
	}

	// Entries can use the functions of ~/.bashrc.
	_, stdout, _, status = run(filepath.Join(home, "src/greet"), "", "--", "printenv", "GREETING")
	if status != 0 || stdout != "hello\n" {
		t.Errorf("exec in greet: exit status %d, stdout %q; want hello", status, stdout)
	}

	// Nothing matches: the command still runs.
	_, stdout, _, status = run(home, "", "--", "pwd")
	if status != 0 || stdout != home+"\n" {
		t.Errorf("exec without a match: exit status %d, stdout %q", status, stdout)
	}

	// A broken configuration: the command doesn't run.
	_, stdout, _, status = run(filepath.Join(home, "src/broken-match"), "", "--", "echo", "ran")
	if status == 0 || strings.Contains(stdout, "ran") {
		t.Errorf("exec in a broken configuration: exit status %d, stdout %q; want a failure and nothing run", status, stdout)
	}
}
