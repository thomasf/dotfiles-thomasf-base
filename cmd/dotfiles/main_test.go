package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSync(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"bashrc":          &fstest.MapFile{Data: []byte("bashrc content"), Mode: 0o644},
		"my_bin":          &fstest.MapFile{Data: []byte("my_bin content"), Mode: 0o755},
		"config/awesome":  &fstest.MapFile{Data: []byte("awesome content"), Mode: 0o644},
		"docs/readme.txt": &fstest.MapFile{Data: []byte("docs content"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
ignore = ["ignored_file"]

[[mount]]
src = "config/*"
dst = ".config"

[[mount]]
src = "docs"
dst = ".local/share/my-docs"

[[mount]]
src = "my_bin"
dst = "my_bin"
`), Mode: 0o644},
		"ignored_file": &fstest.MapFile{Data: []byte("ignore me"), Mode: 0o644},
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

	for _, action := range actions {
		fmt.Println(action.String())
		if err := action.Run(); err != nil {
			t.Fatal(err)
		}
	}

	checkLink := func(link, expectedTarget string) {
		t.Helper()
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("expected link at %s: %v", link, err)
			return
		}
		absExpected, _ := filepath.Abs(expectedTarget)
		if target != absExpected {
			t.Errorf("expected link at %s to point to %s, got %s", link, absExpected, target)
		}
	}

	checkLink(filepath.Join(dstDir, ".local", "share", "my-docs"), filepath.Join(repoDir, "docs"))
	checkLink(filepath.Join(dstDir, ".config", "awesome"), filepath.Join(repoDir, "config", "awesome"))
	checkLink(filepath.Join(dstDir, ".bashrc"), filepath.Join(repoDir, "bashrc"))
	checkLink(filepath.Join(dstDir, "my_bin"), filepath.Join(repoDir, "my_bin"))

	if _, err := os.Lstat(filepath.Join(dstDir, ".ignored_file")); !os.IsNotExist(err) {
		t.Errorf("ignored_file should not have been synced")
	}
}

func TestForceSync(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(""), Mode: 0o644},
		"file":           &fstest.MapFile{Data: []byte("content"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	srcFile := filepath.Join(repoDir, "file")
	dstFile := filepath.Join(dstDir, ".file")
	err := os.WriteFile(dstFile, []byte("existing content"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewRepository("testrepo", repoDir, false, dstDir)
	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	hasError := false
	for _, action := range actions {
		if err := action.Run(); err != nil {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Error("expected error when target exists and force=false")
	}

	repo.force = true
	actions, err = repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	for _, action := range actions {
		if err := action.Run(); err != nil {
			t.Fatal(err)
		}
	}

	target, _ := os.Readlink(dstFile)
	absSrc, _ := filepath.Abs(srcFile)
	if target != absSrc {
		t.Errorf("expected link to %s, got %s", absSrc, target)
	}
}

func TestGoInstallAction(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		".dotfiles.toml": &fstest.MapFile{Data: []byte(""), Mode: 0o644},
		"go.mod":         &fstest.MapFile{Data: []byte("module test"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository("testrepo", repoDir, false, dstDir)
	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	if len(actions) == 0 {
		t.Fatal("expected at least one action")
	}

	if _, ok := actions[0].(*GoInstallAction); !ok {
		t.Errorf("expected first action to be GoInstallAction, got %T", actions[0])
	}
}

func TestHasChanges(t *testing.T) {
	repoDir := t.TempDir()

	_, err := hasChanges(repoDir)
	if err == nil {
		t.Error("expected error for non-git repo")
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")

	changed, err := hasChanges(repoDir)
	if err != nil {
		t.Fatalf("hasChanges failed: %v", err)
	}
	if changed {
		t.Error("expected no changes in new repo")
	}

	err = os.WriteFile(filepath.Join(repoDir, "file"), []byte("content"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = hasChanges(repoDir)
	if err != nil {
		t.Fatalf("hasChanges failed: %v", err)
	}
	if !changed {
		t.Error("expected changes with untracked file")
	}

	run("add", "file")
	run("commit", "-m", "initial")
	changed, err = hasChanges(repoDir)
	if err != nil {
		t.Fatalf("hasChanges failed: %v", err)
	}
	if changed {
		t.Error("expected no changes after commit")
	}

	err = os.WriteFile(filepath.Join(repoDir, "file"), []byte("modified"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = hasChanges(repoDir)
	if err != nil {
		t.Fatalf("hasChanges failed: %v", err)
	}
	if !changed {
		t.Error("expected changes after modification")
	}
}

func TestHasDiff(t *testing.T) {
	repoDir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")

	diff, err := hasDiff(repoDir)
	if err != nil {
		t.Fatalf("hasDiff failed: %v", err)
	}
	if diff {
		t.Error("expected no diff in new repo")
	}

	err = os.WriteFile(filepath.Join(repoDir, "file"), []byte("content"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	diff, err = hasDiff(repoDir)
	if err != nil {
		t.Fatalf("hasDiff failed: %v", err)
	}
	if diff {
		t.Error("expected no diff with untracked file")
	}

	run("add", "file")
	diff, err = hasDiff(repoDir)
	if err != nil {
		t.Fatalf("hasDiff failed: %v", err)
	}
	if diff {
		t.Error("expected no diff with staged-only file")
	}

	err = os.WriteFile(filepath.Join(repoDir, "file"), []byte("modified"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	diff, err = hasDiff(repoDir)
	if err != nil {
		t.Fatalf("hasDiff failed: %v", err)
	}
	if !diff {
		t.Error("expected diff with unstaged modification")
	}
}

func TestDotfilesRun(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"src/dotfiles/testrepo/.dotfiles.toml": &fstest.MapFile{Data: []byte(""), Mode: 0o644},
		"src/dotfiles/testrepo/bashrc":         &fstest.MapFile{Data: []byte("content"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	d := &Dotfiles{}
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}

	args := []string{"-repos", filepath.Join(repoDir, "src", "dotfiles"), "-dst", dstDir, "plan"}
	if err := d.Run(stdout, stderr, args); err != nil {
		t.Fatalf("Dotfiles.Run failed: %v\nStderr: %s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "plan: symlink testrepo/bashrc") {
		t.Errorf("expected plan output, got: %s", out)
	}
}
