package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A selection limits install and plan to the actions selected by their
// arguments. Every repository is still loaded and planned, so the variables
// are the same as in a full install. An empty selection selects everything.
type selection []*selector

// A selector is one argument of install or plan.
type selector struct {
	arg     string
	paths   []string // the absolute path of arg, as realParent and realPath
	matched bool     // whether it selected an action
}

// parseSelection parses the arguments of install and plan. An argument that
// is . or .. or contains a / (or \ on Windows) is a path. Otherwise it is
// the name of a repository, which stands for the repository's directory, or
// if no repository has that name, an existing path.
func (d *Dotfiles) parseSelection(args []string) (selection, error) {
	var sel selection
	for _, arg := range args {
		path := arg
		if arg != "." && arg != ".." && !strings.ContainsAny(arg, "/"+string(filepath.Separator)) {
			if i := slices.IndexFunc(d.repos, func(r string) bool { return filepath.Base(r) == arg }); i >= 0 {
				path = d.repos[i]
			} else if _, err := os.Lstat(arg); err != nil {
				return nil, fmt.Errorf("%s is neither a repository nor a path", arg)
			}
		}
		paths := slices.Compact([]string{realParent(path), realPath(path)})
		sel = append(sel, &selector{arg: arg, paths: paths})
	}
	return sel, nil
}

// filter returns the actions of r that sel selects, and marks the selectors
// that select any of them.
//
// A path selects all actions of the repositories inside it, and the [Pather]
// actions with a path that overlaps it. Actions that are not Pathers, such as
// scripts and git settings, belong to the repository as a whole.
func (sel selection) filter(r *Repository, actions []Action) []Action {
	if len(sel) == 0 {
		return actions
	}
	root := realPath(r.srcRoot)
	var selected []Action
	for _, a := range actions {
		var paths []string
		if p, ok := a.(Pather); ok {
			for _, path := range p.Paths() {
				paths = append(paths, realParent(path))
			}
		}
		keep := false
		for _, s := range sel {
			if s.selects(root, paths) {
				s.matched, keep = true, true
			}
		}
		if keep {
			selected = append(selected, a)
		}
	}
	return selected
}

// selects reports whether s selects an action with the paths of the
// repository root.
func (s *selector) selects(root string, paths []string) bool {
	return slices.ContainsFunc(s.paths, func(sp string) bool {
		return within(root, sp) || slices.ContainsFunc(paths, func(p string) bool { return overlaps(p, sp) })
	})
}

// overlaps reports whether the action path p, which may be a glob, is the
// selected path sp, is inside it or contains it. Containing it selects, for
// example, the symlink of a directory with a selected file in it.
func overlaps(p, sp string) bool {
	if ok, _ := filepath.Match(p, sp); ok {
		return true
	}
	return within(p, sp) || within(sp, p)
}

// unmatched returns the arguments that selected no action.
func (sel selection) unmatched() []string {
	var args []string
	for _, s := range sel {
		if !s.matched {
			args = append(args, s.arg)
		}
	}
	return args
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && filepath.IsLocal(rel)
}

// realPath returns p as an absolute path with symlinks resolved. The part of
// p that does not exist is kept as it is.
func realPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if dir := filepath.Dir(p); dir != p {
		return filepath.Join(realPath(dir), filepath.Base(p))
	}
	return p
}

// realParent is like realPath, but keeps the last element of p as it is, so
// that the path of an installed symlink stays where it is rather than
// pointing into a repository.
func realParent(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Join(realPath(filepath.Dir(p)), filepath.Base(p))
}
