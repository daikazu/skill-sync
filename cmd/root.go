package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via ldflags:
// -X github.com/daikazu/skill-sync/cmd.version={{.Version}}
var version = "dev"

var rootCmd = &cobra.Command{
	Use:          "skill-sync",
	Short:        "Sync Claude Code skills, agents, commands, rules, and settings between machines",
	Version:      version,
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
