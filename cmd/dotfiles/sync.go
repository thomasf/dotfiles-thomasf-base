package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (r *Repository) Sync() ([]Action, error) {
	var actions []Action
	var errs []error
	processed := make(map[string]bool)

	for _, mount := range r.config.Mount {
		isGlob := strings.ContainsAny(mount.Src, "*?[")

		if isGlob {
			pattern := filepath.Join(r.srcRoot, mount.Src)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				errs = append(errs, fmt.Errorf("glob error for mount src %s: %w", mount.Src, err))
				continue
			}

			for _, match := range matches {
				rel, err := filepath.Rel(r.srcRoot, match)
				if err != nil {
					errs = append(errs, fmt.Errorf("rel path error for match %s: %w", match, err))
					continue
				}

				root := strings.Split(rel, string(os.PathSeparator))[0]
				actualDst := filepath.Join(mount.Dst, filepath.Base(match))
				if rel == root || actualDst == r.dotName(root) || strings.HasPrefix(actualDst, r.dotName(root)+string(os.PathSeparator)) {
					processed[root] = true
				}

				if !mount.ShouldRun(r) {
					continue
				}

				if r.IsIgnored(match) {
					continue
				}

				if r.copy {
					actions = append(actions, &CopyFile{
						SrcRoot: r.srcRoot,
						Src:     rel,
						DstRoot: r.dstRoot,
						Dst:     filepath.Join(mount.Dst, filepath.Base(match)),
						Force:   r.force,
					})
				} else {
					actions = append(actions, &SymLinker{
						SrcRoot: r.srcRoot,
						Src:     rel,
						DstRoot: r.dstRoot,
						Dst:     filepath.Join(mount.Dst, filepath.Base(match)),
						Force:   r.force,
					})
				}
			}
		} else {
			srcRel := filepath.Clean(mount.Src)
			root := strings.Split(srcRel, string(os.PathSeparator))[0]

			if srcRel == root || mount.Dst == r.dotName(root) || strings.HasPrefix(mount.Dst, r.dotName(root)+string(os.PathSeparator)) {
				processed[root] = true
			}

			if !mount.ShouldRun(r) {
				continue
			}

			src := filepath.Join(r.srcRoot, srcRel)
			if r.IsIgnored(src) {
				continue
			}

			if r.copy {
				actions = append(actions, &CopyFile{
					SrcRoot: r.srcRoot,
					Src:     srcRel,
					DstRoot: r.dstRoot,
					Dst:     mount.Dst,
					Force:   r.force,
				})
			} else {
				actions = append(actions, &SymLinker{
					SrcRoot: r.srcRoot,
					Src:     srcRel,
					DstRoot: r.dstRoot,
					Dst:     mount.Dst,
					Force:   r.force,
				})
			}
		}
	}

	entries, err := os.ReadDir(r.srcRoot)
	if err != nil {
		errs = append(errs, fmt.Errorf("readdir error for srcRoot %s: %w", r.srcRoot, err))
	} else {
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(r.srcRoot, name)
			if r.IsIgnored(path) {
				continue
			}

			if processed[name] || processed["."] {
				continue
			}

			action, err := r.syncRootItem(name)
			if err != nil {
				errs = append(errs, fmt.Errorf("sync error for root item %s: %w", name, err))
				continue
			}
			if action != nil {
				actions = append(actions, action)
			}
		}
	}

	if r.config.Git != nil {
		actions = append(actions, &GitConfigAction{
			SrcRoot: r.srcRoot,
			Config:  r.config.Git,
		})
	}

	for _, s := range r.config.Script {
		if (s.Phase == "" || s.Phase == "default") && s.ShouldRun(r) {
			actions = append(actions, &ScriptAction{
				SrcRoot: r.srcRoot,
				Script:  s.Src,
			})
		}
	}

	return actions, errors.Join(errs...)
}

func (r *Repository) GoInstall() []Action {
	var actions []Action
	if stat, err := os.Stat(filepath.Join(r.srcRoot, "cmd")); err == nil && stat.IsDir() {
		actions = append(actions, &GoInstallAction{
			SrcRoot: r.srcRoot,
		})
	}
	return actions
}

func (r *Repository) PreScript() []Action {
	var actions []Action
	for _, s := range r.config.Script {
		if s.Phase == "pre" && s.ShouldRun(r) {
			actions = append(actions, &ScriptAction{
				SrcRoot: r.srcRoot,
				Script:  s.Src,
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
				SrcRoot: r.srcRoot,
				Script:  s.Src,
			})
		}
	}
	return actions
}

func (r *Repository) syncRootItem(name string) (Action, error) {
	targetName := r.dotName(name)

	if r.copy {
		return &CopyFile{
			SrcRoot: r.srcRoot,
			Src:     name,
			DstRoot: r.dstRoot,
			Dst:     targetName,
			Force:   r.force,
		}, nil
	}
	return &SymLinker{
		SrcRoot: r.srcRoot,
		Src:     name,
		DstRoot: r.dstRoot,
		Dst:     targetName,
		Force:   r.force,
	}, nil
}

func (r *Repository) dotName(name string) string {
	if strings.HasPrefix(name, ".") || name == "." {
		return name
	}
	return "." + name
}
