package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
	"github.com/spf13/cobra"
)

func configPath() string { return filepath.Join(flagSyncDir, "config.json") }

func mutateConfig(f func(*state.Config) error) error {
	cfg, err := state.LoadConfig(configPath())
	if err != nil {
		return err
	}
	if err := f(cfg); err != nil {
		return err
	}
	return cfg.Save(configPath())
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or change what syncs",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := state.LoadConfig(configPath())
		if err != nil {
			return err
		}
		fmt.Println("remote:", cfg.Remote)
		fmt.Println("default shareable settings keys:", settings.DefaultShareable)
		fmt.Println("extra included keys:", cfg.IncludeKeys)
		fmt.Println("excluded keys:", cfg.ExcludeKeys)
		if len(cfg.Policies) == 0 {
			fmt.Println("policies: none")
		}
		for id, p := range cfg.Policies {
			fmt.Printf("policy: %s → %s\n", id, p)
		}
		return nil
	},
}

var configIncludeCmd = &cobra.Command{
	Use:   "include-key <settings-key>",
	Short: "Also sync this settings.json key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateConfig(func(c *state.Config) error {
			c.IncludeKeys = appendUnique(c.IncludeKeys, args[0])
			c.ExcludeKeys = remove(c.ExcludeKeys, args[0])
			return nil
		})
	},
}

var configExcludeCmd = &cobra.Command{
	Use:   "exclude-key <settings-key>",
	Short: "Stop syncing this settings.json key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateConfig(func(c *state.Config) error {
			c.ExcludeKeys = appendUnique(c.ExcludeKeys, args[0])
			c.IncludeKeys = remove(c.IncludeKeys, args[0])
			return nil
		})
	},
}

var configPolicyCmd = &cobra.Command{
	Use:   "policy <item-id> <never-sync|always-ask|default>",
	Short: "Set a per-item sync policy",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := item.Parse(args[0])
		if err != nil {
			return err
		}
		return mutateConfig(func(c *state.Config) error {
			if c.Policies == nil {
				c.Policies = map[item.ID]state.Policy{}
			}
			switch args[1] {
			case "never-sync":
				c.Policies[id] = state.PolicyNeverSync
			case "always-ask":
				c.Policies[id] = state.PolicyAlwaysAsk
			case "default":
				delete(c.Policies, id)
			default:
				return fmt.Errorf("unknown policy %q", args[1])
			}
			return nil
		})
	},
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func remove(s []string, v string) []string {
	var out []string
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func init() {
	configCmd.AddCommand(configIncludeCmd, configExcludeCmd, configPolicyCmd)
	rootCmd.AddCommand(configCmd)
}
