package main

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/parser"
)

// The events that pathenv [[run]] entries run on.
const (
	pathenvActivate = "activate"
	pathenvRefresh  = "refresh"
)

// A pathenvFile is a pathenv file: a configuration, which has paths, or an
// include file (*.include.toml), which is used through [[run]] include.
type pathenvFile struct {
	Paths     []string          `toml:"paths,omitempty"`
	Condition string            `toml:"condition,omitempty"`
	Vars      map[string]string `toml:"vars,omitempty"`
	Run       []pathenvRun      `toml:"run,omitempty"`

	name    string      // repo:file, without .toml or .include.toml
	short   string      // file
	path    string      // the file
	include bool        // an include file
	repo    *Repository // the repository the file is in
	decoded bool        // the file was read and decoded, so its keys are known
	err     error       // why the file is left out, or nil

	prefixes []string     // Paths, rendered, absolute and clean
	eval     *pathenvEval // a configuration's evaluator, without a location
}

// A pathenvRun is a [[run]] entry: one of Src, File and Include.
type pathenvRun struct {
	Src       string     `toml:"src,omitempty"`
	File      string     `toml:"file,omitempty"`
	Include   string     `toml:"include,omitempty"`
	On        StringList `toml:"on,omitempty"` // activate (default), refresh
	Condition string     `toml:"condition,omitempty"`
}

// A pathenvSet is the pathenv files of all repositories.
type pathenvSet struct {
	files   []*pathenvFile          // in the order of orderRepos, then of [pathenv] dirs and file names
	byName  map[string]*pathenvFile // by name repo:file and by file; not the files with a duplicate name
	dstRoot string                  // relative paths are relative to it
	shell   string                  // the shell the code is for
	errs    []error                 // errors not about one file, such as an unreadable directory
}

// loadPathenv reads the pathenv files of repos, in the directories of their
// [pathenv] dirs, and renders the paths of the configurations. A file that
// fails to load, and every file that includes it, gets an err and is left
// out of matching. shell is the shell the code is for, the variable shell of
// conditions.
func loadPathenv(repos []*Repository, dstRoot, shell string) *pathenvSet {
	s := &pathenvSet{byName: map[string]*pathenvFile{}, dstRoot: dstRoot, shell: shell}
	for _, r := range orderRepos(repos) {
		repoName := filepath.Base(r.srcRoot)
		for _, dir := range r.config.Pathenv.Dirs {
			dir = filepath.Join(r.srcRoot, dir)
			entries, err := os.ReadDir(dir)
			if err != nil {
				s.errs = append(s.errs, fmt.Errorf("%s: pathenv.dirs: %w", r.configPath, err))
				continue
			}
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || strings.HasPrefix(name, ".") || filepath.Ext(name) != ".toml" {
					continue
				}
				short, include := strings.CutSuffix(strings.TrimSuffix(name, ".toml"), ".include")
				f := &pathenvFile{
					name:    repoName + ":" + short,
					short:   short,
					path:    filepath.Join(dir, name),
					include: include,
					repo:    r,
				}
				if !pathenvNameRe.MatchString(f.name) {
					f.fail(fmt.Errorf("%s: invalid name %s: want letters, digits and ._- in the repository and file name", f.path, f.name))
				} else {
					f.load()
				}
				s.files = append(s.files, f)
			}
		}
	}

	byShort := map[string][]*pathenvFile{}
	for _, f := range s.files {
		byShort[f.short] = append(byShort[f.short], f)
	}
	for _, f := range s.files {
		if fs := byShort[f.short]; len(fs) > 1 {
			var others []string
			for _, o := range fs {
				if o != f {
					others = append(others, o.path)
				}
			}
			f.fail(fmt.Errorf("the name %s is also used by %s", f.short, strings.Join(others, ", ")))
			continue
		}
		s.byName[f.name] = f
		s.byName[f.short] = f
	}
	s.checkIncludes()
	for _, f := range s.files {
		if !f.include && f.decoded {
			f.renderPaths(s)
		}
	}
	return s
}

// renderPaths sets the evaluator of the configuration f and renders its
// paths into prefixes. A path that renders empty is left out; one that fails
// to render makes f broken. Relative paths are relative to s.dstRoot.
func (f *pathenvFile) renderPaths(s *pathenvSet) {
	f.eval = &pathenvEval{s: s, vars: s.vars(f)}
	for i, p := range f.Paths {
		p, err := f.eval.render(f, fmt.Sprintf("paths[%d]", i), p)
		if err != nil {
			f.fail(err)
			continue
		}
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(s.dstRoot, p)
		}
		f.prefixes = append(f.prefixes, filepath.Clean(p))
	}
}

