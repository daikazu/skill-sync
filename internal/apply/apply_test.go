package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

func setup(t *testing.T) (Applier, string, string) {
	t.Helper()
	claude, repoDir := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(claude, "skills/local-skill"), 0o755)
	os.WriteFile(filepath.Join(claude, "skills/local-skill/SKILL.md"), []byte("local"), 0o644)
	os.WriteFile(filepath.Join(claude, "settings.json"),
		[]byte(`{"model":"opus","env":{"KEEP":"me"}}`), 0o644)
	os.MkdirAll(filepath.Join(repoDir, "skills/remote-skill"), 0o755)
	os.WriteFile(filepath.Join(repoDir, "skills/remote-skill/SKILL.md"), []byte("remote"), 0o644)
	os.WriteFile(filepath.Join(repoDir, "settings.json"), []byte(`{"model":"fable"}`), 0o644)
	return Applier{
		ClaudeDir:  claude,
		RepoDir:    repoDir,
		BackupsDir: filepath.Join(claude, "backups/skill-sync"),
	}, claude, repoDir
}

func change(id string, st classify.State, act plan.Action) plan.Change {
	return plan.Change{Result: classify.Result{ID: item.ID(id), State: st}, Action: act}
}

func TestApplyPullPushDelete(t *testing.T) {
	a, claude, repoDir := setup(t)
	local, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	remote, _, _ := scan.Repo(repoDir)

	changes := []plan.Change{
		change("skill/remote-skill", classify.NewRemote, plan.ActPull),
		change("skill/local-skill", classify.NewLocal, plan.ActPush),
		change("setting/model", classify.Pull, plan.ActPull),
	}
	base, err := a.Apply(changes, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/remote-skill/SKILL.md")); string(b) != "remote" {
		t.Fatal("pull skill failed")
	}
	if b, _ := os.ReadFile(filepath.Join(repoDir, "skills/local-skill/SKILL.md")); string(b) != "local" {
		t.Fatal("push skill failed")
	}
	d, _ := settings.Load(filepath.Join(claude, "settings.json"))
	if v, _ := d.Get("model"); string(v) != `"fable"` {
		t.Fatalf("setting pull failed: %s", v)
	}
	if _, ok := d.Get("env"); !ok {
		t.Fatal("device-local key env must survive settings write")
	}
	if base[item.ID("skill/remote-skill")] != remote[item.ID("skill/remote-skill")].Hash {
		t.Fatal("base update for pull missing")
	}
	if base[item.ID("skill/local-skill")] != local[item.ID("skill/local-skill")].Hash {
		t.Fatal("base update for push missing")
	}

	del := []plan.Change{
		change("skill/remote-skill", classify.DeletedRemote, plan.ActDeleteLocal),
		change("skill/local-skill", classify.DeletedLocal, plan.ActDeleteRemote),
	}
	local2, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	remote2, _, _ := scan.Repo(repoDir)
	base2, err := a.Apply(del, local2, remote2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/remote-skill")); err == nil {
		t.Fatal("delete-local failed")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "skills/local-skill")); err == nil {
		t.Fatal("delete-remote failed")
	}
	if v, ok := base2[item.ID("skill/remote-skill")]; !ok || v != "" {
		t.Fatal("deletion must clear base entry")
	}
}

func TestSnapshotAndRestore(t *testing.T) {
	a, claude, repoDir := setup(t)
	local, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	remote, _, _ := scan.Repo(repoDir)

	// local-skill will be deleted locally; settings.json will change
	changes := []plan.Change{
		change("skill/local-skill", classify.DeletedRemote, plan.ActDeleteLocal),
		change("setting/model", classify.Pull, plan.ActPull),
	}
	snap, err := a.Snapshot(changes)
	if err != nil || snap == "" {
		t.Fatalf("snapshot: %q %v", snap, err)
	}
	if _, err := a.Apply(changes, local, remote); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/local-skill")); err == nil {
		t.Fatal("precondition: skill should be deleted")
	}
	if err := Restore(snap, claude); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/local-skill/SKILL.md")); string(b) != "local" {
		t.Fatal("restore did not bring skill back")
	}
	d, _ := settings.Load(filepath.Join(claude, "settings.json"))
	if v, _ := d.Get("model"); string(v) != `"opus"` {
		t.Fatalf("restore did not revert settings: %s", v)
	}
	var meta map[string]any
	b, err := os.ReadFile(filepath.Join(snap, "snapshot.json"))
	if err != nil || json.Unmarshal(b, &meta) != nil {
		t.Fatal("snapshot metadata missing")
	}
}

func TestSnapshotEmptyWhenNoLocalChanges(t *testing.T) {
	a, _, _ := setup(t)
	snap, err := a.Snapshot([]plan.Change{
		change("skill/local-skill", classify.NewLocal, plan.ActPush),
	})
	if err != nil || snap != "" {
		t.Fatalf("push-only plan must not snapshot: %q %v", snap, err)
	}
}
