package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/builder"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/utils"
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Build and install package from source in current directory (upm -s)",
	Long:  "Build a humanix source package from the manifest files in current working directory inside a bwrap sandbox.",
	RunE:  runSource,
}

func runSource(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	slog.Info("source build requested", "dir", cwd, "dry-run", Flags.DryRun)

	if Flags.DryRun {
		fmt.Printf("%ssource: would build and install package from %s\n", DryRunPrefix(), cwd)
		return nil
	}

	destDir, err := os.MkdirTemp("", "upm-source-dest-*")
	if err != nil {
		return fmt.Errorf("failed creating temporary destination: %w", err)
	}
	defer os.RemoveAll(destDir)

	b := builder.New()
	result, err := b.BuildSource(cmd.Context(), cwd, destDir)
	if err != nil {
		return err
	}

	// Open DB and register package
	dbConn, err := db.Open(utils.DBPath())
	if err != nil {
		return fmt.Errorf("failed opening database: %w", err)
	}
	defer dbConn.Close()

	database := db.New(dbConn)
	if err := database.RegisterInstalled(cmd.Context(), &result.Package, result.Files); err != nil {
		return fmt.Errorf("failed registering package in database: %w", err)
	}

	fmt.Printf("==> Successfully built and installed %s %s from source\n", result.Package.Name, result.Package.Version)
	fmt.Printf("    Installed files: %d\n", len(result.Files))

	return nil
}
