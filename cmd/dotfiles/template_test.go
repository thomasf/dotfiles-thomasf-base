package main

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pelletier/go-toml/v2"
	"mvdan.cc/sh/v3/interp"
	shsyntax "mvdan.cc/sh/v3/syntax"
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
		{text: `{{shellQuote "plain-word_1.2"}}`, want: `plain-word_1.2`},
		{text: `{{shellQuote "two words"}}`, want: `'two words'`},
		{text: `{{shellQuote ""}}`, want: `''`},
		{text: `{{shellQuote "nul\x00"}}`, wantErr: true},
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
		case *SymlinkAction:
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

// TestShellQuote checks that the shell reads a word quoted by shellQuote back
// as the original string.
func TestShellQuote(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"plain", "two words", "it's", `say "hi"`, "$HOME `id` $(id)", "a\\b", "line\nbreak", "*?[x]", "~user", "", "-n"} {
		q, err := shellQuote(s)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		f, err := shsyntax.NewParser().Parse(strings.NewReader("printf %s "+q), "")
		if err != nil {
			t.Errorf("%q quoted as %s: %v", s, q, err)
			continue
		}
		var out strings.Builder
		runner, err := interp.New(interp.StdIO(nil, &out, os.Stderr))
		if err != nil {
			t.Fatal(err)
		}
		if err := runner.Run(context.Background(), f); err != nil {
			t.Errorf("%q quoted as %s: %v", s, q, err)
			continue
		}
		if out.String() != s {
			t.Errorf("%q quoted as %s reads back as %q", s, q, out.String())
		}
	}
}

func TestGitValueTemplates(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	config := `
[vars]
email = 'me@example.com'

[git]
'user.email' = '{{.vars.email}}'
'core.pager' = '{{if eq .os "plan9-nope"}}less{{end}}'
'alias.st' = 'status'
`
	if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(repoDir, t.TempDir())
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"user.email": "me@example.com", "core.pager": "", "alias.st": "status"}
	if !maps.Equal(repo.config.Git, want) {
		t.Errorf("got git config %q, want %q", repo.config.Git, want)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte("[git]\n'user.email' = '{{.vars.nope}}'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewRepository(repoDir, t.TempDir()).LoadConfig(); err == nil || !strings.Contains(err.Error(), "git.user.email") {
		t.Errorf("got error %v, want one naming git.user.email", err)
	}
}
