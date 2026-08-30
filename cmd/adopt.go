package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/pack"
	"github.com/spf13/cobra"
)

var adoptCmd = &cobra.Command{
	Use:   "adopt <item-id>",
	Short: "Adopt a package-owned item into your personal sync",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := item.Parse(args[0])
		if err != nil {
			return err
		}
		pkgName, err := pack.Adopt(getSyncer().LedgerPath(), id)
		if err != nil {
			return err
		}
		fmt.Printf("adopted %s from package %s — it now syncs with your personal content\n", id, pkgName)
		return nil
	},
}

func init() { rootCmd.AddCommand(adoptCmd) }
