package syncer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
)

type device struct {
	claude, sync string
	s            *Syncer
}

func newDevice(t *testing.T, origin string) device {
	t.Helper()
	claude, sync := t.TempDir(), filepath.Join(t.TempDir(), "sync")
	if err := Init(claude, sync, origin); err != nil {
		t.Fatal(err)
	}
	return device{claude, sync, &Syncer{ClaudeDir: claude, SyncDir: sync}}
}

func bare(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", d).CombinedOutput(); err != nil {
		t.Fatalf("bare: %v\n%s", err, out)
	}
	return d
}

func writeSkill(t *testing.T, claude, name, content string) {
	t.Helper()
	dir := filepath.Join(claude, "skills", name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644)
}

func readSkill(t *testing.T, claude, name string) string {
	b, _ := os.ReadFile(filepath.Join(claude, "skills", name, "SKILL.md"))
	return string(b)
}

func TestTwoDeviceSyncAndConflict(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "shared", "v1")
	os.WriteFile(filepath.Join(a.claude, "settings.json"), []byte(`{"model":"opus"}`), 0o644)

	sum, err := a.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Pushed == 0 {
		t.Fatalf("device A should push new items: %+v", sum)
	}

	b := newDevice(t, origin)
	if _, err := b.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	if readSkill(t, b.claude, "shared") != "v1" {
		t.Fatal("device B should receive skill")
	}

	// no changes → up to date on both
	sum, _ = a.s.Run(nil)
	if !sum.UpToDate {
		t.Fatalf("A should be up to date: %+v", sum)
	}

	// divergent edits → conflict surfaced to resolver
	writeSkill(t, a.claude, "shared", "edited-on-A")
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, b.claude, "shared", "edited-on-B")
	var sawConflict bool
	resolver := func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error) {
		for _, c := range p.Conflicts {
			if c.ID == item.ID("skill/shared") {
				sawConflict = true
			}
		}
		return map[item.ID]plan.Resolution{"skill/shared": plan.ResLocal}, true, nil
	}
	if _, err := b.s.Run(resolver); err != nil {
		t.Fatal(err)
	}
	if !sawConflict {
		t.Fatal("divergent edit must surface as conflict")
	}
	// B chose local → A pulls B's version
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	if readSkill(t, a.claude, "shared") != "edited-on-B" {
		t.Fatal("conflict resolution did not propagate")
	}
}

func TestUnresolvedConflictLeavesBothSidesUntouched(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "s", "v1")
	a.s.Run(nil)
	b := newDevice(t, origin)
	b.s.Run(nil)
	writeSkill(t, a.claude, "s", "A")
	a.s.Run(nil)
	writeSkill(t, b.claude, "s", "B")
	sum, err := b.s.Run(nil) // nil resolver: conflict stays unresolved
	if err != nil {
		t.Fatal(err)
	}
	if sum.SkippedConflicts != 1 {
		t.Fatalf("want 1 skipped conflict: %+v", sum)
	}
	if readSkill(t, b.claude, "s") != "B" {
		t.Fatal("unresolved conflict must not modify local")
	}
}

// TestRecoversStrandedCommit reproduces a crash between commit and push:
// a commit exists in the device's local checkout but never reached the
// remote. Run must push it on the next sync, even though this run's own
// classification finds nothing new to change.
func TestRecoversStrandedCommit(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "s", "v1")
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}

	// Simulate the crash: commit directly into the repo checkout without
	// pushing, bypassing Run entirely.
	const marker = "stranded-commit-marker"
	gitIn := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", a.s.RepoDir()}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(a.s.RepoDir(), "stranded.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn("add", "-A")
	gitIn("-c", "user.name=test", "-c", "user.email=test@localhost", "commit", "-m", marker)

	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("git", "-C", origin, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log origin: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), marker) {
		t.Fatalf("stranded commit did not reach origin:\n%s", out)
	}
}

func TestInitAdoptsRemoteOnFreshMachine(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "s", "v1")
	os.WriteFile(filepath.Join(a.claude, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	a.s.Run(nil)

	fresh := newDevice(t, origin)
	sum, err := fresh.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Pulled == 0 || readSkill(t, fresh.claude, "s") != "v1" {
		t.Fatalf("fresh machine must adopt remote: %+v", sum)
	}
}
