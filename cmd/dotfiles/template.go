package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/pelletier/go-toml/v2"
)

// renderTemplate executes text as a Go text/template (see parseTemplate). The
// data is the variables from exprVars.
func renderTemplate(name, text, dstRoot string, vars map[string]string) ([]byte, error) {
	t, err := parseTemplate(name, text, dstRoot, vars)
	if err != nil {
		return nil, err
	}
	return executeTemplate(t, vars)
}

// parseTemplate parses text as a Go text/template. The funcs are those from
// exprFuncs plus expr, which evaluates an expr expression like a condition,
// with the content vars has when it is called.
func parseTemplate(name, text, dstRoot string, vars map[string]string) (*template.Template, error) {
	funcs := exprFuncs(dstRoot)
	funcs["expr"] = func(src string) (any, error) {
		return evalExpr(exprEnv(dstRoot, vars), src)
	}
	return template.New(name).Option("missingkey=error").Funcs(funcs).Parse(text)
}

// executeTemplate executes t, parsed by parseTemplate with the same vars.
func executeTemplate(t *template.Template, vars map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, exprVars(vars)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderValue renders a config value, such as a src or dst path, with
// renderTemplate. A value without "{{" is returned as is.
func renderValue(name, value, dstRoot string, vars map[string]string) (string, error) {
	if !strings.Contains(value, "{{") {
		return value, nil
	}
	out, err := renderTemplate(name, value, dstRoot, vars)
	return string(out), err
}

// TemplateAction renders the template Src, with the resolved [vars] Vars, and
// writes the result to Dst. Dst gets the permission bits Mode, or those of Src
// if Mode is 0. Existing says what happens when Dst exists with different
// content; Force turns ExistingError into ExistingOverwrite.
type TemplateAction struct {
	SrcRoot  string
	Src      string
	DstRoot  string
	Dst      string
	Mode     fs.FileMode
	Existing ExistingAction
	Force    bool
	Vars     map[string]string
}

func (t *TemplateAction) DstAbsolutePath() string {
	return filepath.Join(t.DstRoot, t.Dst)
}

func (t *TemplateAction) String() string {
	return fmt.Sprintf("[%s] template: %s -> %s", filepath.Base(t.SrcRoot), t.Src, t.Dst)
}

func (t *TemplateAction) Run() error {
	dst := t.DstAbsolutePath()
	if t.Existing == ExistingSkip {
		if _, err := os.Lstat(dst); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	src := filepath.Join(t.SrcRoot, t.Src)
	text, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	perm := t.Mode
	if perm == 0 {
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		perm = info.Mode().Perm()
	}
	out, err := renderTemplate(t.Src, string(text), t.DstRoot, t.Vars)
	if err != nil {
		return err
	}
	if err := validateFormat(t.Dst, out); err != nil {
		return fmt.Errorf("rendered %s is not valid: %w", t.Dst, err)
	}
	if t.Existing == ExistingError && !t.Force {
		old, err := os.ReadFile(dst)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err == nil && !bytes.Equal(old, out) {
			return fmt.Errorf("%s exists with different content (use -f to overwrite)", dst)
		}
	}
	changed, err := writeFile(dst, out, perm)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(os.Stdout, "%s: updated\n", t)
	}
	return nil
}

// validateFormat checks that data parses in the format implied by the
// extension of path. Other extensions are not checked.
func validateFormat(path string, data []byte) error {
	var v any
	switch filepath.Ext(path) {
	case ".json":
		return json.Unmarshal(data, &v)
	case ".toml":
		return toml.Unmarshal(data, &v)
	}
	return nil
}

// writeFile makes the regular file path have the content data and the
// permission bits perm, and reports whether it changed anything. A file that
// already has that content is never rewritten; if only its permissions
// differ, they are changed in place. Otherwise the file is replaced
// atomically, and missing parent directories are created.
//
// A path that is a symlink is refused, so that writing never changes a file
// in another location, such as one linked by a mount.
func writeFile(path string, data []byte, perm fs.FileMode) (bool, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&fs.ModeSymlink != 0 {
			return false, fmt.Errorf("refusing to write through symlink: %s", path)
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("not a regular file: %s", path)
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, data) {
			if info.Mode().Perm() == perm {
				return false, nil
			}
			return true, os.Chmod(path, perm)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return false, err
		}
	default:
		return false, err
	}

	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return false, err
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Chmod(perm), f.Close())
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return false, err
	}
	return true, nil
}
