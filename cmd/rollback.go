package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/daikazu/skill-sync/internal/apply"
	"github.com/spf13/cobra"
)

var rollbackList, rollbackYes bool

var rollbackCmd = &cobra.Command{
	Use:   "rollback [snapshot-name]",
	Short: "Restore ~/.claude files from a pre-apply snapshot",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backups := filepath.Join(flagClaudeDir, "backups", "skill-sync")
		names, err := apply.ListSnapshots(backups)
		if err != nil {
			return err
		}
		if rollbackList {
			for _, n := range names {
				fmt.Println(n)
			}
			return nil
		}
		if len(names) == 0 {
			return fmt.Errorf("no snapshots found in %s", backups)
		}
		name := names[0]
		if len(args) == 1 {
			name = args[0]
		}
		if !rollbackYes {
			var ok bool
			if err := huh.NewConfirm().
				Title(fmt.Sprintf("Restore snapshot %s over %s?", name, flagClaudeDir)).
				Value(&ok).Run(); err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("aborted")
			}
		}
		if err := apply.Restore(filepath.Join(backups, name), flagClaudeDir); err != nil {
			return err
		}
		fmt.Println("restored", name)
		return nil
	},
}

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackList, "list", false, "list snapshots")
	rollbackCmd.Flags().BoolVarP(&rollbackYes, "yes", "y", false, "skip confirmation")
	rootCmd.AddCommand(rollbackCmd)
}
