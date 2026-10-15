package main

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// maxStarlarkSteps limits the work one run of [[starlark]] code may do, so
// that a runaway loop fails instead of hanging.
const maxStarlarkSteps = 10_000_000

// starlarkFileOptions are the Starlark dialect of [[starlark]] entries: if
// and for statements may be used at the top level, and globals may be
// reassigned.
var starlarkFileOptions = &syntax.FileOptions{TopLevelControl: true, GlobalReassign: true}

// compileStarlark compiles the Starlark code src of a [[starlark]] entry.
// name is used as the file name in error messages.
func compileStarlark(name, src string) (*starlark.Program, error) {
	names := starlarkNames()
	_, prog, err := starlark.SourceProgramOptions(starlarkFileOptions, name, src, func(name string) bool { return names[name] })
	if err != nil {
		return nil, err
	}
	if prog.NumLoads() > 0 {
		_, pos := prog.Load(0)
		return nil, fmt.Errorf("%s: load is not supported", pos)
	}
	return prog, nil
}

// starlarkNames returns the names predeclared in [[starlark]] code.
var starlarkNames = sync.OnceValue(func() map[string]bool {
	names := map[string]bool{}
	for name := range starlarkPredeclared("", "", "", nil, starlark.NewDict(0)) {
		names[name] = true
	}
	return names
})

// starlarkPredeclared returns the predeclared names of [[starlark]] code run
// on dst with the document doc: the variables from exprVars and the
// functions from exprFuncs, as in conditions and templates, plus merge and
// read.
func starlarkPredeclared(srcRoot, dstRoot, dst string, vars map[string]string, doc *starlark.Dict) starlark.StringDict {
	predeclared := starlark.StringDict{
		"doc":   doc,
		"dst":   starlark.String(dst),
		"merge": mergeBuiltin,
		"read":  readBuiltin(srcRoot),
	}
	for name, v := range exprVars(vars) {
		sv, err := fromGo(v, name)
		if err != nil {
			panic(err) // exprVars only has strings and maps of strings
		}
		predeclared[name] = sv
	}
	funcs := exprFuncs(dstRoot)
	for _, name := range slices.Sorted(maps.Keys(funcs)) {
		predeclared[name] = starlarkFunc(name, funcs[name])
	}
	return predeclared
}

// starlarkFunc wraps fn, one of the functions from exprFuncs, as a Starlark
// builtin.
func starlarkFunc(name string, fn any) *starlark.Builtin {
	var impl func(args []string) (starlark.Value, error)
	nargs := 1
	switch fn := fn.(type) {
	case func(string) string:
		impl = func(args []string) (starlark.Value, error) { return starlark.String(fn(args[0])), nil }
	case func(string) bool:
		impl = func(args []string) (starlark.Value, error) { return starlark.Bool(fn(args[0])), nil }
	case func(string) (bool, error):
		impl = func(args []string) (starlark.Value, error) {
			v, err := fn(args[0])
			return starlark.Bool(v), err
		}
	case func(...string) string:
		nargs = -1
		impl = func(args []string) (starlark.Value, error) { return starlark.String(fn(args...)), nil }
	default:
		panic(fmt.Sprintf("starlarkFunc: unsupported type %T of %s", fn, name))
	}
	return starlark.NewBuiltin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(kwargs) > 0 {
			return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
		}
		if nargs >= 0 && len(args) != nargs {
			return nil, fmt.Errorf("%s: got %d arguments, want %d", b.Name(), len(args), nargs)
		}
		strs := make([]string, len(args))
		for i, a := range args {
			s, ok := starlark.AsString(a)
			if !ok {
				return nil, fmt.Errorf("%s: argument %d is a %s, not a string", b.Name(), i+1, a.Type())
			}
			strs[i] = s
		}
		return impl(strs)
	})
}

// mergeBuiltin is merge(target, patch) in [[starlark]] code. It applies
// patch to target as a JSON Merge Patch (RFC 7386), in place, and returns
// None.
var mergeBuiltin = starlark.NewBuiltin("merge", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var target, patch *starlark.Dict
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &target, &patch); err != nil {
		return nil, err
	}
	return starlark.None, mergePatch(target, patch)
})

