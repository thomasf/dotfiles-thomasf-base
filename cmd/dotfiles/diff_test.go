package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPlanDiff(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"repo/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[template]]
src = "test.tmpl"
dst = ".testrc"
`), Mode: 0o644},
		"repo/test.tmpl": &fstest.MapFile{Data: []byte("greeting = hello\nversion = 2\n"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	runPlanDiff := func() string {
		d := &Dotfiles{}
		stdout := &strings.Builder{}
		stderr := &strings.Builder{}
		if err := d.Run(stdout, stderr, []string{"-repos", repoDir, "-dst", dstDir, "plan", "-diff"}); err != nil {
			t.Fatalf("d.Run failed: %v", err)
		}
		return stdout.String()
	}

	// 1. Target does not exist yet: shows addition
	out := runPlanDiff()
	if !strings.Contains(out, "+greeting = hello") {
		t.Errorf("expected added content, got:\n%s", out)
	}

	// 2. Target exists with different content: shows modification
	dstFile := filepath.Join(dstDir, ".testrc")
	if err := os.WriteFile(dstFile, []byte("greeting = old\nversion = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runPlanDiff()
	if !strings.Contains(out, "-greeting = old") || !strings.Contains(out, "+greeting = hello") {
		t.Errorf("expected modification diff, got:\n%s", out)
	}

	// 3. Target exists with identical content: no diff
	if err := os.WriteFile(dstFile, []byte("greeting = hello\nversion = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runPlanDiff()
	if strings.Contains(out, "---") || strings.Contains(out, "+++") {
		t.Errorf("expected no diff when identical, got:\n%s", out)
	}
}

func TestStarlarkPlanDiff(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"repo/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[starlark]]
src = "doc['b'] = 2\ndoc['a'] = 1"
dst = "settings.json"
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	dstFile := filepath.Join(dstDir, "settings.json")
	// Target has keys in different order, but values are already equal
	if err := os.WriteFile(dstFile, []byte("{\n  \"b\": 2,\n  \"a\": 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Dotfiles{}
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	if err := d.Run(stdout, stderr, []string{"-repos", repoDir, "-dst", dstDir, "plan", "-diff"}); err != nil {
		t.Fatalf("d.Run failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "plan: [repo] starlark: settings.json") {
		t.Errorf("missing plan line in output: %s", out)
	}
	if strings.Contains(out, "---") || strings.Contains(out, "+++") {
		t.Errorf("expected no diff output when semantic values are equal, got:\n%s", out)
	}
}

func TestPlanDiffExistingSkip(t *testing.T) {
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"repo/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[template]]
src = "test.tmpl"
dst = ".seedrc"
existing = "skip"
`), Mode: 0o644},
		"repo/test.tmpl": &fstest.MapFile{Data: []byte("val = template\n"), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	dstFile := filepath.Join(dstDir, ".seedrc")
	if err := os.WriteFile(dstFile, []byte("val = custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Default plan -diff: destination exists, so template is skipped, no diff shown
	d := &Dotfiles{}
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	if err := d.Run(stdout, stderr, []string{"-repos", repoDir, "-dst", dstDir, "plan", "-diff"}); err != nil {
		t.Fatalf("d.Run failed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "plan: [repo] template: test.tmpl -> .seedrc") {
		t.Errorf("missing plan line in output: %s", out)
	}
	if strings.Contains(out, "---") || strings.Contains(out, "+++") {
		t.Errorf("expected no diff for existing skipped template, got:\n%s", out)
	}

	// With -overwrite-skipped: diff is shown
	d = &Dotfiles{}
	stdout = &strings.Builder{}
	stderr = &strings.Builder{}
	if err := d.Run(stdout, stderr, []string{"-repos", repoDir, "-dst", dstDir, "-overwrite-skipped", "plan", "-diff"}); err != nil {
		t.Fatalf("d.Run failed: %v", err)
	}
	out = stdout.String()
	if !strings.Contains(out, "-val = custom") || !strings.Contains(out, "+val = template") {
		t.Errorf("expected diff with -overwrite-skipped, got:\n%s", out)
	}
}
