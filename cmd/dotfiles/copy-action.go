package main

import (
	"fmt"
	"io"
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

func (c *CopyAction) String() string {
	return fmt.Sprintf("[%s] copy: %s -> %s", filepath.Base(c.SrcRoot), c.Src, c.Dst)
}

func (c *CopyAction) Run() error {
	fullSrc := filepath.Join(c.SrcRoot, c.Src)
	fullDst := filepath.Join(c.DstRoot, c.Dst)

	dstDir := filepath.Dir(fullDst)
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}

	if _, err := os.Lstat(fullDst); err == nil {
		if !c.Force {
			return fmt.Errorf("target exists: %s", fullDst)
		}

		if err := os.RemoveAll(fullDst); err != nil {
			return err
		}
	}

	srcInfo, err := os.Stat(fullSrc)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return filepath.Walk(fullSrc, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(fullSrc, path)
			if err != nil {
				return err
			}
			target := filepath.Join(fullDst, rel)
			if info.IsDir() {
				return os.MkdirAll(target, info.Mode())
			}
			return copyFile(path, target, info.Mode())
		})
	}

	return copyFile(fullSrc, fullDst, srcInfo.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
