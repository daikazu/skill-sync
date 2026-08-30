package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/repo"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent sync history",
	RunE: func(cmd *cobra.Command, args []string) error {
		g := repo.Git{Dir: getSyncer().RepoDir()}
		entries, err := g.Log(20)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("no syncs yet")
		}
		for _, e := range entries {
			fmt.Printf("%s  %s  %s\n", e.Hash, e.Date, e.Subject)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(logCmd) }
