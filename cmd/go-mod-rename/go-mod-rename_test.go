package main

import (
	goformat "go/format"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRefactorFile(t *testing.T) {
	tests := []struct {
		name         string
		src          string
		oldMod       string
		newMod       string
		dryRun       bool
		wantSrc      string
		wantModified bool
		wantErr      bool
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
			wantModified: true,
			wantErr:      false,
		},
		{
			name: "prefix match requires a path boundary",
			src: `package foo

import (
	"old/pkg"
	"oldie/pkg"
)
`,
			oldMod: "old",
			newMod: "new",
			dryRun: false,
			wantSrc: `package foo

import (
	"new/pkg"
	"oldie/pkg"
)
`,
			wantModified: true,
			wantErr:      false,
		},
		{
			name: "old path occurring twice is replaced only as a prefix",
			src: `package foo

import "old/pkg/old"
`,
			oldMod: "old",
			newMod: "new",
			dryRun: false,
			wantSrc: `package foo

import "new/pkg/old"
`,
			wantModified: true,
			wantErr:      false,
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
			wantModified: true,
			wantErr:      false,
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
			wantModified: false,
			wantErr:      false,
		},
		{
			name: "dry run does not modify file",
			src: `package foo

import "old/pkg"
`,
			oldMod: "old",
			newMod: "new",
			dryRun: true,
			wantSrc: `package foo

import "old/pkg"
`,
			wantModified: true,
			wantErr:      false,
		},
		{
			name:         "unparseable file is an error",
			src:          "this is not go source\n",
			oldMod:       "old",
			newMod:       "new",
			dryRun:       false,
			wantSrc:      "this is not go source\n",
			wantModified: false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyFS(t, fstest.MapFS{"test.go": &fstest.MapFile{Data: []byte(tt.src)}})
			tmpFile := filepath.Join(dir, "test.go")

			fset := token.NewFileSet()
			modified, err := refactorFile(fset, tmpFile, tt.oldMod, tt.newMod, tt.dryRun)
			if (err != nil) != tt.wantErr {
				t.Errorf("refactorFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if modified != tt.wantModified {
				t.Errorf("refactorFile() modified = %v, want %v", modified, tt.wantModified)
			}

			formattedWantSrc := tt.wantSrc
			if tt.wantModified && !tt.dryRun {
				fBytes, err := goformat.Source([]byte(tt.wantSrc))
				if err == nil {
					formattedWantSrc = string(fBytes)
				}
			}

			assertFS(t, dir, fstest.MapFS{"test.go": &fstest.MapFile{Data: []byte(formattedWantSrc)}})
		})
	}
}

func TestPackageName(t *testing.T) {
	tests := []struct {
		modPath string
		want    string
	}{
		{"example.com/m", "m"},
		{"example.com/m/pkg", "pkg"},
		// A major-version suffix is part of the path but not of the name, so a
		// /vN bump must not look like a renamed package.
		{"example.com/m/v2", "m"},
		{"example.com/m/v10", "m"},
		{"example.com/m/pkg/v3", "pkg"},
		// Only a trailing element counts.
		{"example.com/v2/m", "m"},
		// Not a version suffix.
		{"example.com/m/vendor", "vendor"},
		{"example.com/m/v2x", "v2x"},
		{"m", "m"},
	}

	for _, tt := range tests {
		t.Run(tt.modPath, func(t *testing.T) {
			if got := packageName(tt.modPath); got != tt.want {
				t.Errorf("packageName(%q) = %q, want %q", tt.modPath, got, tt.want)
			}
		})
	}
}

func TestUpdateGoMod(t *testing.T) {
	chdirFS(t, fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module testmod\n\ngo 1.25.0\n")},
	})

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
