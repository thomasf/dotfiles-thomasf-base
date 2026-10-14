package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

var RepositoryConfigFileNames = []string{"dotfiles.toml", ".dotfiles.toml"}

var ErrConfigMissing = errors.New("config file missing")

type Mount struct {
	Src       string `toml:"src"`
	Dst       string `toml:"dst"`
	Condition string `toml:"condition,omitempty"`
}

func (m Mount) ShouldRun(r *Repository) bool {
	return evalCondition(r.dstRoot, r.configPath, fmt.Sprintf("Mount(src=%s, dst=%s)", m.Src, m.Dst), m.Condition)
}

type Script struct {
	Condition string `toml:"condition,omitempty"`
	Phase     string `toml:"phase,omitempty"` // pre, post or default empty/not set
	Src       string `toml:"src"`
}

func (s Script) ShouldRun(r *Repository) bool {
	return evalCondition(r.dstRoot, r.configPath, fmt.Sprintf("Script(src=%s, phase=%s)", s.Src, s.Phase), s.Condition)
}

type Go struct {
	Src       string `toml:"src"`
	Condition string `toml:"condition,omitempty"`
}

func (g Go) ShouldRun(r *Repository) bool {
	return evalCondition(r.dstRoot, r.configPath, fmt.Sprintf("Go(src=%s)", g.Src), g.Condition)
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
	return evalCondition(r.dstRoot, r.configPath, fmt.Sprintf("Template(src=%s)", t.Src), t.Condition)
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
	return evalCondition(r.dstRoot, r.configPath, fmt.Sprintf("Ignore(match=%v)", i.Match), i.Condition)
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
	Repositories Repositories      `toml:"repositories"`
}

type Repository struct {
	force      bool
	copy       bool
	srcRoot    string
	dstRoot    string
	configPath string
	config     Config
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

func (r *Repository) LoadConfig() error {
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
		return err
	}
	if err := config.validate(); err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}
	if err := config.render(r.dstRoot); err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}

	r.config = config

	return nil
}

func (c *Config) validate() error {
	var errs []error
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
	return errors.Join(errs...)
}

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

// render renders every src and dst of mounts, go packages and templates as a
// template (see renderTemplate). An entry with a src or dst that renders to an
// empty string is dropped; for templates, only that dst is dropped.
func (c *Config) render(dstRoot string) error {
	var errs []error
	render := func(name, value string) string {
		out, err := renderValue(name, value, dstRoot)
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

	return errors.Join(errs...)
}

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

func (r *Repository) IsIgnored(path string) bool {
	base := filepath.Base(path)
	if alwaysIgnoredFiles[base] {
		return true
	}

	patterns := []string{".#.*"}
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
