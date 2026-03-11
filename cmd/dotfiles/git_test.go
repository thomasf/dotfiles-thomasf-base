package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestGitConfig(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "testrepo")
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[git]
"user.name" = "Test User"
"user.email" = "test@example.com"
"core.editor" = "emacs"
"advice.detachedHead" = ""
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

	expectedActionString := "[testrepo] git config: set 3, unset 1"

	found := false
	for _, action := range actions {
		if _, ok := action.(*GitConfigAction); ok {
			found = true
			if action.String() != expectedActionString {
				t.Errorf("Expected action string:\n%s\nGot:\n%s", expectedActionString, action.String())
			}
		}
	}

	if !found {
		t.Error("Missing git config action")
	}
}
