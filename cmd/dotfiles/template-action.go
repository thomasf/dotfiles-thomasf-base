package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

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

func (t *TemplateAction) Paths() []string {
	return []string{filepath.Join(t.SrcRoot, t.Src), t.DstAbsolutePath()}
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
