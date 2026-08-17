package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// copyFS materializes a fixture in a temporary directory of its own and
// returns that directory.
func copyFS(t *testing.T, fsys fs.FS) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.CopyFS(dir, fsys); err != nil {
		t.Fatalf("failed to copy fixture: %v", err)
	}
	return dir
}

// chdirFS materializes a fixture in a temporary directory and switches to it
// for the duration of the test.
func chdirFS(t *testing.T, fsys fs.FS) string {
	t.Helper()

	dir := copyFS(t, fsys)
	t.Chdir(dir)
	return dir
}

// assertFS checks that dir holds exactly the files in want, with exactly the
// content given. Files below dir that want does not name are failures too, so
// stray writes cannot slip through.
func assertFS(t *testing.T, dir string, want fstest.MapFS) {
	t.Helper()

	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk %s: %v", dir, err)
	}

	for name, f := range want {
		content, ok := got[name]
		if !ok {
			t.Errorf("missing file %s", name)
			continue
		}
		if content != string(f.Data) {
			t.Errorf("%s =\n%s\nwant\n%s", name, content, f.Data)
		}
	}
	for name, content := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected file %s:\n%s", name, content)
		}
	}
}
