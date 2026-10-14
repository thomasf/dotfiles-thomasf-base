package main

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/expr-lang/expr"
)

func TestResolveVars(t *testing.T) {
	t.Parallel()
	raw := map[string]string{
		"literal":  "plain text",
		"tmpl":     "{{ .os }}-x",
		"expr":     "{{ expr `1 + 2` }}",
		"ref":      "{{ .vars.tmpl }}/y",
		"refRoot":  "{{ $.vars.literal }}",
		"refIndex": `{{ index .vars "literal" }}`,
		"refExpr":  "{{ expr `vars.ref + \"/z\"` }}",
		"default":  "{{ expr `vars.missing ?? \"d\"` }}",
		"orIndex":  `{{ or (index .vars "missing") "o" }}`,
		"chain":    "{{ .vars.refExpr }}!",
		"braces":   "no {{ \"{{\" }} here",
		"bool":     "false",
		"isFalse":  "{{ if parseBool .vars.bool }}yes{{ else }}no{{ end }}",
	}
	vars, err := resolveVars(raw, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"literal":  "plain text",
		"tmpl":     runtime.GOOS + "-x",
		"expr":     "3",
		"ref":      runtime.GOOS + "-x/y",
		"refRoot":  "plain text",
		"refIndex": "plain text",
		"refExpr":  runtime.GOOS + "-x/y/z",
		"default":  "d",
		"orIndex":  "o",
		"chain":    runtime.GOOS + "-x/y/z!",
		"braces":   "no {{ here",
		"bool":     "false",
		"isFalse":  "no",
	}
	for name, w := range want {
		if vars[name] != w {
			t.Errorf("vars.%s = %q, want %q", name, vars[name], w)
		}
	}
}

func TestResolveVarsErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     map[string]string
		wantErr string
	}{
		{
			name:    "cycle",
			raw:     map[string]string{"a": "{{ .vars.b }}", "b": "{{ expr `vars.c` }}", "c": "{{ .vars.a }}"},
			wantErr: "reference cycle: a -> b -> c -> a",
		},
		{
			name:    "self",
			raw:     map[string]string{"a": "{{ .vars.a }}"},
			wantErr: "vars.a: reference cycle: a -> a",
		},
		{
			name:    "undefined in template",
			raw:     map[string]string{"a": "{{ .vars.nope }}"},
			wantErr: `vars.a: template: vars.a:1:8: executing "vars.a" at <.vars.nope>: map has no entry for key "nope"`,
		},
		{
			name:    "undefined in expr",
			raw:     map[string]string{"a": "{{ expr `vars.nope` }}"},
			wantErr: "undefined var vars.nope",
		},
		{
			name:    "parse error",
			raw:     map[string]string{"a": "{{ .vars.b"},
			wantErr: "vars.a: template",
		},
		{
			name:    "failed dependency",
			raw:     map[string]string{"a": "{{ .vars.b }}", "b": "{{ .nope }}"},
			wantErr: "vars.b:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveVars(tt.raw, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			if strings.Count(err.Error(), "\n") > 0 {
				t.Errorf("want a single error, got: %v", err)
			}
		})
	}
}

func TestEvalExprVars(t *testing.T) {
	t.Parallel()
	env := exprEnv(t.TempDir(), map[string]string{"size": "11", "gui": "true", "empty": ""})
	tests := []struct {
		src     string
		want    any
		wantErr string
	}{
		{src: `vars.size`, want: "11"},
		{src: `int(vars.size) + 2`, want: 13},
		{src: `parseBool(vars.gui)`, want: true},
		{src: `vars.missing ?? "d"`, want: "d"},
		{src: `vars?.missing ?? "d"`, want: "d"},
		{src: `vars.empty ?? "d"`, want: ""},
		{src: `vars["size"]`, want: "11"},
		{src: `vars.missing`, wantErr: "undefined var vars.missing"},
		{src: `vars.missing == "x"`, wantErr: "undefined var vars.missing"},
		{src: `(vars.missing ?? "x") + vars.other`, wantErr: "undefined var vars.other"},
		{src: `parseBool("nope")`, wantErr: "invalid syntax"},
	}
	for _, tt := range tests {
		got, err := evalExpr(env, tt.src)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: err = %v, want containing %q", tt.src, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s = %#v, want %#v", tt.src, got, tt.want)
		}
	}

	if _, err := evalExpr(env, `vars.missing == ""`, expr.AsBool()); err == nil {
		t.Error("condition with an undefined var: want error")
	}
}

