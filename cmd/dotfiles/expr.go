package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
	theme   *activeTheme      // the theme variant in use, or nil
}

// exprVars returns the variables available in condition expressions and
// templates. ec.vars is available as vars. Its values are copied into a
// map[string]any, so that a missing var is nil rather than "", which ??
// needs. ec.theme is available as theme (see activeTheme.value).
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
		"theme":    ec.theme.value(),
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
//   - hexDigits, rgb, hex0x: a #rrggbb color written as rrggbb, as r,g,b in
//     decimal, or as 0xrrggbb (see colorFunc).
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
		"hexDigits": colorFunc("hexDigits", func(hex string) string { return hex }),
		"rgb": colorFunc("rgb", func(hex string) string {
			n, _ := strconv.ParseUint(hex, 16, 32) // checked by colorFunc
			return fmt.Sprintf("%d,%d,%d", n>>16, n>>8&0xff, n&0xff)
		}),
		"hex0x": colorFunc("hex0x", func(hex string) string { return "0x" + hex }),
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
// vars.x to a var that is not defined is an error, and so is one such as
// theme.colors.x to a color or other theme value that does not exist, unless
// it is the left operand of ??, as in vars.x ?? "default".
func evalExpr(env map[string]any, src string, opts ...expr.Option) (any, error) {
	// References are checked first: compiling fails on them too, with a
	// less helpful error. Compiling reports a syntax error.
	if refs, err := exprRefs(src); err == nil {
		for _, ref := range refs {
			if err := checkRef(env, ref); err != nil {
				return nil, err
			}
		}
	}
	program, err := expr.Compile(src, append([]expr.Option{expr.Env(env)}, opts...)...)
	if err != nil {
		return nil, err
	}
	return expr.Run(program, env)
}

// checkedRoots are the variables whose references checkRef checks.
var checkedRoots = []string{"vars", "theme"}

// checkRef reports an error if ref is a reference into vars or theme in env
// to a key that does not exist, unless ref is optional.
func checkRef(env map[string]any, ref exprRef) error {
	if !slices.Contains(checkedRoots, ref.path[0]) {
		return nil
	}
	v, ok := env[ref.path[0]]
	if !ok {
		return nil
	}
	for i, key := range ref.path[1:] {
		m, ok := v.(map[string]any)
		if !ok {
			if v == nil && ref.path[0] == "theme" {
				return errors.New("no theme is selected for theme." + strings.Join(ref.path[1:], ".") + " (set [theme] dark and light)")
			}
			return nil
		}
		if v, ok = m[key]; !ok {
			if ref.optional {
				return nil
			}
			name := strings.Join(ref.path[:i+2], ".")
			if ref.path[0] == "vars" {
				return fmt.Errorf("undefined var %s (use %[1]s ?? default if it is optional)", name)
			}
			return fmt.Errorf("undefined %s (use %[1]s ?? default if it is optional)", name)
		}
	}
	return nil
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
	refs, err := exprRefs(src)
	if err != nil {
		return nil, err
	}
	var varRefs []varRef
	for _, ref := range refs {
		if len(ref.path) == 2 && ref.path[0] == "vars" {
			varRefs = append(varRefs, varRef{name: ref.path[1], optional: ref.optional})
		}
	}
	return varRefs, nil
}

// An exprRef is a reference such as vars.x, theme.colors.x or
// theme["colors"]["x"] in an expression: a variable and the keys after it.
// It is optional if it is the left operand of ??.
type exprRef struct {
	path     []string
	optional bool
}

// exprRefs returns the references in the expression src. For theme.colors.x,
// both theme.colors and theme.colors.x are returned.
func exprRefs(src string) ([]exprRef, error) {
	tree, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	v := &refVisitor{optional: map[ast.Node]bool{}}
	ast.Walk(&tree.Node, v)
	refs := make([]exprRef, len(v.members))
	for i, m := range v.members {
		refs[i] = exprRef{path: memberPath(m), optional: v.optional[m]}
	}
	return refs, nil
}

// memberPath returns the variable and keys of n, such as [theme colors x]
// for theme.colors.x, or nil if n is not a variable followed by constant
// keys.
func memberPath(n ast.Node) []string {
	switch n := n.(type) {
	case *ast.IdentifierNode:
		return []string{n.Value}
	case *ast.MemberNode:
		key, ok := n.Property.(*ast.StringNode)
		if !ok {
			return nil
		}
		if p := memberPath(n.Node); p != nil {
			return append(p, key.Value)
		}
	}
	return nil
}

type refVisitor struct {
	members  []*ast.MemberNode
	optional map[ast.Node]bool // left operands of ??
}

func (v *refVisitor) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.MemberNode:
		if memberPath(n) != nil {
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
