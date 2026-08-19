package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func renameUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "Usage: go-mod-rename rename -new <module path> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Rewrites the module path in go.mod and every import of its own packages.\n\n")
		fs.PrintDefaults()
	}
}

func runRename(args []string) {
	fs := flag.NewFlagSet("rename", flag.ExitOnError)
	oldPathFlag := fs.String("old", "", "Old module path (auto-detected from go.mod if empty)")
	newPathFlag := fs.String("new", "", "New module path (required)")
	dirFlag := fs.String("C", ".", "Directory to operate in")
	forceFlag := fs.Bool("f", false, "Apply changes")
	dryRunFlag := fs.Bool("dry-run", false, "Preview which files would change")
	fs.Usage = renameUsage(fs)
	_ = fs.Parse(args)

	newPath := strings.TrimRight(strings.TrimSpace(*newPathFlag), "/")
	oldPath := strings.TrimRight(strings.TrimSpace(*oldPathFlag), "/")

	if newPath == "" {
		fmt.Fprintln(os.Stderr, "Error: -new path is required.")
		fs.Usage()
		os.Exit(1)
	}

	if *forceFlag && *dryRunFlag {
		fmt.Fprintln(os.Stderr, "Error: cannot specify both -f and -dry-run.")
		os.Exit(1)
	}

	if err := os.Chdir(*dirFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if oldPath == "" {
		mod, err := readGoMod()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Could not auto-detect module name: %v\n", err)
			os.Exit(1)
		}
		if mod.Module.Path == "" {
			fmt.Fprintln(os.Stderr, "Error: go.mod has no module directive; pass -old explicitly.")
			os.Exit(1)
		}
		oldPath = strings.TrimRight(mod.Module.Path, "/")
	}

	if oldPath == newPath {
		fmt.Printf("Module path is already %s; nothing to do.\n", oldPath)
		return
	}

	if !*forceFlag && !*dryRunFlag {
		fmt.Fprintf(os.Stderr, "Detected module: %s\nUse -f to apply changes to -new %s, or -dry-run to preview.\n", oldPath, newPath)
		os.Exit(1)
	}

	if *dryRunFlag {
		fmt.Printf("[Dry-Run] Target: %s -> %s\n", oldPath, newPath)
		fmt.Printf("[Dry-Run] Would run: go mod edit -module %s\n", newPath)
	} else if err := updateGoMod(newPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating go.mod: %v\n", err)
		os.Exit(1)
	}

	changed, err := rewriteImports(".", oldPath, newPath, *dryRunFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	warnPackageName(oldPath, newPath)
	fmt.Printf("Finished. %d file(s) with updated imports.\n", changed)
}

func updateGoMod(newPath string) error {
	fmt.Printf("Updating go.mod module path to: %s\n", newPath)
	return runGoCommand("mod", "edit", "-module", newPath)
}

func runGoCommand(args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
