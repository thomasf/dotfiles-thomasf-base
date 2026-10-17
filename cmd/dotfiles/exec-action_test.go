package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecAction(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()

	action := &ExecAction{
		SrcRoot: repoDir,
		Name:    "touch",
		Args:    []string{"touched.txt"},
	}

	if err := action.Run(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(repoDir, "touched.txt")); os.IsNotExist(err) {
		t.Error("file was not touched")
	}
}
