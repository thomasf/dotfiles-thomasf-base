package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.starlark.net/resolve"
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
		return nil, starlarkCompileError(src, err)
	}
	if prog.NumLoads() > 0 {
		_, pos := prog.Load(0)
		return nil, errors.New(starlarkMessage(src, pos, "load is not supported"))
	}
	return prog, nil
}

// starlarkCompileError returns err, an error from compiling src, with the
// lines of src that it is about. A resolve.ErrorList has all its errors, not
// only the first.
func starlarkCompileError(src string, err error) error {
	var msgs []string
	if list, ok := errors.AsType[resolve.ErrorList](err); ok {
		for _, e := range list {
			msgs = append(msgs, starlarkMessage(src, e.Pos, e.Msg))
		}
	} else if e, ok := errors.AsType[syntax.Error](err); ok {
		msgs = append(msgs, starlarkMessage(src, e.Pos, e.Msg))
	} else {
		return err
	}
	return errors.New(strings.Join(msgs, "\n"))
}

// starlarkContextLines is the number of lines of src shown before the line
// of an error.
const starlarkContextLines = 3

// starlarkMessage returns msg at pos in src, followed by up to
// starlarkContextLines lines of src before it, and that line with a caret
// under the column:
//
//	starlark[0].src:3:16: got ',', want primary expression
//	  1 | size = 10
//	  2 | merge(doc, {
//	  3 |     "a": size +,
//	    |                ^
func starlarkMessage(src string, pos syntax.Position, msg string) string {
	s := fmt.Sprintf("%s: %s", pos, msg)
	lines := strings.Split(src, "\n")
	if pos.Line < 1 || int(pos.Line) > len(lines) {
		return s
	}
	line := lines[pos.Line-1]
	num := strconv.Itoa(int(pos.Line))
	for n := max(1, int(pos.Line)-starlarkContextLines); n < int(pos.Line); n++ {
		s += strings.TrimRight(fmt.Sprintf("\n  %*d | %s", len(num), n, lines[n-1]), " ")
	}
	s += fmt.Sprintf("\n  %s | %s", num, line)
	if pos.Col < 1 {
		return s
	}
	// Keep the tabs before the column, so that the caret lines up.
	var indent strings.Builder
	for i, r := range []rune(line) {
		if i >= int(pos.Col)-1 {
			break
		}
		if r == '\t' {
			indent.WriteRune('\t')
		} else {
			indent.WriteRune(' ')
		}
	}
	return s + fmt.Sprintf("\n  %s | %s^", strings.Repeat(" ", len(num)), indent.String())
}

// starlarkNames returns the names predeclared in [[starlark]] code.
var starlarkNames = sync.OnceValue(func() map[string]bool {
	names := map[string]bool{}
	for name := range starlarkPredeclared("", "", evalContext{}, starlark.NewDict(0)) {
		names[name] = true
	}
	return names
})

// starlarkPredeclared returns the predeclared names of [[starlark]] code run
// on dst with the document doc: the variables from exprVars and the
// functions from exprFuncs, as in conditions and templates, plus merge and
// read.
func starlarkPredeclared(srcRoot, dst string, ec evalContext, doc *starlark.Dict) starlark.StringDict {
	predeclared := starlark.StringDict{
		"doc":   doc,
		"dst":   starlark.String(dst),
		"merge": mergeBuiltin,
		"read":  readBuiltin(srcRoot, ec.theme.dir()),
	}
	for name, v := range exprVars(ec) {
		sv, err := fromGo(v, name)
		if err != nil {
			panic(err) // exprVars only has strings and maps of strings
		}
		predeclared[name] = sv
	}
	funcs := exprFuncs(ec.dstRoot)
	for _, name := range slices.Sorted(maps.Keys(funcs)) {
		predeclared[name] = starlarkFunc(name, funcs[name])
	}
	return predeclared
}

// starlarkFunc wraps fn, one of the functions from exprFuncs, as a Starlark
// builtin.
func starlarkFunc(name string, fn any) *starlark.Builtin {
	if fn, ok := fn.(func(string, string, float64) (string, error)); ok {
		// mix: two strings and a number, an int or a float.
		return starlark.NewBuiltin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var s1, s2 string
			var n starlark.Value
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 3, &s1, &s2, &n); err != nil {
				return nil, err
			}
			f, ok := starlark.AsFloat(n)
			if !ok {
				return nil, fmt.Errorf("%s: argument 3 is a %s, not a number", b.Name(), n.Type())
			}
			v, err := fn(s1, s2, f)
			return starlark.String(v), err
		})
	}
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
	case func(string) (string, error):
		impl = func(args []string) (starlark.Value, error) {
			v, err := fn(args[0])
			return starlark.String(v), err
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
// repository at srcRoot. It returns the file path, relative to srcRoot or an
// absolute path inside themeDir, the directory of the theme in use, decoded
// in the format of its extension. With default, a missing file returns
// default instead of failing.
func readBuiltin(srcRoot, themeDir string) *starlark.Builtin {
	return starlark.NewBuiltin("read", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var path string
		var def starlark.Value
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "path", &path, "default?", &def); err != nil {
			return nil, err
		}
		file := filepath.Join(srcRoot, path)
		if filepath.IsAbs(path) && themeDir != "" {
			file = path
			if rel, err := filepath.Rel(themeDir, path); err != nil || !filepath.IsLocal(rel) {
				return nil, fmt.Errorf("read: %s: an absolute path must be inside the theme directory %s", path, themeDir)
			}
		} else if !filepath.IsLocal(path) {
			return nil, fmt.Errorf("read: %s: path must be relative and inside the repository, or inside theme[\"dir\"]", path)
		}
		format, err := formatOf(path)
		if err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}
		data, err := os.ReadFile(file)
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

// starlarkError returns err, an error from running src, with the line of src
// that caused it and the lines it was called from, innermost first, rather
// than a backtrace that includes built-ins.
func starlarkError(src string, err error) error {
	ee, ok := errors.AsType[*starlark.EvalError](err)
	if !ok {
		return err
	}
	var msgs []string
	for _, frame := range slices.Backward(ee.CallStack) {
		if pos := frame.Pos; pos.IsValid() && pos.Filename() != "<builtin>" {
			msg := "called from here"
			if len(msgs) == 0 {
				msg = ee.Msg
			}
			msgs = append(msgs, starlarkMessage(src, pos, msg))
		}
	}
	if len(msgs) == 0 {
		return err
	}
	return errors.New(strings.Join(msgs, "\n"))
}
