package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wonskur/upm/internal/db"
	"github.com/wonskur/upm/internal/utils"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed packages",
	Long:  "List all packages currently installed on the humanix system along with their provider and version.",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbConn, err := db.Open(utils.DBPath())
		if err != nil {
			return fmt.Errorf("failed opening database: %w", err)
		}
		defer dbConn.Close()

		database := db.New(dbConn)
		pkgs, err := database.ListInstalled(cmd.Context())
		if err != nil {
			return fmt.Errorf("failed listing installed packages: %w", err)
		}

		if len(pkgs) == 0 {
			fmt.Println("No packages currently installed.")
			return nil
		}

		fmt.Printf("%-24s %-16s %-12s\n", "NAME", "VERSION", "PROVIDER")
		fmt.Printf("%-24s %-16s %-12s\n", "----", "-------", "--------")
		for _, p := range pkgs {
			fmt.Printf("%-24s %-16s %-12s\n", p.Name, p.Version, p.Provider)
		}
		fmt.Printf("\nTotal installed: %d\n", len(pkgs))
		return nil
	},
}
