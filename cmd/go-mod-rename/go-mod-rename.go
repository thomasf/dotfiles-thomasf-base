package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	oldPathFlag := flag.String("old", "", "Old module path (auto-detected if empty)")
	newPathFlag := flag.String("new", "", "New module path (required)")
	forceFlag := flag.Bool("f", false, "Force execution")
	dryRunFlag := flag.Bool("dry-run", false, "Preview which files would change")

	flag.Parse()

	if *newPathFlag == "" {
		fmt.Println("Error: -new path is required.")
		os.Exit(1)
	}

	oldPath := *oldPathFlag
	if oldPath == "" {
		detected, err := detectModuleName()
		if err != nil {
			fmt.Printf("Error: Could not auto-detect module name: %v\n", err)
			os.Exit(1)
		}
		oldPath = detected
	}

	if !*forceFlag && !*dryRunFlag {
		fmt.Printf("Detected module: %s\nUse -f to apply changes to -new %s, or -dry-run to preview.\n", oldPath, *newPathFlag)
		os.Exit(1)
	}

	if *dryRunFlag {
		fmt.Printf("[Dry-Run] Target: %s -> %s\n", oldPath, *newPathFlag)
	} else {
		if err := updateGoMod(*newPathFlag); err != nil {
			fmt.Printf("Error updating go.mod: %v\n", err)
			os.Exit(1)
		}
	}

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		if strings.Contains(path, "/vendor/") || strings.HasPrefix(path, ".git/") {
			return nil
		}
		return refactorFile(fset, path, oldPath, *newPathFlag, *dryRunFlag)
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println("Finished.")
	}
}

func detectModuleName() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimPrefix(line, "module "), nil
		}
	}
	return "", fmt.Errorf("no module name found in go.mod")
}

func refactorFile(fset *token.FileSet, path, old, new string, dryRun bool) error {
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	modified := false
	for _, imp := range file.Imports {
		val := strings.Trim(imp.Path.Value, `"`)
		if val == old || strings.HasPrefix(val, old+"/") {
			newVal := strings.Replace(val, old, new, 1)
			imp.Path.Value = fmt.Sprintf(`"%s"`, newVal)
			modified = true
		}
	}

	if !modified {
		return nil
	}

	if dryRun {
		fmt.Printf("[Dry-Run] Would update imports in: %s\n", path)
		return nil
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return err
	}

	fmt.Printf("Updating: %s\n", path)
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func updateGoMod(newPath string) error {
	fmt.Printf("Updating go.mod module path to: %s\n", newPath)
	return exec.Command("go", "mod", "edit", "-module", newPath).Run()
}
