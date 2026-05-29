package main

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMountSingleFile(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"config/my_app_config": &fstest.MapFile{Data: []byte("config content"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[mount]]
src = "config/my_app_config"
dst = ".my_app_config"
`), Mode: 0o644},
	}

	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, a := range actions {
		s := a.String()
		if strings.Contains(s, "config/my_app_config") && strings.Contains(s, ".my_app_config") {
			found = true
			break
		}
	}

	if !found {
		t.Error("missing single file mount action")
	}
}

func TestMountRoot(t *testing.T) {
	t.Parallel()
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

	repo := NewRepository(repoDir, dstDir)
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

func TestMountWildcards(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"config/app1":        &fstest.MapFile{Data: []byte("app1"), Mode: 0o644},
		"config/app2":        &fstest.MapFile{Data: []byte("app2"), Mode: 0o644},
		"config/.hidden":     &fstest.MapFile{Data: []byte("hidden"), Mode: 0o644},
		"config/subdir/file": &fstest.MapFile{Data: []byte("nested"), Mode: 0o644},
		"other/file":         &fstest.MapFile{Data: []byte("other"), Mode: 0o644},
		"ignored/file":       &fstest.MapFile{Data: []byte("ignore"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[ignore]]
match = ["ignored"]

[[mount]]
src = "config/*"
dst = ".config"

[[mount]]
src = "other/*"
dst = "others"
`), Mode: 0o644},
	}

	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	var foundApp1, foundApp2, foundHidden, foundOtherFile, foundOtherRoot, foundIgnored, foundNested bool

	for _, a := range actions {
		s := a.String()
		if strings.Contains(s, "config/app1") && strings.Contains(s, ".config/app1") {
			foundApp1 = true
		}
		if strings.Contains(s, "config/app2") && strings.Contains(s, ".config/app2") {
			foundApp2 = true
		}
		if strings.Contains(s, "config/.hidden") && strings.Contains(s, ".config/.hidden") {
			foundHidden = true
		}
		if strings.Contains(s, "config/subdir") && strings.Contains(s, ".config/subdir") {
			foundNested = true
		}
		if strings.Contains(s, "other/file") && strings.Contains(s, "others/file") {
			foundOtherFile = true
		}
		if strings.Contains(s, "symlink: other -> .other") {
			foundOtherRoot = true
		}
		if strings.Contains(s, "ignored") {
			foundIgnored = true
		}
	}

	if !foundApp1 {
		t.Error("missing config/app1 action")
	}
	if !foundApp2 {
		t.Error("missing config/app2 action")
	}
	if !foundHidden {
		t.Error("missing config/.hidden action")
	}
	if !foundNested {
		t.Error("missing config/subdir action")
	}
	if !foundOtherFile {
		t.Error("missing other/file action")
	}
	if !foundOtherRoot {
		t.Error("expected 'other' to be symlinked to '.other' because its mount destination 'others' is not in the .other/ shadow")
	}
	if foundIgnored {
		t.Error("ignored files should not be in actions")
	}
}
