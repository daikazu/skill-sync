package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
	"github.com/spf13/cobra"
)

var packagesCmd = &cobra.Command{
	Use:   "packages",
	Short: "List installed skill packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		led, err := state.LoadLedger(getSyncer().LedgerPath())
		if err != nil {
			return err
		}
		if len(led.Packages) == 0 {
			fmt.Println("no packages installed")
			return nil
		}
		local, _, err := scan.Claude(flagClaudeDir, settings.KeyOverrides{})
		if err != nil {
			return err
		}
		for name, rec := range led.Packages {
			fmt.Printf("%s %s (%d items)\n", name, rec.Version, len(rec.Items))
			for id, h := range rec.Items {
				marker := ""
				if loc, ok := local[id]; !ok {
					marker = " (missing)"
				} else if loc.Hash != h {
					marker = " (modified)"
				}
				fmt.Printf("  %s%s\n", id, marker)
			}
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(packagesCmd) }
