package main

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
)

// StarlarkAction runs the compiled Starlark code Program on Dst, a path
// relative to DstRoot that may be a glob. Each file is decoded into the dict
// doc, the code changes it, and the result is written back if it differs.
// Missing says what to do when Dst does not exist; a created file gets the
// permission bits Mode, or 0o644. Format, when set, overrides the format
// implied by the extension of Dst. Src, the code Program was compiled from,
// is shown in error messages.
type StarlarkAction struct {
	SrcRoot string
	DstRoot string
	Dst     string
	Program *starlark.Program
	Src     string
	Missing MissingAction
	Format  string
	Indent  int
	Mode    fs.FileMode
	Vars    map[string]string
}

func (t *StarlarkAction) String() string {
	return fmt.Sprintf("[%s] starlark: %s", filepath.Base(t.SrcRoot), t.Dst)
}

func (t *StarlarkAction) Run() error {
	if !strings.ContainsAny(t.Dst, "*?[") {
		return t.runFile(t.Dst)
	}
	matches, err := filepath.Glob(filepath.Join(t.DstRoot, t.Dst))
	if err != nil {
		return err
	}
	if len(matches) == 0 && t.Missing == MissingError {
		return errors.New("no files match")
	}
	var errs []error
	for _, m := range matches {
		dst, err := filepath.Rel(t.DstRoot, m)
		if err == nil {
			err = t.runFile(dst)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dst, err))
		}
	}
	return errors.Join(errs...)
}

// runFile runs the code on the file dst, relative to DstRoot.
func (t *StarlarkAction) runFile(dst string) error {
	format := t.Format
	if format == "" {
		var err error
		if format, err = formatOf(dst); err != nil {
			return err
		}
	}
	path := filepath.Join(t.DstRoot, dst)
	perm := cmp.Or(t.Mode, 0o644)
	var old []byte
	info, err := os.Lstat(path)
	exists := err == nil
	switch {
	case exists:
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink: %s", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file: %s", path)
		}
		if old, err = os.ReadFile(path); err != nil {
			return err
		}
		perm = info.Mode().Perm()
	case errors.Is(err, fs.ErrNotExist):
		switch t.Missing {
		case MissingCreate:
		case MissingError:
			return fmt.Errorf("%s does not exist", path)
		default:
			return nil
		}
	default:
		return err
	}

	out, err := t.apply(dst, format, old)
	if err != nil {
		return err
	}
	result, err := decodeDocument(format, out)
	if err != nil {
		return fmt.Errorf("decoding the result: %w", err)
	}
	again, err := t.apply(dst, format, out)
	if err != nil {
		return fmt.Errorf("running again on the result: %w", err)
	}
	againDoc, err := decodeDocument(format, again)
	if err != nil {
		return fmt.Errorf("decoding the result of running again: %w", err)
	}
	if !valuesEqual(result, againDoc) {
		return errors.New("not idempotent: running it again on its result changes the result")
	}
	if exists {
		cur, err := decodeDocument(format, old)
		if err != nil {
			return err
		}
		if valuesEqual(cur, result) {
			return nil
		}
	}

	changed, err := writeFile(path, out, perm)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(os.Stdout, "[%s] starlark: %s: updated\n", filepath.Base(t.SrcRoot), dst)
	}
	return nil
}

// apply decodes data in format, runs the code on it as doc and returns the
// encoded result. The code may also assign a new dict to doc.
func (t *StarlarkAction) apply(dst, format string, data []byte) ([]byte, error) {
	doc, err := decodeDocument(format, data)
	if err != nil {
		return nil, err
	}
	thread := &starlark.Thread{
		Name: t.String(),
		Print: func(_ *starlark.Thread, msg string) {
			fmt.Fprintf(os.Stderr, "%s: %s\n", t, msg)
		},
	}
	thread.SetMaxExecutionSteps(maxStarlarkSteps)
	globals, err := t.Program.Init(thread, starlarkPredeclared(t.SrcRoot, t.DstRoot, dst, t.Vars, doc))
	if err != nil {
		return nil, starlarkError(t.Src, err)
	}
	if v, ok := globals["doc"]; ok {
		if doc, ok = v.(*starlark.Dict); !ok {
			return nil, fmt.Errorf("doc must be a dict, not a %s", v.Type())
		}
	}
	return encodeDocument(format, doc, cmp.Or(t.Indent, 2))
}
