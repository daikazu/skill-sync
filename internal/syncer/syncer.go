// Package syncer orchestrates the full sync loop: fetch, scan,
// classify, plan, resolve, snapshot, apply, commit, push.
package syncer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/apply"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/repo"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
)

type Syncer struct {
	ClaudeDir string
	SyncDir   string
}

func (s *Syncer) RepoDir() string    { return filepath.Join(s.SyncDir, "repo") }
func (s *Syncer) statePath() string  { return filepath.Join(s.SyncDir, "state.json") }
func (s *Syncer) configPath() string { return filepath.Join(s.SyncDir, "config.json") }
func (s *Syncer) LedgerPath() string { return filepath.Join(s.SyncDir, "ledger.json") }
func (s *Syncer) backupsDir() string {
	return filepath.Join(s.ClaudeDir, "backups", "skill-sync")
}

func Init(claudeDir, syncDir, remoteURL string) error {
	repoDir := filepath.Join(syncDir, "repo")
	if _, err := os.Stat(repoDir); err == nil {
		return fmt.Errorf("%s already exists — already initialized", repoDir)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		return err
	}
	if err := repo.Clone(remoteURL, repoDir); err != nil {
		return err
	}
	cfg := &state.Config{Remote: remoteURL}
	return cfg.Save(filepath.Join(syncDir, "config.json"))
}

type Resolver func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error)

type Summary struct {
	Pulled, Pushed, DeletedLocal, DeletedRemote int
	Resolved, SkippedConflicts, SkippedItems    int
	Warnings                                    []string
	SnapshotDir                                 string
	UpToDate                                    bool
	RemoteDeletions                             []item.ID
}

func (s *Syncer) Run(resolve Resolver) (*Summary, error) {
	if _, err := os.Stat(s.ClaudeDir); err != nil {
		// An absent claude dir would scan as empty and delete every item
		// from the repo (and then from every machine). Refuse instead.
		return nil, fmt.Errorf("claude dir %s does not exist — check --claude-dir", s.ClaudeDir)
	}
	for attempt := 0; ; attempt++ {
		sum, err := s.runOnce(resolve)
		if errors.Is(err, repo.ErrPushRejected) && attempt < 2 {
			continue // re-pull and re-classify
		}
		return sum, err
	}
}

func (s *Syncer) runOnce(resolve Resolver) (*Summary, error) {
	g := repo.Git{Dir: s.RepoDir()}
	if err := g.Pull(); err != nil {
		return nil, fmt.Errorf("fetching sync repo: %w", err)
	}
	cfg, err := state.LoadConfig(s.configPath())
	if err != nil {
		return nil, err
	}
	ledger, err := state.LoadLedger(s.LedgerPath())
	if err != nil {
		return nil, err
	}
	dev, err := state.LoadDevice(s.statePath())
	if err != nil {
		return nil, err
	}
	overrides := settings.KeyOverrides{Include: cfg.IncludeKeys, Exclude: cfg.ExcludeKeys}
	local, unscanL, warnsL, err := scan.Claude(s.ClaudeDir, overrides)
	if err != nil {
		return nil, err
	}
	remote, unscanR, warnsR, err := scan.Repo(s.RepoDir())
	if err != nil {
		return nil, err
	}
	filterRemoteByAllowlist(remote, overrides)
	sum := &Summary{Warnings: append(warnsL, warnsR...)}

	results, excludedWarns := excludeUnscannable(
		classify.All(local, remote, dev.LastSynced), append(unscanL, unscanR...))
	sum.Warnings = append(sum.Warnings, excludedWarns...)
	p := plan.Build(results, cfg, ledger)
	p.Local, p.Remote = local, remote
	sum.SkippedItems = len(p.Skipped)

	choices := map[item.ID]plan.Resolution{}
	if len(p.Conflicts) > 0 && resolve != nil {
		var proceed bool
		choices, proceed, err = resolve(p)
		if err != nil {
			return nil, err
		}
		if !proceed {
			return nil, fmt.Errorf("sync aborted")
		}
	}
	changes := plan.Resolve(p, choices)
	for _, c := range p.Conflicts {
		if r, ok := choices[c.ID]; ok && r != plan.ResSkip {
			sum.Resolved++
		} else {
			sum.SkippedConflicts++
		}
	}

	real := 0
	for _, c := range changes {
		switch c.Action {
		case plan.ActPull:
			sum.Pulled++
		case plan.ActPush:
			sum.Pushed++
		case plan.ActDeleteLocal:
			sum.DeletedLocal++
			sum.RemoteDeletions = append(sum.RemoteDeletions, c.Result.ID)
		case plan.ActDeleteRemote:
			sum.DeletedRemote++
		}
		if c.Action != plan.ActBaseOnly {
			real++
		}
	}

	a := apply.Applier{ClaudeDir: s.ClaudeDir, RepoDir: s.RepoDir(), BackupsDir: s.backupsDir()}
	if snap, err := a.Snapshot(changes); err != nil {
		return nil, err
	} else {
		sum.SnapshotDir = snap
	}
	baseUpdates, err := a.Apply(changes, local, remote)
	if err != nil {
		return sum, fmt.Errorf("apply failed (restore with: skill-sync rollback): %w", err)
	}

	man, err := repo.LoadManifest(s.RepoDir())
	if err != nil {
		return sum, err
	}
	for id, h := range baseUpdates {
		if h == "" {
			delete(dev.LastSynced, id)
			delete(man.Items, id)
		} else {
			dev.LastSynced[id] = h
			man.Items[id] = h
		}
	}
	if err := man.Save(s.RepoDir()); err != nil {
		return sum, err
	}

	host, _ := os.Hostname()
	if _, err := g.CommitAll(fmt.Sprintf("%s: %d change(s)", host, real)); err != nil {
		return sum, err
	}
	// Always push when commits exist: a prior run may have committed but
	// failed to push (network), and pushing an up-to-date branch is a no-op.
	if g.HasCommits() {
		if err := g.Push(); err != nil {
			return sum, err // ErrPushRejected triggers retry in Run
		}
	}
	if err := dev.Save(s.statePath()); err != nil {
		return sum, err
	}
	sum.UpToDate = real == 0 && sum.SkippedConflicts == 0
	return sum, nil
}

