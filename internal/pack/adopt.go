package pack

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/state"
)

// Adopt transfers item id out of package ownership and into the user's
// personal sync. It only edits the ledger — the file on disk is not
// touched — so the next sync simply treats the item like anything else
// the user owns, instead of excluding it as package-managed.
//
// If removing id leaves the owning package's record with no items, the
// whole record is dropped rather than left behind empty; an empty record
// would otherwise linger in `skill-sync packages` output as a package with
// zero items still "installed".
func Adopt(ledgerPath string, id item.ID) (pkg string, err error) {
	led, err := state.LoadLedger(ledgerPath)
	if err != nil {
		return "", err
	}
	owner, _, owned := led.Owner(id)
	if !owned {
		return "", fmt.Errorf("item %s is not owned by any package", id)
	}
	rec := led.Packages[owner]
	delete(rec.Items, id)
	if len(rec.Items) == 0 {
		delete(led.Packages, owner)
	} else {
		led.Packages[owner] = rec
	}
	if err := led.Save(ledgerPath); err != nil {
		return "", err
	}
	return owner, nil
}
