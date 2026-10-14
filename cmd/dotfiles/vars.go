package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"text/template/parse"
)

// resolveVars resolves raw, the [vars] seen by one repository, and returns
// their values. Every value is rendered as a template (see renderValue). A
// var may refer to other vars; they are resolved first.
func resolveVars(raw map[string]string, dstRoot string) (map[string]string, error) {
	vars := make(map[string]string, len(raw))
	const (
		resolving = iota + 1
		done
		failed
	)
	state := make(map[string]int, len(raw))
	var errs []error

	// resolve resolves the var name and reports whether it succeeded. path is
	// the chain of vars that led to it, for reporting cycles. Every failure is
	// reported once, where it happens.
	var resolve func(name string, path []string) bool
	resolve = func(name string, path []string) bool {
		switch state[name] {
		case done:
			return true
		case failed:
			return false
		case resolving:
			i := slices.Index(path, name)
			errs = append(errs, fmt.Errorf("vars.%s: reference cycle: %s", name, strings.Join(append(path[i:], name), " -> ")))
			return false
		}
		value := raw[name]
		if !strings.Contains(value, "{{") {
			vars[name] = value
			state[name] = done
			return true
		}
		state[name] = resolving
		path = append(path, name)
		fail := func(err error) bool {
			errs = append(errs, fmt.Errorf("vars.%s: %w", name, err))
			state[name] = failed
			return false
		}
		t, err := parseTemplate("vars."+name, value, dstRoot, vars)
		if err != nil {
			return fail(err)
		}
		deps, err := templateVarRefs(t.Tree.Root)
		if err != nil {
			return fail(err)
		}
		for _, dep := range deps {
			if _, ok := raw[dep]; ok && !resolve(dep, path) {
				state[name] = failed
				return false
			}
		}
		out, err := executeTemplate(t, vars)
		if err != nil {
			return fail(err)
		}
		vars[name] = string(out)
		state[name] = done
		return true
	}

	for _, name := range slices.Sorted(maps.Keys(raw)) {
		resolve(name, nil)
	}
	return vars, errors.Join(errs...)
}

// templateVarRefs returns the names of the vars that the template node refers
// to as .vars.x, $.vars.x, index .vars "x", or vars.x in the argument of
// expr. Other ways of reaching a var, such as {{with .vars}}{{.x}}{{end}},
// are not found.
func templateVarRefs(node parse.Node) ([]string, error) {
	var names []string
	var errs []error
	var walk func(parse.Node)
	walkBranch := func(n *parse.BranchNode) {
		walk(n.Pipe)
		walk(n.List)
		walk(n.ElseList)
	}
	walk = func(node parse.Node) {
		switch n := node.(type) {
		case *parse.ListNode:
			if n == nil {
				return
			}
			for _, c := range n.Nodes {
				walk(c)
			}
		case *parse.ActionNode:
			walk(n.Pipe)
		case *parse.IfNode:
			walkBranch(&n.BranchNode)
		case *parse.RangeNode:
			walkBranch(&n.BranchNode)
		case *parse.WithNode:
			walkBranch(&n.BranchNode)
		case *parse.TemplateNode:
			walk(n.Pipe)
		case *parse.PipeNode:
			if n == nil {
				return
			}
			for _, c := range n.Cmds {
				walk(c)
			}
		case *parse.ChainNode:
			walk(n.Node)
		case *parse.CommandNode:
			if len(n.Args) > 1 {
				if fn, ok := n.Args[0].(*parse.IdentifierNode); ok {
					switch fn.Ident {
					case "index":
						field, ok1 := n.Args[1].(*parse.FieldNode)
						if len(n.Args) > 2 && ok1 && slices.Equal(field.Ident, []string{"vars"}) {
							if s, ok := n.Args[2].(*parse.StringNode); ok {
								names = append(names, s.Text)
							}
						}
					case "expr":
						if s, ok := n.Args[1].(*parse.StringNode); ok {
							refs, err := exprVarRefs(s.Text)
							errs = append(errs, err)
							for _, ref := range refs {
								names = append(names, ref.name)
							}
						}
					}
				}
			}
			for _, a := range n.Args {
				walk(a)
			}
		case *parse.FieldNode:
			if len(n.Ident) > 1 && n.Ident[0] == "vars" {
				names = append(names, n.Ident[1])
			}
		case *parse.VariableNode:
			if len(n.Ident) > 2 && n.Ident[0] == "$" && n.Ident[1] == "vars" {
				names = append(names, n.Ident[2])
			}
		}
	}
	walk(node)
	return names, errors.Join(errs...)
}

// varSource is a definition of a var in a repository's config.
type varSource struct {
	repo  *Repository
	value string
}

// mergeVars returns the [vars] each of repos sees: its own vars, and for vars
// it does not define, the definition of the first repository in order. The
// order is that of the names in the merged [repositories] sections, then the
// path of the repositories not named there. Its own [default_vars] fill in
// what no repository defines; they are not seen by other repositories.
//
// It also returns a warning for every var that is defined differently by
// several repositories and used by one that does not define it, because that
// repository gets only the first definition.
func mergeVars(repos []*Repository) (map[*Repository]map[string]string, []string) {
	var names []string
	for _, r := range repos {
		for _, name := range r.config.Repositories.Names {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	rank := func(r *Repository) int {
		if i := slices.Index(names, filepath.Base(r.srcRoot)); i >= 0 {
			return i
		}
		return len(names)
	}
	ordered := slices.Clone(repos)
	slices.SortStableFunc(ordered, func(a, b *Repository) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.srcRoot, b.srcRoot))
	})

	sources := map[string][]varSource{}
	for _, r := range ordered {
		for name, value := range r.config.Vars {
			sources[name] = append(sources[name], varSource{r, value})
		}
	}

	views := make(map[*Repository]map[string]string, len(repos))
	for _, r := range repos {
		view := maps.Clone(r.config.Vars)
		if view == nil {
			view = map[string]string{}
		}
		for name, srcs := range sources {
			if _, ok := view[name]; !ok {
				view[name] = srcs[0].value
			}
		}
		for name, value := range r.config.DefaultVars {
			if _, ok := view[name]; !ok {
				view[name] = value
			}
		}
		views[r] = view
	}

	var warnings []string
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		srcs := sources[name]
		if len(srcs) == len(repos) || !slices.ContainsFunc(srcs, func(s varSource) bool { return s.value != srcs[0].value }) {
			continue
		}
		var paths []string
		for _, s := range srcs {
			paths = append(paths, s.repo.configPath)
		}
		warnings = append(warnings, fmt.Sprintf("vars.%s is defined differently in %s; repositories that do not define it use %s",
			name, strings.Join(paths, ", "), srcs[0].repo.configPath))
	}
	return views, warnings
}
