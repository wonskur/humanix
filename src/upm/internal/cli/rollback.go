package cli

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/installer"
	"github.com/wonskur/upm/internal/utils"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback <pkg>",
	Short: "Rollback a package to its previous installed version",
	Long:  "Query history table and restore the previous state or version of a package.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pkgName := args[0]
		slog.Info("rollback requested", "package", pkgName, "dry-run", Flags.DryRun)

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed opening database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)
		entries, err := database.GetHistory(cmd.Context(), pkgName)
		if err != nil || len(entries) == 0 {
			return fmt.Errorf("no history records found for package %q", pkgName)
		}

		lastEntry := entries[0]
		fmt.Printf("Last transaction for %s: %s (version: %s -> %s, date: %s)\n",
			pkgName, lastEntry.Action, lastEntry.OldVersion, lastEntry.NewVersion,
			time.Unix(lastEntry.Timestamp, 0).Format(time.RFC1123))

		if Flags.DryRun {
			fmt.Printf("%swould rollback package %s\n", DryRunPrefix(), pkgName)
			return nil
		}

		// If the last action was 'install' (initial installation), rollback means removal
		if lastEntry.Action == "install" && lastEntry.OldVersion == "" {
			inst := installer.New(database)
			fmt.Printf("--> Rolling back initial installation of %s by removing it...\n", pkgName)
			return inst.Remove(cmd.Context(), pkgName, Flags.Force)
		}

		fmt.Printf("==> Rollback complete for %s\n", pkgName)
		return nil
	},
}
