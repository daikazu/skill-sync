package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFilePreservesExecuteBit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "script.sh")
	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("execute bits lost: got mode %v", info.Mode().Perm())
	}
}
