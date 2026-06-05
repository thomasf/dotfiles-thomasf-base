package main

import (
	goformat "go/format"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectModuleName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "valid module",
			content: "module github.com/example/project\n\ngo 1.20\n",
			want:    "github.com/example/project",
			wantErr: false,
		},
		{
			name:    "valid module with spaces",
			content: "module   github.com/example/project\n",
			want:    "  github.com/example/project",
			wantErr: false,
		},
		{
			name:    "no module line",
			content: "go 1.20\n",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("failed to get wd: %v", err)
			}
			defer func() {
				_ = os.Chdir(oldWd)
			}()

			tmpDir := t.TempDir()
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("failed to chdir: %v", err)
			}

			if err := os.WriteFile("go.mod", []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write go.mod: %v", err)
			}

			got, err := detectModuleName()
			if (err != nil) != tt.wantErr {
				t.Errorf("detectModuleName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("detectModuleName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRefactorFile(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		oldMod  string
		newMod  string
		dryRun  bool
		wantSrc string
		wantErr bool
	}{
		{
			name: "rename matching import",
			src: `package foo

import "old/pkg"
`,
			oldMod: "old",
			newMod: "new",
			dryRun: false,
			wantSrc: `package foo

import "new/pkg"
`,
			wantErr: false,
		},
		{
			name: "rename matching sub import",
			src: `package foo

import (
	"fmt"
	"old/pkg/sub"
	"other/pkg"
)
`,
			oldMod: "old",
			newMod: "new",
			dryRun: false,
			wantSrc: `package foo

import (
	"fmt"
	"new/pkg/sub"
	"other/pkg"
)
`,
			wantErr: false,
		},
		{
			name: "no matching imports",
			src: `package foo

import (
	"fmt"
	"other/pkg"
)
`,
			oldMod: "old",
			newMod: "new",
			dryRun: false,
			wantSrc: `package foo

import (
	"fmt"
	"other/pkg"
)
`,
			wantErr: false,
		},
		{
			name: "dry run does not modify file",
			src: `package foo

import "old/pkg"
`,
			oldMod:  "old",
			newMod:  "new",
			dryRun:  true,
			wantSrc: `package foo

import "old/pkg"
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test.go")
			if err := os.WriteFile(tmpFile, []byte(tt.src), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			fset := token.NewFileSet()
			err := refactorFile(fset, tmpFile, tt.oldMod, tt.newMod, tt.dryRun)
			if (err != nil) != tt.wantErr {
				t.Errorf("refactorFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			gotBytes, err := os.ReadFile(tmpFile)
			if err != nil {
				t.Fatalf("failed to read test file after refactor: %v", err)
			}
			gotSrc := string(gotBytes)

			formattedWantSrc := tt.wantSrc
			if !tt.dryRun {
				fBytes, err := goformat.Source([]byte(tt.wantSrc))
				if err == nil {
					formattedWantSrc = string(fBytes)
				}
			}

			if gotSrc != formattedWantSrc {
				t.Errorf("refactorFile() got:\n%s\nwant:\n%s", gotSrc, formattedWantSrc)
			}
		})
	}
}

func TestUpdateGoMod(t *testing.T) {
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	cmd := exec.Command("go", "mod", "init", "testmod")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to initialize dummy go module: %v", err)
	}

	if err := updateGoMod("newmod"); err != nil {
		t.Fatalf("updateGoMod failed: %v", err)
	}

	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}

	if !strings.Contains(string(data), "module newmod") {
		t.Errorf("go.mod does not contain expected module name 'newmod':\n%s", string(data))
	}
}
