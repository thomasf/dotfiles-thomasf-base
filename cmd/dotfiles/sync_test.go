package main

import (
	"os"
	"testing"
	"testing/fstest"
)

func TestSyncMounts(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".config-base/flake8": &fstest.MapFile{Data: []byte("flake8 content"), Mode: 0o644},
		".config2/file2":      &fstest.MapFile{Data: []byte("file2 content"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[mount]]
src = ".config-base/flake8"
dst = ".config/flake8"

[[mount]]
src = ".config2"
dst = "second-config"
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

	var foundConfigBase, foundFlake8, foundSecondConfig, foundConfig2 bool

	for _, a := range actions {
		s, ok := a.(*SymLinker)
		if !ok {
			continue
		}
		switch {
		case s.Src == ".config-base" && s.Dst == ".config-base":
			foundConfigBase = true
		case s.Src == ".config-base/flake8" && s.Dst == ".config/flake8":
			foundFlake8 = true
		case s.Src == ".config2" && s.Dst == ".config2":
			foundConfig2 = true
		case s.Src == ".config2" && s.Dst == "second-config":
			foundSecondConfig = true
		}
	}

	if !foundFlake8 {
		t.Error(".config-base/flake8 -> .config/flake8")
	}
	if !foundConfigBase {
		t.Error(".config-base -> .config-base")
	}
	if !foundSecondConfig {
		t.Error(".config2 -> second-config")
	}
	if foundConfig2 {
		t.Error(".config2 -> NOTHING")
	}
}
