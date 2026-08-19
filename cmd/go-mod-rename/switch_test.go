package main

import (
	"slices"
	"testing"
	"testing/fstest"
)

func TestBuildModEdits(t *testing.T) {
	tests := []struct {
		name    string
		mod     *goMod
		oldPath string
		newPath string
		version string
		want    []string
	}{
		{
			name: "direct require keeps the current version",
			mod: &goMod{
				Require: []goModRequire{
					{Path: "example.com/other", Version: "v1.0.0"},
					{Path: "example.com/old", Version: "v1.2.3"},
				},
			},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			want: []string{
				"-droprequire=example.com/old",
				"-require=example.com/new@v1.2.3",
			},
		},
		{
			name: "explicit version wins",
			mod: &goMod{
				Require: []goModRequire{{Path: "example.com/old", Version: "v1.2.3"}},
			},
			oldPath: "example.com/old",
			newPath: "example.com/new/v2",
			version: "v2.0.0",
			want: []string{
				"-droprequire=example.com/old",
				"-require=example.com/new/v2@v2.0.0",
			},
		},
		{
			name:    "not required and no version yields no edits",
			mod:     &goMod{},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			want:    nil,
		},
		{
			name:    "not required but version given adds a require",
			mod:     &goMod{},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			version: "v1.0.0",
			want:    []string{"-require=example.com/new@v1.0.0"},
		},
		{
			name: "replace of the old path is moved to the new path",
			mod: &goMod{
				Require: []goModRequire{{Path: "example.com/old", Version: "v1.2.3"}},
				Replace: []goModReplace{
					{
						Old: goModVersion{Path: "example.com/old"},
						New: goModVersion{Path: "../local/old"},
					},
				},
			},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			want: []string{
				"-droprequire=example.com/old",
				"-require=example.com/new@v1.2.3",
				"-dropreplace=example.com/old",
				"-replace=example.com/new=../local/old",
			},
		},
		{
			name: "versioned replace keeps its versions",
			mod: &goMod{
				Replace: []goModReplace{
					{
						Old: goModVersion{Path: "example.com/old", Version: "v1.0.0"},
						New: goModVersion{Path: "example.com/fork", Version: "v1.1.0"},
					},
				},
			},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			want: []string{
				"-dropreplace=example.com/old@v1.0.0",
				"-replace=example.com/new@v1.0.0=example.com/fork@v1.1.0",
			},
		},
		{
			name: "replace pointing at the old path is retargeted",
			mod: &goMod{
				Replace: []goModReplace{
					{
						Old: goModVersion{Path: "example.com/thing"},
						New: goModVersion{Path: "example.com/old", Version: "v1.2.3"},
					},
				},
			},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			want: []string{
				"-replace=example.com/thing=example.com/new@v1.2.3",
			},
		},
		{
			name: "self replace maps both sides",
			mod: &goMod{
				Replace: []goModReplace{
					{
						Old: goModVersion{Path: "example.com/old"},
						New: goModVersion{Path: "example.com/old", Version: "v1.9.0"},
					},
				},
			},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			want: []string{
				"-dropreplace=example.com/old",
				"-replace=example.com/new=example.com/new@v1.9.0",
			},
		},
		{
			name: "tool directive pointing at old path is updated",
			mod: &goMod{
				Tool: []goModTool{
					{Path: "example.com/old/cmd/mytool"},
					{Path: "example.com/other/tool"},
				},
			},
			oldPath: "example.com/old",
			newPath: "example.com/new",
			want: []string{
				"-droptool=example.com/old/cmd/mytool",
				"-tool=example.com/new/cmd/mytool",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildModEdits(tt.mod, tt.oldPath, tt.newPath, tt.version)
			if !slices.Equal(got, tt.want) {
				t.Errorf("buildModEdits() =\n%v\nwant\n%v", got, tt.want)
			}
		})
	}
}

// TestReadGoModModulePath covers the go.mod spellings that a line-oriented
// parser gets wrong: a trailing comment and the parenthesized block form.
func TestReadGoModModulePath(t *testing.T) {
	tests := []struct {
		name    string
		files   fstest.MapFS
		want    string
		wantErr bool
	}{
		{
			name: "plain module directive",
			files: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module github.com/example/project\n\ngo 1.25.0\n")},
			},
			want: "github.com/example/project",
		},
		{
			name: "extra spaces",
			files: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module   github.com/example/project\n")},
			},
			want: "github.com/example/project",
		},
		{
			name: "trailing comment",
			files: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module github.com/example/project // Deprecated: moved\n")},
			},
			want: "github.com/example/project",
		},
		{
			name: "parenthesized block",
			files: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("module (\n\tgithub.com/example/project\n)\n\ngo 1.25.0\n")},
			},
			want: "github.com/example/project",
		},
		{
			name: "no module directive",
			files: fstest.MapFS{
				"go.mod": &fstest.MapFile{Data: []byte("go 1.25.0\n")},
			},
			want: "",
		},
		{
			name:    "no go.mod at all",
			files:   fstest.MapFS{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirFS(t, tt.files)

			mod, err := readGoMod()
			if (err != nil) != tt.wantErr {
				t.Fatalf("readGoMod() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if mod.Module.Path != tt.want {
				t.Errorf("module path = %q, want %q", mod.Module.Path, tt.want)
			}
		})
	}
}

