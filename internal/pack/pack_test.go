package pack

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

func fixtureItems(t *testing.T) map[item.ID]scan.Scanned {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "skills/demo"), 0o755)
	os.WriteFile(filepath.Join(d, "skills/demo/SKILL.md"), []byte("demo"), 0o644)
	os.WriteFile(filepath.Join(d, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	m, _, err := scan.Claude(d, settings.KeyOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBuildOpenLoadRoundTrip(t *testing.T) {
	items := fixtureItems(t)
	man := Manifest{Name: "test-pack", Version: "1.0.0", Author: "t", CreatedAt: "2026-08-30T00:00:00Z",
		Items: map[item.ID]PackItem{}}
	for id, s := range items {
		man.Items[id] = PackItem{Hash: s.Hash}
	}
	out := filepath.Join(t.TempDir(), "test.skillpack")
	if err := Build(out, man, items); err != nil {
		t.Fatal(err)
	}
	got, err := Open(out)
	if err != nil || got.Name != "test-pack" || len(got.Items) != len(items) {
		t.Fatalf("open: %+v %v", got, err)
	}
	man2, contents, err := Load(out, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != len(man2.Items) {
		t.Fatalf("load contents: %v", contents)
	}
	for id, s := range contents {
		if s.Hash != man2.Items[id].Hash {
			t.Fatalf("%s hash mismatch after round trip", id)
		}
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	evil := filepath.Join(t.TempDir(), "evil.skillpack")
	f, _ := os.Create(evil)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("owned")
	tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	f.Close()
	if err := Extract(evil, t.TempDir()); err == nil {
		t.Fatal("traversal entry must be rejected")
	}
}

// singleEntryPack writes a .skillpack containing exactly one tar entry,
// for exercising Extract's per-entry validation.
func singleEntryPack(t *testing.T, hdr tar.Header, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "evil.skillpack")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr.Size = int64(len(body))
	if err := tw.WriteHeader(&hdr); err != nil {
		t.Fatal(err)
	}
	if len(body) > 0 {
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractRejectsAbsolutePath(t *testing.T) {
	p := singleEntryPack(t, tar.Header{Name: "/etc/evil", Mode: 0o644, Typeflag: tar.TypeReg}, []byte("owned"))
	if err := Extract(p, t.TempDir()); err == nil {
		t.Fatal("absolute path entry must be rejected")
	}
}

func TestExtractRejectsSymlink(t *testing.T) {
	p := singleEntryPack(t, tar.Header{
		Name: "link", Linkname: "../../etc/passwd", Mode: 0o644, Typeflag: tar.TypeSymlink,
	}, nil)
	if err := Extract(p, t.TempDir()); err == nil {
		t.Fatal("symlink entry must be rejected")
	}
}

func TestExtractRejectsHardlink(t *testing.T) {
	p := singleEntryPack(t, tar.Header{
		Name: "hardlink", Linkname: "somefile", Mode: 0o644, Typeflag: tar.TypeLink,
	}, nil)
	if err := Extract(p, t.TempDir()); err == nil {
		t.Fatal("hardlink entry must be rejected")
	}
}

func TestExtractRejectsTraversalDir(t *testing.T) {
	p := singleEntryPack(t, tar.Header{Name: "../../evil-dir", Mode: 0o755, Typeflag: tar.TypeDir}, nil)
	if err := Extract(p, t.TempDir()); err == nil {
		t.Fatal("traversal dir entry must be rejected")
	}
}

func TestLoadDetectsTamper(t *testing.T) {
	items := fixtureItems(t)
	man := Manifest{Name: "p", Version: "1", Items: map[item.ID]PackItem{}}
	for id := range items {
		man.Items[id] = PackItem{Hash: "wrong"}
	}
	out := filepath.Join(t.TempDir(), "p.skillpack")
	Build(out, man, items)
	if _, _, err := Load(out, t.TempDir()); err == nil {
		t.Fatal("hash mismatch must fail Load")
	}
}
