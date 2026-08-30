package syncer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/state"
)

func repoHas(t *testing.T, d device, id string) bool {
	t.Helper()
	m, _, _, err := scan.Repo(d.s.RepoDir())
	if err != nil {
		t.Fatal(err)
	}
	_, ok := m[item.ID(id)]
	return ok
}

// A corrupted settings.json must flag-and-skip settings items, never
// classify them as deleted and cascade the deletion through the repo to
// every other machine.
func TestCorruptSettingsDoesNotPropagateDeletion(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	os.WriteFile(filepath.Join(a.claude, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	b := newDevice(t, origin)
	if _, err := b.s.Run(nil); err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(b.claude, "settings.json"), []byte(`{not json`), 0o644)
	sum, err := b.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.DeletedRemote != 0 || sum.DeletedLocal != 0 {
		t.Fatalf("corrupt settings.json must not delete anything: %+v", sum)
	}
	if !repoHas(t, b, "setting/model") {
		t.Fatal("setting/model must survive in the repo")
	}
	var excluded bool
	for _, w := range sum.Warnings {
		if strings.Contains(w, "excluded from this sync: setting/model") {
			excluded = true
		}
	}
	if !excluded {
		t.Fatalf("expected an excluded-from-sync warning, got: %v", sum.Warnings)
	}

	sumA, err := a.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sumA.DeletedLocal != 0 {
		t.Fatalf("device A must be unaffected: %+v", sumA)
	}
	as, _ := os.ReadFile(filepath.Join(a.claude, "settings.json"))
	if !strings.Contains(string(as), "opus") {
		t.Fatalf("A's model setting lost: %s", as)
	}
}

// An unreadable file inside a skill must skip the skill for this sync,
// not delete it from the repo (and then from every other machine).
func TestUnreadableSkillFileDoesNotPropagateDeletion(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "prized", "v1")
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	b := newDevice(t, origin)
	if _, err := b.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	// add a file on B and push it so both sides agree, then break it
	bad := filepath.Join(b.claude, "skills", "prized", "extra.md")
	os.WriteFile(bad, []byte("x"), 0o644)
	if _, err := b.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	os.Chmod(bad, 0o000)
	t.Cleanup(func() { os.Chmod(bad, 0o644) })

	sum, err := b.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.DeletedRemote != 0 {
		t.Fatalf("unreadable skill must not be deleted from the repo: %+v", sum)
	}
	if !repoHas(t, b, "skill/prized") {
		t.Fatal("skill/prized must survive in the repo")
	}

	sumA, err := a.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sumA.DeletedLocal != 0 {
		t.Fatalf("device A must keep the skill: %+v", sumA)
	}
	if readSkill(t, a.claude, "prized") != "v1" {
		t.Fatal("skill deleted or altered on A")
	}
}

// Excluding a settings key on one device must mean "this machine
// ignores that key": no pull over the local value, no repo deletion, no
// cascade to other machines.
func TestExcludedKeyIsMachineLocal(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	os.WriteFile(filepath.Join(a.claude, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}

	b := newDevice(t, origin)
	cfgPath := filepath.Join(b.sync, "config.json")
	cfg, err := state.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExcludeKeys = []string{"model"}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(b.claude, "settings.json"), []byte(`{"model":"haiku-device-local"}`), 0o644)

	for i := 0; i < 2; i++ { // second sync is where the delete cascade used to fire
		sum, err := b.s.Run(nil)
		if err != nil {
			t.Fatal(err)
		}
		if sum.DeletedRemote != 0 || sum.DeletedLocal != 0 {
			t.Fatalf("sync %d: excluded key must not delete anything: %+v", i+1, sum)
		}
		bs, _ := os.ReadFile(filepath.Join(b.claude, "settings.json"))
		if !strings.Contains(string(bs), "haiku-device-local") {
			t.Fatalf("sync %d: B's device-local model value overwritten: %s", i+1, bs)
		}
	}
	if !repoHas(t, b, "setting/model") {
		t.Fatal("setting/model must survive in the repo")
	}

	sumA, err := a.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sumA.DeletedLocal != 0 {
		t.Fatalf("device A must be unaffected: %+v", sumA)
	}
	as, _ := os.ReadFile(filepath.Join(a.claude, "settings.json"))
	if !strings.Contains(string(as), "opus") {
		t.Fatalf("A lost its model setting: %s", as)
	}
}

func TestRunErrorsWhenClaudeDirMissing(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "s", "v1")
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	missing := &Syncer{ClaudeDir: filepath.Join(t.TempDir(), "nope"), SyncDir: a.sync}
	if _, err := missing.Run(nil); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing claude dir must refuse to sync: %v", err)
	}
	// the repo must be untouched
	if !repoHas(t, a, "skill/s") {
		t.Fatal("repo must not be wiped by a missing claude dir")
	}
}
