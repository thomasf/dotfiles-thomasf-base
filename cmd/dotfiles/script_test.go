package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestScriptsPrePostRepo(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[script]]
condition = "os == '` + runtime.GOOS + `'"
phase = "pre"
src = "echo pre >> log.txt"

[[script]]
condition = "os != 'not-` + runtime.GOOS + `'"
phase = "post"
src = "echo post >> log.txt"

[[script]]
src = "echo repo >> log.txt"
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	pre := repo.PreScript()
	if len(pre) == 0 {
		t.Fatal("pre scripts should not be empty")
	}
	for _, a := range pre {
		if err := a.Run(); err != nil {
			t.Fatal(err)
		}
	}

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	foundRepoScript := false
	for _, a := range actions {
		if _, ok := a.(*ScriptAction); ok {
			foundRepoScript = true
			if err := a.Run(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !foundRepoScript {
		t.Fatal("repo script action not found in sync actions")
	}

	post := repo.PostScript()
	if len(post) == 0 {
		t.Fatal("post scripts should not be empty")
	}
	for _, a := range post {
		if err := a.Run(); err != nil {
			t.Fatal(err)
		}
	}

	logFile := filepath.Join(repoDir, "log.txt")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}

	expected := "pre\nrepo\npost\n"
	if string(data) != expected {
		t.Errorf("expected log %q, got %q", expected, string(data))
	}
}

func TestScriptConditionFalse(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[script]]
condition = "os == 'not-an-os'"
src = "echo should-not-run >> log.txt"
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	for _, a := range actions {
		if _, ok := a.(*ScriptAction); ok {
			t.Fatal("script action should not have been created for false condition")
		}
	}
}

func TestScriptConditionHostname(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()
	hostname, _ := os.Hostname()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[script]]
condition = "hostname == '` + hostname + `'"
src = "echo hostname-match >> log.txt"
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, a := range actions {
		if _, ok := a.(*ScriptAction); ok {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected script action to be created for matching hostname %q", hostname)
	}
}

func TestScriptTemplate(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	mfs := fstest.MapFS{
		"dotfiles.toml": &fstest.MapFile{Data: []byte(`
[vars]
greeting = 'hello world'

[[script]]
src = '''
echo "{{.vars.greeting}} from {{.os}}" > out.txt
echo '${HOME:+set}' >> out.txt
'''

[[script]]
src = '{{if eq .os "plan9-nope"}}echo never > never.txt{{end}}'
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(repoDir, t.TempDir())
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}
	var scripts int
	for _, a := range actions {
		if _, ok := a.(*ScriptAction); ok {
			scripts++
			if err := a.Run(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if scripts != 1 {
		t.Errorf("got %d script actions, want 1: a script that renders empty is dropped", scripts)
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "hello world from " + runtime.GOOS + "\n${HOME:+set}\n"; string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}

	// A template error is reported when the config is loaded.
	if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte("[[script]]\nsrc = 'echo {{.vars.nope}}'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewRepository(repoDir, t.TempDir()).LoadConfig(); err == nil || !strings.Contains(err.Error(), "script[0].src") {
		t.Errorf("got error %v, want one naming script[0].src", err)
	}
}
