package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// An Action is one change that a command makes, such as creating a symlink,
// writing a file or running git. Commands build a list of actions and then
// run them in order, or for plan and -dry-run only print them.
type Action interface {
	// Run makes the change. An error fails only this action; the actions
	// after it still run.
	Run() error

	// String describes in one line what Run does, starting with the name of
	// the repository in brackets:
	//
	//	[base] symlink: bashrc -> .bashrc
	//
	// It must not change anything: plan and -dry-run print it instead of
	// calling Run. Errors from Run are reported prefixed with it.
	String() string
}

// A Targeter is an [Action] that writes a single destination path. Before
// running any action, install warns about Targeters that share a destination,
// since they would overwrite each other or fail. Actions that write several
// paths, or none, do not implement it.
type Targeter interface {
	// DstAbsolutePath returns the path that Run writes, joined to the
	// destination root, so that actions writing the same file return the
	// same path.
	DstAbsolutePath() string
}

// writeFile makes the regular file path have the content data and the
// permission bits perm, and reports whether it changed anything. A file that
// already has that content is never rewritten; if only its permissions
// differ, they are changed in place. Otherwise the file is replaced
// atomically, and missing parent directories are created.
//
// A path that is a symlink is refused, so that writing never changes a file
// in another location, such as one linked by a mount.
func writeFile(path string, data []byte, perm fs.FileMode) (bool, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&fs.ModeSymlink != 0 {
			return false, fmt.Errorf("refusing to write through symlink: %s", path)
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("not a regular file: %s", path)
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, data) {
			if info.Mode().Perm() == perm {
				return false, nil
			}
			return true, os.Chmod(path, perm)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return false, err
		}
	default:
		return false, err
	}

	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return false, err
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Chmod(perm), f.Close())
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return false, err
	}
	return true, nil
}