// pathenvNameRe matches the names of pathenv files, which are listed in
// PATHENV_ACTIVE separated by spaces.
var pathenvNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+:[A-Za-z0-9._-]+$`)

// fail records err as the reason f is left out, unless it has one already.
func (f *pathenvFile) fail(err error) {
	if f.err == nil {
		f.err = err
	}
}

// load reads, decodes and validates the file.
func (f *pathenvFile) load() {
	data, err := os.ReadFile(f.path)
	if err != nil {
		f.fail(err)
		return
	}
	if err := decodeTOML(f.path, data, f); err != nil {
		f.err = err
		return
	}
	f.decoded = true
	if err := f.validate(); err != nil {
		f.fail(fmt.Errorf("%s: %w", f.path, err))
	}
}

// validate checks the keys of the file, the syntax of its expressions and
// templates, and its events. Includes are checked by checkIncludes.
func (f *pathenvFile) validate() error {
	var errs []error
	switch {
	case f.include && len(f.Paths) > 0:
		errs = append(errs, errors.New("paths: not allowed in an include file"))
	case !f.include && len(f.Paths) == 0:
		errs = append(errs, errors.New("paths is required (or name the file *.include.toml)"))
	}
	for i, p := range f.Paths {
		if p == "" {
			errs = append(errs, fmt.Errorf("paths[%d] is empty", i))
		}
	}
	if err := checkExpr("condition", f.Condition); err != nil {
		errs = append(errs, err)
	}
	for _, name := range slices.Sorted(maps.Keys(f.Vars)) {
		if !varNameRe.MatchString(name) {
			errs = append(errs, fmt.Errorf("vars.%s: invalid name: want letters, digits and _, not starting with a digit", name))
		}
	}
	for i, r := range f.Run {
		key := fmt.Sprintf("run %d", i+1)
		n := 0
		for _, v := range []string{r.Src, r.File, r.Include} {
			if v != "" {
				n++
			}
		}
		if n != 1 {
			errs = append(errs, fmt.Errorf("%s: want exactly one of src, file and include", key))
		}
		if r.Include != "" && len(r.On) > 0 {
			errs = append(errs, fmt.Errorf("%s: on cannot be used with include", key))
		}
		for _, event := range r.On {
			if event != pathenvActivate && event != pathenvRefresh {
				errs = append(errs, fmt.Errorf("%s: invalid on %q: want activate or refresh", key, event))
			}
		}
		if err := checkExpr(key+": condition", r.Condition); err != nil {
			errs = append(errs, err)
		}
		if r.Src != "" {
			if err := checkTemplate(key+": src", r.Src); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// checkExpr reports a syntax error in the expression src.
func checkExpr(name, src string) error {
	if src == "" {
		return nil
	}
	if _, err := parser.Parse(src); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// checkTemplate reports a syntax error in the template text.
func checkTemplate(name, text string) error {
	_, err := parseTemplate(name, text, evalContext{})
	return err
}

// checkIncludes resolves the [[run]] include entries of every file. An
// include of an unknown name or of a configuration is an error, and so is a
// cycle. A file that includes a file that is left out is left out too.
func (s *pathenvSet) checkIncludes() {
	const (
		visiting = iota + 1
		done
	)
	state := map[*pathenvFile]int{}
	var visit func(f *pathenvFile, path []*pathenvFile)
	visit = func(f *pathenvFile, path []*pathenvFile) {
		switch state[f] {
		case done:
			return
		case visiting:
			i := slices.Index(path, f)
			var names []string
			for _, p := range append(path[i:], f) {
				names = append(names, p.name)
			}
			for _, p := range path[i:] {
				p.fail(fmt.Errorf("%s: include cycle: %s", p.path, strings.Join(names, " -> ")))
			}
			return
		}
		state[f] = visiting
		path = append(path, f)
		for i, r := range f.Run {
			if r.Include == "" {
				continue
			}
			t := s.byName[r.Include]
			switch {
			case t == nil:
				f.fail(fmt.Errorf("%s: run %d: include %q: no such include file", f.path, i+1, r.Include))
				continue
			case !t.include:
				f.fail(fmt.Errorf("%s: run %d: include %q: %s is a configuration, not an include file", f.path, i+1, r.Include, t.name))
				continue
			}
			visit(t, path)
			if t.err != nil {
				f.fail(fmt.Errorf("%s: run %d: include %q: %s is broken", f.path, i+1, r.Include, t.name))
			}
		}
		state[f] = done
	}
	for _, f := range s.files {
		if f.decoded {
			visit(f, nil)
		}
	}
}

// loadErrors returns the errors of the set and of its files.
func (s *pathenvSet) loadErrors() []error {
	errs := slices.Clone(s.errs)
	for _, f := range s.files {
		if f.err != nil {
			errs = append(errs, f.err)
		}
	}
	return errs
}

// vars returns the [vars] of the configuration c and the files it includes,
// merged: an include file's own vars are its defaults, and the including
// file's win. Every include is counted, whatever its condition.
func (s *pathenvSet) vars(c *pathenvFile) map[string]string {
	vars := map[string]string{}
	seen := map[*pathenvFile]bool{}
	var collect func(f *pathenvFile)
	collect = func(f *pathenvFile) {
		if seen[f] {
			return
		}
		seen[f] = true
		for _, r := range f.Run {
			if t := s.byName[r.Include]; r.Include != "" && t != nil {
				collect(t)
			}
		}
		maps.Copy(vars, f.Vars)
	}
	collect(c)
	return vars
}

// A pathenvLocation is where a configuration matched: the location
// variables of its templates and conditions.
type pathenvLocation struct {
	Root     string `json:"root"`     // the nearest checkout at or above the directory, not above Prefix; else Prefix
	Worktree string `json:"worktree"` // after @ in the base name of Root, or ""
	Prefix   string `json:"prefix"`   // the paths entry that matched, without a trailing /
}

// A pathenvEval evaluates the conditions and templates of a configuration
// and the files it includes.
type pathenvEval struct {
	s    *pathenvSet
	vars map[string]string // the configuration's merged [vars], from pathenvSet.vars
	loc  *pathenvLocation  // nil before matching
}

// context returns the evalContext of f: the vars of its repository and the
// configuration's.
func (e *pathenvEval) context(f *pathenvFile) evalContext {
	vars := maps.Clone(f.repo.vars)
	if vars == nil {
		vars = map[string]string{}
	}
	maps.Copy(vars, e.vars)
	return evalContext{dstRoot: e.s.dstRoot, vars: vars, theme: f.repo.theme}
}

// values returns the variables of f's expressions and templates: those from
// exprVars, shell, and the location variables once there is a match.
func (e *pathenvEval) values(ec evalContext) map[string]any {
	v := exprVars(ec)
	v["shell"] = e.s.shell
	if e.loc != nil {
		v["root"] = e.loc.Root
		v["worktree"] = e.loc.Worktree
		v["prefix"] = e.loc.Prefix
	}
	return v
}

// env returns the expression environment of f.
func (e *pathenvEval) env(f *pathenvFile) map[string]any {
	ec := e.context(f)
	env := e.values(ec)
	maps.Copy(env, exprFuncs(ec.dstRoot))
	return env
}

// condition evaluates the expression src of f. The empty expression is true.
func (e *pathenvEval) condition(f *pathenvFile, name, src string) (bool, error) {
	if src == "" {
		return true, nil
	}
	out, err := evalExpr(e.env(f), src, expr.AsBool())
	if err != nil {
		return false, fmt.Errorf("%s: %s: %w", f.path, name, err)
	}
	return out.(bool), nil
}

// render renders the template text of f. A text without {{ is returned as
// it is.
func (e *pathenvEval) render(f *pathenvFile, name, text string) (string, error) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}
	ec := e.context(f)
	funcs := templateFuncs(ec)
	funcs["expr"] = func(src string) (any, error) { return evalExpr(e.env(f), src) }
	t, err := template.New(name).Option("missingkey=error").Funcs(funcs).Parse(text)
	if err != nil {
		return "", fmt.Errorf("%s: %w", f.path, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, e.values(ec)); err != nil {
		return "", fmt.Errorf("%s: %w", f.path, err)
	}
	return buf.String(), nil
}

// A pathenvMatch is the configuration for a directory, and where it matched.
type pathenvMatch struct {
	file *pathenvFile
	loc  pathenvLocation
	eval *pathenvEval
}

// A pathenvCandidate is a configuration whose paths entry prefix contains a
// directory.
type pathenvCandidate struct {
	file    *pathenvFile
	prefix  string // the paths entry, absolute and clean
	stopDir string // where the search for Root stops: prefix, or for a worktree its top
}

// match returns the configuration for dir, an absolute and clean path: of
// those whose paths contain dir and whose condition is true, the one with
// the longest prefix. For a directory in a worktree <repo>@<name>, the
// configurations of the main checkout <repo> match too. It returns nil if
// none matches. If the configuration that would match is broken, or two have
// the same longest prefix, it returns an error.
func (s *pathenvSet) match(dir string) (*pathenvMatch, error) {
	type target struct{ dir, stopDir string }
	targets := []target{{dir: dir}}
	if strings.Contains(dir, "@") {
		if main, top, ok := worktreeMain(dir); ok {
			if rel, err := filepath.Rel(top, dir); err == nil {
				targets = append(targets, target{filepath.Join(main, rel), top})
			}
		}
	}

	var best []pathenvCandidate // those with the longest prefix
	for _, f := range s.files {
		if f.include || !f.decoded {
			continue
		}
		c := pathenvCandidate{}
		for _, p := range f.prefixes {
			for _, t := range targets {
				if within(t.dir, p) && len(p) > len(c.prefix) {
					c = pathenvCandidate{file: f, prefix: p, stopDir: cmp.Or(t.stopDir, p)}
				}
			}
		}
		if c.file == nil {
			continue
		}
		if f.err == nil {
			ok, err := f.eval.condition(f, "condition", f.Condition)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		switch {
		case len(best) == 0 || len(c.prefix) > len(best[0].prefix):
			best = []pathenvCandidate{c}
		case len(c.prefix) == len(best[0].prefix):
			best = append(best, c)
		}
	}

	var names []string
	for _, c := range best {
		if c.file.err != nil {
			return nil, fmt.Errorf("%s matches %s but is broken: %w", c.file.name, dir, c.file.err)
		}
		names = append(names, c.file.name)
	}
	switch len(best) {
	case 0:
		return nil, nil
	case 1:
	default:
		return nil, fmt.Errorf("%s all match %s with the same prefix %s", strings.Join(names, " and "), dir, best[0].prefix)
	}
	c := best[0]
	loc := pathenvLocation{Root: c.prefix, Prefix: c.prefix}
	if root, ok := checkoutRoot(dir, c.stopDir); ok {
		loc.Root = root
	}
	if _, wt, ok := strings.Cut(filepath.Base(loc.Root), "@"); ok {
		loc.Worktree = wt
	}
	e := *c.file.eval
	e.loc = &loc
	return &pathenvMatch{file: c.file, loc: loc, eval: &e}, nil
}

// checkoutRoot returns the nearest directory at or above dir that contains
// .git, a directory or a file, looking no further up than stop.
func checkoutRoot(dir, stop string) (string, bool) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d, true
		}
		if d == stop || d == filepath.Dir(d) || !within(d, stop) {
			return "", false
		}
	}
}

// worktreeMain returns the main checkout of the repository whose worktree
// dir is in, and the top of that worktree: the nearest directory at or above
// dir with a .git file. ok is false if dir is not in a worktree.
func worktreeMain(dir string) (main, top string, ok bool) {
	for top = dir; ; top = filepath.Dir(top) {
		if info, err := os.Lstat(filepath.Join(top, ".git")); err == nil {
			if info.IsDir() {
				return "", "", false // a main checkout
			}
			break
		}
		if top == filepath.Dir(top) {
			return "", "", false
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", "", false
	}
	// A submodule has a .git file too, with its common dir elsewhere.
	common := strings.TrimSpace(string(out))
	if filepath.Base(common) != ".git" {
		return "", "", false
	}
	return filepath.Dir(common), top, true
}

// A pathenvEntry is the code of a [[run]] entry of a configuration, with
// includes spliced in.
type pathenvEntry struct {
	on     []string // the events it runs on
	code   string
	origin string // the file and entry, and the entries that included it
}

// runsOn reports whether the entry runs on event.
func (e pathenvEntry) runsOn(event string) bool {
	return slices.Contains(e.on, event)
}

// entries returns the entries of the matched configuration for both events,
// with the entries of the include files spliced in where they are included,
// and the include files used. Entries and includes whose condition is false
// are left out.
func (m *pathenvMatch) entries() ([]pathenvEntry, []*pathenvFile, error) {
	var entries []pathenvEntry
	var includes []*pathenvFile
	var walk func(f *pathenvFile, from string) error
	walk = func(f *pathenvFile, from string) error {
		for i, r := range f.Run {
			origin := fmt.Sprintf("%s [[run]] %d", f.name, i+1)
			key := fmt.Sprintf("run %d", i+1)
			ok, err := m.eval.condition(f, key+": condition", r.Condition)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if r.Include != "" {
				t := m.eval.s.byName[r.Include]
				ok, err := m.eval.condition(t, "condition", t.Condition)
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				if !slices.Contains(includes, t) {
					includes = append(includes, t)
				}
				next := origin
				if from != "" {
					next += ", from " + from
				}
				if err := walk(t, next); err != nil {
					return err
				}
				continue
			}
			var code string
			if r.Src != "" {
				if code, err = m.eval.render(f, key+": src", r.Src); err != nil {
					return err
				}
			} else {
				path := r.File
				if !filepath.IsAbs(path) {
					path = filepath.Join(filepath.Dir(f.path), path)
				}
				q, err := shellQuote(path)
				if err != nil {
					return fmt.Errorf("%s: %s: file: %w", f.path, key, err)
				}
				code = ". " + q
			}
			on := []string{pathenvActivate}
			if len(r.On) > 0 {
				on = slices.Clone(r.On)
			}
			if from != "" {
				origin += ", from " + from
			}
			entries = append(entries, pathenvEntry{on: on, code: code, origin: origin})
		}
		return nil
	}
	if err := walk(m.file, ""); err != nil {
		return nil, nil, err
	}
	return entries, includes, nil
}
