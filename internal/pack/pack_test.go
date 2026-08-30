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
