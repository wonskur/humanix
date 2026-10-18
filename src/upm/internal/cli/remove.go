package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/installer"
	"github.com/wonskur/upm/internal/utils"
)

var removeCmd = &cobra.Command{
	Use:   "remove <pkg>...",
	Short: "Remove installed packages and their files",
	Long:  "Remove specified packages from the system and clean up wrappers or database entries.",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("remove invoked", "packages", args, "force", Flags.Force, "dry-run", Flags.DryRun)

		if Flags.DryRun {
			for _, pkgName := range args {
				fmt.Printf("%swould remove package %s and associated files\n", DryRunPrefix(), pkgName)
			}
			return nil
		}

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)
		inst := installer.New(database)

		for _, pkgName := range args {
			if err := inst.Remove(cmd.Context(), pkgName, Flags.Force); err != nil {
				return err
			}
			fmt.Printf("==> Successfully removed package: %s\n", pkgName)
		}

		return nil
	},
}
