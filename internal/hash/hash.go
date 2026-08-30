// Package hash computes deterministic SHA-256 content hashes for items.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Tree hashes a directory: sorted relative paths, each contributing
// "<path>\n<filehash>\n". Regular files only; empty dirs are ignored.
func Tree(root string) (string, error) {
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fh, err := File(p)
		if err != nil {
			return err
		}
		lines = append(lines, filepath.ToSlash(rel)+"\n"+fh+"\n")
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	return hex.EncodeToString(sum[:]), nil
}

func JSONValue(raw json.RawMessage) (string, error) {
	c, err := Canonical(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:]), nil
}

// Canonical re-marshals JSON with sorted object keys and no insignificant
// whitespace, recursively.
func Canonical(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	return json.Marshal(sortValue(v))
}

// sortValue relies on encoding/json marshaling map[string]any keys in
// sorted order; it exists to normalize nested structures uniformly.
func sortValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, vv := range t {
			m[k] = sortValue(vv)
		}
		return m
	case []any:
		for i := range t {
			t[i] = sortValue(t[i])
		}
		return t
	default:
		return v
	}
}
