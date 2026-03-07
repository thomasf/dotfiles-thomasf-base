package main

import (
	"os"
	"testing"
	"testing/fstest"
)

func TestGitConfig(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[git]
"user.name" = "Test User"
"user.email" = "test@example.com"
"core.editor" = "vim"
"advice.detachedHead" = ""
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

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	// Verify expected actions
	expectedActions := map[string]string{
		"user.name":           "git config --global user.name Test User",
		"user.email":          "git config --global user.email test@example.com",
		"core.editor":         "git config --global core.editor vim",
		"advice.detachedHead": "git config --global --unset advice.detachedHead",
	}

	foundActions := make(map[string]bool)

	for _, action := range actions {
		if gitAction, ok := action.(*GitConfigAction); ok {
			expected, exists := expectedActions[gitAction.Key]
			if !exists {
				t.Errorf("Unexpected git config action: %s", gitAction.Key)
				continue
			}
			foundActions[gitAction.Key] = true
			if action.String() != expected {
				t.Errorf("Expected action string '%s', got '%s'", expected, action.String())
			}
		}
	}

	for key := range expectedActions {
		if !foundActions[key] {
			t.Errorf("Missing git config action for key: %s", key)
		}
	}
}
