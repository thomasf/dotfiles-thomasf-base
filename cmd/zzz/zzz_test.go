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
	if err := store.SaveEntries(entries); err != nil {
		t.Fatal(err)
	}

	cleanupEntries(store)

	nextEntries, err := store.LoadEntries()
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

func TestAddRemoveEntry(t *testing.T) {
	tmpDir := t.TempDir()

	dataFile := filepath.Join(tmpDir, "zzz.db")
	store := &Store{Path: dataFile, Stderr: os.Stderr}

	addEntry(store, "/path/to/foo")
	addEntry(store, "/path/to/bar")
	addEntry(store, "/path/to/foo") // Increase rank

	{
		entries, err := store.LoadEntries()
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

	removeEntry(store, "/path/to/bar")

	{
		entries, err := store.LoadEntries()
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) != 1 {
			t.Errorf("expected 1 entries, got %d", len(entries))
		}

		for _, e := range entries {
			if e.Path == "/path/to/foo" {
				if e.Rank != 2 {
					t.Errorf("expected rank 2 for foo, got %f", e.Rank)
				}
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
	if err := store.SaveEntries(entries); err != nil {
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

func TestLoadEntriesDeduplication(t *testing.T) {
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "zzz.db")
	store := &Store{Path: dataFile, Stderr: os.Stderr}

	content := `/path/one|10|1000
/path/two|5|2000
/path/one|15|3000
/path/one/|8|4000
`
	if err := os.WriteFile(dataFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := store.LoadEntries()
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 deduplicated entries, got %d", len(entries))
	}

	if entries[0].Path != "/path/one" || entries[0].Rank != 15 || entries[0].Time != 4000 {
		t.Errorf("unexpected entry 0: %+v", entries[0])
	}
	if entries[1].Path != "/path/two" || entries[1].Rank != 5 || entries[1].Time != 2000 {
		t.Errorf("unexpected entry 1: %+v", entries[1])
	}
}

func TestMatchParts(t *testing.T) {
	tests := []struct {
		s          string
		lowerParts []string
		expected   bool
	}{
		{"dummy-project-go", []string{"dummy"}, true},
		{"dummy-project-go", []string{"project", "go"}, true},
		{"dummy-project-go", []string{"dpg"}, true},
		{"dummy-project-go", []string{"d", "p", "g"}, true},
		{"src/dummy-project-go", []string{"dpg"}, true},
		{"src/dummy-project-go", []string{"src", "dpg"}, true},
		{"some-dir/my_project.go", []string{"mpg"}, true},
		{"some-dir/my_project.go", []string{"mpgo"}, false},
		{"some-dir/my_project.go", []string{"dir", "mp"}, true},
		{"short", []string{"s"}, true},
		{"short", []string{"x"}, false},
	}

	for _, tt := range tests {
		result := matchParts(tt.s, tt.lowerParts)
		if result != tt.expected {
			t.Errorf("matchParts(%q, %v) = %v, expected %v", tt.s, tt.lowerParts, result, tt.expected)
		}
	}
}

func TestMatchAcronym(t *testing.T) {
	tests := []struct {
		s        string
		part     string
		expected int
	}{
		{"dummy-project-go", "dpg", 15},
		{"dummy-project-go", "dp", 7},
		{"dummy-project-go", "x", -1},
		{"a/b/c", "abc", 5},
		{"a_b_c", "abc", 5},
		{"a.b.c", "abc", 5},
		{"ab.bc.cd", "abc", 7},
		{"my-dir", "md", 4},
	}

	for _, tt := range tests {
		result := matchAcronym(tt.s, tt.part)
		if result != tt.expected {
			t.Errorf("matchAcronym(%q, %q) = %d, expected %d", tt.s, tt.part, result, tt.expected)
		}
	}
}

func TestRunSearchAcronymRanking(t *testing.T) {
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "zzz.db")
	store := &Store{Path: dataFile, Stderr: os.Stderr}

	now := time.Now().Unix()
	entries := []Entry{
		{Path: "/src/dummy-project-go", Rank: 30, Time: now}, // huge rank
		{Path: "/src/dummy-project", Rank: 10, Time: now},    // smaller rank
	}
	if err := store.SaveEntries(entries); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runSearch(store, []string{"dp"}, false)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if output != "/src/dummy-project" {
		t.Errorf("expected /src/dummy-project, got %q", output)
	}
}
