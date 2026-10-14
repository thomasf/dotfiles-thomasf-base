package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/expr-lang/expr"
)

// exprVars returns the variables available in condition expressions and
// templates.
func exprVars() map[string]any {
	hostname, _ := os.Hostname()
	return map[string]any{
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"hostname": hostname,
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
		"env": os.Getenv,
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
func exprEnv(dstRoot string) map[string]any {
	env := exprVars()
	maps.Copy(env, exprFuncs(dstRoot))
	return env
}

// evalExpr compiles and runs the expr expression src in env.
func evalExpr(env map[string]any, src string, opts ...expr.Option) (any, error) {
	program, err := expr.Compile(src, append([]expr.Option{expr.Env(env)}, opts...)...)
	if err != nil {
		return nil, err
	}
	return expr.Run(program, env)
}

func evalCondition(dstRoot, configPath, element, condition string) bool {
	if condition == "" {
		return true
	}
	out, err := evalExpr(exprEnv(dstRoot), condition, expr.AsBool())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error evaluating expression in file: %s, element: %s\n", configPath, element)
		fmt.Fprintf(os.Stderr, "Condition: %s\n", condition)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	return out.(bool)
}
