package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/utils"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean cache directory and downloaded archives",
	Long:  "Remove downloaded archives, temporary files, and obsolete cache entries from /var/cache/upm.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cacheDir := utils.CacheDir()
		slog.Info("clean cache", "dir", cacheDir, "dry-run", Flags.DryRun)

		if Flags.DryRun {
			fmt.Printf("%swould clean cache directory: %s\n", DryRunPrefix(), cacheDir)
			return nil
		}

		entries, err := os.ReadDir(cacheDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Cache directory is empty.")
				return nil
			}
			return fmt.Errorf("failed reading cache directory: %w", err)
		}

		var count int
		var totalBytes int64

		for _, entry := range entries {
			p := filepath.Join(cacheDir, entry.Name())
			if info, err := entry.Info(); err == nil {
				totalBytes += info.Size()
			}
			if err := os.RemoveAll(p); err == nil {
				count++
			}
		}

		fmt.Printf("==> Cleaned %d files (%s) from %s\n", count, humanize.Bytes(uint64(totalBytes)), cacheDir)
		return nil
	},
}
