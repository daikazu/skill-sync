package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/state"
)

func TestAdoptRemovesOwnershipButKeepsFile(t *testing.T) {
	claude, backups, lp := env(t)
	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)

	pkgName, err := Adopt(lp, item.ID("skill/tool"))
	if err != nil {
		t.Fatal(err)
	}
	if pkgName != "agency" {
		t.Fatalf("pkgName = %q, want agency", pkgName)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/tool/SKILL.md")); err != nil {
		t.Fatal("adopted item's file must remain on disk")
	}

	led, _ := state.LoadLedger(lp)
	if _, _, ok := led.Owner(item.ID("skill/tool")); ok {
		t.Fatal("adopted item must no longer be reported as package-owned")
	}
	if len(led.Packages) != 0 {
		t.Fatalf("package record with no items left must be dropped: %+v", led.Packages)
	}
}

func TestAdoptUnownedItemErrors(t *testing.T) {
	_, _, lp := env(t)
	if _, err := Adopt(lp, item.ID("skill/nope")); err == nil {
		t.Fatal("expected error adopting an item no package owns")
	}
}
