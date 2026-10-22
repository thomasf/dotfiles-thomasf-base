package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSelection(t *testing.T) {
	reposDir := t.TempDir()
	dst := t.TempDir()
	alpha := filepath.Join(reposDir, "alpha")
	beta := filepath.Join(reposDir, "beta")
	for path, fsys := range map[string]fstest.MapFS{
		alpha: {
			"bashrc":            {Data: []byte("bashrc")},
			"config/foo/a":      {Data: []byte("a")},
			"tmpl/vimrc.tmpl":   {Data: []byte("vimrc")},
			"tmpl/gitrc.tmpl":   {Data: []byte("gitrc")},
			"tools/hello/go.go": {Data: []byte("package main")},
			"dotfiles.toml": {Data: []byte(`
[[ignore]]
match = ["tmpl", "tools"]

[[mount]]
src = "config/*"
dst = ".config"

[[template]]
src = "tmpl/vimrc.tmpl"
dst = [".vimrc"]

[[template]]
src = "tmpl/gitrc.tmpl"
dst = [".gitrc"]

[[starlark]]
src = "doc['x'] = 1"
dst = [".config/foo/*.json"]

[[go]]
src = "tools/hello"

[[script]]
src = "true"
`)},
		},
		beta: {
			"profile":       {Data: []byte("profile")},
			"dotfiles.toml": {Data: []byte("")},
		},
	} {
		if err := os.CopyFS(path, fsys); err != nil {
			t.Fatal(err)
		}
	}
	// An installed symlink, which a path selects by its own location rather
	// than its target's.
	if err := os.Symlink(filepath.Join(alpha, "bashrc"), filepath.Join(dst, ".bashrc")); err != nil {
		t.Fatal(err)
	}

	alphaAll := []string{
		"[alpha] go install tools/hello",
		"[alpha] symlink: config/foo -> .config/foo",
		"[alpha] symlink: bashrc -> .bashrc",
		"[alpha] template: tmpl/vimrc.tmpl -> .vimrc",
		"[alpha] template: tmpl/gitrc.tmpl -> .gitrc",
		"[alpha] starlark: .config/foo/*.json",
		"[alpha] run script: true",
	}
	tests := []struct {
		name string
		dir  string // the working directory, relative to reposDir
		args []string
		want []string
	}{
		{name: "all", args: nil, want: append(slices.Clone(alphaAll), "[beta] symlink: profile -> .profile")},
		{name: "repo", args: []string{"alpha"}, want: alphaAll},
		{name: "repo root", dir: "alpha", args: []string{"."}, want: alphaAll},
		{name: "two repos", dir: "alpha", args: []string{"beta", "."}, want: append(slices.Clone(alphaAll), "[beta] symlink: profile -> .profile")},
		{name: "all repos", args: []string{reposDir}, want: append(slices.Clone(alphaAll), "[beta] symlink: profile -> .profile")},
		{name: "subdir", dir: "alpha/tmpl", args: []string{"."}, want: []string{
			"[alpha] template: tmpl/vimrc.tmpl -> .vimrc",
			"[alpha] template: tmpl/gitrc.tmpl -> .gitrc",
		}},
		{name: "template", dir: "alpha", args: []string{"./tmpl/vimrc.tmpl"}, want: []string{
			"[alpha] template: tmpl/vimrc.tmpl -> .vimrc",
		}},
		{name: "go file", dir: "alpha", args: []string{"tools/hello/go.go"}, want: []string{
			"[alpha] go install tools/hello",
		}},
		{name: "inside mounted dir", dir: "alpha", args: []string{"config/foo/a"}, want: []string{
			"[alpha] symlink: config/foo -> .config/foo",
		}},
		{name: "destination", args: []string{filepath.Join(dst, ".vimrc")}, want: []string{
			"[alpha] template: tmpl/vimrc.tmpl -> .vimrc",
		}},
		{name: "installed symlink", args: []string{filepath.Join(dst, ".bashrc")}, want: []string{
			"[alpha] symlink: bashrc -> .bashrc",
		}},
		{name: "starlark glob", args: []string{filepath.Join(dst, ".config/foo/x.json")}, want: []string{
			"[alpha] symlink: config/foo -> .config/foo",
			"[alpha] starlark: .config/foo/*.json",
		}},
		{name: "destination dir", args: []string{filepath.Join(dst, ".config")}, want: []string{
			"[alpha] symlink: config/foo -> .config/foo",
			"[alpha] starlark: .config/foo/*.json",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(filepath.Join(reposDir, tt.dir))
			var stdout, stderr bytes.Buffer
			d := &Dotfiles{Stdout: &stdout, Stderr: &stderr, DstPath: dst, repos: []string{alpha, beta}}
			if err := d.PlanCommand(tt.args); err != nil {
				t.Fatal(err)
			}
			if len(d.runErrors) > 0 {
				t.Fatalf("errors: %v\nstderr:\n%s", d.runErrors, stderr.String())
			}
			var got []string
			for line := range strings.Lines(stdout.String()) {
				got = append(got, strings.TrimPrefix(strings.TrimSuffix(line, "\n"), "plan: "))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}

	t.Run("help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		d := &Dotfiles{Stdout: &stdout, Stderr: &stderr, DstPath: dst, repos: []string{alpha, beta}}
		if err := d.InstallCommand([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("err = %v, want flag.ErrHelp", err)
		}
		if !strings.HasPrefix(stderr.String(), "Usage: dotfiles install") || stdout.Len() > 0 {
			t.Errorf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	})

	t.Run("unknown name", func(t *testing.T) {
		t.Chdir(reposDir)
		d := &Dotfiles{repos: []string{alpha, beta}}
		if _, err := d.parseSelection([]string{"gamma"}); err == nil {
			t.Error("no error for a name that is neither a repository nor a path")
		}
	})

	t.Run("nothing selected", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		d := &Dotfiles{Stdout: &stdout, Stderr: &stderr, DstPath: dst, repos: []string{alpha, beta}}
		if err := d.PlanCommand([]string{"beta", filepath.Join(dst, "nothing")}); err != nil {
			t.Fatal(err)
		}
		if len(d.runErrors) != 1 || !strings.Contains(d.runErrors[0].Error(), "nothing") {
			t.Errorf("errors: %v, want one about the unmatched path", d.runErrors)
		}
		if got := stdout.String(); got != "plan: [beta] symlink: profile -> .profile\n" {
			t.Errorf("stdout = %q", got)
		}
	})
}
