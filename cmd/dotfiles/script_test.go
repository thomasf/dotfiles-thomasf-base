package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestScriptAction(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()

	outputFile := filepath.Join(repoDir, "output.txt")
	script := "echo 'hello world' > output.txt"

	action := &ScriptAction{
		SrcRoot: repoDir,
		Script:  script,
	}

	err := action.Run()
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}

	if strings.TrimSpace(string(data)) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func TestScriptActionEnv(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()

	outputFile := filepath.Join(repoDir, "env_output.txt")
	script := "FOO=bar; echo $FOO > env_output.txt"

	action := &ScriptAction{
		SrcRoot: repoDir,
		Script:  script,
	}

	err := action.Run()
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}

	if strings.TrimSpace(string(data)) != "bar" {
		t.Errorf("expected 'bar', got %q", string(data))
	}
}

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

func TestShellAction(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()

	action := &ExecCommandAction{
		SrcRoot: repoDir,
		Command: "touch",
		Args:    []string{"touched.txt"},
	}

	if err := action.Run(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(repoDir, "touched.txt")); os.IsNotExist(err) {
		t.Error("file was not touched")
	}
}
