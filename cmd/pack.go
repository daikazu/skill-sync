package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/pack"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/tui"
	"github.com/spf13/cobra"
)

var (
	packAll     bool
	packOut     string
	packName    string
	packVersion string
)

func author() string {
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	return os.Getenv("USER")
}

var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Build a .skillpack (backup with --all, or a curated team package)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// unscannable items are simply absent from the pack; the warnings
		// below already say why, and packing never deletes anything.
		items, _, warns, err := scan.Claude(flagClaudeDir, settings.KeyOverrides{})
		if err != nil {
			return err
		}
		for _, w := range warns {
			fmt.Println("warning:", w)
		}
		var selected []item.ID
		if packAll {
			for id := range items {
				selected = append(selected, id)
			}
		} else {
			var list []scan.Scanned
			for _, s := range items {
				list = append(list, s)
			}
			var proceed bool
			selected, proceed, err = tui.RunPicker(list)
			if err != nil || !proceed {
				return err
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("nothing selected")
		}
		man := pack.Manifest{
			Name: packName, Version: packVersion, Author: author(),
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Items:     map[item.ID]pack.PackItem{},
		}
		for _, id := range selected {
			man.Items[id] = pack.PackItem{Hash: items[id].Hash}
		}
		out := packOut
		if out == "" {
			out = fmt.Sprintf("%s-%s.skillpack", packName, packVersion)
		}
		if err := pack.Build(out, man, items); err != nil {
			return err
		}
		fmt.Printf("packed %d item(s) → %s\n", len(selected), out)
		return nil
	},
}

func init() {
	packCmd.Flags().BoolVar(&packAll, "all", false, "include every shareable item (full backup)")
	packCmd.Flags().StringVarP(&packOut, "output", "o", "", "output file")
	packCmd.Flags().StringVar(&packName, "name", "claude-profile", "package name")
	packCmd.Flags().StringVar(&packVersion, "version", "0.1.0", "package version")
	rootCmd.AddCommand(packCmd)
}
