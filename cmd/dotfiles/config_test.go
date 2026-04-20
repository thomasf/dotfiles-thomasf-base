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
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[ignore]]
match = ["hidden_ignore"]
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Errorf("failed to load hidden config: %v", err)
	}

	if len(repo.config.Ignore) != 1 || repo.config.Ignore[0].Match[0] != "hidden_ignore" {
		t.Errorf("config not loaded correctly from hidden file")
	}
}

func TestLoadConfigNormal(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[ignore]]
match = ["normal_ignore"]
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Errorf("failed to load normal config: %v", err)
	}

	if len(repo.config.Ignore) != 1 || repo.config.Ignore[0].Match[0] != "normal_ignore" {
		t.Errorf("config not loaded correctly from normal file")
	}
}

func TestLoadConfigGo(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[go]]
src = "cmd/myapp"
condition = "os == 'linux'"

[[go]]
src = "cmd/other"
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	err := repo.LoadConfig()
	if err != nil {
		t.Errorf("failed to load config with go sections: %v", err)
	}

	if len(repo.config.Go) != 2 {
		t.Errorf("expected 2 go sections, got %d", len(repo.config.Go))
	}

	if repo.config.Go[0].Src != "cmd/myapp" || repo.config.Go[0].Condition != "os == 'linux'" {
		t.Errorf("first go section not loaded correctly")
	}

	if repo.config.Go[1].Src != "cmd/other" || repo.config.Go[1].Condition != "" {
		t.Errorf("second go section not loaded correctly")
	}
}
