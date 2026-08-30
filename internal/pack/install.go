package pack

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/apply"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
)

type InstallPlan struct {
	Fresh, Upgrade, AlreadyCurrent, ModifiedOwned, Collisions []item.ID
}

type CollisionChoice string

const (
	ChoiceRename  CollisionChoice = "rename"
	ChoiceSkip    CollisionChoice = "skip"
	ChoiceReplace CollisionChoice = "replace"
)

type ModifiedChoice string

const (
	KeepLocal   ModifiedChoice = "keep-local"
	TakePackage ModifiedChoice = "take-package"
)

func BuildInstallPlan(man *Manifest, contents, local map[item.ID]scan.Scanned,
	led *state.Ledger, pkgName string) InstallPlan {
	var ip InstallPlan
	for id := range man.Items {
		pkgHash := contents[id].Hash
		loc, present := local[id]
		if !present {
			ip.Fresh = append(ip.Fresh, id)
			continue
		}
		if loc.Hash == pkgHash {
			ip.AlreadyCurrent = append(ip.AlreadyCurrent, id)
			continue
		}
		owner, installedHash, owned := led.Owner(id)
		if owned && owner == pkgName {
			if loc.Hash == installedHash {
				ip.Upgrade = append(ip.Upgrade, id)
			} else {
				ip.ModifiedOwned = append(ip.ModifiedOwned, id)
			}
			continue
		}
		ip.Collisions = append(ip.Collisions, id)
	}
	return ip
}

func RenamedID(id item.ID, pkgName string) item.ID {
	return item.NewID(id.Type(), id.Name()+"-"+pkgName)
}

func canRename(id item.ID) bool {
	t := id.Type()
	return t == item.TypeSkill || t == item.TypeAgent || t == item.TypeCommand
}

type InstallSummary struct {
	Installed, Upgraded, Renamed, Replaced, Skipped, Current int
	SnapshotDir                                              string
}

func ApplyInstall(claudeDir, backupsDir, ledgerPath string, man *Manifest,
	contents map[item.ID]scan.Scanned, ip InstallPlan,
	collisions map[item.ID]CollisionChoice, modified map[item.ID]ModifiedChoice,
) (*InstallSummary, error) {
	sum := &InstallSummary{Current: len(ip.AlreadyCurrent)}
	led, err := state.LoadLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	rec := state.PackageRecord{Version: man.Version, Items: map[item.ID]string{}}
	if old, ok := led.Packages[man.Name]; ok {
		for id, h := range old.Items {
			rec.Items[id] = h // carry forward; overwritten below where reinstalled
		}
	}

	// synthetic pull changes against the pack contents
	var changes []plan.Change
	remote := map[item.ID]scan.Scanned{}
	pull := func(targetID item.ID, src scan.Scanned) {
		src.ID = targetID
		remote[targetID] = src
		changes = append(changes, plan.Change{
			Result: classify.Result{ID: targetID, State: classify.NewRemote},
			Action: plan.ActPull,
		})
		rec.Items[targetID] = src.Hash
	}

	for _, id := range ip.Fresh {
		pull(id, contents[id])
		sum.Installed++
	}
	for _, id := range ip.AlreadyCurrent {
		rec.Items[id] = contents[id].Hash
	}
	for _, id := range ip.Upgrade {
		pull(id, contents[id])
		sum.Upgraded++
	}
	for _, id := range ip.ModifiedOwned {
		if modified[id] == TakePackage {
			pull(id, contents[id])
			sum.Upgraded++
		} else {
			sum.Skipped++ // keep-local: ledger keeps the old installed hash
		}
	}
	for _, id := range ip.Collisions {
		choice := collisions[id]
		if choice == ChoiceRename && !canRename(id) {
			choice = ChoiceSkip
		}
		switch choice {
		case ChoiceRename:
			pull(RenamedID(id, man.Name), contents[id])
			sum.Renamed++
		case ChoiceReplace:
			pull(id, contents[id])
			sum.Replaced++
		default:
			sum.Skipped++
		}
	}

	// snapshot everything the pulls will touch
	var rels []string
	needSettings := false
	for _, c := range changes {
		switch c.Result.ID.Type() {
		case item.TypeSetting, item.TypePlugins:
			needSettings = true
		default:
			rels = append(rels, apply.LocalRelPath(c.Result.ID))
		}
	}
	if needSettings {
		rels = append(rels, "settings.json")
	}
	snap, err := apply.SnapshotPaths(claudeDir, backupsDir, rels)
	if err != nil {
		return nil, err
	}
	sum.SnapshotDir = snap

	a := apply.Applier{ClaudeDir: claudeDir, BackupsDir: backupsDir}
	if _, err := a.Apply(changes, nil, remote); err != nil {
		return sum, fmt.Errorf("install failed (restore with: skill-sync rollback): %w", err)
	}
	led.Packages[man.Name] = rec
	return sum, led.Save(ledgerPath)
}

func Uninstall(claudeDir, backupsDir, ledgerPath, pkgName string) (removed, kept []item.ID, err error) {
	led, err := state.LoadLedger(ledgerPath)
	if err != nil {
		return nil, nil, err
	}
	rec, ok := led.Packages[pkgName]
	if !ok {
		return nil, nil, fmt.Errorf("package %q is not installed", pkgName)
	}
	// Package-installed custom settings keys may fall outside the default
	// shareable allowlist; include them explicitly so the scan surfaces them.
	var settingNames []string
	for id := range rec.Items {
		if id.Type() == item.TypeSetting {
			settingNames = append(settingNames, id.Name())
		}
	}
	local, _, err := scan.Claude(claudeDir, settings.KeyOverrides{Include: settingNames})
	if err != nil {
		return nil, nil, err
	}
	var changes []plan.Change
	var rels []string
	for id, installedHash := range rec.Items {
		loc, present := local[id]
		if !present {
			removed = append(removed, id) // already gone
			continue
		}
		if loc.Hash != installedHash {
			kept = append(kept, id)
			continue
		}
		changes = append(changes, plan.Change{
			Result: classify.Result{ID: id, State: classify.DeletedRemote},
			Action: plan.ActDeleteLocal,
		})
		if id.Type() == item.TypeSetting || id.Type() == item.TypePlugins {
			rels = append(rels, "settings.json")
		} else {
			rels = append(rels, apply.LocalRelPath(id))
		}
		removed = append(removed, id)
	}
	if _, err := apply.SnapshotPaths(claudeDir, backupsDir, rels); err != nil {
		return nil, nil, err
	}
	a := apply.Applier{ClaudeDir: claudeDir, BackupsDir: backupsDir}
	if _, err := a.Apply(changes, local, nil); err != nil {
		return removed, kept, err
	}
	delete(led.Packages, pkgName)
	return removed, kept, led.Save(ledgerPath)
}
