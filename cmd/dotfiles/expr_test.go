package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/expr-lang/expr"
)

func TestEvalExpr(t *testing.T) {
	t.Setenv("DOTFILES_EXPR_TEST", "from-env")

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "tool-a"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	dstRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(dstRoot, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dstRoot, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dstRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstRoot, "bin", "tool-b"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolB := filepath.Join(dstRoot, "bin", "tool-b")

	tests := []struct {
		src     string
		want    bool
		wantErr bool
	}{
		{src: `os == "` + runtime.GOOS + `"`, want: true},
		{src: `arch == "` + runtime.GOARCH + `"`, want: true},
		{src: `env("DOTFILES_EXPR_TEST") == "from-env"`, want: true},
		{src: `lookPath("nope-x", "tool-a") == "` + filepath.Join(binDir, "tool-a") + `"`, want: true},
		{src: `lookPath("nope-x") == ""`, want: true},
		{src: `exists("file") && exists("dir") && !exists("nope")`, want: true},
		{src: `isFile("file") && !isFile("dir")`, want: true},
		{src: `isDir("dir") && !isDir("file")`, want: true},
		{src: `isFile("` + filepath.Join(dstRoot, "file") + `")`, want: true},
		{src: `lookPath("bin/tool-b") == "` + toolB + `"`, want: true},
		{src: `lookPath("bin/nope", "tool-a") == "` + filepath.Join(binDir, "tool-a") + `"`, want: true},
		{src: `lookPath("dir") == ""`, want: true},
		{src: `nope("x")`, wantErr: true},
		{src: `os ===`, wantErr: true},
	}
	for _, tt := range tests {
		got, err := evalExpr(exprEnv(dstRoot, nil), tt.src, expr.AsBool())
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %v", tt.src, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.src, got, tt.want)
		}
	}
}

func TestResolvePath(t *testing.T) {
	t.Parallel()
	tests := []struct{ path, want string }{
		{".config/foo", "/dst/.config/foo"},
		{"/etc/foo", "/etc/foo"},
	}
	for _, tt := range tests {
		if got := resolvePath("/dst", tt.path); got != tt.want {
			t.Errorf("resolvePath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
