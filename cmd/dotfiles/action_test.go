package main

import (
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
