package state

import (
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
)

func TestDeviceRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "state.json")
	d, err := LoadDevice(p)
	if err != nil || len(d.LastSynced) != 0 {
		t.Fatalf("missing should be empty: %v", err)
	}
	d.LastSynced[item.ID("skill/x")] = "abc"
	if err := d.Save(p); err != nil {
		t.Fatal(err)
	}
	d2, _ := LoadDevice(p)
	if d2.LastSynced[item.ID("skill/x")] != "abc" {
		t.Fatal("round trip failed")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c, _ := LoadConfig(p)
	c.Remote = "git@github.com:daikazu/claude-sync.git"
	c.Policies = map[item.ID]Policy{"skill/x": PolicyNeverSync}
	c.Save(p)
	c2, _ := LoadConfig(p)
	if c2.Remote != c.Remote || c2.Policies["skill/x"] != PolicyNeverSync {
		t.Fatal("round trip failed")
	}
}

func TestLedgerOwner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ledger.json")
	l, _ := LoadLedger(p)
	l.Packages["agency-tools"] = PackageRecord{
		Version: "1.0.0",
		Items:   map[item.ID]string{"skill/code-review": "h1"},
	}
	l.Save(p)
	l2, _ := LoadLedger(p)
	pkg, h, ok := l2.Owner(item.ID("skill/code-review"))
	if !ok || pkg != "agency-tools" || h != "h1" {
		t.Fatalf("owner: %s %s %v", pkg, h, ok)
	}
	if _, _, ok := l2.Owner(item.ID("skill/other")); ok {
		t.Fatal("unowned item reported owned")
	}
}
