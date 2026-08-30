// Package fsutil provides small file-copy helpers used by repo, apply,
// and pack.
package fsutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// CopyTree replaces dstDir with the regular files under srcDir.
func CopyTree(srcDir, dstDir string) error {
	if err := os.RemoveAll(dstDir); err != nil {
		return err
	}
	return filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		return CopyFile(p, filepath.Join(dstDir, rel))
	})
}
