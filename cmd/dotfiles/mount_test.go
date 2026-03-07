package main

import (
	"os"
	"testing"
	"testing/fstest"
)

func TestMountRoot(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"bashrc": &fstest.MapFile{Data: []byte("content"), Mode: 0o644},
		"zshrc":  &fstest.MapFile{Data: []byte("content"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[mount]]
src = "."
dst = "notes"
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

	if len(actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(actions))
		for i, a := range actions {
			t.Logf("Action %d: %s", i, a.String())
		}
	}
}
