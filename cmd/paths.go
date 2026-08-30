package cmd

import (
	"os"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/syncer"
)

var flagClaudeDir, flagSyncDir string

func defaultDir(sub string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return sub
	}
	return filepath.Join(home, sub)
}

func getSyncer() *syncer.Syncer {
	return &syncer.Syncer{ClaudeDir: flagClaudeDir, SyncDir: flagSyncDir}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagClaudeDir, "claude-dir", defaultDir(".claude"), "Claude Code config directory")
	rootCmd.PersistentFlags().StringVar(&flagSyncDir, "sync-dir", defaultDir(".claude-sync"), "skill-sync data directory")
}
