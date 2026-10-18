package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	pkg "github.com/wonskur/upm/internal/package"
	"github.com/wonskur/upm/internal/repo"
	"github.com/wonskur/upm/internal/utils"
)

var infoCmd = &cobra.Command{
	Use:   "info <pkg>",
	Short: "Display detailed information about a package",
	Long:  "Display details including version, provider, dependencies, license, and install status.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pkgName := args[0]

		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)

		var p *pkg.Package
		isInstalled := false

		// Check installed first
		instPkg, err := database.GetInstalled(cmd.Context(), pkgName)
		if err == nil && instPkg != nil {
			p = instPkg
			isInstalled = true
		} else {
			// Fallback to local available repos
			availPkg, err := database.GetAvailable(cmd.Context(), pkgName, "")
			if err == nil && availPkg != nil {
				p = availPkg
			} else {
				// Fallback to AUR RPC
				aurPkg, aurErr := repo.NewAURProvider().Get(cmd.Context(), pkgName)
				if aurErr == nil && aurPkg != nil {
					p = aurPkg
				} else {
					return fmt.Errorf("package %q not found in installed, repository, or AUR", pkgName)
				}
			}
		}

		fmt.Printf("Name        : %s\n", p.Name)
		fmt.Printf("Version     : %s\n", p.Version)
		fmt.Printf("Provider    : %s\n", p.Provider)
		if isInstalled {
			fmt.Printf("Status      : Installed\n")
			if p.InstallDate > 0 {
				fmt.Printf("Install Date: %s\n", time.Unix(p.InstallDate, 0).Format(time.RFC1123))
			}
		} else {
			fmt.Printf("Status      : Available\n")
		}
		if p.Description != "" {
			fmt.Printf("Description : %s\n", p.Description)
		}
		if p.URL != "" {
			fmt.Printf("URL         : %s\n", p.URL)
		}
		if p.License != "" {
			fmt.Printf("License     : %s\n", p.License)
		}
		if p.Checksum != "" {
			fmt.Printf("SHA256      : %s\n", p.Checksum)
		}
		if len(p.Depends) > 0 {
			var depList []string
			for _, d := range p.Depends {
				if d.Version != "" {
					depList = append(depList, fmt.Sprintf("%s (%s)", d.Name, d.Version))
				} else {
					depList = append(depList, d.Name)
				}
			}
			fmt.Printf("Depends On  : %s\n", strings.Join(depList, ", "))
		}
		return nil
	},
}
