package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestScriptAction(t *testing.T) {
	repoDir := t.TempDir()

	outputFile := filepath.Join(repoDir, "output.txt")
	script := "echo 'hello world' > output.txt"

	action := &ScriptAction{
		RepoName: "testrepo",
		RepoPath: repoDir,
		Script:   script,
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
	repoDir := t.TempDir()

	outputFile := filepath.Join(repoDir, "env_output.txt")
	script := "FOO=bar; echo $FOO > env_output.txt"

	action := &ScriptAction{
		RepoName: "testrepo",
		RepoPath: repoDir,
		Script:   script,
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
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
script-pre = "echo pre >> log.txt"
script-post = "echo post >> log.txt"
script = "echo repo >> log.txt"
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository("testrepo", repoDir, false, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	pre := repo.PreScript()
	if pre == nil {
		t.Fatal("pre script should not be nil")
	}
	if err := pre.Run(); err != nil {
		t.Fatal(err)
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
	if post == nil {
		t.Fatal("post script should not be nil")
	}
	if err := post.Run(); err != nil {
		t.Fatal(err)
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

func TestShellAction(t *testing.T) {
	repoDir := t.TempDir()

	action := &ExecCommandAction{
		RepoName: "testrepo",
		RepoPath: repoDir,
		Command:  "touch",
		Args:     []string{"touched.txt"},
	}

	if err := action.Run(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(repoDir, "touched.txt")); os.IsNotExist(err) {
		t.Error("file was not touched")
	}
}
