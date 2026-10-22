package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type CopyAction struct {
	SrcRoot string
	Src     string
	DstRoot string
	Dst     string
	Force   bool
}

func (c *CopyAction) DstAbsolutePath() string {
	return filepath.Join(c.DstRoot, c.Dst)
}

func (c *CopyAction) Paths() []string {
	return []string{filepath.Join(c.SrcRoot, c.Src), c.DstAbsolutePath()}
}

func (c *CopyAction) String() string {
	return fmt.Sprintf("[%s] copy: %s -> %s", filepath.Base(c.SrcRoot), c.Src, c.Dst)
}

// Run makes Dst a copy of Src, file by file. Files that already have the
// content and permissions of their source are left alone, and files in Dst
// that are not in Src are kept. Symlinks in Src are copied as what they point
// to; dangling ones, such as Emacs lock files, are skipped.
//
// A destination of the wrong type, such as a symlink from an install without
// copying or a file where Src has a directory, is only replaced with Force.
func (c *CopyAction) Run() error {
	return c.copy(filepath.Join(c.SrcRoot, c.Src), c.DstAbsolutePath())
}

func (c *CopyAction) copy(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := c.clear(dst, info.IsDir()); err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		changed, err := writeFile(dst, data, info.Mode().Perm())
		if changed {
			rel, _ := filepath.Rel(c.DstRoot, dst)
			fmt.Fprintf(os.Stdout, "[%s] copy: %s: updated\n", filepath.Base(c.SrcRoot), rel)
		}
		return err
	}

	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		path := filepath.Join(src, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
				continue
			}
		}
		errs = append(errs, c.copy(path, filepath.Join(dst, e.Name())))
	}
	return errors.Join(errs...)
}

// clear removes dst if it is not a directory when dir is set, or not a
// regular file otherwise, which requires Force. A dst that does not exist is
// fine.
func (c *CopyAction) clear(dst string, dir bool) error {
	info, err := os.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if dir && info.IsDir() || !dir && info.Mode().IsRegular() {
		return nil
	}
	if !c.Force {
		return fmt.Errorf("target exists: %s", dst)
	}
	return os.RemoveAll(dst)
}
