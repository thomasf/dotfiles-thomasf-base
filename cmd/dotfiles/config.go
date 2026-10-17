package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.starlark.net/starlark"
)

var RepositoryConfigFileNames = []string{"dotfiles.toml", ".dotfiles.toml"}

var ErrConfigMissing = errors.New("config file missing")

type Mount struct {
	Src       string `toml:"src"`
	Dst       string `toml:"dst"`
	Condition string `toml:"condition,omitempty"`
}

func (m Mount) ShouldRun(r *Repository) bool {
	return evalCondition(r.exprEnv(), r.configPath, fmt.Sprintf("Mount(src=%s, dst=%s)", m.Src, m.Dst), m.Condition)
}

type Script struct {
	Condition string `toml:"condition,omitempty"`
	Phase     string `toml:"phase,omitempty"` // pre, post or default empty/not set
	Src       string `toml:"src"`
}

func (s Script) ShouldRun(r *Repository) bool {
	return evalCondition(r.exprEnv(), r.configPath, fmt.Sprintf("Script(src=%s, phase=%s)", s.Src, s.Phase), s.Condition)
}

type Go struct {
	Src       string `toml:"src"`
	Condition string `toml:"condition,omitempty"`
}

func (g Go) ShouldRun(r *Repository) bool {
	return evalCondition(r.exprEnv(), r.configPath, fmt.Sprintf("Go(src=%s)", g.Src), g.Condition)
}

// Template renders the Go text/template Src and writes the result to each
// path in Dst. Mode, an octal string such as "0644", sets the permissions of
// the output; by default they are those of Src. Existing says what happens
// when a destination already exists with different content.
type Template struct {
	Src       string         `toml:"src"`
	Dst       StringList     `toml:"dst"`
	Mode      string         `toml:"mode,omitempty"`
	Existing  ExistingAction `toml:"existing,omitempty"`
	Condition string         `toml:"condition,omitempty"`
}

// ExistingAction says what a template does when its destination already
// exists with different content. The empty value means ExistingOverwrite.
type ExistingAction string

const (
	ExistingOverwrite ExistingAction = "overwrite" // replace the destination
	ExistingSkip      ExistingAction = "skip"      // leave an existing destination alone
	ExistingError     ExistingAction = "error"     // fail, unless forced with -f
)

func (t Template) ShouldRun(r *Repository) bool {
	return evalCondition(r.exprEnv(), r.configPath, fmt.Sprintf("Template(src=%s)", t.Src), t.Condition)
}

// Starlark runs the Starlark code Src on each file in Dst: the file is
// decoded into the dict doc, Src changes it, and the result is written back
// if it differs. Missing says what happens when a file does not exist. Format
// overrides the format implied by the extensions of Dst, Indent sets the
// indent of JSON output, and Mode, an octal string, the permissions
// of a created file.
type Starlark struct {
	Src       string        `toml:"src"`
	Dst       StringList    `toml:"dst"`
	Condition string        `toml:"condition,omitempty"`
	Missing   MissingAction `toml:"missing,omitempty"`
	Format    string        `toml:"format,omitempty"`
	Indent    int           `toml:"indent,omitempty"`
	Mode      string        `toml:"mode,omitempty"`

	program *starlark.Program // Src, compiled by Config.validate
}

// defaultIndent is the indent of JSON written by a [[starlark]] entry without
// indent.
const defaultIndent = 2

// MissingAction says what a [[starlark]] entry does when its destination
// does not exist. The empty value means MissingSkip.
type MissingAction string

const (
	MissingSkip   MissingAction = "skip"   // do nothing for that destination
	MissingCreate MissingAction = "create" // start from an empty dict and write it
	MissingError  MissingAction = "error"  // fail
)

func (t Starlark) ShouldRun(r *Repository) bool {
	return evalCondition(r.exprEnv(), r.configPath, fmt.Sprintf("Starlark(dst=%v)", t.Dst), t.Condition)
}

