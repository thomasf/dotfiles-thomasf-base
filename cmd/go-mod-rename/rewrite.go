package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// rewriteImports walks root and rewrites every import of oldPath, or of a
// package below it, to newPath. Files that fail to parse are reported and
// skipped. It returns the number of files that changed.
func rewriteImports(root, oldPath, newPath string, dryRun bool) (int, error) {
	fset := token.NewFileSet()
	changed := 0

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}

		modified, err := refactorFile(fset, p, oldPath, newPath, dryRun)
		if err != nil {
			fmt.Printf("Skipping %s: %v\n", p, err)
			return nil
		}
		if modified {
			changed++
		}
		return nil
	})

	return changed, err
}

func skipDir(name string) bool {
	if name == "vendor" || name == "node_modules" {
		return true
	}
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

// refactorFile rewrites the imports of a single file, reporting whether it
// contained anything to change.
func refactorFile(fset *token.FileSet, path, old, new string, dryRun bool) (bool, error) {
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return false, err
	}

	modified := false
	for _, imp := range file.Imports {
		val := strings.Trim(imp.Path.Value, `"`)
		if val == old || strings.HasPrefix(val, old+"/") {
			newVal := new + strings.TrimPrefix(val, old)
			imp.Path.Value = fmt.Sprintf(`"%s"`, newVal)
			modified = true
		}
	}

	if !modified {
		return false, nil
	}

	if dryRun {
		fmt.Printf("[Dry-Run] Would update imports in: %s\n", path)
		return true, nil
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return false, err
	}

	fmt.Printf("Updating: %s\n", path)
	return true, os.WriteFile(path, buf.Bytes(), 0644)
}

// majorVersionSuffix matches the trailing major-version element of a module
// path, which is part of the path but not of the package name.
var majorVersionSuffix = regexp.MustCompile(`/v[0-9]+$`)

// packageName returns the identifier an unaliased import of modPath binds.
func packageName(modPath string) string {
	return path.Base(majorVersionSuffix.ReplaceAllString(modPath, ""))
}

// warnPackageName reports paths whose package name changed, since unaliased
// imports of those packages keep referring to the old identifier.
func warnPackageName(oldPath, newPath string) {
	oldName, newName := packageName(oldPath), packageName(newPath)
	if oldName != newName {
		fmt.Printf("Note: the package name changed (%s -> %s); package identifiers in the source are not renamed.\n",
			oldName, newName)
	}
}
