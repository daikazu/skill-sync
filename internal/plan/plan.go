// Package plan turns classification results into an executable sync
// plan, honoring per-item policies and package ownership.
package plan

import (
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/state"
)

type Action string

const (
	ActPull         Action = "pull"
	ActPush         Action = "push"
	ActDeleteLocal  Action = "delete-local"
	ActDeleteRemote Action = "delete-remote"
	ActBaseOnly     Action = "base-only"
)

type Change struct {
	Result classify.Result
	Action Action
}

type Plan struct {
	Auto      []Change
	Conflicts []classify.Result
	Skipped   []classify.Result
}

var oneSided = map[classify.State]Action{
	classify.Push:          ActPush,
	classify.Pull:          ActPull,
	classify.NewLocal:      ActPush,
	classify.NewRemote:     ActPull,
	classify.DeletedRemote: ActDeleteLocal,
	classify.DeletedLocal:  ActDeleteRemote,
	classify.InSync:        ActBaseOnly,
}

func Build(results []classify.Result, cfg *state.Config, ledger *state.Ledger) Plan {
	var p Plan
	for _, r := range results {
		if _, _, owned := ledger.Owner(r.ID); owned {
			p.Skipped = append(p.Skipped, r)
			continue
		}
		switch cfg.Policies[r.ID] {
		case state.PolicyNeverSync:
			p.Skipped = append(p.Skipped, r)
			continue
		case state.PolicyAlwaysAsk:
			if r.State != classify.InSync {
				p.Conflicts = append(p.Conflicts, r)
				continue
			}
		}
		if r.State.IsConflict() {
			p.Conflicts = append(p.Conflicts, r)
			continue
		}
		p.Auto = append(p.Auto, Change{Result: r, Action: oneSided[r.State]})
	}
	return p
}

type Resolution string

const (
	ResLocal  Resolution = "local"
	ResRemote Resolution = "remote"
	ResSkip   Resolution = "skip"
)

func Resolve(p Plan, choices map[item.ID]Resolution) []Change {
	out := append([]Change{}, p.Auto...)
	for _, r := range p.Conflicts {
		switch choices[r.ID] {
		case ResLocal:
			if r.Local == "" {
				out = append(out, Change{Result: r, Action: ActDeleteRemote})
			} else {
				out = append(out, Change{Result: r, Action: ActPush})
			}
		case ResRemote:
			if r.Remote == "" {
				out = append(out, Change{Result: r, Action: ActDeleteLocal})
			} else {
				out = append(out, Change{Result: r, Action: ActPull})
			}
		}
	}
	return out
}
