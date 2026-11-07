package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
)

// LintCommand validates configurations, template syntax, referenced source files,
// and destination conflicts across repositories.
func (d *Dotfiles) LintCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles lint", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles lint [repo...]\n\n")
		fmt.Fprintf(d.Stderr, "Validate configurations, templates, and referenced files across repositories.\n\n")
		fmt.Fprintf(d.Stderr, "Checks dotfiles.toml for valid keys, syntax, and expression conditions;\n")
		fmt.Fprintf(d.Stderr, "verifies that mount and template source files exist; validates Go template\n")
		fmt.Fprintf(d.Stderr, "and Starlark syntax; and checks for conflicting destination targets.\n")
		fmt.Fprintf(d.Stderr, "Without arguments, all repositories are validated.\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	selectedRepos := fs.Args()

	repos := d.loadRepos(allParts, WithForce(d.Force), WithCopy(d.Copy), WithEvent(eventInstall))
	if len(selectedRepos) > 0 {
		var filtered []*Repository
		for _, name := range selectedRepos {
			idx := slices.IndexFunc(repos, func(r *Repository) bool {
				return filepath.Base(r.srcRoot) == name || r.srcRoot == name
			})
			if idx < 0 {
				return fmt.Errorf("lint: repository %q not found", name)
			}
			filtered = append(filtered, repos[idx])
		}
		repos = filtered
	}

	var issues []string

	destMap := make(map[string][]Action)

	for _, r := range repos {
		repoName := filepath.Base(r.srcRoot)

		if r.configPath == "" {
			continue
		}

		// 1. Validate mount sources
		for _, m := range r.config.Mount {
			if !m.ShouldRun(r) {
				continue
			}
			if strings.ContainsAny(m.Src, "*?[") {
				matches, err := filepath.Glob(filepath.Join(r.srcRoot, m.Src))
				if err != nil {
					issues = append(issues, fmt.Sprintf("[%s] mount %q: glob error: %v", repoName, m.Src, err))
				} else if len(matches) == 0 {
					issues = append(issues, fmt.Sprintf("[%s] mount %q: no matching files found", repoName, m.Src))
				}
			} else {
				srcPath := filepath.Join(r.srcRoot, m.Src)
				if _, err := os.Lstat(srcPath); err != nil {
					issues = append(issues, fmt.Sprintf("[%s] mount %q: source file does not exist", repoName, m.Src))
				}
			}
		}

		// 2. Validate template sources & syntax
		for _, t := range r.config.Template {
			if !t.ShouldRun(r) {
				continue
			}
			srcPath := filepath.Join(r.srcRoot, t.Src)
			content, err := os.ReadFile(srcPath)
			if err != nil {
				issues = append(issues, fmt.Sprintf("[%s] template %q: cannot read source: %v", repoName, t.Src, err))
				continue
			}
			tmpl := template.New(t.Src).Funcs(templateFuncs(r.evalContext()))
			if _, err := tmpl.Parse(string(content)); err != nil {
				issues = append(issues, fmt.Sprintf("[%s] template %q: syntax error: %v", repoName, t.Src, err))
			}
		}

		// 3. Validate starlark entries
		for _, s := range r.config.Starlark {
			if !s.ShouldRun(r) {
				continue
			}
			if s.Src != "" {
				srcPath := filepath.Join(r.srcRoot, s.Src)
				if info, err := os.Lstat(srcPath); err == nil && info.Mode().IsRegular() {
					code, err := os.ReadFile(srcPath)
					if err != nil {
						issues = append(issues, fmt.Sprintf("[%s] starlark %q: cannot read source: %v", repoName, s.Src, err))
					} else if _, err := compileStarlark(s.Src, string(code)); err != nil {
						issues = append(issues, fmt.Sprintf("[%s] starlark %q: compile error: %v", repoName, s.Src, err))
					}
				}
			}
		}

		// 4. Check action destinations for collisions
		actions, err := r.Sync()
		if err != nil {
			issues = append(issues, fmt.Sprintf("[%s] sync planning error: %v", repoName, err))
		}
		for _, a := range actions {
			if t, ok := a.(Targeter); ok {
				dst := t.DstAbsolutePath()
				if dst != "" {
					destMap[dst] = append(destMap[dst], a)
				}
			}
		}
	}

	// Report destination collisions
	for dst, acts := range destMap {
		if hasCollision(acts) {
			var actStrs []string
			for _, a := range acts {
				actStrs = append(actStrs, a.String())
			}
			issues = append(issues, fmt.Sprintf("multiple actions target same destination %s: %s", dst, strings.Join(actStrs, ", ")))
		}
	}

	if len(d.runErrors) > 0 || len(issues) > 0 {
		if len(issues) > 0 {
			fmt.Fprintf(d.Stderr, "lint: %d issue(s) found:\n", len(issues))
			for _, issue := range issues {
				fmt.Fprintf(d.Stderr, "  - %s\n", issue)
				d.collectErr(fmt.Errorf("lint: %s", issue))
			}
		}
		return fmt.Errorf("lint: failed")
	}

	fmt.Fprintf(d.Stdout, "all %d repositories ok\n", len(repos))
	return nil
}
