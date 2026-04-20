package main

import (
	"os"
	"runtime"
	"testing"
	"testing/fstest"
)

func TestSyncMounts(t *testing.T) {
	t.Parallel()
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

func TestGoInstall(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"cmd/myapp/main.go":   &fstest.MapFile{Data: []byte("package main"), Mode: 0o644},
		"cmd/skipped/main.go": &fstest.MapFile{Data: []byte("package main"), Mode: 0o644},
		"other/prog/main.go":  &fstest.MapFile{Data: []byte("package main"), Mode: 0o644},
		"dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[go]]
src = "cmd/myapp"

[[go]]
src = "other/prog"
`), Mode: 0o644},
	}

	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}

	actions := repo.GoInstall()
	if len(actions) != 3 {
		t.Errorf("expected 3 actions, got %d", len(actions))
	}

	var foundMyApp, foundOtherProg, foundSkipped bool
	for _, a := range actions {
		ga, ok := a.(*GoInstallAction)
		if !ok {
			continue
		}
		switch ga.Path {
		case "cmd/myapp":
			foundMyApp = true
		case "other/prog":
			foundOtherProg = true
		case "cmd/skipped":
			foundSkipped = true
		}
	}

	if !foundMyApp {
		t.Error("expected to find go install cmd/myapp")
	}
	if !foundOtherProg {
		t.Error("expected to find go install other/prog")
	}
	if !foundSkipped {
		t.Error("expected to find go install cmd/skipped")
	}
}

func TestGoInstallDefault(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"cmd/myapp/main.go": &fstest.MapFile{Data: []byte("package main"), Mode: 0o644},
		"dotfiles.toml":     &fstest.MapFile{Data: []byte(``), Mode: 0o644},
	}

	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}

	actions := repo.GoInstall()
	if len(actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(actions))
	}

	ga, ok := actions[0].(*GoInstallAction)
	if !ok || ga.Path != "cmd/myapp" {
		t.Errorf("expected GoInstallAction with Path 'cmd/myapp', got %+v", actions[0])
	}
}

func TestGoInstallCondition(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"cmd/linux-only/main.go":   &fstest.MapFile{Data: []byte("package main"), Mode: 0o644},
		"cmd/windows-only/main.go": &fstest.MapFile{Data: []byte("package main"), Mode: 0o644},
		"dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[go]]
src = "cmd/linux-only"
condition = "os == 'linux'"

[[go]]
src = "cmd/windows-only"
condition = "os == 'windows'"
`), Mode: 0o644},
	}

	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}

	actions := repo.GoInstall()

	for _, a := range actions {
		ga := a.(*GoInstallAction)
		if ga.Path == "cmd/linux-only" && runtime.GOOS != "linux" {
			t.Errorf("got linux-only on %s", runtime.GOOS)
		}
		if ga.Path == "cmd/windows-only" && runtime.GOOS != "windows" {
			t.Errorf("got windows-only on %s", runtime.GOOS)
		}
	}
}
