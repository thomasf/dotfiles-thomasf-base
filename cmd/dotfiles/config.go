package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

var RepositoryConfigFileNames = []string{"dotfiles.toml", ".dotfiles.toml"}

var ErrConfigMissing = errors.New("config file missing")

type Mount struct {
	Src string `toml:"src"`
	Dst string `toml:"dst"`
}

type Config struct {
	Ignore     []string          `toml:"ignore"`
	Mount      []Mount           `toml:"mount"`
	Git        map[string]string `toml:"git"`
	Script     string            `toml:"script"`
	ScriptPre  string            `toml:"script-pre"`
	ScriptPost string            `toml:"script-post"`
}

type Repository struct {
	name    string
	srcPath string
	config  Config
	force   bool
	dstPath string
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
	for _, name := range RepositoryConfigFileNames {
		configPath := filepath.Join(r.srcPath, name)
		data, err = os.ReadFile(configPath)
		if err == nil {
			break
		}
	}

	if err != nil {
		return ErrConfigMissing
	}

	var config Config

	if err := toml.Unmarshal(data, &config); err != nil {
		return err
	}

	r.config.Ignore = config.Ignore
	r.config.Mount = config.Mount
	r.config.Git = config.Git
	r.config.Script = config.Script
	r.config.ScriptPre = config.ScriptPre
	r.config.ScriptPost = config.ScriptPost

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
	for _, pattern := range append([]string{".#.*"}, r.config.Ignore...) {
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
