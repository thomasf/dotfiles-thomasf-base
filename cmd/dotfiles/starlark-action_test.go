package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// starlarkRepo writes a repository with the config file config and the
// other files, and a destination directory with dstFiles, and returns the
// loaded repository.
func starlarkRepo(t *testing.T, config string, files, dstFiles map[string]string) *Repository {
	t.Helper()
	repoDir, dstDir := t.TempDir(), t.TempDir()
	mfs := fstest.MapFS{"dotfiles.toml": {Data: []byte(config), Mode: 0o644}}
	for name, data := range files {
		mfs[name] = &fstest.MapFile{Data: []byte(data), Mode: 0o644}
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}
	dfs := fstest.MapFS{}
	for name, data := range dstFiles {
		dfs[name] = &fstest.MapFile{Data: []byte(data), Mode: 0o600}
	}
	if err := os.CopyFS(dstDir, dfs); err != nil {
		t.Fatal(err)
	}
	for name := range dstFiles {
		if err := os.Chmod(filepath.Join(dstDir, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	return repo
}

// runStarlark runs the starlark actions of repo and returns their errors.
func runStarlark(t *testing.T, repo *Repository) []error {
	t.Helper()
	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}
	var errs []error
	for _, a := range actions {
		if ta, ok := a.(*StarlarkAction); ok {
			if err := ta.Run(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

func readDst(t *testing.T, repo *Repository, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo.dstRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStarlarkInPlace(t *testing.T) {
	t.Parallel()
	repo := starlarkRepo(t, `
[[starlark]]
dst = '.claude/settings.json'
src = '''
merge(doc, {"env": {"DISABLE_TELEMETRY": "1", "OLD": None}, "attribution": {"commit": ""}})
allow = doc.setdefault("permissions", {}).setdefault("allow", [])
for rule in ["Bash(go test:*)"]:
    if rule not in allow:
        allow.append(rule)
'''
`, nil, map[string]string{
		".claude/settings.json": `{"model": "opus", "env": {"OLD": "x", "KEEP": "1"}}`,
	})
	if errs := runStarlark(t, repo); len(errs) > 0 {
		t.Fatal(errs)
	}
	want := `{
  "attribution": {
    "commit": ""
  },
  "env": {
    "DISABLE_TELEMETRY": "1",
    "KEEP": "1"
  },
  "model": "opus",
  "permissions": {
    "allow": [
      "Bash(go test:*)"
    ]
  }
}
`
	if got := readDst(t, repo, ".claude/settings.json"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	path := filepath.Join(repo.dstRoot, ".claude/settings.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %v", info.Mode().Perm())
	}

	// A file that only differs in formatting is not rewritten.
	reformatted := `{"permissions": {"allow": ["Bash(go test:*)"]}, "attribution": {"commit": ""}, "env": {"DISABLE_TELEMETRY": "1", "KEEP": "1"}, "model": "opus"}`
	if err := os.WriteFile(path, []byte(reformatted), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if errs := runStarlark(t, repo); len(errs) > 0 {
		t.Fatal(errs)
	}
	if got := readDst(t, repo, ".claude/settings.json"); got != reformatted {
		t.Errorf("reformatted file was rewritten:\n%s", got)
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) {
		t.Errorf("reformatted file was touched: %v %v", info.ModTime(), err)
	}
}

func TestStarlarkMissing(t *testing.T) {
	t.Parallel()
	repo := starlarkRepo(t, `
[[starlark]]
dst = 'skip.json'
src = 'doc["a"] = 1'

[[starlark]]
dst = 'create.toml'
missing = 'create'
mode = '0640'
src = 'doc["a"] = 1'

[[starlark]]
dst = 'error.json'
missing = 'error'
src = 'doc["a"] = 1'
`, nil, nil)
	errs := runStarlark(t, repo)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "error.json does not exist") {
		t.Errorf("got errors %v, want one for error.json", errs)
	}
	if _, err := os.Stat(filepath.Join(repo.dstRoot, "skip.json")); !os.IsNotExist(err) {
		t.Errorf("skip.json: %v", err)
	}
	if got := readDst(t, repo, "create.toml"); got != "a = 1\n" {
		t.Errorf("create.toml: %q", got)
	}
	if info, err := os.Stat(filepath.Join(repo.dstRoot, "create.toml")); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("create.toml mode: %v %v", info.Mode(), err)
	}
}

func TestStarlarkGenerate(t *testing.T) {
	t.Parallel()
	repo := starlarkRepo(t, `
[vars]
size = '11'

[[ignore]]
match = ['data']

[[starlark]]
dst = '.config/{{.os}}.toml'
missing = 'create'
src = '''
doc.clear()
merge(doc, read("data/common.toml"))
merge(doc, read("data/host." + hostname + ".toml", default = {}))
doc["font"]["size"] = int(vars["size"])
doc["os"] = os
doc["dst"] = dst
'''
`, map[string]string{
		"data/common.toml": "[font]\nfamily = 'Iosevka'\nsize = 12\n",
	}, map[string]string{
		".config/" + runtime.GOOS + ".toml": "stale = true\n",
	})
	if errs := runStarlark(t, repo); len(errs) > 0 {
		t.Fatal(errs)
	}
	want := "dst = '.config/" + runtime.GOOS + ".toml'\nos = '" + runtime.GOOS + "'\n\n[font]\nfamily = 'Iosevka'\nsize = 11\n"
	if got := readDst(t, repo, ".config/"+runtime.GOOS+".toml"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestStarlarkGlob(t *testing.T) {
	t.Parallel()
	repo := starlarkRepo(t, `
[[starlark]]
dst = 'apps/*/settings.json'
src = 'doc["telemetry"] = "off"'

[[starlark]]
dst = 'none/*.json'
missing = 'error'
src = 'pass'
`, nil, map[string]string{
		"apps/a/settings.json": "{}",
		"apps/b/settings.json": `{"x": 1}`,
	})
	errs := runStarlark(t, repo)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "no files match") {
		t.Errorf("got errors %v, want one for none/*.json", errs)
	}
	if got := readDst(t, repo, "apps/a/settings.json"); got != "{\n  \"telemetry\": \"off\"\n}\n" {
		t.Errorf("apps/a: %q", got)
	}
	if got := readDst(t, repo, "apps/b/settings.json"); got != "{\n  \"telemetry\": \"off\",\n  \"x\": 1\n}\n" {
		t.Errorf("apps/b: %q", got)
	}
}

func TestStarlarkErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		dst     string
		wantErr string
	}{
		{name: "fail", src: "x = 1\nfail(\"boom\")", wantErr: "starlark[0].src:2:5: fail: boom"},
		{name: "runtime", src: "doc[\"a\"][\"b\"] = 1", wantErr: "starlark[0].src:1:4: key \"a\" not in dict"},
		{name: "not idempotent", src: "doc.setdefault(\"l\", []).append(1)", wantErr: "not idempotent"},
		{name: "not a dict", src: "doc = []", wantErr: "doc must be a dict"},
		{name: "steps", src: "for i in range(100000000):\n    pass", wantErr: "too many steps"},
		{name: "recursion", src: "def f(n):\n    return f(n - 1) if n else 0\nf(3)", wantErr: "called recursively"},
		{name: "read outside", src: "read(\"../x.json\")", wantErr: "inside the repository"},
		{name: "read missing", src: "read(\"x.json\")", wantErr: "no such file"},
		{name: "read tmpl", src: "read(\"x.json.tmpl\")", wantErr: "unknown format"},
		{name: "toml null", src: "doc[\"a\"] = None", dst: "a.toml", wantErr: "a: TOML has no null"},
		{name: "unknown format", src: "pass", dst: "a.ini", wantErr: "unknown format of a.ini"},
	}
	for _, tt := range tests {
		dst := tt.dst
		if dst == "" {
			dst = "a.json"
		}
		repo := starlarkRepo(t, "[[starlark]]\ndst = '"+dst+"'\nmissing = 'create'\nsrc = '''\n"+tt.src+"\n'''\n", nil, nil)
		errs := runStarlark(t, repo)
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), tt.wantErr) {
			t.Errorf("%s: got errors %v, want %q", tt.name, errs, tt.wantErr)
		}
		if _, err := os.Stat(filepath.Join(repo.dstRoot, dst)); !os.IsNotExist(err) {
			t.Errorf("%s: dst was written", tt.name)
		}
	}
}

func TestStarlarkSymlink(t *testing.T) {
	t.Parallel()
	repo := starlarkRepo(t, "[[starlark]]\ndst = 'link.json'\nsrc = 'doc[\"a\"] = 1'\n", nil, map[string]string{"real.json": "{}"})
	if err := os.Symlink("real.json", filepath.Join(repo.dstRoot, "link.json")); err != nil {
		t.Fatal(err)
	}
	errs := runStarlark(t, repo)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "symlink") {
		t.Errorf("got errors %v, want a symlink error", errs)
	}
}

func TestStarlarkPredeclared(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "tool-a"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("DOTFILES_TRANSFORM_TEST", "from-env")
	hostname, _ := os.Hostname()
	repo := starlarkRepo(t, `
[vars]
gui = 'true'

[[starlark]]
dst = 'out.json'
missing = 'create'
src = '''
doc["os"] = os
doc["arch"] = arch
doc["hostname"] = hostname
doc["vars"] = vars
doc["env"] = env("DOTFILES_TRANSFORM_TEST")
doc["gui"] = parseBool(vars["gui"])
doc["tool"] = lookPath("nope-x", "tool-a")
doc["exists"] = [exists("out.json"), exists("file"), isFile("file"), isDir("dir"), isDir("file")]
'''
`, nil, map[string]string{"file": "", "dir/x": ""})
	if errs := runStarlark(t, repo); len(errs) > 0 {
		t.Fatal(errs)
	}
	doc, err := decodeDocument(formatJSON, []byte(readDst(t, repo, "out.json")))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"arch": "` + runtime.GOARCH + `", "env": "from-env", "exists": [False, True, True, True, False], "gui": True, "hostname": "` +
		hostname + `", "os": "` + runtime.GOOS + `", "tool": "` + filepath.Join(binDir, "tool-a") + `", "vars": {"gui": "true"}}`
	if got := doc.String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
