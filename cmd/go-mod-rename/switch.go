package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// goMod mirrors the parts of "go mod edit -json" this tool cares about.
type goMod struct {
	Module  goModModule
	Require []goModRequire
	Replace []goModReplace
}

type goModModule struct {
	Path string
}

type goModRequire struct {
	Path     string
	Version  string
	Indirect bool
}

type goModVersion struct {
	Path    string
	Version string
}

type goModReplace struct {
	Old goModVersion
	New goModVersion
}

func switchUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "Usage: go-mod-rename switch -old <module path> -new <module path> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Rewrites the imports of a dependency that moved, plus the require and\n")
		fmt.Fprintf(os.Stderr, "replace directives naming it in go.mod.\n\n")
		fs.PrintDefaults()
	}
}

func runSwitch(args []string) {
	fs := flag.NewFlagSet("switch", flag.ExitOnError)
	oldPathFlag := fs.String("old", "", "Old module path of the dependency (required)")
	newPathFlag := fs.String("new", "", "New module path of the dependency (required)")
	versionFlag := fs.String("version", "", "Version to require for the new path (default: the version currently required for the old path)")
	dirFlag := fs.String("C", ".", "Directory to operate in")
	forceFlag := fs.Bool("f", false, "Apply changes")
	dryRunFlag := fs.Bool("dry-run", false, "Preview which files would change")
	noTidyFlag := fs.Bool("no-tidy", false, "Do not run 'go mod tidy' afterwards")
	fs.Usage = switchUsage(fs)
	_ = fs.Parse(args)

	if *oldPathFlag == "" || *newPathFlag == "" {
		fmt.Fprintln(os.Stderr, "Error: both -old and -new are required.")
		fs.Usage()
		os.Exit(1)
	}
	if *oldPathFlag == *newPathFlag {
		fmt.Fprintln(os.Stderr, "Error: -old and -new are identical.")
		os.Exit(1)
	}

	if err := os.Chdir(*dirFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	mod, err := readGoMod()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading go.mod: %v\n", err)
		os.Exit(1)
	}
	if mod.Module.Path == *oldPathFlag {
		fmt.Fprintf(os.Stderr, "Error: %s is this project's own module; use the rename command instead.\n", *oldPathFlag)
		os.Exit(1)
	}

	edits := buildModEdits(mod, *oldPathFlag, *newPathFlag, *versionFlag)

	if !*forceFlag && !*dryRunFlag {
		fmt.Printf("Project module: %s\nUse -f to rewrite %s -> %s, or -dry-run to preview.\n", mod.Module.Path, *oldPathFlag, *newPathFlag)
		os.Exit(1)
	}

	if len(edits) == 0 {
		fmt.Printf("Note: %s is not required or replaced in go.mod; only imports are rewritten.\n", *oldPathFlag)
	} else if *dryRunFlag {
		fmt.Printf("[Dry-Run] Would run: go mod edit %s\n", strings.Join(edits, " "))
	} else {
		fmt.Printf("Updating go.mod: go mod edit %s\n", strings.Join(edits, " "))
		if err := runGoCommand(append([]string{"mod", "edit"}, edits...)...); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating go.mod: %v\n", err)
			os.Exit(1)
		}
	}

	changed, err := rewriteImports(".", *oldPathFlag, *newPathFlag, *dryRunFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *noTidyFlag {
		fmt.Println("Skipping 'go mod tidy'; go.sum is probably stale.")
		// "go mod edit -require" cannot set the marker back, so only tidy can
		// restore it.
		if req, ok := findRequire(mod, *oldPathFlag); ok && req.Indirect {
			fmt.Printf("Note: %s was an indirect requirement; %s is now recorded as a direct one.\n",
				*oldPathFlag, *newPathFlag)
		}
	} else if *dryRunFlag {
		fmt.Println("[Dry-Run] Would run: go mod tidy")
	} else if err := runGoCommand("mod", "tidy"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: 'go mod tidy' failed: %v\n", err)
	}

	warnPackageName(*oldPathFlag, *newPathFlag)
	fmt.Printf("Finished. %d file(s) with updated imports.\n", changed)
}

// buildModEdits returns the "go mod edit" flags needed to point every require
// and replace directive mentioning oldPath at newPath.
func buildModEdits(mod *goMod, oldPath, newPath, version string) []string {
	var edits []string

	if req, ok := findRequire(mod, oldPath); ok {
		v := version
		if v == "" {
			v = req.Version
		}
		edits = append(edits,
			"-droprequire="+oldPath,
			fmt.Sprintf("-require=%s@%s", newPath, v))
	} else if version != "" {
		edits = append(edits, fmt.Sprintf("-require=%s@%s", newPath, version))
	}

	for _, r := range mod.Replace {
		switch {
		case r.Old.Path == oldPath:
			target := r.New.Path
			if target == oldPath {
				target = newPath
			}
			edits = append(edits,
				"-dropreplace="+withVersion(oldPath, r.Old.Version),
				fmt.Sprintf("-replace=%s=%s",
					withVersion(newPath, r.Old.Version),
					withVersion(target, r.New.Version)))
		case r.New.Path == oldPath:
			edits = append(edits,
				fmt.Sprintf("-replace=%s=%s",
					withVersion(r.Old.Path, r.Old.Version),
					withVersion(newPath, r.New.Version)))
		}
	}

	return edits
}

func findRequire(mod *goMod, path string) (goModRequire, bool) {
	for _, r := range mod.Require {
		if r.Path == path {
			return r, true
		}
	}
	return goModRequire{}, false
}

func withVersion(path, version string) string {
	if version == "" {
		return path
	}
	return path + "@" + version
}

func readGoMod() (*goMod, error) {
	out, err := exec.Command("go", "mod", "edit", "-json").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("%s", exitErr.Stderr)
		}
		return nil, err
	}
	var mod goMod
	if err := json.Unmarshal(out, &mod); err != nil {
		return nil, err
	}
	return &mod, nil
}
