// Package pack builds, validates, and installs .skillpack archives:
// portable bundles that double as backups and team distributions.
package pack

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/repo"
	"github.com/daikazu/skill-sync/internal/scan"
)

type PackItem struct {
	Hash        string `json:"hash"`
	Description string `json:"description,omitempty"`
}

type Manifest struct {
	Name      string               `json:"name"`
	Version   string               `json:"version"`
	Author    string               `json:"author,omitempty"`
	CreatedAt string               `json:"createdAt"`
	Items     map[item.ID]PackItem `json:"items"`
}

func Build(outPath string, man Manifest, contents map[item.ID]scan.Scanned) error {
	stage, err := os.MkdirTemp("", "skillpack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for id := range man.Items {
		s, ok := contents[id]
		if !ok {
			return fmt.Errorf("manifest item %s has no content", id)
		}
		if err := repo.WriteItem(stage, s); err != nil {
			return err
		}
	}
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), append(mb, '\n'), 0o644); err != nil {
		return err
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(stage, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || !d.Type().IsRegular() {
			return werr
		}
		rel, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: info.Size(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func eachEntry(path string, fn func(hdr *tar.Header, r io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a .skillpack (gzip): %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(hdr, tr); err != nil {
			return err
		}
	}
}

func safeRel(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe entry %q", name)
	}
	return clean, nil
}

func Open(path string) (*Manifest, error) {
	var man *Manifest
	err := eachEntry(path, func(hdr *tar.Header, r io.Reader) error {
		if hdr.Name != "manifest.json" {
			return nil
		}
		var m Manifest
		if err := json.NewDecoder(r).Decode(&m); err != nil {
			return fmt.Errorf("manifest.json: %w", err)
		}
		man = &m
		return nil
	})
	if err != nil {
		return nil, err
	}
	if man == nil {
		return nil, fmt.Errorf("%s: no manifest.json — not a skillpack", path)
	}
	if man.Items == nil {
		man.Items = map[item.ID]PackItem{}
	}
	return man, nil
}

func Extract(path, destDir string) error {
	return eachEntry(path, func(hdr *tar.Header, r io.Reader) error {
		if hdr.Typeflag == tar.TypeDir {
			return nil
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("unsafe entry %q: not a regular file", hdr.Name)
		}
		rel, err := safeRel(hdr.Name)
		if err != nil {
			return err
		}
		dst := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, r)
		return err
	})
}

// Load extracts the pack and returns its manifest plus the scanned
// content for each manifest item, verifying hashes.
func Load(path, tmpDir string) (*Manifest, map[item.ID]scan.Scanned, error) {
	if err := Extract(path, tmpDir); err != nil {
		return nil, nil, err
	}
	man, err := Open(path)
	if err != nil {
		return nil, nil, err
	}
	all, _, err := scan.Repo(tmpDir)
	if err != nil {
		return nil, nil, err
	}
	contents := map[item.ID]scan.Scanned{}
	for id, pi := range man.Items {
		s, ok := all[id]
		if !ok {
			return nil, nil, fmt.Errorf("pack is missing item %s", id)
		}
		if s.Hash != pi.Hash {
			return nil, nil, fmt.Errorf("pack item %s content does not match its manifest hash", id)
		}
		contents[id] = s
	}
	return man, contents, nil
}