// StringList is a list of strings that can be written in TOML as a single
// string or as an array of strings.
type StringList []string

// UnmarshalText decodes a single TOML string. Arrays are decoded by go-toml
// directly, because StringList is a slice.
func (l *StringList) UnmarshalText(b []byte) error {
	*l = StringList{string(b)}
	return nil
}

type Ignore struct {
	Condition string   `toml:"condition,omitempty"`
	Match     []string `toml:"match"`
}

func (i Ignore) ShouldRun(r *Repository) bool {
	return evalCondition(r.exprEnv(), r.configPath, fmt.Sprintf("Ignore(match=%v)", i.Match), i.Condition)
}

// Remote kinds.
const (
	RemoteKindPrivate = "private" // pulled from and pushed to
	RemoteKindBackup  = "backup"  // pushed to; skipped with a warning when unavailable
	RemoteKindPublish = "publish" // pushed to by publish, only from public repositories
)

// Remote is a git remote template. Every occurrence of {name} in URL and
// PushURL is replaced by the repository name. PushURL, when set, is used for
// pushing and URL only for fetching, as with git's remote.<name>.pushurl.
type Remote struct {
	Kind      string `toml:"kind"`
	URL       string `toml:"url"`
	PushURL   string `toml:"pushurl,omitempty"`
	Condition string `toml:"condition,omitempty"`
}

// Repositories lists the known dotfiles repositories and the remotes they
// should have. The sections from all repositories are merged.
type Repositories struct {
	Names  []string          `toml:"names"`
	Remote map[string]Remote `toml:"remote"`
}

type Config struct {
	Public       bool              `toml:"public,omitempty,omitzero"`
	Ignore       []Ignore          `toml:"ignore"`
	Mount        []Mount           `toml:"mount"`
	Git          map[string]string `toml:"git"`
	Script       []Script          `toml:"script"`
	Go           []Go              `toml:"go"`
	Template     []Template        `toml:"template"`
	Starlark     []Starlark        `toml:"starlark"`
	Repositories Repositories      `toml:"repositories"`
	Vars         map[string]string `toml:"vars"`
	DefaultVars  map[string]string `toml:"default_vars"` // only for this repository
}

type Repository struct {
	force      bool
	copy       bool
	srcRoot    string
	dstRoot    string
	configPath string
	config     Config
	vars       map[string]string // resolved [vars] (see resolveVars)
}

type Option func(*Repository)

func WithForce(force bool) Option {
	return func(r *Repository) {
		r.force = force
	}
}

func WithCopy(copy bool) Option {
	return func(r *Repository) {
		r.copy = copy
	}
}

func NewRepository(srcRoot, dstRoot string, opts ...Option) *Repository {
	r := &Repository{
		srcRoot: srcRoot,
		dstRoot: dstRoot,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// LoadConfig loads the config of the repository on its own: only its own
// [vars] and [default_vars] are available. Use loadRepos to load several
// repositories that share their vars.
func (r *Repository) LoadConfig() error {
	if err := r.readConfig(); err != nil {
		return err
	}
	views, _ := mergeVars([]*Repository{r})
	return r.resolve(views[r])
}

// readConfig reads, decodes and validates the config file. The config is not
// usable until resolve is called.
func (r *Repository) readConfig() error {
	var data []byte
	var err error
	var configPath string
	for _, name := range RepositoryConfigFileNames {
		configPath = filepath.Join(r.srcRoot, name)
		data, err = os.ReadFile(configPath)
		if err == nil {
			break
		}
	}

	if err != nil {
		return ErrConfigMissing
	}

	r.configPath = configPath

	var config Config
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		var derr *toml.DecodeError
		if errors.As(err, &derr) {
			row, col := derr.Position()
			return fmt.Errorf("%s:%d:%d: %s: %w", configPath, row, col, strings.Join(derr.Key(), "."), err)
		}
		return err
	}
	if err := config.validate(); err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}

	r.config = config

	return nil
}

