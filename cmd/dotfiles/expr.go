package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// An evalContext is what conditions, templates and [[starlark]] code are
// evaluated with.
type evalContext struct {
	dstRoot string            // relative paths in functions are relative to it
	vars    map[string]string // the resolved [vars] of a repository
}

// exprVars returns the variables available in condition expressions and
// templates. ec.vars is available as vars. Its values are copied into a
// map[string]any, so that a missing var is nil rather than "", which ??
// needs.
func exprVars(ec evalContext) map[string]any {
	hostname, _ := os.Hostname()
	m := make(map[string]any, len(ec.vars))
	for name, v := range ec.vars {
		m[name] = v
	}
	return map[string]any{
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"hostname": hostname,
		"vars":     m,
	}
}

// exprFuncs returns the functions available in condition expressions and
// templates:
//
//   - env: os.Getenv.
//   - lookPath: the path of the first of its arguments found in PATH, or ""
//     if none is found. A name containing a slash is not looked up in PATH but
//     resolved like a path.
//   - exists, isFile, isDir: whether a path exists, is a regular file or is a
//     directory, following symlinks.
//   - parseBool: strconv.ParseBool, to use a var as a bool.
//
// Paths are resolved by resolvePath, never against the working directory. Not
// finding something is a value, not an error, so that conditions and
// templates can test for it.
func exprFuncs(dstRoot string) map[string]any {
	stat := func(path string) (fs.FileInfo, bool) {
		info, err := os.Stat(resolvePath(dstRoot, path))
		return info, err == nil
	}
	return map[string]any{
		"env":       os.Getenv,
		"parseBool": strconv.ParseBool,
		"lookPath": func(names ...string) string {
			for _, name := range names {
				if strings.Contains(name, "/") {
					name = resolvePath(dstRoot, name)
				}
				if path, err := exec.LookPath(name); err == nil {
					return path
				}
			}
			return ""
		},
		"exists": func(path string) bool {
			_, ok := stat(path)
			return ok
		},
		"isFile": func(path string) bool {
			info, ok := stat(path)
			return ok && info.Mode().IsRegular()
		},
		"isDir": func(path string) bool {
			info, ok := stat(path)
			return ok && info.IsDir()
		},
	}
}

// resolvePath returns path as an absolute path. A relative path is relative
// to dstRoot.
func resolvePath(dstRoot, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dstRoot, path)
}

// exprEnv returns the expr environment: the variables from exprVars and the
// functions from exprFuncs.
func exprEnv(ec evalContext) map[string]any {
	env := exprVars(ec)
	maps.Copy(env, exprFuncs(ec.dstRoot))
	return env
}

// evalExpr compiles and runs the expr expression src in env. A reference
// vars.x to a var that is not defined is an error, unless it is the left
// operand of ??, as in vars.x ?? "default".
func evalExpr(env map[string]any, src string, opts ...expr.Option) (any, error) {
	program, err := expr.Compile(src, append([]expr.Option{expr.Env(env)}, opts...)...)
	if err != nil {
		return nil, err
	}
	if vars, ok := env["vars"].(map[string]any); ok {
		refs, err := exprVarRefs(src)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if _, ok := vars[ref.name]; !ok && !ref.optional {
				return nil, fmt.Errorf("undefined var vars.%s (use vars.%[1]s ?? default if it is optional)", ref.name)
			}
		}
	}
	return expr.Run(program, env)
}

// varRef is a reference vars.name in an expression. It is optional if it is
// the left operand of ??.
type varRef struct {
	name     string
	optional bool
}

// exprVarRefs returns the references vars.x and vars["x"] in the expression
// src.
func exprVarRefs(src string) ([]varRef, error) {
	tree, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	v := &varRefVisitor{optional: map[ast.Node]bool{}}
	ast.Walk(&tree.Node, v)
	refs := make([]varRef, len(v.members))
	for i, m := range v.members {
		refs[i] = varRef{
			name:     m.Property.(*ast.StringNode).Value,
			optional: v.optional[m],
		}
	}
	return refs, nil
}

type varRefVisitor struct {
	members  []*ast.MemberNode
	optional map[ast.Node]bool // left operands of ??
}

func (v *varRefVisitor) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.MemberNode:
		id, ok := n.Node.(*ast.IdentifierNode)
		if !ok || id.Value != "vars" {
			return
		}
		if _, ok := n.Property.(*ast.StringNode); ok {
			v.members = append(v.members, n)
		}
	case *ast.BinaryNode:
		if n.Operator != "??" {
			return
		}
		left := n.Left
		if c, ok := left.(*ast.ChainNode); ok { // vars?.x
			left = c.Node
		}
		v.optional[left] = true
	}
}

func evalCondition(env map[string]any, configPath, element, condition string) bool {
	if condition == "" {
		return true
	}
	out, err := evalExpr(env, condition, expr.AsBool())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error evaluating expression in file: %s, element: %s\n", configPath, element)
		fmt.Fprintf(os.Stderr, "Condition: %s\n", condition)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	return out.(bool)
}