// filterRemoteByAllowlist drops repo settings items whose key this
// device does not sync (default allowlist plus include/exclude
// overrides). An excluded key means "this machine ignores that key":
// it must neither pull-overwrite the local device value nor classify
// as deleted-local and cascade a repo-wide deletion.
func filterRemoteByAllowlist(remote map[item.ID]scan.Scanned, o settings.KeyOverrides) {
	for id := range remote {
		if id.Type() == item.TypeSetting && !settings.KeyAllowed(id.Name(), o) {
			delete(remote, id)
		}
	}
}

// excludeUnscannable removes every classification result matching the
// unscannable set, so nothing about such an item is applied, pushed,
// deleted, or has its base updated. A skipped item must never look
// deleted (spec: "flagged and skipped; sync continues for everything
// else").
func excludeUnscannable(results []classify.Result, unscannable []string) ([]classify.Result, []string) {
	if len(unscannable) == 0 {
		return results, nil
	}
	kept := results[:0]
	var warns []string
	for _, r := range results {
		if scan.Unscannable(r.ID, unscannable) {
			warns = append(warns, "excluded from this sync: "+string(r.ID))
			continue
		}
		kept = append(kept, r)
	}
	return kept, warns
}

func (s *Syncer) Status() (*plan.Plan, []string, error) {
	g := repo.Git{Dir: s.RepoDir()}
	var warns []string
	if err := g.Pull(); err != nil {
		warns = append(warns, "could not reach remote — status may be stale: "+err.Error())
	}
	cfg, err := state.LoadConfig(s.configPath())
	if err != nil {
		return nil, nil, err
	}
	ledger, err := state.LoadLedger(s.LedgerPath())
	if err != nil {
		return nil, nil, err
	}
	dev, err := state.LoadDevice(s.statePath())
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(s.ClaudeDir); err != nil {
		warns = append(warns, fmt.Sprintf("claude dir %s does not exist — check --claude-dir", s.ClaudeDir))
	}
	overrides := settings.KeyOverrides{Include: cfg.IncludeKeys, Exclude: cfg.ExcludeKeys}
	local, unscanL, wl, err := scan.Claude(s.ClaudeDir, overrides)
	if err != nil {
		return nil, nil, err
	}
	remote, unscanR, wr, err := scan.Repo(s.RepoDir())
	if err != nil {
		return nil, nil, err
	}
	filterRemoteByAllowlist(remote, overrides)
	warns = append(warns, append(wl, wr...)...)
	results, excludedWarns := excludeUnscannable(
		classify.All(local, remote, dev.LastSynced), append(unscanL, unscanR...))
	warns = append(warns, excludedWarns...)
	p := plan.Build(results, cfg, ledger)
	p.Local, p.Remote = local, remote
	return &p, warns, nil
}
