package main

import (
	"os"
	"testing"
	"testing/fstest"
)

func TestLoadConfigHidden(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte("ignore = [\"hidden_ignore\"]"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository("testrepo", repoDir, false, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Errorf("failed to load hidden config: %v", err)
	}

	if len(repo.config.Ignore) != 1 || repo.config.Ignore[0] != "hidden_ignore" {
		t.Errorf("config not loaded correctly from hidden file")
	}
}

func TestLoadConfigNormal(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"dotfiles.toml": &fstest.MapFile{Data: []byte("ignore = [\"normal_ignore\"]"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository("testrepo", repoDir, false, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Errorf("failed to load normal config: %v", err)
	}

	if len(repo.config.Ignore) != 1 || repo.config.Ignore[0] != "normal_ignore" {
		t.Errorf("config not loaded correctly from normal file")
	}
}
