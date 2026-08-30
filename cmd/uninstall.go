package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/pack"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <package-name>",
	Short: "Remove a package's items (keeps anything you've modified)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s := getSyncer()
		removed, kept, err := pack.Uninstall(flagClaudeDir,
			filepath.Join(flagClaudeDir, "backups", "skill-sync"), s.LedgerPath(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("removed %d item(s)\n", len(removed))
		for _, id := range kept {
			fmt.Printf("kept (you modified it): %s\n", id)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(uninstallCmd) }
