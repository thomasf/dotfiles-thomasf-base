package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestCopyAction(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"file1":          &fstest.MapFile{Data: []byte("file1 content"), Mode: 0o644},
		"dir1/file2":     &fstest.MapFile{Data: []byte("file2 content"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(``), Mode: 0o644},
	}

	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir, WithCopy(true)) // copy = true
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	for _, a := range actions {
		if err := a.Run(); err != nil {
			t.Fatalf("action failed: %v", err)
		}
	}

	// Verify file1 was copied
	file1Path := filepath.Join(dstDir, ".file1")
	data1, err := os.ReadFile(file1Path)
	if err != nil {
		t.Fatalf("failed to read copied file1: %v", err)
	}
	if string(data1) != "file1 content" {
		t.Errorf("file1 content mismatch: got %q, want %q", string(data1), "file1 content")
	}

	// Verify dir1/file2 was copied
	file2Path := filepath.Join(dstDir, ".dir1", "file2")
	data2, err := os.ReadFile(file2Path)
	if err != nil {
		t.Fatalf("failed to read copied file2: %v", err)
	}
	if string(data2) != "file2 content" {
		t.Errorf("file2 content mismatch: got %q, want %q", string(data2), "file2 content")
	}

	// Verify they are NOT symlinks
	info1, err := os.Lstat(file1Path)
	if err != nil {
		t.Fatal(err)
	}
	if info1.Mode()&os.ModeSymlink != 0 {
		t.Error("file1 is a symlink, should be a regular file")
	}

	info2, err := os.Lstat(filepath.Join(dstDir, ".dir1"))
	if err != nil {
		t.Fatal(err)
	}
	if info2.Mode()&os.ModeSymlink != 0 {
		t.Error(".dir1 is a symlink, should be a regular directory")
	}
}

func TestCopyActionRerun(t *testing.T) {
	src := t.TempDir()
	dstRoot := t.TempDir()
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	write(filepath.Join(src, "dir", "a"), "a1")
	write(filepath.Join(src, "dir", "sub", "b"), "b1")
	if err := os.Symlink("a", filepath.Join(src, "dir", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(src, "dir", ".#lock")); err != nil {
		t.Fatal(err)
	}
	a := &CopyAction{SrcRoot: src, Src: "dir", DstRoot: dstRoot, Dst: ".dir"}
	dst := a.DstAbsolutePath()

	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if got := read(filepath.Join(dst, "link")); got != "a1" {
		t.Errorf("link = %q, want the content of a", got)
	}
	if info, err := os.Lstat(filepath.Join(dst, "link")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("link is not copied as a regular file: %v, %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(dst, ".#lock")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("dangling symlink copied: %v", err)
	}

	// Running again, without Force, updates changed files and keeps files
	// that are only in the destination.
	write(filepath.Join(src, "dir", "sub", "b"), "b2")
	write(filepath.Join(dst, "local"), "state")
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if got := read(filepath.Join(dst, "sub", "b")); got != "b2" {
		t.Errorf("sub/b = %q, want b2", got)
	}
	if got := read(filepath.Join(dst, "local")); got != "state" {
		t.Errorf("local = %q, want it kept", got)
	}

	// A file whose mode changed in the source is updated.
	if err := os.Chmod(filepath.Join(src, "dir", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dst, "a")); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("a mode = %v, %v, want 0755", info, err)
	}
}

func TestCopyActionReplacesSymlinkOnlyWithForce(t *testing.T) {
	src := t.TempDir()
	dstRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "dir", "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &CopyAction{SrcRoot: src, Src: "dir", DstRoot: dstRoot, Dst: ".dir"}
	dst := a.DstAbsolutePath()
	// As left by an install without copying.
	if err := os.Symlink(filepath.Join(src, "dir"), dst); err != nil {
		t.Fatal(err)
	}

	if err := a.Run(); err == nil {
		t.Fatal("Run without Force succeeded on a symlink")
	}
	a.Force = true
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(dst); err != nil || !info.IsDir() {
		t.Fatalf("dst is not a directory: %v, %v", info, err)
	}
	if data, err := os.ReadFile(filepath.Join(src, "dir", "a")); err != nil || string(data) != "a" {
		t.Errorf("source changed: %q, %v", data, err)
	}
}
