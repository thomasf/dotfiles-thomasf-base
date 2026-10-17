package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type SymlinkAction struct {
	SrcRoot string
	Src     string
	DstRoot string
	Dst     string
	Force   bool
}

func (s *SymlinkAction) DstAbsolutePath() string {
	return filepath.Join(s.DstRoot, s.Dst)
}

func (s *SymlinkAction) String() string {
	return fmt.Sprintf("[%s] symlink: %s -> %s", filepath.Base(s.SrcRoot), s.Src, s.Dst)
}

func (s *SymlinkAction) Run() error {
	fullSrc := filepath.Join(s.SrcRoot, s.Src)
	fullDst := filepath.Join(s.DstRoot, s.Dst)

	absSrc, err := filepath.Abs(fullSrc)
	if err != nil {
		return err
	}

	dstDir := filepath.Dir(fullDst)
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}

	if info, err := os.Lstat(fullDst); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(fullDst)
			if err == nil && target == absSrc {
				// already correctly linked
				return nil
			}
		}

		if !s.Force {
			return fmt.Errorf("target exists: %s", fullDst)
		}

		if err := os.RemoveAll(fullDst); err != nil {
			return err
		}
	}

	return os.Symlink(absSrc, fullDst)
}
