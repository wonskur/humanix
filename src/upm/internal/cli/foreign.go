package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/installer"
	"github.com/wonskur/upm/internal/utils"
)

var foreignCmd = &cobra.Command{
	Use:   "foreign <path>",
	Short: "Install a foreign package from local archive file (upm -i)",
	Long:  "Install a local .rpm, .deb, or .pkg.tar.zst archive into /opt/upm/<provider>/<pkg> with a bwrap wrapper.",
	Args:  cobra.ExactArgs(1),
	RunE:  runForeign,
}

func runForeign(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	slog.Info("foreign install requested", "file", filePath, "force", Flags.Force, "dry-run", Flags.DryRun)

	if Flags.DryRun {
		fmt.Printf("%sforeign: would install package from %s into /opt/upm/<provider>/<pkg>\n", DryRunPrefix(), filePath)
		return nil
	}

	dbConn, err := db.Open(utils.DBPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbConn.Close()

	database := db.New(dbConn)
	inst := installer.New(database)

	result, err := inst.InstallForeign(cmd.Context(), nil, filePath)
	if err != nil {
		return err
	}

	fmt.Printf("==> Installed foreign package %s (%s) version %s\n", result.Package.Name, result.Package.Provider, result.Package.Version)
	fmt.Printf("    Sandbox path: %s\n", utils.ProviderOptDir(result.Package.Provider, result.Package.Name))
	fmt.Printf("    Files registered: %d\n", len(result.Files))

	return nil
}
