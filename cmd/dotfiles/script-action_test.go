package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
