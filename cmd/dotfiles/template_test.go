package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestStringList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		doc     string
		want    StringList
		wantErr bool
	}{
		{doc: `v = 'a'`, want: StringList{"a"}},
		{doc: `v = ['a', "b", '''c''']`, want: StringList{"a", "b", "c"}},
		{doc: "v = '''\nmulti\nline'''", want: StringList{"multi\nline"}},
		{doc: `v = []`, want: StringList{}},
		{doc: `v = ['a', 2]`, wantErr: true},
	}
	for _, tt := range tests {
		var c struct {
			V StringList `toml:"v"`
		}
		err := toml.Unmarshal([]byte(tt.doc), &c)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %q", tt.doc, c.V)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.doc, err)
			continue
		}
		if !slices.Equal(c.V, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.doc, c.V, tt.want)
		}
	}
}

func TestRenderTemplate(t *testing.T) {
	t.Setenv("DOTFILES_TEMPLATE_TEST", "from-env")
	hostname, _ := os.Hostname()

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
	if err := os.Symlink("file", filepath.Join(dstRoot, "link")); err != nil {
		t.Fatal(err)
	}
	absFile := filepath.Join(dstRoot, "file")

	tests := []struct {
		text    string
		want    string
		wantErr bool
	}{
		{text: `{{.os}}`, want: runtime.GOOS},
		{text: `{{.arch}}`, want: runtime.GOARCH},
		{text: `{{if expr "arch == '` + runtime.GOARCH + `'"}}yes{{end}}`, want: "yes"},
		{text: `{{.hostname}}`, want: hostname},
		{text: `{{env "DOTFILES_TEMPLATE_TEST"}}`, want: "from-env"},
		{text: `{{if expr "os == '` + runtime.GOOS + `'"}}yes{{end}}`, want: "yes"},
		{text: `{{if expr "os == 'plan9-nope'"}}yes{{end}}`, want: ""},
		{text: `{{expr "1 + 2"}}`, want: "3"},
		{text: `{{lookPath "nope-x" "tool-a"}}`, want: filepath.Join(binDir, "tool-a")},
		{text: `{{lookPath "nope-x"}}`, want: ""},
		{text: `{{if lookPath "nope-x"}}y{{else}}n{{end}}`, want: "n"},
		{text: `{{exists "file"}} {{exists "dir"}} {{exists "nope"}}`, want: "true true false"},
		{text: `{{isFile "file"}} {{isFile "dir"}} {{isFile "link"}} {{isFile "nope"}}`, want: "true false true false"},
		{text: `{{isDir "dir"}} {{isDir "file"}} {{isDir "nope"}}`, want: "true false false"},
		{text: `{{isFile "` + absFile + `"}}`, want: "true"},
		{text: `{{.hostnme}}`, wantErr: true},
		{text: `{{expr "os ==="}}`, wantErr: true},
		{text: `{{if}}`, wantErr: true},
	}
	for _, tt := range tests {
		got, err := renderTemplate("test", tt.text, dstRoot, nil)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %q", tt.text, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.text, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("%s: got %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestSyncTemplates(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"templates/a.tmpl": &fstest.MapFile{Data: []byte("a"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[template]]
src = 'templates/a.tmpl'
dst = '.config/a/{{.os}}'

[[template]]
src = 'templates/a.tmpl'
dst = ['.config/b', '{{if false}}.config/never{{end}}', '.config/c']

[[template]]
src = 'templates/a.tmpl'
dst = '.config/skipped'
condition = "os == 'plan9-nope'"
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, a := range actions {
		if ta, ok := a.(*TemplateAction); ok {
			got = append(got, ta.Dst)
		}
	}
	want := []string{".config/a/" + runtime.GOOS, ".config/b", ".config/c"}
	if !slices.Equal(got, want) {
		t.Errorf("got template dsts %q, want %q", got, want)
	}
}

func TestLoadConfigTemplateValidation(t *testing.T) {
	t.Parallel()
	for _, cfg := range []string{
		"[[template]]\ndst = 'x'\n",
		"[[template]]\nsrc = 'x'\n",
		"[[template]]\nsrc = 'x'\ndst = []\n",
		"[[template]]\nsrc = 'x'\ndst = 'y'\nmode = '999'\n",
		"[[template]]\nsrc = 'x'\ndst = 'y'\nmode = 644\n",
		"[[template]]\nsrc = 'x'\ndst = 'y'\nexisting = 'keep'\n",
	} {
		repoDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := NewRepository(repoDir, t.TempDir()).LoadConfig(); err == nil {
			t.Errorf("expected error for config %q", cfg)
		}
	}
}

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

func TestParseFileMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s       string
		want    fs.FileMode
		wantErr bool
	}{
		{s: "", want: 0},
		{s: "0644", want: 0o644},
		{s: "755", want: 0o755},
		{s: "0600", want: 0o600},
		{s: "0", wantErr: true},
		{s: "999", wantErr: true},
		{s: "1777", wantErr: true},
		{s: "0o644", wantErr: true},
		{s: "rw-r--r--", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseFileMode(tt.s)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%q: expected error, got %v", tt.s, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%q: got %v, %v; want %v", tt.s, got, err, tt.want)
		}
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

func TestLoadConfigRendersPaths(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"files/" + runtime.GOOS + ".conf":       &fstest.MapFile{Data: []byte("x"), Mode: 0o644},
		"cmd/tool-" + runtime.GOOS + "/main.go": &fstest.MapFile{Data: []byte("package main"), Mode: 0o644},
		"templates/" + runtime.GOOS + ".tmpl":   &fstest.MapFile{Data: []byte("x"), Mode: 0o644},
		".dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[mount]]
src = 'files/{{.os}}.conf'
dst = '.config/{{.os}}/x.conf'

[[mount]]
src = '{{if false}}files/never{{end}}'
dst = '.config/never'

[[go]]
src = 'cmd/tool-{{.os}}'

[[go]]
src = '{{if false}}cmd/never{{end}}'

[[template]]
src = 'templates/{{.os}}.tmpl'
dst = '.config/t-{{.os}}'

[[template]]
src = '{{if false}}templates/never.tmpl{{end}}'
dst = '.config/never'
`), Mode: 0o644},
	}
	if err := os.CopyFS(repoDir, mfs); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(repoDir, dstDir)
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}

	if len(repo.config.Mount) != 1 || repo.config.Mount[0].Src != "files/"+runtime.GOOS+".conf" || repo.config.Mount[0].Dst != ".config/"+runtime.GOOS+"/x.conf" {
		t.Errorf("mounts: %+v", repo.config.Mount)
	}
	if len(repo.config.Go) != 1 || repo.config.Go[0].Src != "cmd/tool-"+runtime.GOOS {
		t.Errorf("go: %+v", repo.config.Go)
	}
	if len(repo.config.Template) != 1 || repo.config.Template[0].Src != "templates/"+runtime.GOOS+".tmpl" || !slices.Equal(repo.config.Template[0].Dst, []string{".config/t-" + runtime.GOOS}) {
		t.Errorf("templates: %+v", repo.config.Template)
	}

	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}
	var foundMount, foundTemplate bool
	for _, a := range actions {
		switch a := a.(type) {
		case *SymLinker:
			if a.Src == "files/"+runtime.GOOS+".conf" && a.Dst == ".config/"+runtime.GOOS+"/x.conf" {
				foundMount = true
			}
		case *TemplateAction:
			if a.Src == "templates/"+runtime.GOOS+".tmpl" && a.Dst == ".config/t-"+runtime.GOOS {
				foundTemplate = true
			}
		}
	}
	if !foundMount || !foundTemplate {
		t.Errorf("mount action found: %v, template action found: %v", foundMount, foundTemplate)
	}
}

func TestLoadConfigRenderError(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	cfg := "[[mount]]\nsrc = 'a'\ndst = '{{.nope}}'\n"
	if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	err := NewRepository(repoDir, t.TempDir()).LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "mount[0].dst") || !strings.Contains(err.Error(), "dotfiles.toml") {
		t.Errorf("expected error naming mount[0].dst and the config file, got %v", err)
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

func TestSyncTemplateExistingForce(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	cfg := "[[template]]\nsrc = 't.tmpl'\ndst = 'out'\nexisting = 'error'\n"
	if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		repo := NewRepository(repoDir, t.TempDir(), WithForce(force))
		if err := repo.LoadConfig(); err != nil {
			t.Fatal(err)
		}
		actions, err := repo.Sync()
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range actions {
			if ta, ok := a.(*TemplateAction); ok {
				if ta.Existing != ExistingError || ta.Force != force {
					t.Errorf("force=%v: action has Existing %q, Force %v", force, ta.Existing, ta.Force)
				}
			}
		}
	}
}
