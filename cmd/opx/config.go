package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is ~/.config/opx/config.toml.
type Config struct {
	DefaultProfile string `toml:"default_profile"`
}

// Profile is ~/.config/opx/profiles/<name>.toml. The zero Profile uses
// op's default account and asks for the password.
type Profile struct {
	Name string `toml:"-"`
	// Account is passed to op as --account: a shorthand, sign-in address,
	// account ID or user ID.
	Account string `toml:"account"`
	// PasswordCommand prints the account password on its first line.
	PasswordCommand []string `toml:"password_command"`
}

// defaultConfigDir is $XDG_CONFIG_HOME/opx or ~/.config/opx, also on macOS
// where os.UserConfigDir is ~/Library/Application Support.
func defaultConfigDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "opx"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opx"), nil
}

// loadProfile loads the profile called name, or else the default one:
// default_profile from config.toml, or the only profile there is.
func loadProfile(dir, name string) (Profile, error) {
	var cfg Config
	if err := decodeTOML(filepath.Join(dir, "config.toml"), &cfg); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Profile{}, err
	}
	names, err := profileNames(dir)
	if err != nil {
		return Profile{}, err
	}
	if name == "" {
		name = cfg.DefaultProfile
	}
	switch {
	case name != "":
		if !slices.Contains(names, name) {
			return Profile{}, fmt.Errorf("no profile %q in %s (have: %s)",
				name, filepath.Join(dir, "profiles"), strings.Join(names, ", "))
		}
	case len(names) == 1:
		name = names[0]
	case len(names) == 0:
		return Profile{}, nil
	default:
		return Profile{}, fmt.Errorf("profiles %s: set default_profile in %s or pick one with -p",
			strings.Join(names, ", "), filepath.Join(dir, "config.toml"))
	}
	p := Profile{Name: name}
	if err := decodeTOML(filepath.Join(dir, "profiles", name+".toml"), &p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func profileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "profiles"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".toml"); ok && !e.IsDir() {
			names = append(names, name)
		}
	}
	return names, nil
}

func decodeTOML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := toml.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if e, ok := errors.AsType[*toml.StrictMissingError](err); ok {
			return fmt.Errorf("%s: unknown keys:\n%s", path, e.String())
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
