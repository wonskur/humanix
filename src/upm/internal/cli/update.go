package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
	"github.com/wonskur/upm/internal/utils"
)

var updateCmd = &cobra.Command{
	Use:   "update [pkg]...",
	Short: "Update installed packages to their latest versions",
	Long:  "Update either all installed packages or specific named packages to their latest available versions.",
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("update invoked", "packages", args, "dry-run", Flags.DryRun)

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed opening database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)

		var targetPkgs []pkg.Package
		if len(args) == 0 {
			installed, err := database.ListInstalled(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed listing installed packages: %w", err)
			}
			targetPkgs = installed
		} else {
			for _, name := range args {
				p, err := database.GetInstalled(cmd.Context(), name)
				if err != nil {
					return fmt.Errorf("package %q is not installed", name)
				}
				targetPkgs = append(targetPkgs, *p)
			}
		}

		if len(targetPkgs) == 0 {
			fmt.Println("No packages to update.")
			return nil
		}

		type updateCandidate struct {
			Name       string
			CurrentVer string
			LatestVer  string
			Provider   string
		}

		var candidates []updateCandidate
		for _, p := range targetPkgs {
			avail, err := database.GetAvailable(cmd.Context(), p.Name, p.Provider)
			if err == nil && avail != nil && avail.Version != p.Version {
				candidates = append(candidates, updateCandidate{
					Name:       p.Name,
					CurrentVer: p.Version,
					LatestVer:  avail.Version,
					Provider:   p.Provider,
				})
			}
		}

		if len(candidates) == 0 {
			fmt.Println("All packages are up to date.")
			return nil
		}

		fmt.Printf("%-24s %-16s %-16s %-10s\n", "PACKAGE", "CURRENT", "LATEST", "PROVIDER")
		fmt.Printf("%-24s %-16s %-16s %-10s\n", "-------", "-------", "------", "--------")
		for _, c := range candidates {
			fmt.Printf("%-24s %-16s %-16s %-10s\n", c.Name, c.CurrentVer, c.LatestVer, c.Provider)
		}

		if Flags.DryRun {
			fmt.Printf("\n%s%d packages would be updated.\n", DryRunPrefix(), len(candidates))
			return nil
		}

		fmt.Printf("\n==> %d packages ready to update.\n", len(candidates))
		return nil
	},
}
