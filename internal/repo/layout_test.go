package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

func TestWriteAndDeleteFileItems(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("s"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "x.md"), []byte("x"), 0o644)
	root := t.TempDir()

	s := scan.Scanned{ID: item.ID("skill/demo"), Path: src}
	if err := WriteItem(root, s); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "skills/demo/sub/x.md")); string(b) != "x" {
		t.Fatal("tree not copied")
	}
	// overwrite replaces stale files
	os.Remove(filepath.Join(src, "sub", "x.md"))
	if err := WriteItem(root, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills/demo/sub/x.md")); err == nil {
		t.Fatal("stale file must be gone after rewrite")
	}
	if err := DeleteItem(root, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills/demo")); err == nil {
		t.Fatal("delete failed")
	}
	if err := DeleteItem(root, s.ID); err != nil {
		t.Fatal("deleting a missing item must be a no-op, got", err)
	}
}

func TestWriteAndDeleteAgentCommandRulesItems(t *testing.T) {
	cases := []struct {
		name string
		id   item.ID
		rel  string // expected path under root
	}{
		{"agent", item.NewID(item.TypeAgent, "myagent"), filepath.Join("agents", "myagent.md")},
		{"command", item.NewID(item.TypeCommand, "mycmd"), filepath.Join("commands", "mycmd.md")},
		{"rules", item.NewID(item.TypeRules, "CLAUDE.md"), filepath.Join("rules", "CLAUDE.md")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "src.md")
			if err := os.WriteFile(src, []byte("content-"+c.name), 0o644); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()

			s := scan.Scanned{ID: c.id, Path: src}
			if err := WriteItem(root, s); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(root, c.rel))
			if err != nil || string(b) != "content-"+c.name {
				t.Fatalf("write failed: %v %q", err, b)
			}
			if err := DeleteItem(root, c.id); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, c.rel)); err == nil {
				t.Fatal("delete failed")
			}
			if err := DeleteItem(root, c.id); err != nil {
				t.Fatal("deleting a missing item must be a no-op, got", err)
			}
		})
	}
}

func TestDeleteValueItemsWhenDocsMissing(t *testing.T) {
	root := t.TempDir()
	// settings.json and plugins.json don't exist at all in this root.
	if err := DeleteItem(root, item.ID("setting/model")); err != nil {
		t.Fatal("delete of setting with no settings.json must be a no-op, got", err)
	}
	if err := DeleteItem(root, item.ID("plugins/enabledPlugins:p@m")); err != nil {
		t.Fatal("delete of plugin entry with no plugins.json must be a no-op, got", err)
	}
}

func TestWriteAndDeleteValueItems(t *testing.T) {
	root := t.TempDir()
	err := WriteItem(root, scan.Scanned{ID: item.ID("setting/model"), Value: json.RawMessage(`"opus"`)})
	if err != nil {
		t.Fatal(err)
	}
	err = WriteItem(root, scan.Scanned{ID: item.ID("plugins/enabledPlugins:p@m"), Value: json.RawMessage(`true`)})
	if err != nil {
		t.Fatal(err)
	}
	m, _, _, _ := scan.Repo(root)
	if _, ok := m[item.ID("setting/model")]; !ok {
		t.Fatal("setting not written")
	}
	if _, ok := m[item.ID("plugins/enabledPlugins:p@m")]; !ok {
		t.Fatal("plugin entry not written")
	}
	DeleteItem(root, item.ID("setting/model"))
	DeleteItem(root, item.ID("plugins/enabledPlugins:p@m"))
	m2, _, _, _ := scan.Repo(root)
	if len(m2) != 0 {
		t.Fatalf("value items not deleted: %v", m2)
	}
	_ = settings.KeyEnabledPlugins // keep import if unused elsewhere
}

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	m, err := LoadManifest(root)
	if err != nil || m.Schema != 1 || len(m.Items) != 0 {
		t.Fatalf("empty manifest: %+v %v", m, err)
	}
	m.Items[item.ID("skill/x")] = "h"
	if err := m.Save(root); err != nil {
		t.Fatal(err)
	}
	m2, _ := LoadManifest(root)
	if m2.Items[item.ID("skill/x")] != "h" {
		t.Fatal("round trip failed")
	}
}
