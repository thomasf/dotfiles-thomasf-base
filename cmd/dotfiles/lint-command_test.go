package main

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLintCommand(t *testing.T) {
	t.Run("valid repo passes lint", func(t *testing.T) {
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
			"repo/mytool.tmpl":             &fstest.MapFile{Data: []byte("val = 1\n"), Mode: 0o644},
			"repo/config-tool/config.json": &fstest.MapFile{Data: []byte("{}"), Mode: 0o644},
		}
		if err := os.CopyFS(repoDir, mfs); err != nil {
			t.Fatal(err)
		}

		d := &Dotfiles{}
		stdout := &strings.Builder{}
		stderr := &strings.Builder{}
		args := []string{"-repos", repoDir, "-dst", dstDir, "lint"}
		if err := d.Run(stdout, stderr, args); err != nil {
			t.Fatalf("expected clean lint, got: %v\nstderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "all 1 repositories ok") {
			t.Errorf("expected ok message, got: %s", stdout.String())
		}
	})

	t.Run("missing mount source fails", func(t *testing.T) {
		repoDir := t.TempDir()
		dstDir := t.TempDir()

		mfs := fstest.MapFS{
			"repo/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[mount]]
src = "missing-src"
dst = ".config/tool"
`), Mode: 0o644},
		}
		if err := os.CopyFS(repoDir, mfs); err != nil {
			t.Fatal(err)
		}

		d := &Dotfiles{}
		stdout := &strings.Builder{}
		stderr := &strings.Builder{}
		args := []string{"-repos", repoDir, "-dst", dstDir, "lint"}
		if err := d.Run(stdout, stderr, args); err == nil {
			t.Fatal("expected lint error for missing mount src, got nil")
		}
		if !strings.Contains(stderr.String(), "source file does not exist") {
			t.Errorf("expected missing source error, got:\n%s", stderr.String())
		}
	})

	t.Run("template syntax error fails", func(t *testing.T) {
		repoDir := t.TempDir()
		dstDir := t.TempDir()

		mfs := fstest.MapFS{
			"repo/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[template]]
src = "broken.tmpl"
dst = ".broken"
`), Mode: 0o644},
			"repo/broken.tmpl": &fstest.MapFile{Data: []byte("{{ if }} unclosed"), Mode: 0o644},
		}
		if err := os.CopyFS(repoDir, mfs); err != nil {
			t.Fatal(err)
		}

		d := &Dotfiles{}
		stdout := &strings.Builder{}
		stderr := &strings.Builder{}
		args := []string{"-repos", repoDir, "-dst", dstDir, "lint"}
		if err := d.Run(stdout, stderr, args); err == nil {
			t.Fatal("expected lint error for template syntax error, got nil")
		}
		if !strings.Contains(stderr.String(), "syntax error") {
			t.Errorf("expected syntax error message, got:\n%s", stderr.String())
		}
	})

	t.Run("multiple starlark actions on same destination allowed", func(t *testing.T) {
		repoDir := t.TempDir()
		dstDir := t.TempDir()

		mfs := fstest.MapFS{
			"repo/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[starlark]]
src = "doc['key1'] = 'val1'"
dst = ".claude/settings.json"

[[starlark]]
src = "doc['key2'] = 'val2'"
dst = ".claude/settings.json"
`), Mode: 0o644},
		}
		if err := os.CopyFS(repoDir, mfs); err != nil {
			t.Fatal(err)
		}

		d := &Dotfiles{}
		stdout := &strings.Builder{}
		stderr := &strings.Builder{}
		args := []string{"-repos", repoDir, "-dst", dstDir, "lint"}
		if err := d.Run(stdout, stderr, args); err != nil {
			t.Fatalf("expected clean lint for multiple starlark actions, got: %v\nstderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "all 1 repositories ok") {
			t.Errorf("expected ok message, got: %s", stdout.String())
		}
	})
}
