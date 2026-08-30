package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/settings"
)

func mkClaude(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "skills/humanizer"), 0o755)
	os.WriteFile(filepath.Join(d, "skills/humanizer/SKILL.md"), []byte("# h"), 0o644)
	os.MkdirAll(filepath.Join(d, "agents"), 0o755)
	os.WriteFile(filepath.Join(d, "agents/php-pro.md"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(d, "commands"), 0o755)
	os.WriteFile(filepath.Join(d, "commands/counselors.md"), []byte("c"), 0o644)
	os.WriteFile(filepath.Join(d, "CLAUDE.md"), []byte("rules"), 0o644)
	os.WriteFile(filepath.Join(d, "settings.json"),
		[]byte(`{"model":"opus","env":{"X":"1"},"enabledPlugins":{"p@m":true}}`), 0o644)
	return d
}

func TestClaudeInventory(t *testing.T) {
	m, warns, err := Claude(mkClaude(t), settings.KeyOverrides{})
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	for _, want := range []string{
		"skill/humanizer", "agent/php-pro", "command/counselors",
		"rules/CLAUDE.md", "setting/model", "plugins/enabledPlugins:p@m",
	} {
		s, ok := m[item.ID(want)]
		if !ok {
			t.Fatalf("missing %s in %v", want, keys(m))
		}
		if s.Hash == "" {
			t.Fatalf("%s has empty hash", want)
		}
	}
	if _, ok := m[item.ID("setting/env")]; ok {
		t.Fatal("env is not shareable, must not be scanned")
	}
	if _, ok := m[item.ID("setting/enabledPlugins")]; ok {
		t.Fatal("enabledPlugins must not appear as a setting item")
	}
}

func TestClaudeEmptyDir(t *testing.T) {
	m, _, err := Claude(t.TempDir(), settings.KeyOverrides{})
	if err != nil || len(m) != 0 {
		t.Fatalf("empty dir: %v %v", m, err)
	}
}

func TestClaudeUnparseableSettingsWarnsAndSkips(t *testing.T) {
	d := mkClaude(t)
	os.WriteFile(filepath.Join(d, "settings.json"), []byte(`{not json`), 0o644)
	m, warns, err := Claude(d, settings.KeyOverrides{})
	if err != nil {
		t.Fatalf("bad settings must not abort scan: %v", err)
	}
	if len(warns) == 0 {
		t.Fatal("expected a warning about settings.json")
	}
	if _, ok := m[item.ID("skill/humanizer")]; !ok {
		t.Fatal("file items must still be scanned")
	}
	if _, ok := m[item.ID("setting/model")]; ok {
		t.Fatal("settings items must be skipped when unparseable")
	}
}

func TestRepoInventory(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "skills/humanizer"), 0o755)
	os.WriteFile(filepath.Join(d, "skills/humanizer/SKILL.md"), []byte("# h"), 0o644)
	os.MkdirAll(filepath.Join(d, "rules"), 0o755)
	os.WriteFile(filepath.Join(d, "rules/CLAUDE.md"), []byte("rules"), 0o644)
	os.WriteFile(filepath.Join(d, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	os.WriteFile(filepath.Join(d, "plugins.json"),
		[]byte(`{"enabledPlugins":{"p@m":true},"extraKnownMarketplaces":{}}`), 0o644)
	m, _, err := Repo(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"skill/humanizer", "rules/CLAUDE.md", "setting/model", "plugins/enabledPlugins:p@m",
	} {
		if _, ok := m[item.ID(want)]; !ok {
			t.Fatalf("missing %s in %v", want, keys(m))
		}
	}
}

func TestSameContentHashesEqualAcrossClaudeAndRepo(t *testing.T) {
	c := mkClaude(t)
	r := t.TempDir()
	os.MkdirAll(filepath.Join(r, "skills/humanizer"), 0o755)
	os.WriteFile(filepath.Join(r, "skills/humanizer/SKILL.md"), []byte("# h"), 0o644)
	os.WriteFile(filepath.Join(r, "settings.json"), []byte(`{"model": "opus"}`), 0o644)
	cm, _, _ := Claude(c, settings.KeyOverrides{})
	rm, _, _ := Repo(r)
	if cm[item.ID("skill/humanizer")].Hash != rm[item.ID("skill/humanizer")].Hash {
		t.Fatal("identical skill must hash equal in both layouts")
	}
	if cm[item.ID("setting/model")].Hash != rm[item.ID("setting/model")].Hash {
		t.Fatal("identical setting must hash equal despite whitespace")
	}
}

func keys(m map[item.ID]Scanned) []item.ID {
	var out []item.ID
	for k := range m {
		out = append(out, k)
	}
	return out
}
