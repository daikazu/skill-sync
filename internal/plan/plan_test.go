package plan

import (
	"testing"

	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/state"
)

func res(id string, st classify.State, l, b, r string) classify.Result {
	return classify.Result{ID: item.ID(id), State: st, Local: l, Base: b, Remote: r}
}

func emptyCfg() *state.Config { return &state.Config{} }
func emptyLedger() *state.Ledger {
	return &state.Ledger{Packages: map[string]state.PackageRecord{}}
}

func TestBuildRouting(t *testing.T) {
	results := []classify.Result{
		res("agent/a", classify.Push, "b", "a", "a"),
		res("agent/b", classify.NewRemote, "", "", "x"),
		res("agent/c", classify.DeletedRemote, "a", "a", ""),
		res("agent/d", classify.Conflict, "b", "a", "c"),
		res("agent/e", classify.InSync, "a", "", "a"),
	}
	p := Build(results, emptyCfg(), emptyLedger())
	if len(p.Conflicts) != 1 || p.Conflicts[0].ID != item.ID("agent/d") {
		t.Fatalf("conflicts: %v", p.Conflicts)
	}
	actions := map[item.ID]Action{}
	for _, c := range p.Auto {
		actions[c.Result.ID] = c.Action
	}
	want := map[item.ID]Action{
		"agent/a": ActPush, "agent/b": ActPull,
		"agent/c": ActDeleteLocal, "agent/e": ActBaseOnly,
	}
	for id, w := range want {
		if actions[id] != w {
			t.Fatalf("%s: got %s want %s", id, actions[id], w)
		}
	}
}

func TestBuildPolicies(t *testing.T) {
	results := []classify.Result{
		res("skill/never", classify.Push, "b", "a", "a"),
		res("skill/ask", classify.Pull, "a", "a", "b"),
	}
	cfg := &state.Config{Policies: map[item.ID]state.Policy{
		"skill/never": state.PolicyNeverSync,
		"skill/ask":   state.PolicyAlwaysAsk,
	}}
	p := Build(results, cfg, emptyLedger())
	if len(p.Skipped) != 1 || p.Skipped[0].ID != item.ID("skill/never") {
		t.Fatalf("skipped: %v", p.Skipped)
	}
	if len(p.Conflicts) != 1 || p.Conflicts[0].ID != item.ID("skill/ask") {
		t.Fatalf("always-ask must route to conflicts: %v", p.Conflicts)
	}
}

func TestBuildPackageOwnedExcluded(t *testing.T) {
	results := []classify.Result{res("skill/team", classify.Push, "b", "a", "a")}
	led := &state.Ledger{Packages: map[string]state.PackageRecord{
		"agency": {Version: "1.0.0", Items: map[item.ID]string{"skill/team": "a"}},
	}}
	p := Build(results, emptyCfg(), led)
	if len(p.Auto) != 0 || len(p.Skipped) != 1 {
		t.Fatalf("package-owned must be skipped: %+v", p)
	}
}

func TestResolve(t *testing.T) {
	p := Plan{
		Auto: []Change{{Result: res("agent/auto", classify.Push, "b", "a", "a"), Action: ActPush}},
		Conflicts: []classify.Result{
			res("agent/mine", classify.Conflict, "b", "a", "c"),
			res("agent/theirs", classify.Conflict, "b", "a", "c"),
			res("agent/skip", classify.Conflict, "b", "a", "c"),
			res("agent/delmod", classify.ConflictDeleteModify, "", "a", "b"),
		},
	}
	got := Resolve(p, map[item.ID]Resolution{
		"agent/mine":   ResLocal,
		"agent/theirs": ResRemote,
		"agent/delmod": ResLocal,
	})
	acts := map[item.ID]Action{}
	for _, c := range got {
		acts[c.Result.ID] = c.Action
	}
	if acts["agent/auto"] != ActPush || acts["agent/mine"] != ActPush ||
		acts["agent/theirs"] != ActPull || acts["agent/delmod"] != ActDeleteRemote {
		t.Fatalf("resolve actions: %v", acts)
	}
	if _, ok := acts["agent/skip"]; ok {
		t.Fatal("unresolved conflict must be omitted")
	}
}
