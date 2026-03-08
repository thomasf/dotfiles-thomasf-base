package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/expr-lang/expr"
	"github.com/pelletier/go-toml/v2"
)

var RepositoryConfigFileNames = []string{"dotfiles.toml", ".dotfiles.toml"}

var ErrConfigMissing = errors.New("config file missing")

type Mount struct {
	Src       string `toml:"src"`
	Dst       string `toml:"dst"`
	Condition string `toml:"condition,omitempty"`
}

type ConditionEnvironment struct {
	OS       string `expr:"os"`
	Hostname string `expr:"hostname"`
}

func (c ConditionEnvironment) Getenv(key string) string {
	return os.Getenv(key)
}

func evalCondition(configPath, element, condition string) bool {
	if condition == "" {
		return true
	}
	hostname, _ := os.Hostname()
	env := ConditionEnvironment{
		OS:       runtime.GOOS,
		Hostname: hostname,
	}
	program, err := expr.Compile(condition, expr.Env(env), expr.AsBool())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error compiling expression in file: %s, element: %s\n", configPath, element)
		fmt.Fprintf(os.Stderr, "Condition: %s\n", condition)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	out, err := expr.Run(program, env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running expression in file: %s, element: %s\n", configPath, element)
		fmt.Fprintf(os.Stderr, "Condition: %s\n", condition)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	return out.(bool)
}

func (m Mount) ShouldRun(r *Repository) bool {
	return evalCondition(r.configPath, fmt.Sprintf("Mount(src=%s, dst=%s)", m.Src, m.Dst), m.Condition)
}

type Script struct {
	Condition string `toml:"condition,omitempty"`
	Phase     string `toml:"phase,omitempty"` // pre, post or default empty/not set
	Src       string `toml:"src"`
}

func (s Script) ShouldRun(r *Repository) bool {
	return evalCondition(r.configPath, fmt.Sprintf("Script(src=%s, phase=%s)", s.Src, s.Phase), s.Condition)
}

type Ignore struct {
	Condition string   `toml:"condition,omitempty"`
	Match     []string `toml:"match"`
}

func (i Ignore) ShouldRun(r *Repository) bool {
	return evalCondition(r.configPath, fmt.Sprintf("Ignore(match=%v)", i.Match), i.Condition)
}

type Config struct {
	Ignore []Ignore          `toml:"ignore"`
	Mount  []Mount           `toml:"mount"`
	Git    map[string]string `toml:"git"`
	Script []Script          `toml:"script"`
}

type Repository struct {
	name       string
	srcPath    string
	config     Config
	force      bool
	dstPath    string
	configPath string
}

func NewRepository(name string, srcPath string, force bool, dstPath string) *Repository {
	return &Repository{
		name:    name,
		srcPath: srcPath,
		force:   force,
		dstPath: dstPath,
	}
}

func (r *Repository) LoadConfig() error {
	var data []byte
	var err error
	var configPath string
	for _, name := range RepositoryConfigFileNames {
		configPath = filepath.Join(r.srcPath, name)
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

	if err := toml.Unmarshal(data, &config); err != nil {
		return err
	}

	r.config.Ignore = config.Ignore
	r.config.Mount = config.Mount
	r.config.Git = config.Git
	r.config.Script = config.Script

	return nil
}

var alwaysIgnoredFiles = map[string]bool{
	"dotfiles.toml":  true,
	".dotfiles.toml": true,
	".dotfilesrc":    true, // TODO: remove after complete migration
	"INSTALLER":      true, // TODO: handle
	"PREINSTALLER":   true, // TODO: handle
	".git":           true,
	".gitignore":     true,
	"go.mod":         true,
	"go.sum":         true,
	"cmd":            true,
	"pkg":            true,
	"vendor":         true,
	".DS_Store":      true,
	"README.md":      true,
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
		rel, err := filepath.Rel(r.srcPath, path)
		if err == nil {
			matched, err = filepath.Match(pattern, rel)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}
