package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWhichCommand(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"repo/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[mount]]
src = "config-tool"
dst = ".config/tool"

[[template]]
src = "mytool.tmpl"
dst = ".mytool"
`), Mode: 0o644},
		"repo/bashrc":                  &fstest.MapFile{Data: []byte("content"), Mode: 0o644},
		"repo/mytool.tmpl":             &fstest.MapFile{Data: []byte("tmpl"), Mode: 0o644},
		"repo/config-tool/config.json": &fstest.MapFile{Data: []byte("{}"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	runWhich := func(args ...string) (string, string, error) {
		d := &Dotfiles{}
		stdout := &strings.Builder{}
		stderr := &strings.Builder{}
		baseArgs := []string{"-repos", repoDir, "-dst", dstDir, "which"}
		err := d.Run(stdout, stderr, append(baseArgs, args...))
		return stdout.String(), stderr.String(), err
	}

	t.Run("destination to source symlink", func(t *testing.T) {
		stdout, _, err := runWhich(filepath.Join(dstDir, ".bashrc"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(repoDir, "repo", "bashrc")
		if strings.TrimSpace(stdout) != expected {
			t.Errorf("got %q, want %q", strings.TrimSpace(stdout), expected)
		}
	})

	t.Run("source to destination symlink", func(t *testing.T) {
		stdout, _, err := runWhich(filepath.Join(repoDir, "repo", "bashrc"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(dstDir, ".bashrc")
		if strings.TrimSpace(stdout) != expected {
			t.Errorf("got %q, want %q", strings.TrimSpace(stdout), expected)
		}
	})

	t.Run("template lookup", func(t *testing.T) {
		stdout, _, err := runWhich(filepath.Join(dstDir, ".mytool"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(repoDir, "repo", "mytool.tmpl")
		if strings.TrimSpace(stdout) != expected {
			t.Errorf("got %q, want %q", strings.TrimSpace(stdout), expected)
		}
	})

	t.Run("directory mount nested file", func(t *testing.T) {
		stdout, _, err := runWhich(filepath.Join(dstDir, ".config", "tool", "config.json"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(repoDir, "repo", "config-tool", "config.json")
		if strings.TrimSpace(stdout) != expected {
			t.Errorf("got %q, want %q", strings.TrimSpace(stdout), expected)
		}
	})

	t.Run("basename lookup", func(t *testing.T) {
		stdout, _, err := runWhich(".bashrc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(repoDir, "repo", "bashrc")
		if strings.TrimSpace(stdout) != expected {
			t.Errorf("got %q, want %q", strings.TrimSpace(stdout), expected)
		}
	})

	t.Run("not managed path fails", func(t *testing.T) {
		_, _, err := runWhich("nonexistent")
		if err == nil {
			t.Fatal("expected error for unmanaged path, got nil")
		}
	})
}