func TestReadGoMod(t *testing.T) {
	chdirFS(t, fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte(`module example.com/project

go 1.25.0

require example.com/dep v1.2.3

replace example.com/dep => ../local/dep
`)},
	})

	mod, err := readGoMod()
	if err != nil {
		t.Fatalf("readGoMod() error: %v", err)
	}

	if mod.Module.Path != "example.com/project" {
		t.Errorf("module path = %q, want %q", mod.Module.Path, "example.com/project")
	}
	req, ok := findRequire(mod, "example.com/dep")
	if !ok {
		t.Fatalf("require for example.com/dep not found in %+v", mod.Require)
	}
	if req.Version != "v1.2.3" {
		t.Errorf("require version = %q, want %q", req.Version, "v1.2.3")
	}
	if len(mod.Replace) != 1 || mod.Replace[0].New.Path != "../local/dep" {
		t.Errorf("unexpected replace directives: %+v", mod.Replace)
	}
}

// projectFS is a project importing example.com/old, mixed with files that
// rewriteImports has to leave alone.
var projectFS = fstest.MapFS{
	"main.go":              &fstest.MapFile{Data: []byte("package main\n\nimport \"example.com/old/pkg\"\n\nvar _ = pkg.X\n")},
	"sub/sub.go":           &fstest.MapFile{Data: []byte("package sub\n\nimport \"example.com/old\"\n")},
	"sub/untouched.go":     &fstest.MapFile{Data: []byte("package sub\n\nimport \"example.com/keep\"\n")},
	"vendor/v/vendored.go": &fstest.MapFile{Data: []byte("package v\n\nimport \"example.com/old/pkg\"\n")},
	".hidden/hidden.go":    &fstest.MapFile{Data: []byte("package h\n\nimport \"example.com/old/pkg\"\n")},
	"_ignored/ignored.go":  &fstest.MapFile{Data: []byte("package ign\n\nimport \"example.com/old/pkg\"\n")},
	"notgo.txt":            &fstest.MapFile{Data: []byte("example.com/old\n")},
	"broken/broken.go.bad": &fstest.MapFile{Data: []byte("package broken\n\nimport \"example.com/old\"\n")},
	"broken/really_bad.go": &fstest.MapFile{Data: []byte("this is not go source\n")},
}

func TestRewriteImports(t *testing.T) {
	dir := copyFS(t, projectFS)

	changed, err := rewriteImports(dir, "example.com/old", "example.com/new", false)
	if err != nil {
		t.Fatalf("rewriteImports() error: %v", err)
	}
	if changed != 2 {
		t.Errorf("rewriteImports() changed = %d, want 2", changed)
	}

	assertFS(t, dir, fstest.MapFS{
		// Rewritten.
		"main.go":    &fstest.MapFile{Data: []byte("package main\n\nimport \"example.com/new/pkg\"\n\nvar _ = pkg.X\n")},
		"sub/sub.go": &fstest.MapFile{Data: []byte("package sub\n\nimport \"example.com/new\"\n")},
		// Left alone: no matching import, skipped directory, not Go source,
		// and Go source that does not parse.
		"sub/untouched.go":     projectFS["sub/untouched.go"],
		"vendor/v/vendored.go": projectFS["vendor/v/vendored.go"],
		".hidden/hidden.go":    projectFS[".hidden/hidden.go"],
		"_ignored/ignored.go":  projectFS["_ignored/ignored.go"],
		"notgo.txt":            projectFS["notgo.txt"],
		"broken/broken.go.bad": projectFS["broken/broken.go.bad"],
		"broken/really_bad.go": projectFS["broken/really_bad.go"],
	})
}

func TestRewriteImportsDryRun(t *testing.T) {
	dir := copyFS(t, projectFS)

	changed, err := rewriteImports(dir, "example.com/old", "example.com/new", true)
	if err != nil {
		t.Fatalf("rewriteImports() error: %v", err)
	}
	if changed != 2 {
		t.Errorf("rewriteImports() changed = %d, want 2", changed)
	}

	assertFS(t, dir, projectFS)
}
