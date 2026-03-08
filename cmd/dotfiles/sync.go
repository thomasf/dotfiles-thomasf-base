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

				root := strings.Split(rel, string(os.PathSeparator))[0]
				actualDst := filepath.Join(pkg.Dst, filepath.Base(match))
				if rel == root || actualDst == r.dotName(root) || strings.HasPrefix(actualDst, r.dotName(root)+string(os.PathSeparator)) {
					processed[root] = true
				}

				if !pkg.ShouldRun(r) {
					continue
				}

				if r.IsIgnored(match) {
					continue
				}

				actions = append(actions, &SymLinker{
					RepoName: r.name,
					SrcRoot:  r.srcPath,
					Src:      rel,
					DstRoot:  r.dstPath,
					Dst:      filepath.Join(pkg.Dst, filepath.Base(match)),
					Force:    r.force,
				})
			}
		} else {
			srcRel := filepath.Clean(pkg.Src)
			root := strings.Split(srcRel, string(os.PathSeparator))[0]

			if srcRel == root || pkg.Dst == r.dotName(root) || strings.HasPrefix(pkg.Dst, r.dotName(root)+string(os.PathSeparator)) {
				processed[root] = true
			}

			if !pkg.ShouldRun(r) {
				continue
			}

			src := filepath.Join(r.srcPath, srcRel)
			if r.IsIgnored(src) {
				continue
			}

			actions = append(actions, &SymLinker{
				RepoName: r.name,
				SrcRoot:  r.srcPath,
				Src:      srcRel,
				DstRoot:  r.dstPath,
				Dst:      pkg.Dst,
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
	targetName := r.dotName(name)

	return &SymLinker{
		RepoName: r.name,
		SrcRoot:  r.srcPath,
		Src:      name,
		DstRoot:  r.dstPath,
		Dst:      targetName,
		Force:    r.force,
	}, nil
}

func (r *Repository) dotName(name string) string {
	if strings.HasPrefix(name, ".") || name == "." {
		return name
	}
	return "." + name
}
