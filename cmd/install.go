package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/pack"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
	"github.com/spf13/cobra"
)

var installYes bool

var installCmd = &cobra.Command{
	Use:   "install <file.skillpack>",
	Short: "Install or upgrade a skill package (never clobbers your own items)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tmp, err := os.MkdirTemp("", "skillpack-install-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		man, contents, err := pack.Load(args[0], tmp)
		if err != nil {
			return err
		}
		local, warns, err := scan.Claude(flagClaudeDir, settings.KeyOverrides{})
		if err != nil {
			return err
		}
		for _, w := range warns {
			fmt.Println("warning:", w)
		}
		led, err := state.LoadLedger(getSyncer().LedgerPath())
		if err != nil {
			return err
		}
		ip := pack.BuildInstallPlan(man, contents, local, led, man.Name)

		collisions := map[item.ID]pack.CollisionChoice{}
		for _, id := range ip.Collisions {
			choice := pack.ChoiceSkip
			if !installYes {
				opts := []huh.Option[pack.CollisionChoice]{
					huh.NewOption("skip (keep mine, don't install theirs)", pack.ChoiceSkip),
					huh.NewOption(fmt.Sprintf("install renamed as %s", pack.RenamedID(id, man.Name)), pack.ChoiceRename),
					huh.NewOption("replace mine (snapshotted, reversible)", pack.ChoiceReplace),
				}
				if err := huh.NewSelect[pack.CollisionChoice]().
					Title(fmt.Sprintf("%s already exists and differs from the package", id)).
					Options(opts...).Value(&choice).Run(); err != nil {
					return err
				}
			}
			collisions[id] = choice
		}
		modified := map[item.ID]pack.ModifiedChoice{}
		for _, id := range ip.ModifiedOwned {
			choice := pack.KeepLocal
			if !installYes {
				if err := huh.NewSelect[pack.ModifiedChoice]().
					Title(fmt.Sprintf("%s was installed by %s but you have edited it", id, man.Name)).
					Options(
						huh.NewOption("keep my edited version", pack.KeepLocal),
						huh.NewOption("take the package version (snapshotted)", pack.TakePackage),
					).Value(&choice).Run(); err != nil {
					return err
				}
			}
			modified[id] = choice
		}

		sum, err := pack.ApplyInstall(flagClaudeDir, filepath.Join(flagClaudeDir, "backups", "skill-sync"),
			getSyncer().LedgerPath(), man, contents, ip, collisions, modified)
		if err != nil {
			return err
		}
		fmt.Printf("%s %s: %d installed, %d upgraded, %d renamed, %d replaced, %d skipped, %d already current\n",
			man.Name, man.Version, sum.Installed, sum.Upgraded, sum.Renamed, sum.Replaced, sum.Skipped, sum.Current)
		if sum.SnapshotDir != "" {
			fmt.Println("pre-install snapshot:", sum.SnapshotDir)
		}
		return nil
	},
}

func init() {
	installCmd.Flags().BoolVarP(&installYes, "yes", "y", false, "accept safe defaults (skip collisions, keep local edits)")
	rootCmd.AddCommand(installCmd)
}
