package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what a sync would do, without changing anything",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, warns, err := getSyncer().Status()
		if err != nil {
			return err
		}
		for _, w := range warns {
			fmt.Println("warning:", w)
		}
		if len(p.Auto) == 0 && len(p.Conflicts) == 0 && len(p.Skipped) == 0 {
			fmt.Println("nothing to sync")
			return nil
		}
		for _, c := range p.Auto {
			fmt.Printf("%-14s %s\n", c.Action, c.Result.ID)
		}
		for _, c := range p.Conflicts {
			fmt.Printf("%-14s %s (%s)\n", "conflict", c.ID, c.State)
		}
		for _, c := range p.Skipped {
			fmt.Printf("%-14s %s\n", "skipped", c.ID)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(statusCmd) }
