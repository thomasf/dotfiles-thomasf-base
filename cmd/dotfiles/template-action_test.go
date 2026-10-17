package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTemplateAction(t *testing.T, src, dst string) *TemplateAction {
	t.Helper()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "t.tmpl"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return &TemplateAction{SrcRoot: repoDir, Src: "t.tmpl", DstRoot: t.TempDir(), Dst: dst}
}

func TestTemplateActionWrite(t *testing.T) {
	t.Parallel()
	a := newTemplateAction(t, `{"os": "{{.os}}"}`, "sub/dir/out.json")
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	path := a.DstAbsolutePath()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"os": "` + runtime.GOOS + `"}`; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("new file mode %v, want 0644 from the template", info.Mode().Perm())
	}

	// An unchanged file is not rewritten.
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("unchanged file was rewritten")
	}
}

func TestTemplateActionMode(t *testing.T) {
	t.Parallel()
	perm := func(path string) fs.FileMode {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)

	// The output follows the template's mode; with unchanged content only
	// the mode is changed, without rewriting the file.
	a := newTemplateAction(t, "same", "out")
	src := filepath.Join(a.SrcRoot, a.Src)
	dst := a.DstAbsolutePath()
	if err := os.Chmod(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dst, old, old); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if got := perm(dst); got != 0o750 {
		t.Errorf("mode %v, want 0750 from the template", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Error("file with unchanged content was rewritten to change its mode")
	}

	// Changed content gets the template's mode too.
	if err := os.WriteFile(src, []byte("new"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if got := perm(dst); got != 0o750 {
		t.Errorf("rewritten mode %v, want 0750", got)
	}

	// Mode overrides the template's mode.
	a.Mode = 0o600
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if got := perm(dst); got != 0o600 {
		t.Errorf("mode %v, want 0600 from Mode", got)
	}
}

func TestTemplateActionInvalidOutput(t *testing.T) {
	t.Parallel()
	for _, dst := range []string{"out.json", "out.toml"} {
		a := newTemplateAction(t, "not = valid { json or toml", dst)
		path := a.DstAbsolutePath()
		if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := a.Run(); err == nil {
			t.Errorf("%s: expected validation error", dst)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "keep" {
			t.Errorf("%s: existing file changed to %q", dst, got)
		}
	}

	// Other extensions are not validated.
	a := newTemplateAction(t, "not = valid { json or toml", "out.conf")
	if err := a.Run(); err != nil {
		t.Errorf("out.conf: %v", err)
	}
}

func TestTemplateActionRenderError(t *testing.T) {
	t.Parallel()
	a := newTemplateAction(t, "{{.missing}}", "out")
	err := a.Run()
	if err == nil || !strings.Contains(err.Error(), "t.tmpl") {
		t.Errorf("expected error naming the template, got %v", err)
	}
	if _, err := os.Lstat(a.DstAbsolutePath()); !os.IsNotExist(err) {
		t.Errorf("dst written despite render error")
	}
}

func TestTemplateActionSymlink(t *testing.T) {
	t.Parallel()
	a := newTemplateAction(t, "new", "link")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, a.DstAbsolutePath()); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(); err == nil {
		t.Error("expected error writing through symlink")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Errorf("symlink target changed to %q", got)
	}

	// A dst inside a symlinked directory is fine.
	realDir := t.TempDir()
	if err := os.Symlink(realDir, filepath.Join(a.DstRoot, "linkdir")); err != nil {
		t.Fatal(err)
	}
	a.Dst = "linkdir/out"
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(realDir, "out")); err != nil {
		t.Error(err)
	}
}

func TestTemplateActionExisting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		existing ExistingAction
		force    bool
		old      string // "" means dst does not exist
		oldPerm  fs.FileMode
		want     string
		wantPerm fs.FileMode
		wantErr  bool
	}{
		{existing: "", old: "old", oldPerm: 0o600, want: "new", wantPerm: 0o644},
		{existing: ExistingOverwrite, old: "old", oldPerm: 0o600, want: "new", wantPerm: 0o644},
		{existing: ExistingOverwrite, old: "", want: "new", wantPerm: 0o644},

		{existing: ExistingSkip, old: "", want: "new", wantPerm: 0o644},
		{existing: ExistingSkip, old: "old", oldPerm: 0o600, want: "old", wantPerm: 0o600},
		{existing: ExistingSkip, old: "new", oldPerm: 0o600, want: "new", wantPerm: 0o600},

		{existing: ExistingError, old: "", want: "new", wantPerm: 0o644},
		{existing: ExistingError, old: "new", oldPerm: 0o600, want: "new", wantPerm: 0o644},
		{existing: ExistingError, old: "old", oldPerm: 0o600, want: "old", wantPerm: 0o600, wantErr: true},
		{existing: ExistingError, force: true, old: "old", oldPerm: 0o600, want: "new", wantPerm: 0o644},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("existing=%q force=%v old=%q", tt.existing, tt.force, tt.old)
		a := newTemplateAction(t, "new", "out")
		a.Existing = tt.existing
		a.Force = tt.force
		dst := a.DstAbsolutePath()
		if tt.old != "" {
			if err := os.WriteFile(dst, []byte(tt.old), tt.oldPerm); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dst, tt.oldPerm); err != nil {
				t.Fatal(err)
			}
		}
		err := a.Run()
		if tt.wantErr != (err != nil) {
			t.Errorf("%s: error %v, want error %v", name, err, tt.wantErr)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != tt.want {
			t.Errorf("%s: content %q, want %q", name, got, tt.want)
		}
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != tt.wantPerm {
			t.Errorf("%s: mode %v, want %v", name, info.Mode().Perm(), tt.wantPerm)
		}
	}
}
