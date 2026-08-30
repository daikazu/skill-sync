package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/syncer"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init <git-remote-url>",
	Short: "Set up syncing on this machine against a private git remote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := syncer.Init(flagClaudeDir, flagSyncDir, args[0]); err != nil {
			return err
		}
		fmt.Println("initialized — run `skill-sync sync` to do the first sync")
		return nil
	},
}

func init() { rootCmd.AddCommand(initCmd) }