func TestMergeVars(t *testing.T) {
	t.Parallel()
	repo := func(name string, vars map[string]string, names ...string) *Repository {
		return &Repository{
			srcRoot:    "/repos/" + name,
			configPath: "/repos/" + name + "/dotfiles.toml",
			config:     Config{Vars: vars, Repositories: Repositories{Names: names}},
		}
	}
	// Directory order is alphabetical; names order puts xmonad before emacs.
	base := repo("base", map[string]string{"font": "11", "same": "s"}, "base", "xmonad", "emacs")
	emacs := repo("emacs", map[string]string{"font": "12", "same": "s", "only": "e"})
	other := repo("other", nil)
	xmonad := repo("xmonad", map[string]string{"font": "13", "late": "x"})
	unlisted := repo("aaa", map[string]string{"late": "a"})

	views, warnings := mergeVars([]*Repository{unlisted, base, emacs, other, xmonad})

	tests := []struct {
		repo *Repository
		want map[string]string
	}{
		{base, map[string]string{"font": "11", "same": "s", "only": "e", "late": "x"}},
		{emacs, map[string]string{"font": "12", "same": "s", "only": "e", "late": "x"}},
		{xmonad, map[string]string{"font": "13", "same": "s", "only": "e", "late": "x"}},
		{other, map[string]string{"font": "11", "same": "s", "only": "e", "late": "x"}},
		{unlisted, map[string]string{"font": "11", "same": "s", "only": "e", "late": "a"}},
	}
	for _, tt := range tests {
		got := views[tt.repo]
		if len(got) != len(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.repo.srcRoot, got, tt.want)
			continue
		}
		for k, v := range tt.want {
			if got[k] != v {
				t.Errorf("%s: vars.%s = %q, want %q", tt.repo.srcRoot, k, got[k], v)
			}
		}
	}

	wantWarnings := []string{
		"vars.font is defined differently in /repos/base/dotfiles.toml, /repos/xmonad/dotfiles.toml, /repos/emacs/dotfiles.toml; repositories that do not define it use /repos/base/dotfiles.toml",
		"vars.late is defined differently in /repos/xmonad/dotfiles.toml, /repos/aaa/dotfiles.toml; repositories that do not define it use /repos/xmonad/dotfiles.toml",
	}
	if !slices.Equal(warnings, wantWarnings) {
		t.Errorf("warnings:\n%s\nwant:\n%s", strings.Join(warnings, "\n"), strings.Join(wantWarnings, "\n"))
	}

	// No warning when every repository defines the var itself.
	_, warnings = mergeVars([]*Repository{repo("a", map[string]string{"x": "1"}), repo("b", map[string]string{"x": "2"})})
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestMergeVarsDefaults(t *testing.T) {
	t.Parallel()
	repo := func(name string, vars, defaults map[string]string) *Repository {
		return &Repository{
			srcRoot:    "/repos/" + name,
			configPath: "/repos/" + name + "/dotfiles.toml",
			config:     Config{Vars: vars, DefaultVars: defaults},
		}
	}
	a := repo("a", nil, map[string]string{"font": "11", "editor": "vi", "own": "a"})
	b := repo("b", map[string]string{"font": "13"}, nil)
	c := repo("c", nil, map[string]string{"editor": "nano"})

	views, warnings := mergeVars([]*Repository{a, b, c})
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	tests := []struct {
		repo *Repository
		want map[string]string
	}{
		// b's [vars] beat a's own default; a's defaults are not seen by others.
		{a, map[string]string{"font": "13", "editor": "vi", "own": "a"}},
		{b, map[string]string{"font": "13"}},
		{c, map[string]string{"font": "13", "editor": "nano"}},
	}
	for _, tt := range tests {
		if got := views[tt.repo]; !maps.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.repo.srcRoot, got, tt.want)
		}
	}
}

