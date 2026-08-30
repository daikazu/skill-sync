package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
)

// buildPack creates a .skillpack containing one skill "tool" with the
// given content and returns its path.
func buildPack(t *testing.T, name, version, content string) string {
	t.Helper()
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "skills/tool"), 0o755)
	os.WriteFile(filepath.Join(src, "skills/tool/SKILL.md"), []byte(content), 0o644)
	items, _, _ := scan.Claude(src, settings.KeyOverrides{})
	man := Manifest{Name: name, Version: version, Items: map[item.ID]PackItem{}}
	for id, s := range items {
		man.Items[id] = PackItem{Hash: s.Hash}
	}
	out := filepath.Join(t.TempDir(), name+".skillpack")
	if err := Build(out, man, items); err != nil {
		t.Fatal(err)
	}
	return out
}

func env(t *testing.T) (claude, backups, ledgerPath string) {
	claude = t.TempDir()
	return claude, filepath.Join(claude, "backups/skill-sync"), filepath.Join(t.TempDir(), "ledger.json")
}

func install(t *testing.T, pk, claude, backups, ledgerPath string,
	col map[item.ID]CollisionChoice, mod map[item.ID]ModifiedChoice) *InstallSummary {
	t.Helper()
	man, contents, err := Load(pk, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	led, _ := state.LoadLedger(ledgerPath)
	ip := BuildInstallPlan(man, contents, local, led, man.Name)
	sum, err := ApplyInstall(claude, backups, ledgerPath, man, contents, ip, col, mod)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func TestFreshInstallRecordsOwnership(t *testing.T) {
	pk := buildPack(t, "agency", "1.0.0", "v1")
	claude, backups, lp := env(t)
	sum := install(t, pk, claude, backups, lp, nil, nil)
	if sum.Installed != 1 {
		t.Fatalf("installed: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "v1" {
		t.Fatal("content not installed")
	}
	led, _ := state.LoadLedger(lp)
	if _, _, ok := led.Owner(item.ID("skill/tool")); !ok {
		t.Fatal("ownership not recorded")
	}
}

func TestUpgradeOnlyTouchesUnmodifiedOwned(t *testing.T) {
	claude, backups, lp := env(t)
	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	sum := install(t, buildPack(t, "agency", "2.0.0", "v2"), claude, backups, lp, nil, nil)
	if sum.Upgraded != 1 {
		t.Fatalf("upgrade: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "v2" {
		t.Fatal("upgrade did not apply")
	}
}

func TestModifiedOwnedKeepLocalSurvivesUpgrade(t *testing.T) {
	claude, backups, lp := env(t)
	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	os.WriteFile(filepath.Join(claude, "skills/tool/SKILL.md"), []byte("my edit"), 0o644)
	sum := install(t, buildPack(t, "agency", "2.0.0", "v2"), claude, backups, lp,
		nil, map[item.ID]ModifiedChoice{"skill/tool": KeepLocal})
	if sum.Skipped != 1 {
		t.Fatalf("keep-local: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "my edit" {
		t.Fatal("local edit must never be silently reverted")
	}
}

func TestCollisionRename(t *testing.T) {
	claude, backups, lp := env(t)
	os.MkdirAll(filepath.Join(claude, "skills/tool"), 0o755)
	os.WriteFile(filepath.Join(claude, "skills/tool/SKILL.md"), []byte("mine"), 0o644)
	pk := buildPack(t, "agency", "1.0.0", "theirs")
	sum := install(t, pk, claude, backups, lp,
		map[item.ID]CollisionChoice{"skill/tool": ChoiceRename}, nil)
	if sum.Renamed != 1 {
		t.Fatalf("rename: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "mine" {
		t.Fatal("user's skill must be untouched")
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool-agency/SKILL.md")); string(b) != "theirs" {
		t.Fatal("renamed install missing")
	}
	led, _ := state.LoadLedger(lp)
	if _, _, ok := led.Owner(item.ID("skill/tool-agency")); !ok {
		t.Fatal("renamed item must be ledger-owned")
	}
}

func TestUninstallKeepsModified(t *testing.T) {
	claude, backups, lp := env(t)
	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	removed, kept, err := Uninstall(claude, backups, lp, "agency")
	if err != nil || len(removed) != 1 || len(kept) != 0 {
		t.Fatalf("uninstall clean: %v %v %v", removed, kept, err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/tool")); err == nil {
		t.Fatal("owned unmodified item must be removed")
	}

	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	os.WriteFile(filepath.Join(claude, "skills/tool/SKILL.md"), []byte("edited"), 0o644)
	removed, kept, err = Uninstall(claude, backups, lp, "agency")
	if err != nil || len(removed) != 0 || len(kept) != 1 {
		t.Fatalf("uninstall modified: %v %v %v", removed, kept, err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/tool")); err != nil {
		t.Fatal("modified item must be kept")
	}
	led, _ := state.LoadLedger(lp)
	if len(led.Packages) != 0 {
		t.Fatal("package record must be gone")
	}
}