// resolve resolves raw, the [vars] the repository sees, and renders the
// templated paths of its config with them.
func (r *Repository) resolve(raw map[string]string) error {
	vars, err := resolveVars(raw, r.dstRoot)
	if err != nil {
		return fmt.Errorf("%s: %w", r.configPath, err)
	}
	r.vars = vars
	if err := r.config.render(r.dstRoot, vars); err != nil {
		return fmt.Errorf("%s: %w", r.configPath, err)
	}
	return nil
}

// exprEnv returns the environment of the repository's conditions.
func (r *Repository) exprEnv() map[string]any {
	return exprEnv(r.dstRoot, r.vars)
}

func (c *Config) validate() error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(c.Vars)) {
		if !varNameRe.MatchString(name) {
			errs = append(errs, fmt.Errorf("vars.%s: invalid name: want letters, digits and _, not starting with a digit", name))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.DefaultVars)) {
		if !varNameRe.MatchString(name) {
			errs = append(errs, fmt.Errorf("default_vars.%s: invalid name: want letters, digits and _, not starting with a digit", name))
		}
		if _, ok := c.Vars[name]; ok {
			errs = append(errs, fmt.Errorf("default_vars.%s: also defined in [vars]", name))
		}
	}
	for i, t := range c.Template {
		if t.Src == "" {
			errs = append(errs, fmt.Errorf("template %d: src is required", i))
		}
		if len(t.Dst) == 0 {
			errs = append(errs, fmt.Errorf("template %d: dst is required", i))
		}
		if _, err := parseFileMode(t.Mode); err != nil {
			errs = append(errs, fmt.Errorf("template %d: %w", i, err))
		}
		switch t.Existing {
		case "", ExistingOverwrite, ExistingSkip, ExistingError:
		default:
			errs = append(errs, fmt.Errorf("template %d: invalid existing %q: want overwrite, skip or error", i, t.Existing))
		}
	}
	for i := range c.Starlark {
		t := &c.Starlark[i]
		if t.Src == "" {
			errs = append(errs, fmt.Errorf("starlark %d: src is required", i))
		} else if prog, err := compileStarlark(fmt.Sprintf("starlark[%d].src", i), t.Src); err != nil {
			errs = append(errs, err)
		} else {
			t.program = prog
		}
		if len(t.Dst) == 0 {
			errs = append(errs, fmt.Errorf("starlark %d: dst is required", i))
		}
		switch t.Missing {
		case "", MissingSkip, MissingCreate, MissingError:
		default:
			errs = append(errs, fmt.Errorf("starlark %d: invalid missing %q: want skip, create or error", i, t.Missing))
		}
		switch t.Format {
		case "", formatJSON, formatTOML:
		default:
			errs = append(errs, fmt.Errorf("starlark %d: invalid format %q: want json or toml", i, t.Format))
		}
		if t.Indent < 0 || t.Indent > 9 {
			errs = append(errs, fmt.Errorf("starlark %d: invalid indent %d: want 1 to 9", i, t.Indent))
		}
		if _, err := parseFileMode(t.Mode); err != nil {
			errs = append(errs, fmt.Errorf("starlark %d: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

// varNameRe matches the names of vars, which must work as vars.x in
// expressions and .vars.x in templates.
var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseFileMode parses permission bits written in octal, as for chmod: "0644"
// or "644". The empty string is 0.
func parseFileMode(s string) (fs.FileMode, error) {
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v == 0 || v > 0o777 {
		return 0, fmt.Errorf("invalid mode %q: want octal permissions such as \"0644\"", s)
	}
	return fs.FileMode(v), nil
}

// render renders every src and dst of mounts, go packages and templates, the
// src of scripts, every [git] value and every dst of [[starlark]] entries as
// a template (see renderTemplate) with the resolved [vars] vars. An entry with a src or dst
// that renders to an empty string is dropped; for templates and [[starlark]]
// entries, only that dst is dropped.
func (c *Config) render(dstRoot string, vars map[string]string) error {
	var errs []error
	render := func(name, value string) string {
		out, err := renderValue(name, value, dstRoot, vars)
		errs = append(errs, err)
		return out
	}

	var mounts []Mount
	for i, m := range c.Mount {
		m.Src = render(fmt.Sprintf("mount[%d].src", i), m.Src)
		m.Dst = render(fmt.Sprintf("mount[%d].dst", i), m.Dst)
		if m.Src != "" && m.Dst != "" {
			mounts = append(mounts, m)
		}
	}
	c.Mount = mounts

	var gos []Go
	for i, g := range c.Go {
		g.Src = render(fmt.Sprintf("go[%d].src", i), g.Src)
		if g.Src != "" {
			gos = append(gos, g)
		}
	}
	c.Go = gos

	for _, k := range slices.Sorted(maps.Keys(c.Git)) {
		c.Git[k] = render("git."+k, c.Git[k]) // an empty value unsets the key
	}

	var scripts []Script
	for i, sc := range c.Script {
		if sc.Src = render(fmt.Sprintf("script[%d].src", i), sc.Src); sc.Src != "" {
			scripts = append(scripts, sc)
		}
	}
	c.Script = scripts

	var templates []Template
	for i, t := range c.Template {
		t.Src = render(fmt.Sprintf("template[%d].src", i), t.Src)
		var dsts StringList
		for j, d := range t.Dst {
			if d = render(fmt.Sprintf("template[%d].dst[%d]", i, j), d); d != "" {
				dsts = append(dsts, d)
			}
		}
		t.Dst = dsts
		if t.Src != "" && len(t.Dst) > 0 {
			templates = append(templates, t)
		}
	}
	c.Template = templates

	var entries []Starlark
	for i, t := range c.Starlark {
		var dsts StringList
		for j, d := range t.Dst {
			name := fmt.Sprintf("starlark[%d].dst[%d]", i, j)
			if d = render(name, d); d == "" {
				continue
			}
			if t.Missing == MissingCreate && strings.ContainsAny(d, "*?[") {
				errs = append(errs, fmt.Errorf("%s: %s: a glob cannot be used with missing = \"create\"", name, d))
			}
			dsts = append(dsts, d)
		}
		t.Dst = dsts
		if len(t.Dst) > 0 {
			entries = append(entries, t)
		}
	}
	c.Starlark = entries

	return errors.Join(errs...)
}

// alwaysIgnoredFiles are the names that are ignored in every repository, in
// addition to alwaysIgnoredPatterns and the patterns of [[ignore]] entries.
var alwaysIgnoredFiles = map[string]bool{
	"dotfiles.toml":  true,
	".dotfiles.toml": true,
	".git":           true,
	".gitignore":     true,
	"go.mod":         true,
	"go.sum":         true,
	"go.work":        true,
	"cmd":            true,
	"pkg":            true,
	"vendor":         true,
	".DS_Store":      true,
	"README.md":      true,
	".claude":        true,
	".agents":        true,
}

// alwaysIgnoredPatterns are the patterns that are ignored in every
// repository: Emacs lock files.
var alwaysIgnoredPatterns = []string{".#.*"}

func (r *Repository) IsIgnored(path string) bool {
	base := filepath.Base(path)
	if alwaysIgnoredFiles[base] {
		return true
	}

	patterns := slices.Clone(alwaysIgnoredPatterns)
	for _, i := range r.config.Ignore {
		if i.ShouldRun(r) {
			patterns = append(patterns, i.Match...)
		}
	}

	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, base)
		if err == nil && matched {
			return true
		}
		rel, err := filepath.Rel(r.srcRoot, path)
		if err == nil {
			matched, err = filepath.Match(pattern, rel)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}