// mergePatch merges patch into target: None deletes a key, a dict merges
// recursively, and any other value replaces the key's value.
func mergePatch(target, patch *starlark.Dict) error {
	for _, item := range patch.Items() {
		k, v := item[0], item[1]
		if v == starlark.None {
			if _, _, err := target.Delete(k); err != nil {
				return err
			}
			continue
		}
		pv, ok := v.(*starlark.Dict)
		if !ok {
			if err := target.SetKey(k, deepCopy(v)); err != nil {
				return err
			}
			continue
		}
		cur, found, err := target.Get(k)
		if err != nil {
			return err
		}
		tv, ok := cur.(*starlark.Dict)
		if !found || !ok {
			tv = starlark.NewDict(pv.Len())
			if err := target.SetKey(k, tv); err != nil {
				return err
			}
		}
		if err := mergePatch(tv, pv); err != nil {
			return err
		}
	}
	return nil
}

// deepCopy copies lists and dicts, so that a merge target never shares a
// mutable value with the patch.
func deepCopy(v starlark.Value) starlark.Value {
	switch v := v.(type) {
	case *starlark.List:
		elems := make([]starlark.Value, v.Len())
		for i := range elems {
			elems[i] = deepCopy(v.Index(i))
		}
		return starlark.NewList(elems)
	case *starlark.Dict:
		d := starlark.NewDict(v.Len())
		for _, item := range v.Items() {
			d.SetKey(item[0], deepCopy(item[1])) // a new dict is never frozen
		}
		return d
	}
	return v
}

// readBuiltin returns read(path, default=...) for [[starlark]] code of the
// repository at srcRoot. It returns the file path, relative to srcRoot,
// decoded in the format of its extension. With default, a missing file
// returns default instead of failing.
func readBuiltin(srcRoot string) *starlark.Builtin {
	return starlark.NewBuiltin("read", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var path string
		var def starlark.Value
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "path", &path, "default?", &def); err != nil {
			return nil, err
		}
		if !filepath.IsLocal(path) {
			return nil, fmt.Errorf("read: %s: path must be relative and inside the repository", path)
		}
		format, err := formatOf(path)
		if err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}
		data, err := os.ReadFile(filepath.Join(srcRoot, path))
		if errors.Is(err, fs.ErrNotExist) && def != nil {
			return def, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}
		doc, err := decodeDocument(format, data)
		if err != nil {
			return nil, fmt.Errorf("read: %s: %w", path, err)
		}
		return doc, nil
	})
}

// starlarkError returns err with the position of the innermost Starlark
// code that caused it, rather than a whole backtrace.
func starlarkError(err error) error {
	ee, ok := errors.AsType[*starlark.EvalError](err)
	if !ok {
		return err
	}
	for _, frame := range slices.Backward(ee.CallStack) {
		if pos := frame.Pos; pos.IsValid() && pos.Filename() != "<builtin>" {
			return fmt.Errorf("%s: %s", pos, ee.Msg)
		}
	}
	return err
}

// StarlarkAction runs the compiled Starlark code Program on Dst, a path
// relative to DstRoot that may be a glob. Each file is decoded into the dict
// doc, the code changes it, and the result is written back if it differs.
// Missing says what to do when Dst does not exist; a created file gets the
// permission bits Mode, or 0o644. Format, when set, overrides the format
// implied by the extension of Dst.
type StarlarkAction struct {
	SrcRoot string
	DstRoot string
	Dst     string
	Program *starlark.Program
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
		return nil, starlarkError(err)
	}
	if v, ok := globals["doc"]; ok {
		if doc, ok = v.(*starlark.Dict); !ok {
			return nil, fmt.Errorf("doc must be a dict, not a %s", v.Type())
		}
	}
	return encodeDocument(format, doc, cmp.Or(t.Indent, 2))
}
