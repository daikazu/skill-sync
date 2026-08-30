package cmd

import (
	"fmt"
	"os"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync this machine with the shared repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := getSyncer()
		resolver := func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error) {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				for _, c := range p.Conflicts {
					fmt.Printf("conflict (unresolved, skipped): %s [%s]\n", c.ID, c.State)
				}
				return nil, true, nil
			}
			return tui.RunReview(p.Conflicts, p.Local, p.Remote)
		}
		sum, err := s.Run(resolver)
		if err != nil {
			return err
		}
		for _, w := range sum.Warnings {
			fmt.Println("warning:", w)
		}
		if sum.UpToDate {
			fmt.Println("up to date")
			return nil
		}
		fmt.Printf("pulled %d, pushed %d, deleted %d local / %d remote",
			sum.Pulled, sum.Pushed, sum.DeletedLocal, sum.DeletedRemote)
		if sum.SkippedConflicts > 0 {
			fmt.Printf(", %d conflict(s) left unresolved", sum.SkippedConflicts)
		}
		fmt.Println()
		for _, id := range sum.RemoteDeletions {
			fmt.Printf("deleted locally (remote deletion): %s\n", id)
		}
		if sum.SnapshotDir != "" {
			fmt.Println("pre-apply snapshot:", sum.SnapshotDir)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(syncCmd) }