func TestLoadConfigDefaultVars(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	cfg := `
[vars]
name = 'x'

[default_vars]
greeting = 'hello {{ .vars.name }}'

[[mount]]
src = 'a'
dst = '{{ .vars.greeting }}'
`
	if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(repoDir, t.TempDir())
	if err := repo.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := repo.config.Mount[0].Dst; got != "hello x" {
		t.Errorf("dst = %q, want %q", got, "hello x")
	}
}

func TestLoadConfigVarsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cfg     string
		wantErr string
	}{
		{"[vars]\nsize = 11\n", "dotfiles.toml:2:8: vars.size: toml: cannot decode TOML integer into string"},
		{"[vars]\ngui = false\n", "dotfiles.toml:2:7: vars.gui: toml: cannot decode TOML boolean into string"},
		{"[vars]\n'font-size' = '11'\n", "vars.font-size: invalid name"},
		{"[vars]\n'1x' = '11'\n", "vars.1x: invalid name"},
		{"[default_vars]\n'a-b' = '11'\n", "default_vars.a-b: invalid name"},
		{"[vars]\nx = '1'\n[default_vars]\nx = '2'\n", "default_vars.x: also defined in [vars]"},
		{"[vars]\nx = '{{ .vars.y }}'\n", `map has no entry for key "y"`},
		{"[vars]\nx = '1'\n[[mount]]\nsrc = 'a'\ndst = '{{ .vars.y }}'\n", `map has no entry for key "y"`},
	}
	for _, tt := range tests {
		repoDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte(tt.cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		err := NewRepository(repoDir, t.TempDir()).LoadConfig()
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%q: err = %v, want containing %q", tt.cfg, err, tt.wantErr)
		}
	}
}

func TestDotfilesRunVars(t *testing.T) {
	reposDir := t.TempDir()
	dstDir := t.TempDir()

	mfs := fstest.MapFS{
		"base/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[repositories]
names = ["base", "work"]

[vars]
email = 'me@example.com'
font  = '11'
gui   = 'true'
`)},
		"work/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[vars]
font = '{{ expr ` + "`int(vars.base_font ?? \"12\") + 2`" + ` }}'

[[mount]]
condition = 'parseBool(vars.gui)'
src = 'files/{{ .vars.font }}.conf'
dst = '.config/font.conf'

[[template]]
src = 'templates/user.tmpl'
dst = '.config/user'
`)},
		"work/files/14.conf":       &fstest.MapFile{Data: []byte("x")},
		"work/templates/user.tmpl": &fstest.MapFile{Data: []byte("{{ .vars.email }} {{ .vars.font }}\n"), Mode: 0o644},
		"zzz/dotfiles.toml": &fstest.MapFile{Data: []byte(`
[[template]]
condition = 'int(vars.font) == 11'
src = 'font.tmpl'
dst = '.config/zzz-font'
`)},
		"zzz/font.tmpl": &fstest.MapFile{Data: []byte("{{ .vars.font }}\n"), Mode: 0o644},
	}
	if err := os.CopyFS(reposDir, mfs); err != nil {
		t.Fatal(err)
	}

	d := &Dotfiles{}
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	args := []string{"-repos", reposDir, "-dst", dstDir, "install"}
	if err := d.Run(stdout, stderr, args); err != nil {
		t.Fatalf("Run: %v\nstderr: %s", err, stderr)
	}

	if target, err := os.Readlink(filepath.Join(dstDir, ".config/font.conf")); err != nil || !strings.HasSuffix(target, "work/files/14.conf") {
		t.Errorf("font.conf -> %q, %v", target, err)
	}
	if got, err := os.ReadFile(filepath.Join(dstDir, ".config/user")); err != nil || string(got) != "me@example.com 14\n" {
		t.Errorf(".config/user = %q, %v", got, err)
	}
	// zzz does not define font, so it gets the one from base, first in names.
	if got, err := os.ReadFile(filepath.Join(dstDir, ".config/zzz-font")); err != nil || string(got) != "11\n" {
		t.Errorf(".config/zzz-font = %q, %v", got, err)
	}
	if !strings.Contains(stderr.String(), "warning: vars.font is defined differently in") {
		t.Errorf("expected vars.font warning, stderr: %s", stderr)
	}
}
