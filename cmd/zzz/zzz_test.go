package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupEntries(t *testing.T) {
	tmpDir := t.TempDir()

	existingDir := filepath.Join(tmpDir, "exists")
	if err := os.Mkdir(existingDir, 0755); err != nil {
		t.Fatal(err)
	}

	nonExistingDir := filepath.Join(tmpDir, "notexists")

	dataFile := filepath.Join(tmpDir, "zzz.db")
	store := &Store{Path: dataFile, Stderr: os.Stderr}

	entries := []Entry{
		{Path: existingDir, Rank: 10, Time: 1000},
		{Path: nonExistingDir, Rank: 5, Time: 500},
	}
	if err := store.Save(entries); err != nil {
		t.Fatal(err)
	}

	cleanupEntries(store)

	nextEntries, err := store.Entries()
	if err != nil {
		t.Fatal(err)
	}

	if len(nextEntries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(nextEntries))
	}

	if len(nextEntries) > 0 && nextEntries[0].Path != existingDir {
		t.Errorf("expected entry path %s, got %s", existingDir, nextEntries[0].Path)
	}
}

func TestAddEntry(t *testing.T) {
	tmpDir := t.TempDir()

	dataFile := filepath.Join(tmpDir, "zzz.db")
	store := &Store{Path: dataFile, Stderr: os.Stderr}

	addEntry(store, "/path/to/foo")
	addEntry(store, "/path/to/bar")
	addEntry(store, "/path/to/foo") // Increase rank

	entries, err := store.Entries()
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	for _, e := range entries {
		if e.Path == "/path/to/foo" {
			if e.Rank != 2 {
				t.Errorf("expected rank 2 for foo, got %f", e.Rank)
			}
		} else if e.Path == "/path/to/bar" {
			if e.Rank != 1 {
				t.Errorf("expected rank 1 for bar, got %f", e.Rank)
			}
		}
	}
}

func TestRunSearch(t *testing.T) {
	tmpDir := t.TempDir()

	dataFile := filepath.Join(tmpDir, "zzz.db")
	store := &Store{Path: dataFile, Stderr: os.Stderr}

	now := time.Now().Unix()
	entries := []Entry{
		{Path: "/foo/bar/baz", Rank: 10, Time: now},
		{Path: "/apple/orange", Rank: 5, Time: now},
	}
	if err := store.Save(entries); err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runSearch(store, []string{"foo", "baz"}, false)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if output != "/foo/bar/baz" {
		t.Errorf("expected /foo/bar/baz, got %q", output)
	}
}
