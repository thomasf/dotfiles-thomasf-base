package main

import (
	"os"
	"path/filepath"
	"strings"
)

func (r *Repository) Sync() ([]Action, error) {
	var actions []Action

	if _, err := os.Stat(filepath.Join(r.srcPath, "go.mod")); err == nil {
		actions = append(actions, &GoInstallAction{
			RepoPath: r.srcPath,
		})
	}

	processed := make(map[string]bool)

	for _, pkg := range r.config.Mount {
		isGlob := strings.ContainsAny(pkg.Src, "*?[")

		if isGlob {
			pattern := filepath.Join(r.srcPath, pkg.Src)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, err
			}

			for _, match := range matches {
				rel, err := filepath.Rel(r.srcPath, match)
				if err != nil {
					return nil, err
				}

				parts := strings.Split(rel, string(os.PathSeparator))
				if len(parts) > 0 {
					processed[parts[0]] = true
				}

				if !pkg.ShouldRun(r) {
					continue
				}

				if r.IsIgnored(match) {
					continue
				}

				dst := filepath.Join(r.dstPath, pkg.Dst, filepath.Base(match))

				actions = append(actions, &SymLinker{
					RepoName: r.name,
					Src:      match,
					SrcRel:   rel,
					Dst:      dst,
					Force:    r.force,
				})
			}
		} else {
			parts := strings.Split(pkg.Src, string(os.PathSeparator))
			if len(parts) > 0 {
				processed[parts[0]] = true
			}

			if !pkg.ShouldRun(r) {
				continue
			}

			src := filepath.Join(r.srcPath, pkg.Src)
			if r.IsIgnored(src) {
				continue
			}

			dst := filepath.Join(r.dstPath, pkg.Dst)

			actions = append(actions, &SymLinker{
				RepoName: r.name,
				Src:      src,
				SrcRel:   pkg.Src,
				Dst:      dst,
				Force:    r.force,
			})
		}
	}

	entries, err := os.ReadDir(r.srcPath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(r.srcPath, name)
		if r.IsIgnored(path) {
			continue
		}

		if processed[name] || processed["."] {
			continue
		}

		action, err := r.syncRootItem(name)
		if err != nil {
			return nil, err
		}
		if action != nil {
			actions = append(actions, action)
		}
	}

	if r.config.Git != nil {
		actions = append(actions, &GitConfigAction{
			Config: r.config.Git,
		})
	}

	for _, s := range r.config.Script {
		if (s.Phase == "" || s.Phase == "default") && s.ShouldRun(r) {
			actions = append(actions, &ScriptAction{
				RepoName: r.name,
				RepoPath: r.srcPath,
				Script:   s.Src,
			})
		}
	}

	return actions, nil
}

func (r *Repository) PreScript() []Action {
	var actions []Action
	for _, s := range r.config.Script {
		if s.Phase == "pre" && s.ShouldRun(r) {
			actions = append(actions, &ScriptAction{
				RepoName: r.name,
				RepoPath: r.srcPath,
				Script:   s.Src,
			})
		}
	}
	return actions
}

func (r *Repository) PostScript() []Action {
	var actions []Action
	for _, s := range r.config.Script {
		if s.Phase == "post" && s.ShouldRun(r) {
			actions = append(actions, &ScriptAction{
				RepoName: r.name,
				RepoPath: r.srcPath,
				Script:   s.Src,
			})
		}
	}
	return actions
}

func (r *Repository) syncRootItem(name string) (Action, error) {
	src := filepath.Join(r.srcPath, name)

	targetName := name
	if !strings.HasPrefix(targetName, ".") {
		targetName = "." + targetName
	}
	dst := filepath.Join(r.dstPath, targetName)

	return &SymLinker{
		RepoName: r.name,
		Src:      src,
		SrcRel:   name,
		Dst:      dst,
		Force:    r.force,
	}, nil
}
